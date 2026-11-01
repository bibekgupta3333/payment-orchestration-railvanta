// Package reconciliation compares mock-provider charges, payment state and
// posted journals over a time window and records the differences as
// discrepancies (WBS 4.2).
//
// The service is evidence-only. It never creates or changes a journal, never
// changes a payment or attempt, and never calls a provider: it only reads those
// tables and writes reconciliation_runs, reconciliation_discrepancies,
// reconciliation_resolution_events and the delivery status of consumed
// payment.late_outcome rows. Resolving a discrepancy records a note, a
// resolver and a timestamp; it does not fix the underlying payment.
package reconciliation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/provider/mock"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Discrepancy kinds, matching the CHECK constraint in migration 0001.
const (
	KindMissingPosting    = "missing_posting"
	KindUnknownOutcome    = "unknown_outcome"
	KindAmountMismatch    = "amount_mismatch"
	KindStatusMismatch    = "status_mismatch"
	KindMissingAtProvider = "missing_at_provider"
)

// Run statuses.
const (
	RunRunning   = "running"
	RunCompleted = "completed"
)

var (
	// ErrInvalidWindow rejects an empty, reversed or oversized window.
	ErrInvalidWindow = errors.New("reconciliation: window_end must be after window_start and span at most 366 days")
	// ErrInvalidRunKey rejects an empty or oversized run key.
	ErrInvalidRunKey = errors.New("reconciliation: run key must be 1 to 255 characters")
	// ErrRunKeyConflict means the run key was used with a different window.
	ErrRunKeyConflict = errors.New("reconciliation: run key already used with a different window")
)

// maxWindow bounds the work one run does.
const maxWindow = 366 * 24 * time.Hour

// Service runs reconciliation.
type Service struct {
	db  *gorm.DB
	now func() time.Time
}

// NewService returns a Service. now defaults to time.Now.
func NewService(db *gorm.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}

// RunInput identifies one run. Re-running the same RunKey and window re-checks
// the window and adds only discrepancies not already stored for that run.
type RunInput struct {
	WorkspaceID string
	RunKey      string
	WindowStart int64 // inclusive, Unix ms
	WindowEnd   int64 // exclusive, Unix ms
	RequestedBy string
}

// RunResult is the stored run after the check.
type RunResult struct {
	Run storage.ReconciliationRun
	// Created is true when this call created the run row.
	Created bool
	// NewDiscrepancies counts discrepancy rows inserted by this call.
	NewDiscrepancies int
}

// Evidence is the JSON stored in reconciliation_discrepancies.details. It is
// mock-provider evidence and is always labelled simulated.
type Evidence struct {
	Source         string           `json:"source"`
	Simulated      bool             `json:"simulated"`
	Summary        string           `json:"summary"`
	Expected       string           `json:"expected,omitempty"`
	Payment        PaymentEvidence  `json:"payment"`
	ProviderCharge *ChargeEvidence  `json:"provider_charge"`
	Journal        *JournalEvidence `json:"journal"`
	LateOutcomes   []LateEvidence   `json:"late_outcomes"`
}

