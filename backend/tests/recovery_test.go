package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/provider"
	"gorm.io/gorm"
)

// The central demo: accept, lose the response (timeout after success), the
// payment is unknown, a lookup at the original provider resolves it, and one
// balanced journal results from one mock charge.
func TestCentralDemoUnknownResolvesThroughLookup(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["timeout_after_success"], "25000", "USD")
	d.drain() // dispatch only: no lookup handler is registered yet
	requireStatus(t, d.payment(id), payment.StatusUnknown)
	atts := d.attempts(id)
	if len(atts) != 1 || atts[0].Status != payment.AttemptUnknown || atts[0].CompletedAt != nil {
		t.Fatalf("attempts %+v", atts)
	}
	key := atts[0].ProviderIdempotencyKey
	if n := len(d.charges("provider_idempotency_key = ?", key)); n != 1 {
		t.Fatalf("charges %d", n)
	}
	if n := len(d.journals(id)); n != 0 {
		t.Fatalf("journal before resolution: %d", n)
	}
	// The API exposes the pending lookup and offers no retry.
	var view payment.RecoveryView
	response := d.do("GET", "/v1/payments/"+id+"/recovery", credentials.OperatorKey, "", "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil || view.LookupStatus != "pending" || view.RetryAllowed {
		t.Fatalf("recovery view %+v %v", view, err)
	}

	recovery := d.addRecovery(payment.RecoveryHooks{}, nil)
	d.drain()
	requireStatus(t, d.payment(id), payment.StatusSucceeded)
	atts = d.attempts(id)
	if len(atts) != 1 || atts[0].Status != payment.AttemptSucceeded || atts[0].ProviderIdempotencyKey != key || atts[0].CompletedAt == nil || atts[0].ProviderReference == nil {
		t.Fatalf("attempts %+v", atts)
	}
	if n := len(d.charges("provider_idempotency_key = ?", key)); n != 1 {
		t.Fatalf("charges %d", n)
	}
	journals := d.journals(id)
	if len(journals) != 1 || journals[0].Status != "posted" {
		t.Fatalf("journals %+v", journals)
	}
	var debit, credit int64
	d.store.DB.Raw("SELECT coalesce(sum(amount_minor),0) FROM ledger_entries WHERE journal_id = ? AND side = 'debit'", journals[0].ID).Scan(&debit)
	d.store.DB.Raw("SELECT coalesce(sum(amount_minor),0) FROM ledger_entries WHERE journal_id = ? AND side = 'credit'", journals[0].ID).Scan(&credit)
	if debit != 25000 || credit != 25000 {
		t.Fatalf("debit %d credit %d", debit, credit)
	}
	if got := joined(d.statuses(id)); got != "created>processing>unknown>succeeded" {
		t.Fatalf("audit %s", got)
	}
	d.requireNoOtherProvider(id)
	d.requireLedger()
	snapshot, err := recovery.Snapshot(context.Background())
	if err != nil || snapshot.Lookups != 1 || snapshot.Resolutions["succeeded"] != 1 || snapshot.Unresolved != 0 || snapshot.UnknownAgeCount != 1 {
		t.Fatalf("metrics %+v %v", snapshot, err)
	}
}

// Timeout after success with the provider unavailable for lookup stays unknown
// (never failed) until the lookup can be answered.
func TestUnknownStaysUnknownWhileProviderCannotAnswer(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["timeout_after_success"], "900", "USD")
	d.drain()
	requireStatus(t, d.payment(id), payment.StatusUnknown)
	recovery := d.addRecovery(payment.RecoveryHooks{}, nil)
	d.adapter.SetAvailable(false)
	for i := 0; i < 12; i++ {
		d.settle(1)
		time.Sleep(4 * time.Millisecond)
	}
	requireStatus(t, d.payment(id), payment.StatusUnknown)
	if a := d.attempts(id); len(a) != 1 || a[0].Status != payment.AttemptUnknown {
		t.Fatalf("attempts %+v", a)
	}
	rows := d.outbox(id, payment.OutboxPaymentLookup)
	if len(rows) != 1 || rows[0].Status == event.StatusDelivered || rows[0].Status == event.StatusFailed || rows[0].Attempts < 3 {
		t.Fatalf("lookup rows %+v", rows)
	}
	if n := len(d.journals(id)); n != 0 {
		t.Fatalf("journals %d", n)
	}
	if recovery.Metrics().Snapshot().LookupUnavailable < 3 {
		t.Fatalf("metrics %+v", recovery.Metrics().Snapshot())
	}
	d.adapter.SetAvailable(true)
	for i := 0; i < 20 && d.payment(id).Status == string(payment.StatusUnknown); i++ {
		d.settle(1)
		time.Sleep(4 * time.Millisecond)
	}
	requireStatus(t, d.payment(id), payment.StatusSucceeded)
	if n := len(d.journals(id)); n != 1 {
		t.Fatalf("journals %d", n)
	}
	d.requireNoOtherProvider(id)
	d.requireLedger()
}

