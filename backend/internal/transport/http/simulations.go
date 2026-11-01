package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/simulation"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// SimulationLabel is carried by every simulation response.
const SimulationLabel = "SIMULATED / SANDBOX ONLY"

// serveRoutingAndSimulation routes the WBS 3.2 read and simulation endpoints.
// It reports false for paths it does not own.
func (h *handler) serveRoutingAndSimulation(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) bool {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/v1/routing/rules":
		h.listRoutingRules(w, r, principal, id)
	case path == "/v1/providers" || strings.HasPrefix(path, "/v1/providers/"):
		return h.serveProviders(w, r, principal, id)
	case r.Method == http.MethodGet && path == "/v1/sandbox/scenarios":
		h.listScenarios(w, id)
	case r.Method == http.MethodPost && path == "/v1/sandbox/simulations":
		h.createSimulation(w, r, principal, id)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/sandbox/simulations/"):
		runID := strings.TrimPrefix(path, "/v1/sandbox/simulations/")
		if !safeID.MatchString(runID) {
			return false
		}
		h.getSimulation(w, r, principal, id, runID)
	default:
		return false
	}
	return true
}

// ScenarioView is one catalog entry; unimplemented scenarios name what they wait for.
type ScenarioView struct {
	ID                string  `json:"id"`
	Label             string  `json:"label"`
	Description       string  `json:"description"`
	Implemented       bool    `json:"implemented"`
	MissingDependency *string `json:"missing_dependency"`
	InjectedFault     string  `json:"injected_fault"`
}

func (h *handler) listScenarios(w http.ResponseWriter, _ string) {
	items := make([]ScenarioView, 0, len(simulation.Catalog))
	for _, s := range simulation.Catalog {
		view := ScenarioView{ID: s.ID, Label: s.Label, Description: s.Description, Implemented: s.Implemented, InjectedFault: s.InjectedFault}
		if !s.Implemented {
			dependency := s.MissingDependency
			view.MissingDependency = &dependency
		}
		items = append(items, view)
	}
	writeJSON(w, 200, map[string]any{"label": SimulationLabel, "items": items})
}

