package simulation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// Invariant result statuses. "pending" is what a run reports until the backend
// has evidence; stored results are only pass or fail.
const (
	InvariantPass    = "pass"
	InvariantFail    = "fail"
	InvariantPending = "pending"
)

// Invariant identifiers.
const (
	InvOnePaymentPerKey     = "one_payment_per_key"
	InvOneBalancedJournal   = "one_balanced_journal"
	InvNoCrossProvider      = "no_cross_provider_after_unknown"
	InvNoTerminalRegression = "no_terminal_regression"
	InvAtMostOneCharge      = "at_most_one_provider_charge"
	InvExpectedOutcome      = "expected_outcome"
)

// Invariant is one check with its stored evidence.
type Invariant struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

// Titles lists the invariants in display order, for pending placeholders.
var Titles = []Invariant{
	{ID: InvOnePaymentPerKey, Title: "One payment per idempotency key"},
	{ID: InvOneBalancedJournal, Title: "One balanced journal for a succeeded payment, none otherwise"},
	{ID: InvNoCrossProvider, Title: "No cross-provider attempt after an unknown outcome"},
	{ID: InvNoTerminalRegression, Title: "No transition out of a terminal state"},
	{ID: InvAtMostOneCharge, Title: "At most one succeeded provider charge per payment"},
	{ID: InvExpectedOutcome, Title: "Payment reached the scenario's expected status"},
}

// Snapshot is the scenario-relevant end state used for the fingerprint.
type Snapshot struct {
	FinalStatus  string
	Transitions  []string
	Attempts     []string
	JournalCount int
}

func verdict(ok bool, evidence string, args ...any) (string, string) {
	status := InvariantPass
	if !ok {
		status = InvariantFail
	}
	return status, fmt.Sprintf(evidence, args...)
}

