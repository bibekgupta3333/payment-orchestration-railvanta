package tests

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/provider"
	"github.com/railvanta/railvanta/backend/internal/provider/mock"
	"gorm.io/gorm"
)

// Each mock scenario ends in its documented payment and attempt states.
func TestEveryMockScenarioReachesDocumentedStates(t *testing.T) {
	cases := []struct {
		scenario         string
		method           string
		payment, attempt string
		code             string
		charges          int
		chargeStatus     string
		journal          bool
		lookup           bool
	}{
		{"success", scenarioMethod["success"], "succeeded", "succeeded", "", 1, mock.StatusSucceeded, true, false},
		{"seeded default", auth.DemoMethodID, "succeeded", "succeeded", "", 1, mock.StatusSucceeded, true, false},
		{"decline", scenarioMethod["decline"], "failed", "failed", provider.CodeDeclined, 1, mock.StatusDeclined, false, false},
		{"delayed_success", scenarioMethod["delayed_success"], "succeeded", "succeeded", "", 1, mock.StatusSucceeded, true, false},
		{"timeout_before_execution", scenarioMethod["timeout_before_execution"], "unknown", "unknown", provider.CodeTimeout, 0, "", false, true},
		{"timeout_after_success", scenarioMethod["timeout_after_success"], "unknown", "unknown", provider.CodeTimeout, 1, mock.StatusSucceeded, false, true},
		{"outage", scenarioMethod["outage"], "unknown", "unknown", provider.CodeProviderUnavailable, 0, "", false, true},
	}
	d := newDispatchEnv(t, dispatchConfig{})
	for _, tc := range cases {
		t.Run(tc.scenario, func(t *testing.T) {
			id := d.accept(tc.method, "25000", "USD")
			d.drain()
			p := d.payment(id)
			requireStatus(t, p, payment.Status(tc.payment))
			attempts := d.attempts(id)
			if len(attempts) != 1 {
				t.Fatalf("attempts %d", len(attempts))
			}
			a := attempts[0]
			if a.Status != tc.attempt || a.ProviderID != auth.DemoProviderID || a.AttemptNumber != 1 ||
				a.ProviderIdempotencyKey != payment.ProviderIdempotencyKey(id, 1) || a.RoutingRuleID == nil || *a.RoutingRuleID != auth.RoutingRuleID(0, 0) ||
				a.RoutingReason == "" || a.SubmittedAt == nil {
				t.Fatalf("attempt %+v", a)
			}
			if (a.ErrorCode == nil) != (tc.code == "") || a.ErrorCode != nil && *a.ErrorCode != tc.code {
				t.Fatalf("error code %v, want %q", a.ErrorCode, tc.code)
			}
			terminal := tc.attempt != "unknown"
			if (a.CompletedAt != nil) != terminal || terminal && *a.CompletedAt < *a.SubmittedAt {
				t.Fatalf("completed_at %v submitted_at %v", a.CompletedAt, a.SubmittedAt)
			}
			if tc.attempt == "succeeded" && (a.ProviderReference == nil || *a.ProviderReference != mock.ProviderReference(auth.DemoProviderID, a.ProviderIdempotencyKey)) {
				t.Fatalf("provider reference %v", a.ProviderReference)
			}
			charges := d.charges("provider_idempotency_key = ?", a.ProviderIdempotencyKey)
			if len(charges) != tc.charges || tc.charges == 1 && charges[0].Status != tc.chargeStatus {
				t.Fatalf("charges %+v", charges)
			}
			if n := len(d.journals(id)); (n == 1) != tc.journal || n > 1 {
				t.Fatalf("journals %d", n)
			}
			dispatch := d.outbox(id, payment.OutboxPaymentDispatch)
			if len(dispatch) != 1 || dispatch[0].Status != event.StatusDelivered || dispatch[0].Attempts != 1 || dispatch[0].LeaseOwner != nil {
				t.Fatalf("dispatch row %+v", dispatch)
			}
			lookups := d.outbox(id, payment.OutboxPaymentLookup)
			if (len(lookups) == 1) != tc.lookup || len(lookups) > 1 {
				t.Fatalf("lookups %+v", lookups)
			}
			if tc.lookup {
				var command payment.LookupCommand
				if err := json.Unmarshal([]byte(lookups[0].Payload), &command); err != nil || command.AttemptID != a.ID ||
					command.ProviderID != a.ProviderID || command.PaymentID != id || command.WorkspaceID != auth.DemoWorkspaceID ||
					lookups[0].Status != event.StatusPending || lookups[0].DeduplicationKey != payment.LookupDeduplicationKey(a.ID) {
					t.Fatalf("lookup command %+v %+v %v", lookups[0], command, err)
				}
			}
			want := "created>processing>" + tc.payment
			if got := joined(d.statuses(id)); got != want {
				t.Fatalf("audit %s, want %s", got, want)
			}
			d.requireLedger()
		})
	}
	snapshot := d.executor.Metrics().Snapshot()
	if snapshot.AttemptsStarted != int64(len(cases)) || snapshot.UnknownOutcomes != 3 || snapshot.AttemptLatencyCount != 4 ||
		snapshot.OutcomesByProvider[auth.DemoProviderID]["succeeded"] != 3 || snapshot.OutcomesByProvider[auth.DemoProviderID]["ambiguous"] != 3 {
		t.Fatalf("metrics %+v", snapshot)
	}
	if lag := d.dispatcher.Metrics().Snapshot(); lag.Claims != int64(len(cases)) || lag.LagCount != int64(len(cases)) {
		t.Fatalf("dispatcher metrics %+v", lag)
	}
}

