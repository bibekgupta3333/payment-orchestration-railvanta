// Package simulation runs sandbox fault scenarios through the REAL payment
// pipeline: a payment is accepted through payment.Service, dispatched and
// recovered by the live dispatcher against the deterministic mock provider,
// and posted to the ledger. The simulator injects no fake state. It chooses a
// seeded mock method (the fault), watches the persisted result, and checks
// backend invariants against stored evidence. Runs are persisted in
// simulation_runs.
package simulation

import (
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/railvanta/railvanta/backend/internal/payment"
)

// Scenario identifiers (the API contract).
const (
	Success                = "success"
	Decline                = "decline"
	TimeoutBeforeExecution = "timeout_before_execution"
	TimeoutAfterSuccess    = "timeout_after_success"
	DuplicateRequest       = "duplicate_request"
	DuplicateWebhook       = "duplicate_webhook"
	ReorderedEvents        = "reordered_events"
	Outage                 = "outage"
	UnknownRecovery        = "unknown_recovery"
	DuplicatePosting       = "duplicate_posting"
	SettlementMismatch     = "settlement_mismatch"
)

// Scenario describes one catalog entry.
type Scenario struct {
	ID          string
	Label       string
	Description string
	// Implemented is false when the backend cannot run the scenario yet.
	Implemented bool
	// MissingDependency names what an unimplemented scenario waits for.
	MissingDependency string
	// InjectedFault says what is injected, in words.
	InjectedFault string
	// MockScenario is the seeded mock method scenario the run uses.
	MockScenario string
	// ExpectedStatus is the final payment status the run must reach.
	ExpectedStatus payment.Status
}

// Catalog lists every scenario in display order.
var Catalog = []Scenario{
	{ID: Success, Label: "Success", Description: "The provider executes the charge and answers immediately.",
		Implemented: true, InjectedFault: "none", MockScenario: "success", ExpectedStatus: payment.StatusSucceeded},
	{ID: Decline, Label: "Decline", Description: "The provider proves the charge had no effect.",
		Implemented: true, InjectedFault: "provider declines the charge", MockScenario: "decline", ExpectedStatus: payment.StatusFailed},
	{ID: TimeoutBeforeExecution, Label: "Timeout before execution", Description: "The request times out and the provider never executed it. A lookup finds no record, so the payment stays unknown and is never sent to another provider.",
		Implemented: true, InjectedFault: "provider times out before executing the charge", MockScenario: "timeout_before_execution", ExpectedStatus: payment.StatusUnknown},
	{ID: TimeoutAfterSuccess, Label: "Timeout after success", Description: "The provider executes the charge but the response is lost. The payment becomes unknown and a lookup at the same provider resolves it to succeeded.",
		Implemented: true, InjectedFault: "response lost after the provider executed the charge", MockScenario: "timeout_after_success", ExpectedStatus: payment.StatusSucceeded},
	{ID: DuplicateRequest, Label: "Duplicate request", Description: "Ten concurrent requests share one Idempotency-Key. Exactly one is accepted and nine replay the stored response. An eleventh request with the same key and a changed amount is rejected with 409 idempotency_conflict. One payment, one provider attempt and one charge result.",
		Implemented: true, InjectedFault: "the create request is sent ten times concurrently with one Idempotency-Key, then once more with a changed amount", MockScenario: "success", ExpectedStatus: payment.StatusSucceeded},
	{ID: DuplicateWebhook, Label: "Duplicate webhook", Description: "After the payment succeeds, the same provider event is delivered several times (3 to 5, from the seed) through the real webhook inbox. One inbox row counts the repeats and no second financial effect occurs.",
		Implemented: true, InjectedFault: "the same provider webhook event is delivered repeatedly", MockScenario: "success", ExpectedStatus: payment.StatusSucceeded},
	{ID: ReorderedEvents, Label: "Reordered events", Description: "After the payment succeeds, a stale processing event and a contradicting late failure arrive through the real webhook inbox. The terminal state holds and the late failure is kept for reconciliation.",
		Implemented: true, InjectedFault: "stale and contradicting provider events arrive after the terminal state", MockScenario: "success", ExpectedStatus: payment.StatusSucceeded},
	{ID: Outage, Label: "Provider outage", Description: "The provider is unavailable. Submission and lookups fail, the payment stays unknown, and no other provider is tried.",
		Implemented: true, InjectedFault: "provider unavailable for submit and lookup", MockScenario: "outage", ExpectedStatus: payment.StatusUnknown},
	{ID: UnknownRecovery, Label: "Unknown-outcome recovery", Description: "An unknown outcome is resolved by the recovery worker through a lookup at the original provider with the stored key.",
		Implemented: true, InjectedFault: "response lost after the provider executed the charge; recovery must resolve it", MockScenario: "timeout_after_success", ExpectedStatus: payment.StatusSucceeded},
	{ID: DuplicatePosting, Label: "Duplicate ledger post", Description: "After success, the success journal is posted twice more with the same posting key. Exactly one balanced journal must exist.",
		Implemented: true, InjectedFault: "the ledger success posting is replayed twice", MockScenario: "success", ExpectedStatus: payment.StatusSucceeded},
	{ID: SettlementMismatch, Label: "Settlement mismatch investigation", Description: "The provider-side charge SUCCEEDED (a labelled simulated provider record) while the internal payment is still unknown. A reconciliation run records an unknown_outcome discrepancy with provider evidence, changes no payment or ledger row, and the discrepancy is moved to investigating with an evidence-only note.",
		Implemented: true, InjectedFault: "simulated provider-side settlement recorded while the internal payment stays unknown; recovery lookups held for the investigation", MockScenario: "outage", ExpectedStatus: payment.StatusUnknown},
}

// ErrUnknownScenario means the identifier is not in the catalog.
var ErrUnknownScenario = errors.New("unknown simulation scenario")

// NotImplementedError is returned for catalogued scenarios the backend cannot run.
type NotImplementedError struct{ Scenario, Dependency string }

func (e *NotImplementedError) Error() string {
	return fmt.Sprintf("scenario %s is not implemented: waiting for %s", e.Scenario, e.Dependency)
}

// Lookup finds a scenario by identifier.
func Lookup(id string) (Scenario, bool) {
	for _, s := range Catalog {
		if s.ID == id {
			return s, true
		}
	}
	return Scenario{}, false
}

// SeedAmount derives a deterministic amount (USD cents) from a seed.
func SeedAmount(seed string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return 1000 + int64(h.Sum32()%90000)
}

// WebhookRepeats derives how many times the duplicate_webhook event is
// delivered (3 to 5) from the seed.
func WebhookRepeats(seed string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte("webhook:" + seed))
	return 3 + int(h.Sum32()%3)
}

// WebhookPlan is one provider event the simulator delivers through the inbox.
type WebhookPlan struct {
	EventID    string `json:"event_id"`
	Type       string `json:"type"`
	Deliveries int    `json:"deliveries"`
}

// PlanWebhooks returns the deterministic event plan for a webhook scenario.
func PlanWebhooks(scenario, runID, seed string) []WebhookPlan {
	id := func(n int) string { return fmt.Sprintf("sim_%s_evt_%d", runID, n) }
	switch scenario {
	case DuplicateWebhook:
		return []WebhookPlan{{id(1), payment.WebhookSucceeded, WebhookRepeats(seed)}}
	case ReorderedEvents:
		return []WebhookPlan{{id(1), payment.WebhookSucceeded, 1}, {id(2), payment.WebhookProcessing, 1}, {id(3), payment.WebhookFailed, 1}}
	}
	return nil
}