// A lookup parked by the dispatcher's MaxAttempts would strand an unknown
// payment; lookups retry without that limit.
func TestLookupRetriesAreNotParked(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["timeout_after_success"], "900", "USD")
	d.drain()
	d.addRecovery(payment.RecoveryHooks{}, nil)
	d.adapter.SetAvailable(false)
	d.exec("UPDATE outbox_events SET attempts = 25 WHERE payment_id = ? AND event_type = ?", id, payment.OutboxPaymentLookup)
	d.settle(2)
	rows := d.outbox(id, payment.OutboxPaymentLookup)
	if len(rows) != 1 || rows[0].Status == event.StatusFailed {
		t.Fatalf("lookup parked: %+v", rows)
	}
}

// No record at the provider is not proof of no effect. With the idempotent
// replay contract the same key is replayed at the same provider; the outcome
// decides. Without the contract the payment stays unknown and nothing is sent.
func TestNoRecordIsNotProofAndReplayReusesTheKey(t *testing.T) {
	t.Run("timeout before execution stays unknown", func(t *testing.T) {
		d := newDispatchEnv(t, dispatchConfig{})
		id := d.accept(scenarioMethod["timeout_before_execution"], "300", "USD")
		d.drain()
		requireStatus(t, d.payment(id), payment.StatusUnknown)
		recovery := d.addRecovery(payment.RecoveryHooks{}, nil)
		for i := 0; i < 6; i++ {
			d.settle(1)
			time.Sleep(4 * time.Millisecond)
		}
		requireStatus(t, d.payment(id), payment.StatusUnknown) // never failed on absence alone
		if n := len(d.attempts(id)); n != 1 {
			t.Fatalf("attempts %d", n)
		}
		if recovery.Metrics().Snapshot().Replays == 0 {
			t.Fatal("expected same-key replays")
		}
		d.requireNoOtherProvider(id)
		d.requireLedger()
	})
	t.Run("replay executes at most once under the same key", func(t *testing.T) {
		d := newDispatchEnv(t, dispatchConfig{hooks: payment.ExecutorHooks{
			AfterPrepare: func(context.Context, payment.PreparedAttempt) error { return errCrash },
		}})
		id := d.accept(scenarioMethod["success"], "300", "USD")
		_ = d.executor.Handle(context.Background(), d.claimDispatch(time.Now(), time.Minute))
		key := d.attempts(id)[0].ProviderIdempotencyKey
		if n := len(d.charges("provider_idempotency_key = ?", key)); n != 0 {
			t.Fatalf("charge before replay: %d", n)
		}
		later := func() time.Time { return time.Now().Add(time.Hour) }
		d.build(dispatchConfig{nowFor: later})
		recovery := d.addRecovery(payment.RecoveryHooks{}, later)
		if n, err := recovery.RecoverStale(context.Background()); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		d.settle(20)
		requireStatus(t, d.payment(id), payment.StatusSucceeded)
		if atts := d.attempts(id); len(atts) != 1 || atts[0].ProviderIdempotencyKey != key {
			t.Fatalf("attempts %+v", atts)
		}
		if n := len(d.charges("provider_idempotency_key = ?", key)); n != 1 {
			t.Fatalf("charges %d", n)
		}
		d.requireLedger()
	})
	t.Run("no replay contract means lookup only", func(t *testing.T) {
		d := newDispatchEnv(t, dispatchConfig{hooks: payment.ExecutorHooks{
			AfterPrepare: func(context.Context, payment.PreparedAttempt) error { return errCrash },
		}})
		id := d.accept(scenarioMethod["success"], "300", "USD")
		_ = d.executor.Handle(context.Background(), d.claimDispatch(time.Now(), time.Minute))
		wrapped := &noReplay{Provider: d.adapter}
		d.providers["mock"] = wrapped
		later := func() time.Time { return time.Now().Add(time.Hour) }
		d.build(dispatchConfig{nowFor: later, extra: payment.ProviderSet{"mock": wrapped}})
		recovery := d.addRecovery(payment.RecoveryHooks{}, later)
		if _, err := recovery.RecoverStale(context.Background()); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			d.settle(1)
			time.Sleep(4 * time.Millisecond)
		}
		requireStatus(t, d.payment(id), payment.StatusUnknown)
		if wrapped.submits != 0 || d.count("mock_provider_charges", "") != 0 {
			t.Fatalf("submits %d charges %d", wrapped.submits, d.count("mock_provider_charges", ""))
		}
	})
}