// PaymentEvidence is the payment state at check time.
type PaymentEvidence struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	AmountMinor string `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// ChargeEvidence is the mock provider's recorded charge.
type ChargeEvidence struct {
	ID                string `json:"id"`
	ProviderReference string `json:"provider_reference"`
	Status            string `json:"status"`
	AmountMinor       string `json:"amount_minor"`
	Currency          string `json:"currency"`
	Scenario          string `json:"scenario"`
}

// JournalEvidence is the posted success journal.
type JournalEvidence struct {
	ID               string `json:"id"`
	DebitTotalMinor  string `json:"debit_total_minor"`
	CreditTotalMinor string `json:"credit_total_minor"`
}

// LateEvidence is a payment.late_outcome row consumed as evidence.
type LateEvidence struct {
	EventID       string `json:"event_id"`
	Outcome       string `json:"outcome"`
	Source        string `json:"source"`
	PaymentStatus string `json:"payment_status"`
	AttemptStatus string `json:"attempt_status"`
	ObservedAt    int64  `json:"observed_at"`
}

type finding struct {
	kind      string
	subject   string // distinguishes findings of one kind for one payment
	paymentID string
	evidence  Evidence
}

func newID() (string, error) { return storage.NewID() }

func fingerprint(f finding) string {
	sum := sha256.Sum256([]byte(f.kind + "\x00" + f.paymentID + "\x00" + f.subject))
	return hex.EncodeToString(sum[:16])
}

// Run executes one reconciliation run in a single write transaction, so
// concurrent calls with the same key serialize and the second sees the first's
// rows.
func (s *Service) Run(ctx context.Context, in RunInput) (RunResult, error) {
	if len(in.RunKey) < 1 || len(in.RunKey) > 255 {
		return RunResult{}, ErrInvalidRunKey
	}
	if in.WindowEnd <= in.WindowStart || time.Duration(in.WindowEnd-in.WindowStart)*time.Millisecond > maxWindow {
		return RunResult{}, ErrInvalidWindow
	}
	var result RunResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result = RunResult{}
		runID, err := newID()
		if err != nil {
			return err
		}
		now := s.now().UnixMilli()
		var requestedBy *string
		if in.RequestedBy != "" {
			requestedBy = &in.RequestedBy
		}
		inserted := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "run_key"}},
			DoNothing: true,
		}).Create(&storage.ReconciliationRun{
			ID: runID, WorkspaceID: in.WorkspaceID, RunKey: in.RunKey, Status: RunRunning,
			WindowStart: in.WindowStart, WindowEnd: in.WindowEnd, CreatedAt: now, RequestedBy: requestedBy,
		})
		if inserted.Error != nil {
			return inserted.Error
		}
		result.Created = inserted.RowsAffected == 1
		var run storage.ReconciliationRun
		if err := tx.Where("workspace_id = ? AND run_key = ?", in.WorkspaceID, in.RunKey).Take(&run).Error; err != nil {
			return err
		}
		if run.WindowStart != in.WindowStart || run.WindowEnd != in.WindowEnd {
			return ErrRunKeyConflict
		}
		scan, err := s.evaluate(tx, run)
		if err != nil {
			return err
		}
		for _, f := range scan.findings {
			details, err := json.Marshal(f.evidence)
			if err != nil {
				return err
			}
			id, err := newID()
			if err != nil {
				return err
			}
			paymentID := f.paymentID
			row := storage.ReconciliationDiscrepancy{
				ID: id, WorkspaceID: run.WorkspaceID, RunID: run.ID, PaymentID: &paymentID,
				Fingerprint: fingerprint(f), Kind: f.kind, Details: string(details), Status: "open", CreatedAt: now,
			}
			res := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "run_id"}, {Name: "fingerprint"}},
				DoNothing: true,
			}).Create(&row)
			if res.Error != nil {
				return res.Error
			}
			result.NewDiscrepancies += int(res.RowsAffected)
		}
		consumed, err := consumeLateOutcomes(tx, run, scan.lateEventIDs, now)
		if err != nil {
			return err
		}
		counts, err := countRun(tx, run, scan.compared)
		if err != nil {
			return err
		}
		updates := map[string]any{
			"status": RunCompleted, "completed_at": now,
			"compared_count": counts.compared, "matched_count": counts.matched,
			"unmatched_count": counts.unmatched, "mismatch_count": counts.mismatch,
			"evidence_consumed_count": gorm.Expr("evidence_consumed_count + ?", consumed),
		}
		if err := tx.Model(&storage.ReconciliationRun{}).Where("id = ? AND workspace_id = ?", run.ID, run.WorkspaceID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", run.ID).Take(&result.Run).Error
	})
	return result, err
}

type runCounts struct{ compared, matched, unmatched, mismatch int64 }

// countRun derives the totals from the stored discrepancies, so they stay
// consistent with what a reader sees.
func countRun(tx *gorm.DB, run storage.ReconciliationRun, compared int64) (runCounts, error) {
	var rows []struct {
		Kind  string
		Count int64
	}
	if err := tx.Table("reconciliation_discrepancies").Select("kind, count(*) AS count").
		Where("run_id = ? AND workspace_id = ?", run.ID, run.WorkspaceID).Group("kind").Scan(&rows).Error; err != nil {
		return runCounts{}, err
	}
	counts := runCounts{compared: compared}
	for _, row := range rows {
		switch row.Kind {
		case KindAmountMismatch, KindStatusMismatch:
			counts.mismatch += row.Count
		default:
			counts.unmatched += row.Count
		}
	}
	var discrepant int64
	if err := tx.Table("reconciliation_discrepancies").
		Where("run_id = ? AND workspace_id = ?", run.ID, run.WorkspaceID).
		Distinct("payment_id").Count(&discrepant).Error; err != nil {
		return runCounts{}, err
	}
	counts.matched = max(compared-discrepant, 0)
	return counts, nil
}

// consumeLateOutcomes marks pending payment.late_outcome rows consumed. It does
// not touch payments, attempts or journals.
func consumeLateOutcomes(tx *gorm.DB, run storage.ReconciliationRun, ids []string, now int64) (int64, error) {
	var total int64
	note := "consumed by reconciliation run " + run.ID
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		res := tx.Model(&storage.OutboxEvent{}).
			Where("workspace_id = ? AND event_type = ? AND status = ? AND id IN ?", run.WorkspaceID, payment.OutboxPaymentLateOutcome, event.StatusPending, ids[start:end]).
			Updates(map[string]any{"status": event.StatusDelivered, "delivered_at": now, "last_error": note})
		if res.Error != nil {
			return 0, res.Error
		}
		total += res.RowsAffected
	}
	return total, nil
}

type scanResult struct {
	findings     []finding
	compared     int64
	lateEventIDs []string
}

type journalFact struct {
	id            string
	debit, credit int64
}

func chunks[T any](items []T, size int, visit func([]T) error) error {
	for start := 0; start < len(items); start += size {
		if err := visit(items[start:min(start+size, len(items))]); err != nil {
			return err
		}
	}
	return nil
}

// evaluate is read-only. It compares each settled payment created in the
// window that has a mock-provider attempt.
func (s *Service) evaluate(tx *gorm.DB, run storage.ReconciliationRun) (scanResult, error) {
	var out scanResult
	var payments []storage.Payment
	err := tx.Where("workspace_id = ? AND created_at >= ? AND created_at < ? AND status NOT IN ?",
		run.WorkspaceID, run.WindowStart, run.WindowEnd, []string{string(payment.StatusCreated), string(payment.StatusProcessing)}).
		Order("created_at, id").Find(&payments).Error
	if err != nil {
		return out, err
	}
	ids := make([]string, len(payments))
	for i, p := range payments {
		ids[i] = p.ID
	}

	// Latest mock attempt per payment.
	latest := map[string]storage.PaymentAttempt{}
	err = chunks(ids, 500, func(part []string) error {
		var attempts []storage.PaymentAttempt
		err := tx.Table("payment_attempts AS a").Select("a.*").
			Joins("JOIN providers AS p ON p.id = a.provider_id AND p.workspace_id = a.workspace_id").
			Where("a.workspace_id = ? AND p.code = 'mock' AND a.payment_id IN ?", run.WorkspaceID, part).
			Scan(&attempts).Error
		for _, a := range attempts {
			if cur, ok := latest[a.PaymentID]; !ok || a.AttemptNumber > cur.AttemptNumber {
				latest[a.PaymentID] = a
			}
		}
		return err
	})
	if err != nil {
		return out, err
	}

	// Charges recorded by the mock provider for those attempts.
	keysByProvider := map[string][]string{}
	for _, a := range latest {
		keysByProvider[a.ProviderID] = append(keysByProvider[a.ProviderID], a.ProviderIdempotencyKey)
	}
	charges := map[string]mock.Charge{} // provider_id + "\x00" + key
	for providerID, keys := range keysByProvider {
		found, err := mock.ChargesByKeys(tx, run.WorkspaceID, providerID, keys)
		if err != nil {
			return out, err
		}
		for key, charge := range found {
			charges[providerID+"\x00"+key] = charge
		}
	}

	// Posted, non-reversal success journals and their checked totals.
	journals := map[string]journalFact{}
	err = chunks(ids, 500, func(part []string) error {
		var rows []storage.LedgerJournal
		err := tx.Where("workspace_id = ? AND status = 'posted' AND reversal_of IS NULL AND payment_id IN ?", run.WorkspaceID, part).Find(&rows).Error
		if err != nil {
			return err
		}
		for _, j := range rows {
			if j.PaymentID == nil || j.PostingKey != ledger.SuccessPostingKey(*j.PaymentID) {
				continue
			}
			views, err := ledger.GetPosted(tx, run.WorkspaceID, j.ID)
			if err != nil {
				return err
			}
			journals[*j.PaymentID] = journalFact{id: j.ID, debit: views.DebitTotal, credit: views.CreditTotal}
		}
		return nil
	})
	if err != nil {
		return out, err
	}

	// Pending late outcomes for these payments.
	lateByPayment := map[string][]LateEvidence{}
	err = chunks(ids, 500, func(part []string) error {
		var rows []storage.OutboxEvent
		err := tx.Where("workspace_id = ? AND event_type = ? AND status = ? AND payment_id IN ?",
			run.WorkspaceID, payment.OutboxPaymentLateOutcome, event.StatusPending, part).Order("created_at, id").Find(&rows).Error
		for _, row := range rows {
			var late payment.LateOutcome
			if json.Unmarshal([]byte(row.Payload), &late) != nil || row.PaymentID == nil {
				continue
			}
			lateByPayment[*row.PaymentID] = append(lateByPayment[*row.PaymentID], LateEvidence{
				EventID: row.ID, Outcome: late.Outcome, Source: late.Source,
				PaymentStatus: late.PaymentStatus, AttemptStatus: late.AttemptStatus, ObservedAt: late.ObservedAt,
			})
			out.lateEventIDs = append(out.lateEventIDs, row.ID)
		}
		return err
	})
	if err != nil {
		return out, err
	}

	for _, p := range payments {
		attempt, hasAttempt := latest[p.ID]
		if !hasAttempt {
			continue // not a mock-provider payment (or canceled before any attempt)
		}
		out.compared++
		var charge *mock.Charge
		if c, ok := charges[attempt.ProviderID+"\x00"+attempt.ProviderIdempotencyKey]; ok {
			charge = &c
		}
		var journal *journalFact
		if j, ok := journals[p.ID]; ok {
			journal = &j
		}
		for _, f := range compare(p, charge, journal) {
			f.evidence.LateOutcomes = lateByPayment[p.ID]
			if f.evidence.LateOutcomes == nil {
				f.evidence.LateOutcomes = []LateEvidence{}
			}
			out.findings = append(out.findings, f)
		}
	}
	sort.SliceStable(out.findings, func(i, j int) bool {
		if out.findings[i].paymentID != out.findings[j].paymentID {
			return out.findings[i].paymentID < out.findings[j].paymentID
		}
		return out.findings[i].kind < out.findings[j].kind
	})
	return out, nil
}

// expectedPayment maps a mock charge status to the payment status it supports.
func expectedPayment(chargeStatus string) string {
	switch chargeStatus {
	case mock.StatusSucceeded:
		return string(payment.StatusSucceeded)
	case mock.StatusDeclined:
		return string(payment.StatusFailed)
	}
	return "" // processing or unknown: no settled payment status is supported
}

func minor(v int64) string { return strconv.FormatInt(v, 10) }

// compare is the pure rule set for one settled payment.
func compare(p storage.Payment, charge *mock.Charge, journal *journalFact) []finding {
	base := Evidence{
		Source: "mock_provider", Simulated: true,
		Payment: PaymentEvidence{ID: p.ID, Status: p.Status, AmountMinor: minor(p.AmountMinor), Currency: p.Currency},
	}
	if charge != nil {
		base.ProviderCharge = &ChargeEvidence{
			ID: charge.ID, ProviderReference: charge.ProviderReference, Status: charge.Status,
			AmountMinor: minor(charge.AmountMinor), Currency: charge.Currency, Scenario: charge.Scenario,
		}
	}
	if journal != nil {
		base.Journal = &JournalEvidence{ID: journal.id, DebitTotalMinor: minor(journal.debit), CreditTotalMinor: minor(journal.credit)}
	}
	make1 := func(kind, subject, summary, expected string) finding {
		e := base
		e.Summary, e.Expected = summary, expected
		return finding{kind: kind, subject: subject, paymentID: p.ID, evidence: e}
	}
	var out []finding

	// Unresolved payments are reported as such and not compared further: the
	// recovery worker owns resolving them, reconciliation only records them.
	if p.Status == string(payment.StatusUnknown) {
		summary := "Payment is still unknown and the mock provider has no recorded charge."
		if charge != nil {
			summary = fmt.Sprintf("Payment is still unknown; the mock provider recorded the charge as %s.", charge.Status)
		}
		return append(out, make1(KindUnknownOutcome, "payment", summary, ""))
	}

	if charge == nil {
		if p.Status == string(payment.StatusSucceeded) {
			out = append(out, make1(KindMissingAtProvider, "charge", "Payment succeeded but the mock provider has no recorded charge.", "a provider charge"))
		}
	} else {
		if charge.AmountMinor != p.AmountMinor || charge.Currency != p.Currency {
			out = append(out, make1(KindAmountMismatch, "charge:"+charge.ID,
				fmt.Sprintf("Provider charge is %s %s but the payment is %s %s.", minor(charge.AmountMinor), charge.Currency, minor(p.AmountMinor), p.Currency),
				p.Currency+" "+minor(p.AmountMinor)))
		}
		if want := expectedPayment(charge.Status); want != p.Status {
			expected := want
			if expected == "" {
				expected = "an unsettled payment"
			}
			out = append(out, make1(KindStatusMismatch, "charge:"+charge.ID,
				fmt.Sprintf("Provider charge is %s but the payment is %s.", charge.Status, p.Status), expected))
		}
	}

	switch {
	case p.Status == string(payment.StatusSucceeded) && journal == nil:
		out = append(out, make1(KindMissingPosting, "journal", "Payment succeeded but has no posted success journal.", "a posted success journal"))
	case p.Status == string(payment.StatusSucceeded) && (journal.debit != p.AmountMinor || journal.credit != p.AmountMinor):
		out = append(out, make1(KindAmountMismatch, "journal:"+journal.id,
			fmt.Sprintf("Journal totals are %s debit and %s credit but the payment is %s.", minor(journal.debit), minor(journal.credit), minor(p.AmountMinor)),
			p.Currency+" "+minor(p.AmountMinor)))
	case p.Status != string(payment.StatusSucceeded) && journal != nil:
		out = append(out, make1(KindStatusMismatch, "journal:"+journal.id,
			fmt.Sprintf("A success journal is posted but the payment is %s.", p.Status), "no success journal"))
	}
	return out
}