func (h *handler) createSimulation(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	if r.Header.Get("Content-Type") != "application/json" && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json;") {
		fail(w, 415, "unsupported_media_type", "Use application/json", id)
		return
	}
	var input struct {
		Scenario    string `json:"scenario"`
		Seed        string `json:"seed"`
		AmountMinor string `json:"amount_minor"`
		Currency    string `json:"currency"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			fail(w, 413, "body_too_large", "Request body exceeds the limit", id)
			return
		}
		fail(w, 400, "validation_error", "Malformed request or invalid field types", id, FieldError{Field: "body", Message: "Use the documented request schema"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		fail(w, 400, "validation_error", "Exactly one JSON value is required", id, FieldError{Field: "body", Message: "Trailing content after the JSON object"})
		return
	}
	var fields []FieldError
	if input.Scenario == "" {
		fields = append(fields, FieldError{Field: "scenario", Message: "Required scenario identifier"})
	}
	if input.Seed == "" {
		input.Seed = "default"
	}
	if !safeID.MatchString(input.Seed) {
		fields = append(fields, FieldError{Field: "seed", Message: "Use 1-64 letters, digits, underscores or hyphens"})
	}
	var amount int64
	if input.AmountMinor != "" {
		parsed, err := payment.ParseAmount(input.AmountMinor)
		if err != nil {
			fields = append(fields, FieldError{Field: "amount_minor", Message: "Use a positive integer decimal string no greater than 9223372036854775807"})
		}
		amount = parsed
	}
	if input.Currency != "" {
		if err := payment.ValidateCurrency(input.Currency); err != nil {
			fields = append(fields, FieldError{Field: "currency", Message: "Use a supported upper-case currency code"})
		}
	}
	if len(fields) > 0 {
		fail(w, 400, "validation_error", "Invalid simulation request", id, fields...)
		return
	}
	run, err := h.sims.Start(r.Context(), simulation.StartRequest{
		WorkspaceID: principal.WorkspaceID, ActorReference: actorReference(principal), Scenario: input.Scenario,
		Seed: input.Seed, AmountMinor: amount, Currency: input.Currency,
	})
	var notImplemented *simulation.NotImplementedError
	switch {
	case err == nil:
	case errors.Is(err, simulation.ErrUnknownScenario):
		fail(w, 422, "unknown_scenario", "The scenario is not part of the simulator catalog", id, FieldError{Field: "scenario", Message: "Fetch GET /v1/sandbox/scenarios for the supported identifiers"})
		return
	case errors.As(err, &notImplemented):
		fail(w, 422, "scenario_not_implemented", "This scenario is not implemented by the backend yet", id, FieldError{Field: "scenario", Message: "Waiting for " + notImplemented.Dependency})
		return
	case errors.Is(err, simulation.ErrMethodUnavailable):
		fail(w, 422, "scenario_unavailable", "This workspace has no seeded mock payment method for the scenario", id, FieldError{Field: "scenario", Message: err.Error()})
		return
	default:
		fail(w, 503, "unavailable", "Simulation could not be started", id)
		return
	}
	view, err := h.simulationView(r, run)
	if err != nil {
		fail(w, 503, "unavailable", "Simulation unavailable", id)
		return
	}
	writeJSON(w, 202, view)
}

func (h *handler) getSimulation(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, runID string) {
	run, err := storage.GetSimulationRun(h.db.WithContext(r.Context()), principal.WorkspaceID, runID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			fail(w, 404, "not_found", "Simulation not found", id)
		} else {
			fail(w, 503, "unavailable", "Simulation unavailable", id)
		}
		return
	}
	if run.Status == storage.SimulationRunning {
		// Resume a run whose worker was lost, for example by a restart.
		h.sims.Watch(run.WorkspaceID, run.ID)
	}
	view, err := h.simulationView(r, run)
	if err != nil {
		fail(w, 503, "unavailable", "Simulation unavailable", id)
		return
	}
	writeJSON(w, 200, view)
}

// SimulationView is the persisted run plus the evidence read from the real
// pipeline's rows. Nothing in it is synthesized by the simulator.
type SimulationView struct {
	ID            string                  `json:"id"`
	Label         string                  `json:"label"`
	Scenario      string                  `json:"scenario"`
	Seed          string                  `json:"seed"`
	Status        string                  `json:"status"`
	PaymentID     *string                 `json:"payment_id"`
	InjectedFault string                  `json:"injected_fault"`
	CreatedAt     string                  `json:"created_at"`
	CompletedAt   *string                 `json:"completed_at"`
	FailureReason *string                 `json:"failure_reason"`
	Fingerprint   *string                 `json:"fingerprint"`
	Request       SimulationRequestView   `json:"request"`
	Response      *SimulationResponseView `json:"response"`
	Payment       *payment.PaymentView    `json:"payment"`
	Routing       SimulationRoutingView   `json:"routing"`
	Attempts      []payment.AttemptView   `json:"attempts"`
	Transitions   []SimulationTransition  `json:"transitions"`
	Events        []SimulationEvent       `json:"events"`
	Webhooks      []SimulationWebhook     `json:"webhooks"`
	Journals      []LedgerJournalView     `json:"journals"`
	Invariants    []simulation.Invariant  `json:"invariants"`
	Timeline      []SimulationTimelineRow `json:"timeline"`
}

// SimulationRequestView is the payment request the simulator sent.
type SimulationRequestView struct {
	PaymentMethodID   string `json:"payment_method_id"`
	AmountMinor       string `json:"amount_minor"`
	Currency          string `json:"currency"`
	IdempotencyKey    string `json:"idempotency_key"`
	MerchantReference string `json:"merchant_reference"`
}

// SimulationResponseView is the stored acceptance response.
type SimulationResponseView struct {
	StatusCode int             `json:"status_code"`
	Body       json.RawMessage `json:"body"`
}

// SimulationRoutingView lists each attempt's routing decision and whether a
// failover to another provider is currently blocked.
type SimulationRoutingView struct {
	FailoverBlocked bool                `json:"failover_blocked"`
	FailoverReason  *string             `json:"failover_reason"`
	Decisions       []SimulationRouting `json:"decisions"`
}

// SimulationRouting is one attempt's persisted routing decision.
type SimulationRouting struct {
	AttemptNumber int64   `json:"attempt_number"`
	ProviderID    string  `json:"provider_id"`
	RuleID        *string `json:"rule_id"`
	Reason        string  `json:"reason"`
}

// SimulationTransition is one audited payment status change.
type SimulationTransition struct {
	FromStatus     *string `json:"from_status"`
	ToStatus       string  `json:"to_status"`
	Reason         string  `json:"reason"`
	ActorReference string  `json:"actor_reference"`
	CreatedAt      string  `json:"created_at"`
}

// SimulationEvent is one durable outbox row of the payment.
type SimulationEvent struct {
	ID          string  `json:"id"`
	EventType   string  `json:"event_type"`
	Status      string  `json:"status"`
	Attempts    int64   `json:"attempts"`
	CreatedAt   string  `json:"created_at"`
	DeliveredAt *string `json:"delivered_at"`
}

// SimulationWebhook is one webhook inbox row received for the payment.
type SimulationWebhook struct {
	ID              string  `json:"id"`
	ProviderEventID string  `json:"provider_event_id"`
	EventType       string  `json:"event_type"`
	Status          string  `json:"status"`
	Outcome         *string `json:"outcome"`
	OutcomeReason   *string `json:"outcome_reason"`
	DuplicateCount  int64   `json:"duplicate_count"`
	SignatureStatus string  `json:"signature_status"`
	ReceivedAt      string  `json:"received_at"`
}

// SimulationTimelineRow is one dated fact, sorted oldest first.
type SimulationTimelineRow struct {
	At     string `json:"at"`
	Source string `json:"source"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type timelineEntry struct {
	at  int64
	row SimulationTimelineRow
}

func (h *handler) simulationView(r *http.Request, run storage.SimulationRun) (SimulationView, error) {
	db := h.db.WithContext(r.Context())
	var input simulation.Input
	if err := json.Unmarshal([]byte(run.InputJSON), &input); err != nil {
		return SimulationView{}, err
	}
	view := SimulationView{
		ID: run.ID, Label: SimulationLabel, Scenario: run.Scenario, Seed: run.Seed, Status: run.Status, PaymentID: run.PaymentID,
		CreatedAt: payment.FormatTimestamp(run.CreatedAt), FailureReason: run.FailureReason, Fingerprint: run.Fingerprint,
		Request: SimulationRequestView{
			PaymentMethodID: input.PaymentMethodID, AmountMinor: input.AmountMinor, Currency: input.Currency,
			IdempotencyKey: input.IdempotencyKey, MerchantReference: input.MerchantReference,
		},
		Routing:     SimulationRoutingView{Decisions: []SimulationRouting{}},
		Attempts:    []payment.AttemptView{},
		Transitions: []SimulationTransition{}, Events: []SimulationEvent{}, Webhooks: []SimulationWebhook{}, Journals: []LedgerJournalView{},
		Invariants: simulation.PendingInvariants(),
	}
	if run.InjectedFault != nil {
		view.InjectedFault = *run.InjectedFault
	}
	if run.CompletedAt != nil {
		value := payment.FormatTimestamp(*run.CompletedAt)
		view.CompletedAt = &value
	}
	// Invariants stay pending until the backend stored evidence for them.
	if run.InvariantsJSON != nil {
		var stored []simulation.Invariant
		if err := json.Unmarshal([]byte(*run.InvariantsJSON), &stored); err == nil && len(stored) > 0 {
			view.Invariants = stored
		}
	}
	var timeline []timelineEntry
	add := func(at int64, source, title, detail string) {
		timeline = append(timeline, timelineEntry{at, SimulationTimelineRow{At: payment.FormatTimestamp(at), Source: source, Title: title, Detail: detail}})
	}
	for _, step := range input.Steps {
		add(step.At, "simulator", step.Step, step.Detail)
	}
	if run.PaymentID != nil {
		ws, paymentID := run.WorkspaceID, *run.PaymentID
		p, err := storage.GetPayment(db, ws, paymentID)
		if err != nil {
			return SimulationView{}, err
		}
		pv := payment.NewPaymentView(p)
		view.Payment = &pv
		var record storage.IdempotencyRecord
		if err := db.Where("workspace_id = ? AND payment_id = ?", ws, paymentID).Order("created_at").Take(&record).Error; err == nil && record.ResponseStatus != nil && record.ResponseBody != nil {
			view.Response = &SimulationResponseView{StatusCode: int(*record.ResponseStatus), Body: json.RawMessage(*record.ResponseBody)}
		}
		attempts, err := storage.ListPaymentAttempts(db, ws, paymentID)
		if err != nil {
			return SimulationView{}, err
		}
		for _, a := range attempts {
			view.Attempts = append(view.Attempts, payment.NewAttemptView(a))
			view.Routing.Decisions = append(view.Routing.Decisions, SimulationRouting{AttemptNumber: a.AttemptNumber, ProviderID: a.ProviderID, RuleID: a.RoutingRuleID, Reason: a.RoutingReason})
			add(a.CreatedAt, "attempt", "Attempt "+strconv.FormatInt(a.AttemptNumber, 10)+" created", "Routed to provider "+a.ProviderID+": "+a.RoutingReason)
			if a.SubmittedAt != nil {
				add(*a.SubmittedAt, "attempt", "Attempt "+strconv.FormatInt(a.AttemptNumber, 10)+" submitted", "Sent to the provider with key "+a.ProviderIdempotencyKey)
			}
			if a.CompletedAt != nil {
				detail := "Attempt status " + a.Status
				if a.ErrorCode != nil {
					detail += " (" + *a.ErrorCode + ")"
				}
				add(*a.CompletedAt, "attempt", "Attempt "+strconv.FormatInt(a.AttemptNumber, 10)+" completed", detail)
			}
			if a.Status == payment.AttemptUnknown || a.Status == payment.AttemptSubmitted {
				reason := "Unsafe failover blocked: attempt " + strconv.FormatInt(a.AttemptNumber, 10) + " at provider " + a.ProviderID + " is " + a.Status +
					", so the payment may already have executed. It is resolved by a lookup at that same provider; it is never routed to another provider."
				view.Routing.FailoverBlocked = true
				view.Routing.FailoverReason = &reason
			}
		}
		transitions, err := storage.ListStateTransitions(db, ws, paymentID)
		if err != nil {
			return SimulationView{}, err
		}
		for _, t := range transitions {
			view.Transitions = append(view.Transitions, SimulationTransition{FromStatus: t.FromStatus, ToStatus: t.ToStatus, Reason: t.Reason, ActorReference: t.ActorReference, CreatedAt: payment.FormatTimestamp(t.CreatedAt)})
			from := "start"
			if t.FromStatus != nil {
				from = *t.FromStatus
			}
			add(t.CreatedAt, "payment", "Payment "+from+" to "+t.ToStatus, t.Reason)
		}
		var outbox []storage.OutboxEvent
		if err := db.Where("workspace_id = ? AND payment_id = ?", ws, paymentID).Order("created_at, id").Find(&outbox).Error; err != nil {
			return SimulationView{}, err
		}
		for _, e := range outbox {
			ev := SimulationEvent{ID: e.ID, EventType: e.EventType, Status: e.Status, Attempts: e.Attempts, CreatedAt: payment.FormatTimestamp(e.CreatedAt)}
			add(e.CreatedAt, "event", e.EventType+" recorded", "Durable event with status "+e.Status)
			if e.DeliveredAt != nil {
				value := payment.FormatTimestamp(*e.DeliveredAt)
				ev.DeliveredAt = &value
				add(*e.DeliveredAt, "event", e.EventType+" delivered", "Handled after "+strconv.FormatInt(e.Attempts, 10)+" delivery attempt(s)")
			}
			view.Events = append(view.Events, ev)
		}
		var inbox []storage.WebhookInbox
		if err := db.Where("workspace_id = ? AND payment_id = ?", ws, paymentID).Order("received_at, id").Find(&inbox).Error; err != nil {
			return SimulationView{}, err
		}
		for _, row := range inbox {
			view.Webhooks = append(view.Webhooks, SimulationWebhook{
				ID: inboxPrefix + row.ID, ProviderEventID: row.ProviderEventID, EventType: row.EventType, Status: row.Status,
				Outcome: row.Outcome, OutcomeReason: row.OutcomeReason, DuplicateCount: row.DuplicateCount,
				SignatureStatus: row.SignatureStatus, ReceivedAt: payment.FormatTimestamp(row.ReceivedAt),
			})
			detail := "Inbox row " + row.Status
			if row.Outcome != nil {
				detail += ", outcome " + *row.Outcome
				if row.OutcomeReason != nil {
					detail += " (" + *row.OutcomeReason + ")"
				}
			}
			add(row.ReceivedAt, "webhook", row.EventType+" received", detail+"; repeats "+strconv.FormatInt(row.DuplicateCount, 10))
		}
		journals, err := ledger.ListPosted(db, ws, ledger.JournalFilter{PaymentID: paymentID, Limit: 20})
		if err != nil {
			return SimulationView{}, err
		}
		for _, jv := range journals {
			view.Journals = append(view.Journals, journalView(jv))
			if jv.Journal.PostedAt != nil {
				add(*jv.Journal.PostedAt, "ledger", "Journal posted", "Posting key "+jv.Journal.PostingKey)
			}
		}
	}
	if run.CompletedAt != nil {
		detail := "Invariants evaluated against persisted evidence"
		if run.FailureReason != nil {
			detail = *run.FailureReason
		}
		add(*run.CompletedAt, "simulator", "run_"+run.Status, detail)
	}
	sort.SliceStable(timeline, func(i, j int) bool { return timeline[i].at < timeline[j].at })
	view.Timeline = make([]SimulationTimelineRow, len(timeline))
	for i, e := range timeline {
		view.Timeline[i] = e.row
	}
	return view, nil
}

func journalView(view ledger.JournalView) LedgerJournalView {
	j := view.Journal
	postedAt := ""
	if j.PostedAt != nil {
		postedAt = payment.FormatTimestamp(*j.PostedAt)
	}
	entries := make([]LedgerEntryView, 0, len(view.Entries))
	for _, e := range view.Entries {
		entries = append(entries, LedgerEntryView{
			ID: e.ID, AccountID: e.AccountID, AccountCode: e.AccountCode, ProviderID: e.AccountProviderID,
			Side: e.Side, AmountMinor: strconv.FormatInt(e.AmountMinor, 10),
		})
	}
	return LedgerJournalView{
		ID: j.ID, PaymentID: j.PaymentID, Currency: j.Currency, Status: j.Status, PostingKey: j.PostingKey, ReversalOf: j.ReversalOf,
		CreatedAt: payment.FormatTimestamp(j.CreatedAt), PostedAt: postedAt, Entries: entries,
	}
}