// The startup scan moves submitted attempts without a live dispatch lease to
// the lookup path and leaves attempts whose lease is still live alone.
func TestStartupScanMovesStaleSubmittedToLookup(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{hooks: payment.ExecutorHooks{
		BeforeRecord: func(context.Context, payment.PreparedAttempt, provider.Result) error { return errCrash },
	}})
	live := d.accept(scenarioMethod["success"], "100", "USD")
	stale := d.accept(scenarioMethod["success"], "200", "USD")
	first := d.claimDispatch(time.Now(), time.Hour) // live lease
	second := d.claimDispatch(time.Now(), time.Millisecond)
	for _, c := range []event.Claim{first, second} {
		_ = d.executor.Handle(context.Background(), c)
	}
	time.Sleep(5 * time.Millisecond)
	recovery := d.addRecovery(payment.RecoveryHooks{}, nil)
	n, err := recovery.RecoverStale(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("recovered %d %v", n, err)
	}
	statuses := map[string]string{live: d.payment(live).Status, stale: d.payment(stale).Status}
	var movedID, keptID string
	for id, status := range statuses {
		if status == string(payment.StatusUnknown) {
			movedID = id
		} else {
			keptID = id
		}
	}
	if movedID == "" || keptID == "" {
		t.Fatalf("statuses %v", statuses)
	}
	atts := d.attempts(movedID)
	if atts[0].Status != payment.AttemptUnknown || atts[0].ErrorCode == nil || *atts[0].ErrorCode != payment.StaleSubmittedCode {
		t.Fatalf("attempts %+v", atts)
	}
	if rows := d.outbox(movedID, payment.OutboxPaymentLookup); len(rows) != 1 {
		t.Fatalf("lookup rows %d", len(rows))
	}
	requireStatus(t, d.payment(keptID), payment.StatusProcessing)
	// Repeating the scan changes nothing more.
	if n, err := recovery.RecoverStale(context.Background()); err != nil || n != 0 {
		t.Fatalf("second scan %d %v", n, err)
	}
}

// No test path may attempt a different provider for a payment that was ever
// unknown or had a stale submitted attempt, even with the recovery worker
// running and the original provider disabled for routing.
func TestRecoveryNeverCreatesAttemptAtAnotherProvider(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	method := scenarioMethod["timeout_after_success"]
	second, secondAdapter := addSecondProvider(d, method)
	d.build(dispatchConfig{extra: payment.ProviderSet{"stripe": secondAdapter}})
	id := d.accept(method, "4200", "USD")
	d.drain()
	d.exec("UPDATE routing_rules SET enabled = 0 WHERE id = ?", auth.RoutingRuleID(0, 0))
	d.addRecovery(payment.RecoveryHooks{}, nil)
	d.exec("UPDATE outbox_events SET status = 'pending', delivered_at = NULL, available_at = 0 WHERE payment_id = ? AND event_type = ?", id, payment.OutboxPaymentDispatch)
	d.settle(20)
	requireStatus(t, d.payment(id), payment.StatusSucceeded)
	if n := d.count("payment_attempts", "provider_id = ?", second); n != 0 {
		t.Fatalf("%d attempts at the second provider", n)
	}
	if n := d.count("mock_provider_charges", "provider_id = ?", second); n != 0 {
		t.Fatalf("%d charges at the second provider", n)
	}
	if err := d.store.DB.Transaction(func(tx *gorm.DB) error {
		return payment.GuardNewAttempt(tx, auth.DemoWorkspaceID, id, second)
	}); err == nil {
		t.Fatal("guard allowed failover")
	}
	d.requireLedger()
}