func TestUnroutablePaymentFailsWithoutAttempt(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	d.exec("UPDATE routing_rules SET enabled = 0 WHERE workspace_id = ? AND currency = 'EUR'", auth.DemoWorkspaceID)
	id := d.accept(scenarioMethod["success"], "100", "EUR")
	d.drain()
	requireStatus(t, d.payment(id), payment.StatusFailed)
	if n := len(d.attempts(id)); n != 0 {
		t.Fatalf("attempts %d", n)
	}
	if n := d.count("mock_provider_charges", ""); n != 0 {
		t.Fatalf("charges %d", n)
	}
	d.requireLedger()
}

// The provider call runs with no transaction open: a write on the only pooled
// connection succeeds while the mock submit is blocked, and the attempt is
// already durable as submitted.
func TestNoTransactionIsOpenDuringProviderCall(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	d := newDispatchEnv(t, dispatchConfig{sleep: func(ctx context.Context, _ mock.Scenario, _ time.Duration) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	id := d.accept(scenarioMethod["delayed_success"], "777", "USD")
	done := make(chan error, 1)
	go func() { _, err := d.dispatcher.RunOnce(context.Background()); done <- err }()
	<-entered
	writeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := d.store.DB.WithContext(writeCtx).Exec("INSERT INTO workspaces(id,name,environment,created_at) VALUES('w-concurrent','Concurrent','sandbox',1)").Error; err != nil {
		t.Fatalf("concurrent write during provider call: %v", err)
	}
	attempts := d.attempts(id)
	if len(attempts) != 1 || attempts[0].Status != payment.AttemptSubmitted || attempts[0].SubmittedAt == nil || attempts[0].CompletedAt != nil {
		t.Fatalf("attempt must be durable as submitted before the call: %+v", attempts)
	}
	requireStatus(t, d.payment(id), payment.StatusProcessing)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	requireStatus(t, d.payment(id), payment.StatusSucceeded)
	d.requireLedger()
}

// Two dispatchers on separate connections race over the same rows.
func TestRacingDispatchersClaimEachRowOnce(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	second := openTestStore(t, d.path)
	t.Cleanup(func() { _ = second.Close() })
	other := &dispatchEnv{env: &env{t: t, path: d.path, store: second, service: payment.NewService(second.DB, payment.Options{})}}
	other.build(dispatchConfig{db: second.DB})
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = d.accept(scenarioMethod["success"], "500", "USD")
	}
	var wg sync.WaitGroup
	for _, dispatcher := range []*event.Dispatcher{d.dispatcher, other.dispatcher} {
		wg.Add(1)
		go func(dispatcher *event.Dispatcher) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if _, err := dispatcher.RunOnce(context.Background()); err != nil {
					t.Error(err)
					return
				}
			}
		}(dispatcher)
	}
	wg.Wait()
	claims := d.dispatcher.Metrics().Snapshot().Claims + other.dispatcher.Metrics().Snapshot().Claims
	if claims != int64(len(ids)) {
		t.Fatalf("claims %d, want %d", claims, len(ids))
	}
	for _, id := range ids {
		requireStatus(t, d.payment(id), payment.StatusSucceeded)
		if n := len(d.attempts(id)); n != 1 {
			t.Fatalf("payment %s has %d attempts", id, n)
		}
		if rows := d.outbox(id, payment.OutboxPaymentDispatch); rows[0].Attempts != 1 {
			t.Fatalf("dispatch row claimed %d times", rows[0].Attempts)
		}
		if n := len(d.journals(id)); n != 1 {
			t.Fatalf("journals %d", n)
		}
	}
	if n := d.count("mock_provider_charges", ""); n != int64(len(ids)) {
		t.Fatalf("charges %d", n)
	}
	d.requireLedger()
}

