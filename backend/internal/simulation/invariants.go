package simulation

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/reconciliation"
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
	// Webhooks lists "type:deliveries:duplicate_count:outcome/reason" per planned event.
	Webhooks []string
	// Extra carries scenario-specific end state (duplicate burst, reconciliation).
	Extra []string
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
		for _, t := range append(append([]Invariant{}, webhookTitles...), scenarioTitles...) {
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

	// Webhook scenarios: evidence from the inbox and the late-outcome events.
	if len(input.Webhooks) > 0 {
		if err := webhookInvariants(db, scenario, run, input, p, journals, transitions, add, &snapshot); err != nil {
			return nil, Snapshot{}, err
		}
	}

	switch scenario.ID {
	case DuplicateRequest:
		if err := duplicateInvariants(db, run, input, p, len(attempts), keys, paymentsForRef, recordsForKey, add, &snapshot); err != nil {
			return nil, Snapshot{}, err
		}
	case SettlementMismatch:
		if err := reconcileInvariants(db, run, input, p, len(journals), add, &snapshot); err != nil {
			return nil, Snapshot{}, err
		}
	}

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

// Webhook scenario invariant identifiers.
const (
	InvOneInboxRow       = "one_inbox_row_per_event"
	InvDuplicateCount    = "duplicate_count_matches_repeats"
	InvSingleEffect      = "single_financial_effect"
	InvLateEventIgnored  = "late_event_ignored_and_recorded"
	InvStateUnchangedByE = "state_unchanged_by_events"
)

var webhookTitles = []Invariant{
	{ID: InvOneInboxRow, Title: "Exactly one inbox row per provider event"},
	{ID: InvDuplicateCount, Title: "Inbox duplicate count equals the repeat deliveries"},
	{ID: InvSingleEffect, Title: "Exactly one financial effect despite repeated events"},
	{ID: InvLateEventIgnored, Title: "Late contradicting events are ignored and recorded as late outcomes"},
	{ID: InvStateUnchangedByE, Title: "Payment state is unchanged by webhook events"},
}

func webhookInvariants(db *gorm.DB, scenario Scenario, run storage.SimulationRun, input Input, p storage.Payment,
	journals []ledger.JournalView, transitions []storage.PaymentStateTransition, add func(id, status, evidence string), snapshot *Snapshot) error {
	ws := run.WorkspaceID
	ids := make([]string, len(input.Webhooks))
	for i, plan := range input.Webhooks {
		ids[i] = plan.EventID
	}
	var rows []storage.WebhookInbox
	if err := db.Where("workspace_id = ? AND provider_event_id IN ?", ws, ids).Find(&rows).Error; err != nil {
		return err
	}
	byEvent := map[string][]storage.WebhookInbox{}
	for _, row := range rows {
		byEvent[row.ProviderEventID] = append(byEvent[row.ProviderEventID], row)
	}
	oneRow, countsOK := true, true
	var rowEvidence, countEvidence []string
	for _, plan := range input.Webhooks {
		list := byEvent[plan.EventID]
		rowEvidence = append(rowEvidence, fmt.Sprintf("%s: %d row(s)", plan.Type, len(list)))
		if len(list) != 1 {
			oneRow, countsOK = false, false
			continue
		}
		row := list[0]
		outcome := "none"
		if row.Outcome != nil {
			outcome = *row.Outcome
			if row.OutcomeReason != nil {
				outcome += "/" + *row.OutcomeReason
			}
		}
		snapshot.Webhooks = append(snapshot.Webhooks, fmt.Sprintf("%s:%d:%d:%s", plan.Type, plan.Deliveries, row.DuplicateCount, outcome))
		countEvidence = append(countEvidence, fmt.Sprintf("%s delivered %d time(s), duplicate_count %d, outcome %s", plan.Type, plan.Deliveries, row.DuplicateCount, outcome))
		if row.DuplicateCount != int64(plan.Deliveries-1) || row.ConflictCount != 0 {
			countsOK = false
		}
	}
	status, evidence := verdict(oneRow, "%s", strings.Join(rowEvidence, "; "))
	add(InvOneInboxRow, status, evidence)
	status, evidence = verdict(countsOK, "%s", strings.Join(countEvidence, "; "))
	add(InvDuplicateCount, status, evidence)

	var lates []storage.OutboxEvent
	if err := db.Where("workspace_id = ? AND payment_id = ? AND event_type = ?", ws, p.ID, payment.OutboxPaymentLateOutcome).Find(&lates).Error; err != nil {
		return err
	}
	terminalTransitions := 0
	for _, t := range transitions {
		if payment.Status(t.ToStatus).Terminal() {
			terminalTransitions++
		}
	}
	switch scenario.ID {
	case DuplicateWebhook:
		status, evidence = verdict(len(journals) == 1 && terminalTransitions == 1 && len(lates) == 0,
			"%d journal(s), %d terminal transition(s), %d late outcome(s) after the repeated deliveries", len(journals), terminalTransitions, len(lates))
		add(InvSingleEffect, status, evidence)
		status, evidence = verdict(p.Status == string(scenario.ExpectedStatus), "payment is %s after the duplicates; expected %s", p.Status, scenario.ExpectedStatus)
		add(InvStateUnchangedByE, status, evidence)
	case ReorderedEvents:
		late := input.Webhooks[len(input.Webhooks)-1]
		ignored := false
		reason := "no inbox row"
		if list := byEvent[late.EventID]; len(list) == 1 && list[0].Outcome != nil && list[0].OutcomeReason != nil {
			reason = *list[0].Outcome + "/" + *list[0].OutcomeReason
			ignored = *list[0].Outcome == payment.InboxIgnored && *list[0].OutcomeReason == "terminal_state"
		}
		declined := 0
		for _, l := range lates {
			if strings.Contains(l.Payload, `"outcome":"declined"`) {
				declined++
			}
		}
		status, evidence = verdict(ignored && declined == 1,
			"late %s event outcome %s; %d payment.late_outcome row(s) for a declined outcome", late.Type, reason, declined)
		add(InvLateEventIgnored, status, evidence)
		status, evidence = verdict(p.Status == string(scenario.ExpectedStatus) && terminalTransitions == 1 && len(journals) == 1,
			"payment is %s with %d terminal transition(s) and %d journal(s) after the reordered events", p.Status, terminalTransitions, len(journals))
		add(InvStateUnchangedByE, status, evidence)
	}
	return nil
}

// Scenario-specific invariant identifiers.
const (
	InvDuplicateBurst   = "duplicate_burst_answers"
	InvDuplicateSingle  = "duplicate_single_effects"
	InvDiscrepancy      = "discrepancy_recorded_with_evidence"
	InvNoLedgerChange   = "reconciliation_changed_no_ledger_rows"
	InvEvidenceOnlyStep = "resolution_is_evidence_only"
)

var scenarioTitles = []Invariant{
	{ID: InvDuplicateBurst, Title: "One first-accept, the rest replays, and a changed amount is rejected with 409"},
	{ID: InvDuplicateSingle, Title: "One payment, one idempotency record, one attempt, one charge and one dispatch command"},
	{ID: InvDiscrepancy, Title: "Reconciliation recorded a discrepancy with simulated provider evidence"},
	{ID: InvNoLedgerChange, Title: "The reconciliation run changed no journal, entry, payment transition or attempt"},
	{ID: InvEvidenceOnlyStep, Title: "The investigation was recorded as evidence only, with no adjusting entry"},
}

func duplicateInvariants(db *gorm.DB, run storage.SimulationRun, input Input, p storage.Payment, attempts int, keys []string,
	paymentsForRef, recordsForKey int64, add func(id, status, evidence string), snapshot *Snapshot) error {
	d := input.Duplicate
	if d == nil {
		add(InvDuplicateBurst, InvariantFail, "no burst evidence was recorded")
		add(InvDuplicateSingle, InvariantFail, "no burst evidence was recorded")
		return nil
	}
	ws := run.WorkspaceID
	status, evidence := verdict(d.Sent == DuplicateBurst && d.FirstAccept == 1 && d.Replays == DuplicateBurst-1 && d.Errors == 0 && d.ConflictStatus == 409 && d.ConflictCode == "idempotency_conflict",
		"%d request(s) sent: %d first-accept (202), %d replay(s), %d error(s); changed-amount request answered HTTP %d %s",
		d.Sent, d.FirstAccept, d.Replays, d.Errors, d.ConflictStatus, d.ConflictCode)
	add(InvDuplicateBurst, status, evidence)
	var charges, dispatches int64
	if len(keys) > 0 {
		if err := db.Table("mock_provider_charges").Where("workspace_id = ? AND provider_idempotency_key IN ?", ws, keys).Count(&charges).Error; err != nil {
			return err
		}
	}
	if err := db.Model(&storage.OutboxEvent{}).Where("workspace_id = ? AND payment_id = ? AND event_type = ?", ws, p.ID, payment.OutboxPaymentDispatch).Count(&dispatches).Error; err != nil {
		return err
	}
	status, evidence = verdict(paymentsForRef == 1 && recordsForKey == 1 && attempts == 1 && charges == 1 && dispatches == 1,
		"payments=%d idempotency_records=%d provider_attempts=%d mock_charges=%d outbox_dispatch_rows=%d",
		paymentsForRef, recordsForKey, attempts, charges, dispatches)
	add(InvDuplicateSingle, status, evidence)
	snapshot.Extra = append(snapshot.Extra, fmt.Sprintf("burst:%d:%d:%d:%d:%d", d.Sent, d.FirstAccept, d.Replays, d.Errors, d.ConflictStatus),
		fmt.Sprintf("rows:%d:%d:%d:%d:%d", paymentsForRef, recordsForKey, attempts, charges, dispatches))
	return nil
}

func reconcileInvariants(db *gorm.DB, run storage.SimulationRun, input Input, p storage.Payment, journals int,
	add func(id, status, evidence string), snapshot *Snapshot) error {
	rc := input.Reconcile
	if rc == nil || rc.DiscrepancyID == "" {
		add(InvDiscrepancy, InvariantFail, "no discrepancy was recorded for the payment")
		add(InvNoLedgerChange, InvariantFail, "no reconciliation evidence was recorded")
		add(InvEvidenceOnlyStep, InvariantFail, "no resolution was recorded")
		return nil
	}
	ws := run.WorkspaceID
	view, err := reconciliation.GetDiscrepancy(db, ws, rc.DiscrepancyID)
	if err != nil {
		return err
	}
	var details reconciliation.Evidence
	_ = json.Unmarshal([]byte(view.Details), &details)
	chargeStatus := "none"
	if details.ProviderCharge != nil {
		chargeStatus = details.ProviderCharge.Status
	}
	kindOK := view.Kind == reconciliation.KindUnknownOutcome || view.Kind == reconciliation.KindStatusMismatch
	status, evidence := verdict(kindOK && details.Simulated && chargeStatus == "succeeded" && details.Payment.Status == string(payment.StatusUnknown),
		"discrepancy %s kind %s, status %s; provider charge %s (simulated=%t) while payment is %s; evidence: %s",
		view.ID, view.Kind, view.Status, chargeStatus, details.Simulated, details.Payment.Status, details.Summary)
	add(InvDiscrepancy, status, evidence)

	now, err := ledgerCounts(db, ws, p.ID)
	if err != nil {
		return err
	}
	unchanged := rc.Before == rc.After && now.Journals == rc.Before.Journals && now.Entries == rc.Before.Entries && now.Transitions == rc.Before.Transitions
	status, evidence = verdict(unchanged && p.Status == string(payment.StatusUnknown),
		"ledger rows changed: 0 required; journals %d->%d, entries %d->%d, payment transitions %d->%d, attempts %d->%d (still %d journals, %d entries now); payment is %s",
		rc.Before.Journals, rc.After.Journals, rc.Before.Entries, rc.After.Entries, rc.Before.Transitions, rc.After.Transitions,
		rc.Before.Attempts, rc.After.Attempts, now.Journals, now.Entries, p.Status)
	add(InvNoLedgerChange, status, evidence)

	noteOK := false
	note := ""
	for _, h := range view.History {
		if h.ToStatus == reconciliation.StatusInvestigating && strings.TrimSpace(h.Note) != "" {
			noteOK, note = true, h.Note
		}
	}
	status, evidence = verdict(noteOK && view.Status == reconciliation.StatusInvestigating && journals == 0 && now.Journals == 0,
		"discrepancy status %s with %d history event(s); %d journal(s) exist for the payment, so no adjusting entry was posted; note: %q",
		view.Status, len(view.History), journals, note)
	add(InvEvidenceOnlyStep, status, evidence)
	snapshot.Extra = append(snapshot.Extra, fmt.Sprintf("discrepancy:%s:%s:%s:%t", view.Kind, chargeStatus, view.Status, unchanged))
	return nil
}