// A late success arriving after the payment failed is rejected and recorded
// for reconciliation, not applied, through both the dispatch and lookup paths.
func TestLateSuccessAfterFailedIsRecordedNotApplied(t *testing.T) {
	t.Run("dispatch result", func(t *testing.T) {
		var d *dispatchEnv
		d = newDispatchEnv(t, dispatchConfig{hooks: payment.ExecutorHooks{
			BeforeRecord: func(ctx context.Context, a payment.PreparedAttempt, _ provider.Result) error {
				// Meanwhile another writer proved the attempt failed.
				return d.store.DB.Transaction(func(tx *gorm.DB) error {
					_, err := payment.ApplyDecline(tx, payment.Outcome{WorkspaceID: a.WorkspaceID, PaymentID: a.PaymentID, AttemptID: a.AttemptID, Code: "declined", ActorReference: "test", Now: time.Now().UnixMilli()})
					return err
				})
			},
		}})
		id := d.accept(scenarioMethod["success"], "640", "USD")
		d.drain()
		requireStatus(t, d.payment(id), payment.StatusFailed)
		rows := d.late(id)
		if len(rows) != 1 {
			t.Fatalf("late rows %d", len(rows))
		}
		var late payment.LateOutcome
		if err := json.Unmarshal([]byte(rows[0].Payload), &late); err != nil || late.Outcome != "succeeded" || late.PaymentStatus != "failed" || late.Source != "dispatch" {
			t.Fatalf("late %+v %v", late, err)
		}
		if rows[0].Status != event.StatusPending {
			t.Fatalf("late row status %s: it must stay visible to reconciliation", rows[0].Status)
		}
		if d.outbox(id, payment.OutboxPaymentDispatch)[0].Status != event.StatusDelivered {
			t.Fatal("dispatch command not completed")
		}
		if n := len(d.journals(id)); n != 0 {
			t.Fatalf("journals %d", n)
		}
		var view payment.RecoveryView
		response := d.do("GET", "/v1/payments/"+id+"/recovery", credentials.OperatorKey, "", "")
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil || len(view.LateOutcomes) != 1 || view.LateOutcomes[0].Outcome != "succeeded" {
			t.Fatalf("view %s", response.Body.String())
		}
		d.requireLedger()
	})
	t.Run("lookup result", func(t *testing.T) {
		d := newDispatchEnv(t, dispatchConfig{})
		id := d.accept(scenarioMethod["timeout_after_success"], "650", "USD")
		d.drain()
		attempt := d.attempts(id)[0]
		// Another path resolved the attempt as failed while the lookup was pending.
		d.exec("UPDATE payment_attempts SET status = 'failed', completed_at = 1 WHERE id = ?", attempt.ID)
		d.exec("UPDATE payments SET status = 'failed', version = version + 1 WHERE id = ?", id)
		d.addRecovery(payment.RecoveryHooks{}, nil)
		d.drain()
		if rows := d.late(id); len(rows) != 1 {
			t.Fatalf("late rows %d", len(rows))
		}
		if rows := d.outbox(id, payment.OutboxPaymentLookup); len(rows) != 1 || rows[0].Status != event.StatusDelivered {
			t.Fatalf("lookup rows %+v", rows)
		}
		requireStatus(t, d.payment(id), payment.StatusFailed)
		if n := len(d.journals(id)); n != 0 {
			t.Fatalf("journals %d", n)
		}
	})
}

// Terminal states never regress through any Apply path or direct update.
func TestTerminalStatesNeverRegress(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	for _, scenario := range []string{"success", "decline"} {
		id := d.accept(scenarioMethod[scenario], "100", "USD")
		d.drain()
		attempt := d.attempts(id)[0]
		before := d.payment(id)
		o := payment.Outcome{WorkspaceID: auth.DemoWorkspaceID, PaymentID: id, AttemptID: attempt.ID, ActorReference: "test", Now: time.Now().UnixMilli()}
		for name, apply := range map[string]func(*gorm.DB, payment.Outcome) (payment.Applied, error){"success": payment.ApplySuccess, "decline": payment.ApplyDecline, "unknown": payment.ApplyUnknown} {
			_ = d.store.DB.Transaction(func(tx *gorm.DB) error { _, err := apply(tx, o); return err })
			after := d.payment(id)
			if after.Status != before.Status || after.Version != before.Version && after.Status != before.Status {
				t.Fatalf("%s/%s moved %s -> %s", scenario, name, before.Status, after.Status)
			}
		}
		if err := d.store.DB.Exec("UPDATE payments SET status = 'processing' WHERE id = ?", id).Error; err == nil {
			t.Fatal("database allowed a terminal payment to regress")
		}
	}
	d.requireLedger()
}

// A provider service that outage-fails the first submit (an outage scenario)
// records unknown, and a lookup against an unavailable provider keeps it so.
func TestOutageScenarioStaysUnknown(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["outage"], "800", "USD")
	d.drain()
	d.addRecovery(payment.RecoveryHooks{}, nil)
	for i := 0; i < 6; i++ {
		d.settle(1)
		time.Sleep(4 * time.Millisecond)
	}
	requireStatus(t, d.payment(id), payment.StatusUnknown)
	if n := d.count("mock_provider_charges", ""); n != 0 {
		t.Fatalf("charges %d", n)
	}
	d.requireLedger()
}
