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
)

// WebhookDependency names what the webhook scenarios wait for.
const WebhookDependency = "Webhook inbox and signature verification (WBS 4.3)"

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
	{ID: DuplicateRequest, Label: "Duplicate request", Description: "The same Idempotency-Key is submitted concurrently. One payment results and every replay returns it.",
		Implemented: true, InjectedFault: "the create request is sent three times with one Idempotency-Key", MockScenario: "success", ExpectedStatus: payment.StatusSucceeded},
	{ID: DuplicateWebhook, Label: "Duplicate webhook", Description: "A provider webhook is delivered twice.",
		Implemented: false, MissingDependency: WebhookDependency, InjectedFault: "duplicate webhook delivery"},
	{ID: ReorderedEvents, Label: "Reordered events", Description: "Provider events arrive out of order.",
		Implemented: false, MissingDependency: WebhookDependency, InjectedFault: "reordered webhook delivery"},
	{ID: Outage, Label: "Provider outage", Description: "The provider is unavailable. Submission and lookups fail, the payment stays unknown, and no other provider is tried.",
		Implemented: true, InjectedFault: "provider unavailable for submit and lookup", MockScenario: "outage", ExpectedStatus: payment.StatusUnknown},
	{ID: UnknownRecovery, Label: "Unknown-outcome recovery", Description: "An unknown outcome is resolved by the recovery worker through a lookup at the original provider with the stored key.",
		Implemented: true, InjectedFault: "response lost after the provider executed the charge; recovery must resolve it", MockScenario: "timeout_after_success", ExpectedStatus: payment.StatusSucceeded},
	{ID: DuplicatePosting, Label: "Duplicate ledger post", Description: "After success, the success journal is posted twice more with the same posting key. Exactly one balanced journal must exist.",
		Implemented: true, InjectedFault: "the ledger success posting is replayed twice", MockScenario: "success", ExpectedStatus: payment.StatusSucceeded},
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