// Many claimers racing on one due row: exactly one compare-and-swap wins.
func TestConcurrentClaimOfOneRowHasOneWinner(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	d.accept(scenarioMethod["success"], "500", "USD")
	stores := []*gorm.DB{d.store.DB}
	for i := 0; i < 3; i++ {
		s := openTestStore(t, d.path)
		t.Cleanup(func() { _ = s.Close() })
		stores = append(stores, s.DB)
	}
	var mu sync.Mutex
	var won []event.Claim
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(db *gorm.DB) {
			defer wg.Done()
			claims, err := event.ClaimDue(context.Background(), db, []string{payment.OutboxPaymentDispatch}, time.Now(), time.Minute, 10)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			won = append(won, claims...)
			mu.Unlock()
		}(stores[i%len(stores)])
	}
	wg.Wait()
	if len(won) != 1 || len(won[0].Token) != 64 {
		t.Fatalf("claims %+v", won)
	}
}

// A dispatcher whose lease expired cannot renew or complete after another
// owner reclaims the row, and its late outcome transaction rolls back.
func TestExpiredLeaseCannotCompleteAfterReclaim(t *testing.T) {
	t.Run("claim primitives", func(t *testing.T) {
		d := newDispatchEnv(t, dispatchConfig{})
		d.accept(scenarioMethod["success"], "500", "USD")
		now := time.Now()
		types := []string{payment.OutboxPaymentDispatch}
		first, err := event.ClaimDue(context.Background(), d.store.DB, types, now, 50*time.Millisecond, 1)
		if err != nil || len(first) != 1 {
			t.Fatalf("first claim %v %v", first, err)
		}
		if again, _ := event.ClaimDue(context.Background(), d.store.DB, types, now.Add(10*time.Millisecond), time.Minute, 1); len(again) != 0 {
			t.Fatal("a live lease was reclaimed")
		}
		second, err := event.ClaimDue(context.Background(), d.store.DB, types, now.Add(time.Second), time.Minute, 1)
		if err != nil || len(second) != 1 || !second[0].Reclaimed || second[0].Token == first[0].Token || second[0].Attempts != 2 {
			t.Fatalf("reclaim %+v %v", second, err)
		}
		stale := first[0]
		if err := event.Renew(context.Background(), d.store.DB, &stale, now.Add(time.Second), time.Minute); !errors.Is(err, event.ErrLeaseLost) {
			t.Fatalf("stale renew: %v", err)
		}
		if err := event.Complete(d.store.DB, stale, now.UnixMilli()); !errors.Is(err, event.ErrLeaseLost) {
			t.Fatalf("stale complete: %v", err)
		}
		if err := event.Release(context.Background(), d.store.DB, stale, now, "x"); !errors.Is(err, event.ErrLeaseLost) {
			t.Fatalf("stale release: %v", err)
		}
		live := second[0]
		if err := event.Renew(context.Background(), d.store.DB, &live, now.Add(time.Second), time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := event.Complete(d.store.DB, live, now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("late outcome after reclaim", func(t *testing.T) {
		var d *dispatchEnv
		var reclaimer *payment.Executor
		d = newDispatchEnv(t, dispatchConfig{hooks: payment.ExecutorHooks{
			BeforeRecord: func(ctx context.Context, attempt payment.PreparedAttempt, result provider.Result) error {
				// The first owner's lease expires mid-call; another owner reclaims.
				claims, err := event.ClaimDue(ctx, d.store.DB, []string{payment.OutboxPaymentDispatch}, time.Now().Add(time.Hour), time.Minute, 1)
				if err != nil || len(claims) != 1 {
					t.Errorf("reclaim %v %v", claims, err)
					return nil
				}
				return reclaimer.Handle(ctx, claims[0])
			},
		}})
		reclaimer = payment.NewExecutor(d.store.DB, payment.ExecutorOptions{Providers: d.providers})
		id := d.accept(scenarioMethod["success"], "900", "USD")
		claims, err := event.ClaimDue(context.Background(), d.store.DB, []string{payment.OutboxPaymentDispatch}, time.Now(), time.Second, 1)
		if err != nil || len(claims) != 1 {
			t.Fatal(claims, err)
		}
		if err := d.executor.Handle(context.Background(), claims[0]); !errors.Is(err, event.ErrLeaseLost) {
			t.Fatalf("stale owner recorded its outcome: %v", err)
		}
		// The reclaimer treated the stale submitted attempt as possibly executed.
		requireStatus(t, d.payment(id), payment.StatusUnknown)
		attempts := d.attempts(id)
		if len(attempts) != 1 || attempts[0].Status != payment.AttemptUnknown || attempts[0].ErrorCode == nil || *attempts[0].ErrorCode != "stale_submitted" {
			t.Fatalf("attempts %+v", attempts)
		}
		if n := len(d.journals(id)); n != 0 {
			t.Fatalf("journals %d", n)
		}
		if rows := d.outbox(id, payment.OutboxPaymentLookup); len(rows) != 1 {
			t.Fatalf("lookup rows %d", len(rows))
		}
		if n := len(d.charges("provider_idempotency_key = ?", attempts[0].ProviderIdempotencyKey)); n != 1 {
			t.Fatalf("charges %d", n)
		}
		d.requireLedger()
	})
}

// Adds a second enabled provider ("stripe" code, backed by another mock
// adapter) that holds a token for method and has a lower-priority USD rule.
func addSecondProvider(d *dispatchEnv, method string) (string, *mock.Adapter) {
	second := "00000000-0000-7000-8000-0000000002ff"
	d.exec("INSERT INTO providers(id,workspace_id,code,enabled,created_at) VALUES(?,?,'stripe',1,1)", second, auth.DemoWorkspaceID)
	d.exec("INSERT INTO payment_method_tokens(id,workspace_id,method_id,provider_id,token_reference,created_at) VALUES(?,?,?,?,?,1)",
		"00000000-0000-7000-8000-0000000003ff", auth.DemoWorkspaceID, method, second, "mock_success_second")
	d.exec("INSERT INTO routing_rules(id,workspace_id,provider_id,rail,currency,priority,min_amount_minor,enabled,version,created_at,updated_at) VALUES(?,?,?,'mock','USD',5,1,1,1,1,1)",
		"00000000-0000-7000-8000-0000000007ff", auth.DemoWorkspaceID, second)
	adapter := mock.New(d.store.DB, mock.Options{Sleep: instantSleep})
	return second, adapter
}

// 2.2.11 and the timeout-after-success gate: an unknown payment keeps its one
// attempt and provider key, and no code path submits it to another provider,
// even when the original provider becomes ineligible and the dispatch command
// is redelivered.
func TestUnknownPaymentIsNeverSubmittedToAnotherProvider(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	method := scenarioMethod["timeout_after_success"]
	second, secondAdapter := addSecondProvider(d, method)
	d.build(dispatchConfig{extra: payment.ProviderSet{"stripe": secondAdapter}})
	id := d.accept(method, "4200", "USD")
	d.drain()
	requireStatus(t, d.payment(id), payment.StatusUnknown)
	original := d.attempts(id)
	if len(original) != 1 || original[0].ProviderID != auth.DemoProviderID || original[0].Status != payment.AttemptUnknown {
		t.Fatalf("attempts %+v", original)
	}
	// The original provider becomes ineligible, so routing alone would pick the second.
	d.exec("UPDATE routing_rules SET enabled = 0 WHERE id = ?", auth.RoutingRuleID(0, 0))
	// Redeliver the dispatch command as if it were never acknowledged.
	d.exec("UPDATE outbox_events SET status = 'pending', delivered_at = NULL, available_at = 0 WHERE payment_id = ? AND event_type = ?", id, payment.OutboxPaymentDispatch)
	d.drain()
	if err := d.store.DB.Transaction(func(tx *gorm.DB) error {
		return payment.GuardNewAttempt(tx, auth.DemoWorkspaceID, id, second)
	}); !errors.Is(err, payment.ErrUnsafeFailover) {
		t.Fatalf("guard allowed failover: %v", err)
	}
	attempts := d.attempts(id)
	if len(attempts) != 1 || attempts[0].ID != original[0].ID || attempts[0].ProviderIdempotencyKey != payment.ProviderIdempotencyKey(id, 1) {
		t.Fatalf("attempts changed: %+v", attempts)
	}
	if n := d.count("payment_attempts", "provider_id = ?", second); n != 0 {
		t.Fatalf("%d attempts at the second provider", n)
	}
	if n := d.count("mock_provider_charges", "provider_id = ?", second); n != 0 {
		t.Fatalf("%d charges at the second provider", n)
	}
	if n := d.count("mock_provider_charges", "provider_id = ?", auth.DemoProviderID); n != 1 {
		t.Fatalf("%d charges at the original provider", n)
	}
	requireStatus(t, d.payment(id), payment.StatusUnknown)
	d.requireLedger()
}

// A guard test independent of the dispatcher: any unresolved attempt blocks a
// new one, and a payment that was ever unknown can only be retried at the
// provider it already used.
func TestGuardNewAttempt(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	method := scenarioMethod["success"]
	second, _ := addSecondProvider(d, method)
	id := d.accept(method, "100", "USD")
	guard := func(providerID string) error {
		return d.store.DB.Transaction(func(tx *gorm.DB) error { return payment.GuardNewAttempt(tx, auth.DemoWorkspaceID, id, providerID) })
	}
	if err := guard(second); err != nil {
		t.Fatalf("fresh payment: %v", err)
	}
	now := time.Now().UnixMilli()
	for _, status := range []string{"submitted", "unknown"} {
		d.exec("DELETE FROM payment_attempts WHERE payment_id = ?", id)
		d.exec("INSERT INTO payment_attempts(id,workspace_id,payment_id,provider_id,routing_reason,attempt_number,provider_idempotency_key,status,created_at,updated_at) VALUES(?,?,?,?,?,1,?,?,?,?)",
			"att-"+status, auth.DemoWorkspaceID, id, auth.DemoProviderID, "test", "key-"+status, status, now, now)
		for _, target := range []string{second, auth.DemoProviderID} {
			if err := guard(target); !errors.Is(err, payment.ErrUnsafeFailover) {
				t.Fatalf("%s attempt allowed a new attempt at %s: %v", status, target, err)
			}
		}
	}
	// Resolved failed after having been unknown: only the original provider.
	d.exec("UPDATE payment_attempts SET status = 'failed' WHERE payment_id = ?", id)
	p := d.payment(id)
	d.exec("INSERT INTO payment_state_transitions(id,workspace_id,payment_id,from_status,to_status,reason,actor_reference,created_at) VALUES('tr-unknown',?,?,?,?,?,?,?)",
		auth.DemoWorkspaceID, id, p.Status, "unknown", "test", "test", now)
	if err := guard(second); !errors.Is(err, payment.ErrUnsafeFailover) {
		t.Fatalf("ever-unknown payment allowed another provider: %v", err)
	}
	if err := guard(auth.DemoProviderID); err != nil {
		t.Fatalf("original provider blocked: %v", err)
	}
}

// Crash after the mock executed but before the outcome committed; reclaim moves
// it to the lookup path and a same-key resubmission records one charge.
func TestCrashAfterSubmitNeverChargesTwice(t *testing.T) {
	crash := errors.New("simulated crash before recording")
	d := newDispatchEnv(t, dispatchConfig{hooks: payment.ExecutorHooks{
		BeforeRecord: func(context.Context, payment.PreparedAttempt, provider.Result) error { return crash },
	}})
	id := d.accept(scenarioMethod["success"], "1500", "USD")
	claims, err := event.ClaimDue(context.Background(), d.store.DB, []string{payment.OutboxPaymentDispatch}, time.Now(), time.Second, 1)
	if err != nil || len(claims) != 1 {
		t.Fatal(claims, err)
	}
	if err := d.executor.Handle(context.Background(), claims[0]); !errors.Is(err, crash) {
		t.Fatal(err)
	}
	requireStatus(t, d.payment(id), payment.StatusProcessing)
	key := d.attempts(id)[0].ProviderIdempotencyKey
	// "Restart": a fresh executor reclaims after the lease expires.
	d.build(dispatchConfig{nowFor: func() time.Time { return time.Now().Add(time.Hour) }})
	d.drain()
	requireStatus(t, d.payment(id), payment.StatusUnknown)
	attempts := d.attempts(id)
	if len(attempts) != 1 || attempts[0].ProviderIdempotencyKey != key {
		t.Fatalf("attempts %+v", attempts)
	}
	// Same-key resubmission (the replay contract 2.4 may use) returns the original charge.
	for i := 0; i < 3; i++ {
		result := d.adapter.Submit(context.Background(), provider.SubmitRequest{
			WorkspaceID: auth.DemoWorkspaceID, ProviderID: auth.DemoProviderID, IdempotencyKey: key, AmountMinor: 1500, Currency: "USD",
			TokenReference: "mock_success_" + scenarioMethod["success"],
		})
		if result.Outcome != provider.OutcomeSucceeded || !result.Replayed {
			t.Fatalf("resubmit %+v", result)
		}
	}
	if n := len(d.charges("provider_idempotency_key = ?", key)); n != 1 {
		t.Fatalf("charges %d", n)
	}
	d.requireLedger()
}

// POST /v1/routing/evaluate writes nothing and agrees with a real dispatch.
func TestRoutingEvaluateIsReadOnlyAndMatchesDispatch(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	before := d.dump()
	type decision struct {
		ProviderID   string `json:"provider_id"`
		ProviderCode string `json:"provider_code"`
		RuleID       string `json:"rule_id"`
		Reason       string `json:"reason"`
	}
	evaluate := func(method, amount, currency, key string) (int, decision) {
		response := d.do("POST", "/v1/routing/evaluate", key, "", `{"payment_method_id":"`+method+`","amount_minor":"`+amount+`","currency":"`+currency+`"}`)
		var out decision
		if response.Code == 200 {
			if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return response.Code, out
	}
	decisions := map[string]decision{}
	for scenario, method := range scenarioMethod {
		code, got := evaluate(method, "31337", "GBP", credentials.OperatorKey)
		if code != 200 || got.ProviderID != auth.DemoProviderID || got.ProviderCode != "mock" || got.RuleID != auth.RoutingRuleID(0, 2) || got.Reason == "" {
			t.Fatalf("%s: %d %+v", scenario, code, got)
		}
		decisions[scenario] = got
	}
	if code, _ := evaluate(auth.OtherMethodID, "100", "USD", credentials.OperatorKey); code != 404 {
		t.Fatalf("other workspace method: %d", code)
	}
	if code, _ := evaluate(auth.DemoMethodID, "0", "USD", credentials.OperatorKey); code != 400 {
		t.Fatalf("zero amount: %d", code)
	}
	if code, _ := evaluate(auth.DemoMethodID, "100", "USD", credentials.ReaderKey); code != 403 {
		t.Fatalf("reader: %d", code)
	}
	after := d.dump()
	for table, sum := range before {
		if after[table] != sum {
			t.Fatalf("evaluate changed table %s", table)
		}
	}
	if len(after) != len(before) || len(before) < 15 {
		t.Fatalf("table sets differ or too few tables: %d %d", len(before), len(after))
	}
	id := d.accept(scenarioMethod["success"], "31337", "GBP")
	d.drain()
	attempt := d.attempts(id)[0]
	want := decisions["success"]
	if attempt.ProviderID != want.ProviderID || *attempt.RoutingRuleID != want.RuleID || attempt.RoutingReason != want.Reason {
		t.Fatalf("dispatch %+v differs from evaluate %+v", attempt, want)
	}
	// With no eligible rule both report it: evaluate 422, dispatch fails unrouted.
	d.exec("UPDATE routing_rules SET enabled = 0 WHERE workspace_id = ? AND currency = 'JPY'", auth.DemoWorkspaceID)
	if code, _ := evaluate(auth.DemoMethodID, "100", "JPY", credentials.OperatorKey); code != 422 {
		t.Fatalf("no eligible provider: %d", code)
	}
}

// Graceful stop: claiming stops, a blocked in-flight submit is cancelled after
// the drain bound and recorded as unknown, then the database can close.
func TestDispatcherLifecycleDrainsInFlightWork(t *testing.T) {
	entered := make(chan struct{}, 4)
	d := newDispatchEnv(t, dispatchConfig{drain: 100 * time.Millisecond, poll: 10 * time.Millisecond, sleep: func(ctx context.Context, _ mock.Scenario, _ time.Duration) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}})
	fast := d.accept(scenarioMethod["success"], "100", "USD")
	d.dispatcher.Start()
	deadline := time.Now().Add(5 * time.Second)
	for d.payment(fast).Status != string(payment.StatusSucceeded) {
		if time.Now().After(deadline) {
			t.Fatal("started dispatcher did not process the payment")
		}
		time.Sleep(10 * time.Millisecond)
	}
	blocked := d.accept(scenarioMethod["delayed_success"], "100", "USD")
	d.dispatcher.Notify()
	<-entered
	stopped := time.Now()
	if err := d.dispatcher.Stop(context.Background()); err == nil {
		t.Fatal("Stop should report the drain timeout")
	}
	if time.Since(stopped) > 3*time.Second {
		t.Fatal("drain was not bounded")
	}
	requireStatus(t, d.payment(blocked), payment.StatusUnknown)
	pending := d.accept(scenarioMethod["success"], "100", "USD")
	time.Sleep(50 * time.Millisecond)
	requireStatus(t, d.payment(pending), payment.StatusCreated)
	d.requireLedger()
	if err := d.store.Close(); err != nil {
		t.Fatal(err)
	}
}