// CheckInvariants evaluates every invariant against persisted rows only.
func CheckInvariants(db *gorm.DB, scenario Scenario, run storage.SimulationRun, input Input, p storage.Payment) ([]Invariant, Snapshot, error) {
	ws := run.WorkspaceID
	transitions, err := storage.ListStateTransitions(db, ws, p.ID)
	if err != nil {
		return nil, Snapshot{}, err
	}
	attempts, err := storage.ListPaymentAttempts(db, ws, p.ID)
	if err != nil {
		return nil, Snapshot{}, err
	}
	journals, err := ledger.ListPosted(db, ws, ledger.JournalFilter{PaymentID: p.ID, Limit: 20})
	if err != nil {
		return nil, Snapshot{}, err
	}
	var paymentsForRef, recordsForKey int64
	if err := db.Model(&storage.Payment{}).Where("workspace_id = ? AND merchant_reference = ?", ws, input.MerchantReference).Count(&paymentsForRef).Error; err != nil {
		return nil, Snapshot{}, err
	}
	var records []storage.IdempotencyRecord
	if err := db.Where("workspace_id = ? AND operation = ? AND key = ?", ws, payment.OperationCreatePayment, input.IdempotencyKey).Find(&records).Error; err != nil {
		return nil, Snapshot{}, err
	}
	recordsForKey = int64(len(records))
	keys := make([]string, 0, len(attempts))
	for _, a := range attempts {
		keys = append(keys, a.ProviderIdempotencyKey)
	}
	var succeededCharges int64
	if len(keys) > 0 {
		if err := db.Table("mock_provider_charges").Where("workspace_id = ? AND provider_idempotency_key IN ? AND status = 'succeeded'", ws, keys).Count(&succeededCharges).Error; err != nil {
			return nil, Snapshot{}, err
		}
	}

	snapshot := Snapshot{FinalStatus: p.Status, JournalCount: len(journals)}
	for _, t := range transitions {
		snapshot.Transitions = append(snapshot.Transitions, t.ToStatus)
	}
	providers := map[string]bool{}
	for _, a := range attempts {
		providers[a.ProviderID] = true
		code := ""
		if a.ErrorCode != nil {
			code = *a.ErrorCode
		}
		snapshot.Attempts = append(snapshot.Attempts, fmt.Sprintf("%d:%s:%s", a.AttemptNumber, a.Status, code))
	}

	var out []Invariant
	add := func(id string, status, evidence string) {
		title := id
		for _, t := range Titles {
			if t.ID == id {
				title = t.Title
			}
		}
		out = append(out, Invariant{ID: id, Title: title, Status: status, Evidence: evidence})
	}

	// 1. One payment per key.
	linked := recordsForKey == 1 && records[0].PaymentID != nil && *records[0].PaymentID == p.ID
	status, evidence := verdict(paymentsForRef == 1 && linked,
		"%d payment row(s) carry merchant reference %s; %d idempotency record(s) for key %s, linked to payment %s: %t",
		paymentsForRef, input.MerchantReference, recordsForKey, input.IdempotencyKey, p.ID, linked)
	add(InvOnePaymentPerKey, status, evidence)

	// 2. One balanced journal.
	wantJournals := 0
	if p.Status == string(payment.StatusSucceeded) {
		wantJournals = 1
	}
	balanced := true
	var detail []string
	for _, j := range journals {
		var debit, credit int64
		for _, e := range j.Entries {
			if e.Side == string(ledger.Debit) {
				debit += e.AmountMinor
			} else {
				credit += e.AmountMinor
			}
		}
		if debit != credit || debit != p.AmountMinor || j.Journal.Currency != p.Currency {
			balanced = false
		}
		detail = append(detail, fmt.Sprintf("journal %s debits %d credits %d %s", j.Journal.ID, debit, credit, j.Journal.Currency))
	}
	status, evidence = verdict(len(journals) == wantJournals && balanced,
		"%d posted journal(s) for a payment that is %s (expected %d); payment amount %d %s; %s",
		len(journals), p.Status, wantJournals, p.AmountMinor, p.Currency, orNone(detail))
	add(InvOneBalancedJournal, status, evidence)

	// 3. No cross-provider attempt after unknown.
	everUnknown := false
	for _, t := range transitions {
		if t.ToStatus == string(payment.StatusUnknown) {
			everUnknown = true
		}
	}
	for _, a := range attempts {
		if a.Status == payment.AttemptUnknown {
			everUnknown = true
		}
	}
	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	status, evidence = verdict(!everUnknown || len(providers) <= 1,
		"payment reached unknown: %t; %d attempt(s) across %d provider(s) [%s]", everUnknown, len(attempts), len(providers), strings.Join(ids, ", "))
	add(InvNoCrossProvider, status, evidence)

	// 4. No terminal regression.
	regression := ""
	var trail []string
	terminalSeen := ""
	for i, t := range transitions {
		trail = append(trail, t.ToStatus)
		from := payment.Status("")
		if t.FromStatus != nil {
			from = payment.Status(*t.FromStatus)
		}
		switch {
		case terminalSeen != "":
			regression = fmt.Sprintf("transition %d (%s) happened after terminal state %s", i+1, t.ToStatus, terminalSeen)
		case payment.ValidateTransition(from, payment.Status(t.ToStatus)) != nil:
			regression = fmt.Sprintf("transition %d (%s -> %s) is illegal", i+1, orStart(t.FromStatus), t.ToStatus)
		}
		if regression != "" {
			break
		}
		if payment.Status(t.ToStatus).Terminal() {
			terminalSeen = t.ToStatus
		}
	}
	status, evidence = verdict(regression == "", "transition trail: %s%s", strings.Join(trail, " -> "), suffix(regression))
	add(InvNoTerminalRegression, status, evidence)

	// 5. At most one succeeded charge.
	status, evidence = verdict(succeededCharges <= 1, "%d succeeded provider charge(s) across %d attempt key(s)", succeededCharges, len(keys))
	add(InvAtMostOneCharge, status, evidence)

	// 6. Expected outcome.
	status, evidence = verdict(p.Status == string(scenario.ExpectedStatus), "payment is %s; scenario %s expects %s", p.Status, scenario.ID, scenario.ExpectedStatus)
	add(InvExpectedOutcome, status, evidence)
	return out, snapshot, nil
}

// PendingInvariants are shown while a run has no evidence yet.
func PendingInvariants() []Invariant {
	out := make([]Invariant, len(Titles))
	for i, t := range Titles {
		out[i] = Invariant{ID: t.ID, Title: t.Title, Status: InvariantPending, Evidence: "Waiting for the backend to evaluate persisted evidence"}
	}
	return out
}

func orNone(items []string) string {
	if len(items) == 0 {
		return "no journal"
	}
	return strings.Join(items, "; ")
}

func suffix(problem string) string {
	if problem == "" {
		return ""
	}
	return "; " + problem
}

func orStart(s *string) string {
	if s == nil {
		return "start"
	}
	return *s
}
