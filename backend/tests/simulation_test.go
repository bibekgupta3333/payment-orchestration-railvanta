package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/simulation"
	"github.com/railvanta/railvanta/backend/internal/storage"
	httpapi "github.com/railvanta/railvanta/backend/internal/transport/http"
)

// simEnv runs the real dispatcher and recovery worker in the background so a
// simulation exercises the same pipeline as production.
func newSimEnv(t *testing.T) *dispatchEnv {
	t.Helper()
	d := newDispatchEnv(t, dispatchConfig{poll: 5 * time.Millisecond})
	d.addRecovery(payment.RecoveryHooks{}, nil)
	d.dispatcher.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = d.dispatcher.Stop(ctx)
	})
	return d
}

func (d *dispatchEnv) simulate(scenario, seed string) (int, httpapi.SimulationView) {
	d.t.Helper()
	body := `{"scenario":"` + scenario + `","seed":"` + seed + `"}`
	response := d.do("POST", "/v1/sandbox/simulations", credentials.OperatorKey, "", body)
	var view httpapi.SimulationView
	if response.Code == 202 {
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
			d.t.Fatal(err)
		}
	}
	return response.Code, view
}

func (d *dispatchEnv) awaitSimulation(id string) httpapi.SimulationView {
	d.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		response := d.do("GET", "/v1/sandbox/simulations/"+id, credentials.OperatorKey, "", "")
		if response.Code != 200 {
			d.t.Fatalf("get simulation: %d %s", response.Code, response.Body.String())
		}
		var view httpapi.SimulationView
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
			d.t.Fatal(err)
		}
		if view.Status != storage.SimulationRunning {
			return view
		}
		for _, inv := range view.Invariants {
			if inv.Status != simulation.InvariantPending {
				d.t.Fatalf("running simulation reported invariant %s as %s before evidence", inv.ID, inv.Status)
			}
		}
		if time.Now().After(deadline) {
			d.t.Fatalf("simulation %s still running", id)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func requireAllPass(t *testing.T, view httpapi.SimulationView) {
	t.Helper()
	if len(view.Invariants) != len(simulation.Titles) {
		t.Fatalf("%d invariants", len(view.Invariants))
	}
	for _, inv := range view.Invariants {
		if inv.Status != simulation.InvariantPass || inv.Evidence == "" {
			t.Errorf("%s: %s (%s)", inv.ID, inv.Status, inv.Evidence)
		}
	}
}

// WBS 3.2.12: every implemented scenario ends in its expected state with every
// backend-checked invariant passing and evidence stored.
func TestSimulationScenariosEndWithPassingInvariants(t *testing.T) {
	d := newSimEnv(t)
	cases := []struct {
		scenario string
		status   string
		journals int
		attempts int
	}{
		{simulation.Success, "succeeded", 1, 1},
		{simulation.Decline, "failed", 0, 1},
		{simulation.TimeoutBeforeExecution, "unknown", 0, 1},
		{simulation.TimeoutAfterSuccess, "succeeded", 1, 1},
		{simulation.DuplicateRequest, "succeeded", 1, 1},
		{simulation.Outage, "unknown", 0, 1},
		{simulation.UnknownRecovery, "succeeded", 1, 1},
		{simulation.DuplicatePosting, "succeeded", 1, 1},
	}
	for _, c := range cases {
		t.Run(c.scenario, func(t *testing.T) {
			code, started := d.simulate(c.scenario, "seed-"+c.scenario)
			if code != 202 || started.Label != httpapi.SimulationLabel || started.Status != storage.SimulationRunning && started.Status != storage.SimulationCompleted {
				t.Fatalf("start: %d %+v", code, started)
			}
			view := d.awaitSimulation(started.ID)
			if view.Status != storage.SimulationCompleted || view.Payment == nil || view.Payment.Status != c.status {
				t.Fatalf("run %s payment %+v failure %v", view.Status, view.Payment, view.FailureReason)
			}
			requireAllPass(t, view)
			if len(view.Journals) != c.journals || len(view.Attempts) != c.attempts || view.Fingerprint == nil {
				t.Fatalf("journals %d attempts %d fingerprint %v", len(view.Journals), len(view.Attempts), view.Fingerprint)
			}
			if got := d.count("payments", "merchant_reference = ?", view.Request.MerchantReference); got != 1 {
				t.Fatalf("%d payments for the run", got)
			}
			d.requireNoOtherProvider(view.Payment.ID)
			d.requireLedger()
			if c.status == "unknown" && !view.Routing.FailoverBlocked {
				t.Fatalf("unknown payment did not report unsafe failover blocked")
			}
			var stored storage.SimulationRun
			if err := d.store.DB.Where("id = ?", view.ID).Take(&stored).Error; err != nil || stored.InvariantsJSON == nil || stored.ResultJSON == nil || stored.InjectedFault == nil {
				t.Fatalf("stored run %+v %v", stored, err)
			}
		})
	}
}

// Acceptance gate: timeout after success ends with one payment, one provider
// charge, one balanced posted journal and no second provider.
func TestSimulationTimeoutAfterSuccessGate(t *testing.T) {
	d := newSimEnv(t)
	_, started := d.simulate(simulation.TimeoutAfterSuccess, "gate")
	view := d.awaitSimulation(started.ID)
	requireAllPass(t, view)
	id := view.Payment.ID
	if got := d.count("mock_provider_charges", "provider_idempotency_key = ?", payment.ProviderIdempotencyKey(id, 1)); got != 1 {
		t.Fatalf("%d provider charges", got)
	}
	if got := d.count("mock_provider_charges", "1 = 1"); got != 1 {
		t.Fatalf("%d charges in total", got)
	}
	if got := len(d.journals(id)); got != 1 {
		t.Fatalf("%d journals", got)
	}
	d.requireNoOtherProvider(id)
	want := []string{"created", "processing", "unknown", "succeeded"}
	got := d.statuses(id)
	if len(got) != len(want) {
		t.Fatalf("transitions %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("transitions %v", got)
		}
	}
}

// Acceptance gate: the same seed gives identical states and invariant results.
func TestSimulationSameSeedIsDeterministic(t *testing.T) {
	d := newSimEnv(t)
	for _, scenario := range []string{simulation.Success, simulation.TimeoutAfterSuccess, simulation.Outage, simulation.DuplicateRequest} {
		_, a := d.simulate(scenario, "repeat")
		first := d.awaitSimulation(a.ID)
		_, b := d.simulate(scenario, "repeat")
		second := d.awaitSimulation(b.ID)
		if first.ID == second.ID || first.PaymentID == nil || *first.PaymentID == *second.PaymentID {
			t.Fatalf("%s: runs must be separate payments", scenario)
		}
		if first.Fingerprint == nil || second.Fingerprint == nil || *first.Fingerprint != *second.Fingerprint {
			t.Fatalf("%s: fingerprints differ %v %v", scenario, first.Fingerprint, second.Fingerprint)
		}
		if first.Request.AmountMinor != second.Request.AmountMinor || first.Payment.Status != second.Payment.Status {
			t.Fatalf("%s: states differ", scenario)
		}
		for i := range first.Invariants {
			if first.Invariants[i].Status != second.Invariants[i].Status {
				t.Fatalf("%s: invariant %s differs", scenario, first.Invariants[i].ID)
			}
		}
	}
	_, other := d.simulate(simulation.Success, "different-seed")
	if view := d.awaitSimulation(other.ID); view.Request.AmountMinor == "" {
		t.Fatal("missing amount")
	}
}

// Scenarios the backend cannot run are rejected with 422 and create nothing.
func TestSimulationRejectsUnimplementedAndUnknownScenarios(t *testing.T) {
	d := newSimEnv(t)
	before := d.dump()
	for scenario, code := range map[string]string{
		simulation.DuplicateWebhook: "scenario_not_implemented",
		simulation.ReorderedEvents:  "scenario_not_implemented",
		"made_up":                   "unknown_scenario",
	} {
		response := d.do("POST", "/v1/sandbox/simulations", credentials.OperatorKey, "", `{"scenario":"`+scenario+`"}`)
		var envelope httpapi.ErrorEnvelope
		_ = json.Unmarshal(response.Body.Bytes(), &envelope)
		if response.Code != 422 || envelope.Code != code {
			t.Fatalf("%s: %d %s", scenario, response.Code, response.Body.String())
		}
		if code == "scenario_not_implemented" && (len(envelope.FieldErrors) != 1 || envelope.FieldErrors[0].Message != "Waiting for "+simulation.WebhookDependency) {
			t.Fatalf("missing dependency: %+v", envelope.FieldErrors)
		}
	}
	for table, sum := range before {
		if d.dump()[table] != sum {
			t.Fatalf("rejected scenario changed %s", table)
		}
	}
	if response := d.do("POST", "/v1/sandbox/simulations", credentials.ReaderKey, "", `{"scenario":"success"}`); response.Code != 403 {
		t.Fatalf("reader: %d", response.Code)
	}
	if response := d.do("POST", "/v1/sandbox/simulations", credentials.OperatorKey, "", `{"scenario":"success","seed":"bad seed!"}`); response.Code != 400 {
		t.Fatalf("seed: %d", response.Code)
	}
	if response := d.do("GET", "/v1/sandbox/simulations/missing", credentials.OperatorKey, "", ""); response.Code != 404 {
		t.Fatalf("missing run: %d", response.Code)
	}
	if response := d.do("GET", "/v1/sandbox/simulations/missing", credentials.OtherWorkspaceKey, "", ""); response.Code != 404 {
		t.Fatalf("other workspace: %d", response.Code)
	}
	response := d.do("GET", "/v1/sandbox/scenarios", credentials.ReaderKey, "", "")
	var catalog struct {
		Items []httpapi.ScenarioView `json:"items"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &catalog) != nil || len(catalog.Items) != len(simulation.Catalog) {
		t.Fatalf("scenarios: %d %s", response.Code, response.Body.String())
	}
	disabled := 0
	for _, item := range catalog.Items {
		if !item.Implemented {
			disabled++
			if item.MissingDependency == nil || *item.MissingDependency == "" {
				t.Fatalf("%s lacks a missing dependency", item.ID)
			}
		}
	}
	if disabled != 2 {
		t.Fatalf("%d disabled scenarios", disabled)
	}
}

// The invariant checker reports violations instead of passing everything.
func TestSimulationInvariantsDetectViolations(t *testing.T) {
	d := newSimEnv(t)
	_, started := d.simulate(simulation.Success, "violations")
	view := d.awaitSimulation(started.ID)
	requireAllPass(t, view)
	var run storage.SimulationRun
	if err := d.store.DB.Where("id = ?", view.ID).Take(&run).Error; err != nil {
		t.Fatal(err)
	}
	p := d.payment(*run.PaymentID)
	var input simulation.Input
	if err := json.Unmarshal([]byte(run.InputJSON), &input); err != nil {
		t.Fatal(err)
	}
	// A transition after a terminal state, a second payment for the key and a
	// scenario expecting a different outcome must each fail their invariant.
	d.exec("INSERT INTO payment_state_transitions(id,workspace_id,payment_id,from_status,to_status,reason,actor_reference,created_at) VALUES('bad-transition',?,?,'succeeded','failed','test regression','test',?)", p.WorkspaceID, p.ID, time.Now().UnixMilli()+1000)
	d.exec("INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,merchant_reference,status,version,created_at,updated_at) VALUES('dup-payment',?,?,1,'USD',?,'created',1,1,1)", p.WorkspaceID, p.PaymentMethodID, input.MerchantReference)
	scenario, _ := simulation.Lookup(simulation.Decline)
	checks, _, err := simulation.CheckInvariants(d.store.DB, scenario, run, input, p)
	if err != nil {
		t.Fatal(err)
	}
	failed := map[string]bool{}
	for _, c := range checks {
		if c.Status == simulation.InvariantFail {
			failed[c.ID] = true
		}
	}
	for _, id := range []string{simulation.InvOnePaymentPerKey, simulation.InvNoTerminalRegression, simulation.InvExpectedOutcome} {
		if !failed[id] {
			t.Errorf("%s did not fail: %+v", id, checks)
		}
	}
	if failed[simulation.InvOneBalancedJournal] || failed[simulation.InvNoCrossProvider] {
		t.Errorf("unrelated invariants failed: %+v", checks)
	}
}

// Evaluation explains its path, writes nothing, and blocks unsafe failover.
func TestRoutingEvaluateExplainsAndBlocksUnsafeFailover(t *testing.T) {
	d := newSimEnv(t)
	_, started := d.simulate(simulation.Outage, "failover")
	view := d.awaitSimulation(started.ID)
	if view.Payment.Status != "unknown" {
		t.Fatalf("status %s", view.Payment.Status)
	}
	before := d.dump()
	outboxBefore := d.count("outbox_events", "1 = 1")
	response := d.do("POST", "/v1/routing/evaluate", credentials.OperatorKey, "", `{"payment_method_id":"`+scenarioMethod["success"]+`","amount_minor":"2500","currency":"USD"}`)
	var plain httpapi.RoutingDecisionView
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &plain) != nil {
		t.Fatalf("evaluate: %d %s", response.Code, response.Body.String())
	}
	if plain.Rule == nil || plain.RuleID == nil || *plain.RuleID != plain.Rule.ID || plain.FailoverBlocked || len(plain.Checks) < 8 || len(plain.Candidates) != 1 {
		t.Fatalf("decision %+v", plain)
	}
	for _, check := range plain.Checks {
		if !check.Passed {
			t.Fatalf("check %+v failed for an eligible request", check)
		}
	}
	response = d.do("POST", "/v1/routing/evaluate", credentials.OperatorKey, "", `{"payment_method_id":"`+scenarioMethod["success"]+`","amount_minor":"2500","currency":"USD","payment_id":"`+view.Payment.ID+`"}`)
	var blocked httpapi.RoutingDecisionView
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &blocked) != nil {
		t.Fatalf("blocked evaluate: %d %s", response.Code, response.Body.String())
	}
	if !blocked.FailoverBlocked || blocked.ProviderID != auth.DemoProviderID || len(blocked.Candidates) != 0 {
		t.Fatalf("blocked %+v", blocked)
	}
	failedChecks := 0
	for _, check := range blocked.Checks {
		if !check.Passed {
			failedChecks++
		}
	}
	if failedChecks != 1 {
		t.Fatalf("expected exactly the failover check to fail: %+v", blocked.Checks)
	}
	response = d.do("POST", "/v1/routing/evaluate", credentials.OperatorKey, "", `{"payment_method_id":"`+scenarioMethod["success"]+`","amount_minor":"2500","currency":"USD","payment_id":"missing"}`)
	if response.Code != 404 {
		t.Fatalf("missing payment: %d", response.Code)
	}
	d.exec("UPDATE routing_rules SET enabled = 0 WHERE workspace_id = ? AND currency = 'JPY'", auth.DemoWorkspaceID)
	response = d.do("POST", "/v1/routing/evaluate", credentials.OperatorKey, "", `{"payment_method_id":"`+scenarioMethod["success"]+`","amount_minor":"2500","currency":"JPY"}`)
	var envelope httpapi.ErrorEnvelope
	_ = json.Unmarshal(response.Body.Bytes(), &envelope)
	if response.Code != 422 || envelope.Code != "no_eligible_provider" || len(envelope.FieldErrors) == 0 {
		t.Fatalf("no eligible: %d %s", response.Code, response.Body.String())
	}
	d.exec("UPDATE routing_rules SET enabled = 1 WHERE workspace_id = ? AND currency = 'JPY'", auth.DemoWorkspaceID)
	if got := d.count("outbox_events", "1 = 1"); got != outboxBefore {
		t.Fatalf("evaluate added outbox rows: %d -> %d", outboxBefore, got)
	}
	for table, sum := range before {
		// The live recovery worker keeps retrying the outage payment's lookup, so
		// outbox rows legitimately change; evaluation adds none (counted above).
		if d.dump()[table] != sum && table != "routing_rules" && table != "outbox_events" {
			t.Fatalf("evaluate changed %s", table)
		}
	}
}

// Rules, providers and health are read-only views of stored data; health shows
// "no observations" until attempts exist.
func TestRoutingRulesAndProviderHealth(t *testing.T) {
	d := newSimEnv(t)
	response := d.do("GET", "/v1/routing/rules", credentials.ReaderKey, "", "")
	var rules struct {
		Items []httpapi.RoutingRuleView `json:"items"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &rules) != nil || len(rules.Items) == 0 {
		t.Fatalf("rules: %d %s", response.Code, response.Body.String())
	}
	if rules.Items[0].ProviderCode != "mock" || rules.Items[0].Rail != "mock" || rules.Items[0].MinAmountMinor == "" {
		t.Fatalf("rule %+v", rules.Items[0])
	}
	health := func() httpapi.ProviderHealthView {
		response := d.do("GET", "/v1/providers/health", credentials.ReaderKey, "", "")
		var body struct {
			Items []httpapi.ProviderHealthView `json:"items"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Items) != 3 {
			t.Fatalf("health: %d %s", response.Code, response.Body.String())
		}
		for _, item := range body.Items {
			if item.Code == "stripe" || item.Code == "plaid" {
				if item.Configured || item.Status != "no_observations" {
					t.Fatalf("%s %+v", item.Code, item)
				}
			}
			if item.Code == "mock" {
				return item
			}
		}
		t.Fatal("no mock health")
		return httpapi.ProviderHealthView{}
	}
	empty := health()
	if empty.Status != "no_observations" || empty.ObservationCount != 0 || !empty.Configured || empty.DataSource != "simulated" {
		t.Fatalf("empty health %+v", empty)
	}
	for _, w := range empty.Windows {
		if w.Observations != 0 || w.SuccessRatePercent != nil || w.P95Millis != nil {
			t.Fatalf("zero evidence reported as data: %+v", w)
		}
	}
	for i := 0; i < 2; i++ {
		_, started := d.simulate(simulation.Success, "health")
		d.awaitSimulation(started.ID)
	}
	_, declined := d.simulate(simulation.Decline, "health")
	d.awaitSimulation(declined.ID)
	seen := health()
	if seen.ObservationCount != 3 || seen.Status != "insufficient_data" {
		t.Fatalf("three observations must not yield a verdict: %+v", seen)
	}
	var day httpapi.ProviderWindowView
	for _, w := range seen.Windows {
		if w.Window == "24h" {
			day = w
		}
	}
	if day.Observations != 3 || day.Succeeded != 2 || day.Failed != 1 || day.SuccessRatePercent == nil || *day.SuccessRatePercent != "66.7" || day.ErrorCounts["declined"] != 1 {
		t.Fatalf("24h window %+v", day)
	}
	response = d.do("GET", "/v1/providers", credentials.ReaderKey, "", "")
	var list struct {
		Items []httpapi.ProviderView `json:"items"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &list) != nil || len(list.Items) != 3 {
		t.Fatalf("providers: %d %s", response.Code, response.Body.String())
	}
	modes := map[string]string{}
	for _, p := range list.Items {
		modes[p.Code] = p.Mode
	}
	if modes["mock"] != "simulated" || modes["stripe"] != "not_configured" || modes["plaid"] != "not_configured" {
		t.Fatalf("modes %v", modes)
	}
	if r := d.do("GET", "/v1/providers/stripe", credentials.ReaderKey, "", ""); r.Code != 200 {
		t.Fatalf("stripe detail: %d", r.Code)
	}
	if r := d.do("GET", "/v1/providers/"+auth.DemoProviderID, credentials.ReaderKey, "", ""); r.Code != 200 {
		t.Fatalf("mock detail: %d", r.Code)
	}
	if r := d.do("GET", "/v1/providers/"+auth.OtherProviderID, credentials.ReaderKey, "", ""); r.Code != 404 {
		t.Fatalf("other workspace provider: %d", r.Code)
	}
}
