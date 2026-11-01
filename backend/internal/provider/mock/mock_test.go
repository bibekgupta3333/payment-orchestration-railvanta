package mock

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/railvanta/railvanta/backend/internal/provider"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

func openDB(t *testing.T, path string) *storage.Store {
	t.Helper()
	s, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	s := openDB(t, filepath.Join(t.TempDir(), "mock.db"))
	exec(t, s.DB, `INSERT INTO workspaces VALUES ('w1','One','sandbox',0),('w2','Two','sandbox',0);
 INSERT INTO providers VALUES ('prov1','w1','mock',1,0),('prov2','w2','mock',1,0);`)
	return s.DB
}

func exec(t *testing.T, db *gorm.DB, sql string) {
	t.Helper()
	if err := db.Exec(sql).Error; err != nil {
		t.Fatalf("SQL failed: %v\n%s", err, sql)
	}
}

func instant(context.Context, Scenario, time.Duration) error { return nil }

func newAdapter(db *gorm.DB, sleep SleepFunc) *Adapter {
	return New(db, Options{Now: func() int64 { return 1000 }, Sleep: sleep})
}

func request(key string, s Scenario) provider.SubmitRequest {
	return provider.SubmitRequest{WorkspaceID: "w1", ProviderID: "prov1", IdempotencyKey: key, AmountMinor: 25000, Currency: "USD", TokenReference: TokenPrefix(s) + "_method1"}
}

func lookup(key string) provider.LookupRequest {
	return provider.LookupRequest{WorkspaceID: "w1", ProviderID: "prov1", IdempotencyKey: key}
}

func charges(t *testing.T, db *gorm.DB, key string) []Charge {
	t.Helper()
	var out []Charge
	if err := db.Where("provider_idempotency_key = ?", key).Find(&out).Error; err != nil {
		t.Fatal(err)
	}
	return out
}

func TestScenarioForToken(t *testing.T) {
	cases := map[string]Scenario{
		"mock_success":                         ScenarioSuccess,
		"mock_success_m1":                      ScenarioSuccess,
		"mock_decline_m1":                      ScenarioDecline,
		"mock_delayed_success_m1":              ScenarioDelayedSuccess,
		"mock_timeout_before_execution":        ScenarioTimeoutBeforeExecution,
		"mock_timeout_after_success_m1":        ScenarioTimeoutAfterSuccess,
		"mock_outage_x":                        ScenarioOutage,
		"mock_reference_00000000-0000-7000":    ScenarioSuccess,
		"mock_declined":                        ScenarioSuccess, // prefix must end at "_" or end of string
		"":                                     ScenarioSuccess,
		"mock_timeout_after_success_decline_x": ScenarioTimeoutAfterSuccess,
	}
	for token, want := range cases {
		if got := ScenarioForToken(token); got != want {
			t.Errorf("ScenarioForToken(%q) = %s, want %s", token, got, want)
		}
	}
}

func TestScenarios(t *testing.T) {
	cases := []struct {
		scenario   Scenario
		outcome    provider.Outcome
		code       string
		rowStatus  string // "" means no row recorded
		lookupWant provider.Outcome
	}{
		{ScenarioSuccess, provider.OutcomeSucceeded, "", StatusSucceeded, provider.OutcomeSucceeded},
		{ScenarioDecline, provider.OutcomeDeclined, provider.CodeDeclined, StatusDeclined, provider.OutcomeDeclined},
		{ScenarioDelayedSuccess, provider.OutcomeSucceeded, "", StatusSucceeded, provider.OutcomeSucceeded},
		{ScenarioTimeoutBeforeExecution, provider.OutcomeAmbiguous, provider.CodeTimeout, "", ""},
		{ScenarioTimeoutAfterSuccess, provider.OutcomeAmbiguous, provider.CodeTimeout, StatusSucceeded, provider.OutcomeSucceeded},
		{ScenarioOutage, provider.OutcomeAmbiguous, provider.CodeProviderUnavailable, "", ""},
	}
	db := testDB(t)
	a := newAdapter(db, instant)
	ctx := context.Background()
	for _, tc := range cases {
		t.Run(string(tc.scenario), func(t *testing.T) {
			key := "key-" + string(tc.scenario)
			res := a.Submit(ctx, request(key, tc.scenario))
			if res.Outcome != tc.outcome || res.Code != tc.code {
				t.Fatalf("submit = %+v, want %s/%s", res, tc.outcome, tc.code)
			}
			if res.Outcome == provider.OutcomeSucceeded && res.ProviderReference != ProviderReference("prov1", key) {
				t.Fatalf("reference %q", res.ProviderReference)
			}
			rows := charges(t, db, key)
			if tc.rowStatus == "" {
				if len(rows) != 0 {
					t.Fatalf("expected no charge, got %+v", rows)
				}
			} else if len(rows) != 1 || rows[0].Status != tc.rowStatus || rows[0].Scenario != string(tc.scenario) || rows[0].AmountMinor != 25000 {
				t.Fatalf("rows %+v", rows)
			}
			found, err := a.Lookup(ctx, lookup(key))
			if err != nil {
				t.Fatal(err)
			}
			if tc.lookupWant == "" {
				if found.Found {
					t.Fatalf("lookup should not find %s: %+v", key, found)
				}
				return
			}
			if !found.Found || found.Result.Outcome != tc.lookupWant {
				t.Fatalf("lookup %+v", found)
			}
		})
	}
}

func TestResubmitReturnsOriginalCharge(t *testing.T) {
	for _, s := range []Scenario{ScenarioSuccess, ScenarioDecline, ScenarioDelayedSuccess, ScenarioTimeoutAfterSuccess} {
		t.Run(string(s), func(t *testing.T) {
			db := testDB(t)
			executions := 0
			a := newAdapter(db, func(context.Context, Scenario, time.Duration) error { executions++; return nil })
			key := "resubmit-" + string(s)
			first := a.Submit(context.Background(), request(key, s))
			pauses := executions
			for i := 0; i < 3; i++ {
				// A changed token must not change the recorded outcome.
				req := request(key, ScenarioDecline)
				if s == ScenarioDecline {
					req = request(key, ScenarioSuccess)
				}
				again := a.Submit(context.Background(), req)
				if !again.Replayed || again.ProviderReference != ProviderReference("prov1", key) {
					t.Fatalf("replay %+v", again)
				}
				if first.Outcome != provider.OutcomeAmbiguous && again.Outcome != first.Outcome {
					t.Fatalf("replay outcome %s, first %s", again.Outcome, first.Outcome)
				}
				if s == ScenarioTimeoutAfterSuccess && again.Outcome != provider.OutcomeSucceeded {
					t.Fatalf("timeout-after-success replay must reveal success: %+v", again)
				}
			}
			if executions != pauses {
				t.Fatalf("replay re-executed the scenario (%d pauses, want %d)", executions, pauses)
			}
			if rows := charges(t, db, key); len(rows) != 1 {
				t.Fatalf("want exactly one charge, got %d", len(rows))
			}
		})
	}
}

func TestConcurrentSubmitsRecordOneCharge(t *testing.T) {
	db := testDB(t)
	a := newAdapter(db, instant)
	var wg sync.WaitGroup
	results := make([]provider.Result, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = a.Submit(context.Background(), request("race", ScenarioSuccess))
		}(i)
	}
	wg.Wait()
	replays := 0
	for _, r := range results {
		if r.Outcome != provider.OutcomeSucceeded {
			t.Fatalf("result %+v", r)
		}
		if r.Replayed {
			replays++
		}
	}
	if replays != len(results)-1 {
		t.Fatalf("want %d replays, got %d", len(results)-1, replays)
	}
	if rows := charges(t, db, "race"); len(rows) != 1 {
		t.Fatalf("want one charge, got %d", len(rows))
	}
}

func TestBlockedDelayedSubmitHoldsNoTransaction(t *testing.T) {
	db := testDB(t)
	entered, release := make(chan struct{}), make(chan struct{})
	a := newAdapter(db, func(ctx context.Context, _ Scenario, _ time.Duration) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	done := make(chan provider.Result, 1)
	go func() { done <- a.Submit(context.Background(), request("blocked", ScenarioDelayedSuccess)) }()
	<-entered
	// The pool has one connection: this write would block if Submit held a transaction.
	writeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.WithContext(writeCtx).Exec("INSERT INTO workspaces VALUES ('w3','Three','sandbox',0)").Error; err != nil {
		t.Fatalf("concurrent write during blocked submit: %v", err)
	}
	if rows := charges(t, db, "blocked"); len(rows) != 1 || rows[0].Status != StatusProcessing {
		t.Fatalf("charge must be durable as processing before the pause: %+v", rows)
	}
	close(release)
	if res := <-done; res.Outcome != provider.OutcomeSucceeded || res.Replayed {
		t.Fatalf("result %+v", res)
	}
	if rows := charges(t, db, "blocked"); len(rows) != 1 || rows[0].Status != StatusSucceeded {
		t.Fatalf("rows %+v", rows)
	}
}

func TestDeadlineDuringDelayedSuccessIsAmbiguousThenSettles(t *testing.T) {
	db := testDB(t)
	a := New(db, Options{DelayedSuccessDelay: time.Hour}) // real sleep, must honour ctx
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	res := a.Submit(ctx, request("deadline", ScenarioDelayedSuccess))
	if res.Outcome != provider.OutcomeAmbiguous || res.Code != provider.CodeDeadlineExceeded {
		t.Fatalf("result %+v", res)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("submit ignored the context deadline")
	}
	// Simulates a restart: a fresh adapter on the same database resolves the key.
	b := newAdapter(db, instant)
	found, err := b.Lookup(context.Background(), lookup("deadline"))
	if err != nil || !found.Found || found.Result.Outcome != provider.OutcomeSucceeded {
		t.Fatalf("lookup %+v %v", found, err)
	}
	if again := b.Submit(context.Background(), request("deadline", ScenarioDelayedSuccess)); !again.Replayed || again.Outcome != provider.OutcomeSucceeded {
		t.Fatalf("replay %+v", again)
	}
	if rows := charges(t, db, "deadline"); len(rows) != 1 {
		t.Fatalf("want one charge, got %d", len(rows))
	}
}

func TestTimeoutBeforeExecutionHonoursDeadline(t *testing.T) {
	db := testDB(t)
	a := New(db, Options{TimeoutDelay: time.Hour})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res := a.Submit(ctx, request("tbe", ScenarioTimeoutBeforeExecution))
	if res.Outcome != provider.OutcomeAmbiguous || !errors.Is(res.Cause, context.DeadlineExceeded) {
		t.Fatalf("result %+v", res)
	}
	if found, err := a.Lookup(context.Background(), lookup("tbe")); err != nil || found.Found {
		t.Fatalf("lookup %+v %v", found, err)
	}
}

func TestCancelledContextBeforeSubmitRecordsNothing(t *testing.T) {
	db := testDB(t)
	a := newAdapter(db, instant)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if res := a.Submit(ctx, request("cancelled", ScenarioSuccess)); res.Outcome != provider.OutcomeAmbiguous {
		t.Fatalf("result %+v", res)
	}
	if rows := charges(t, db, "cancelled"); len(rows) != 0 {
		t.Fatalf("rows %+v", rows)
	}
}

func TestUnavailableIsDistinctFromNotFound(t *testing.T) {
	db := testDB(t)
	a := newAdapter(db, instant)
	a.Submit(context.Background(), request("before-outage", ScenarioSuccess))
	a.SetAvailable(false)
	if res := a.Submit(context.Background(), request("during-outage", ScenarioSuccess)); res.Outcome != provider.OutcomeAmbiguous || res.Code != provider.CodeProviderUnavailable {
		t.Fatalf("result %+v", res)
	}
	if _, err := a.Lookup(context.Background(), lookup("before-outage")); !errors.Is(err, provider.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	a.SetAvailable(true)
	if found, err := a.Lookup(context.Background(), lookup("during-outage")); err != nil || found.Found {
		t.Fatalf("outage submit must leave no record: %+v %v", found, err)
	}
	if found, err := a.Lookup(context.Background(), lookup("before-outage")); err != nil || !found.Found {
		t.Fatalf("lookup %+v %v", found, err)
	}
}

func TestKeyReuseWithDifferentParametersIsAmbiguous(t *testing.T) {
	db := testDB(t)
	a := newAdapter(db, instant)
	a.Submit(context.Background(), request("reuse", ScenarioSuccess))
	changed := request("reuse", ScenarioSuccess)
	changed.AmountMinor = 1
	if res := a.Submit(context.Background(), changed); res.Outcome != provider.OutcomeAmbiguous || res.Code != provider.CodeIdempotencyConflict {
		t.Fatalf("result %+v", res)
	}
	if rows := charges(t, db, "reuse"); len(rows) != 1 || rows[0].AmountMinor != 25000 {
		t.Fatalf("rows %+v", rows)
	}
}

func TestInvalidRequestIsDeclinedWithoutRecord(t *testing.T) {
	db := testDB(t)
	a := newAdapter(db, instant)
	for name, mutate := range map[string]func(*provider.SubmitRequest){
		"zero amount":  func(r *provider.SubmitRequest) { r.AmountMinor = 0 },
		"bad currency": func(r *provider.SubmitRequest) { r.Currency = "usd" },
		"no key":       func(r *provider.SubmitRequest) { r.IdempotencyKey = "" },
		"no token":     func(r *provider.SubmitRequest) { r.TokenReference = "" },
	} {
		req := request("invalid", ScenarioSuccess)
		mutate(&req)
		res := a.Submit(context.Background(), req)
		if res.Outcome != provider.OutcomeDeclined || res.Code != provider.CodeInvalidRequest || !errors.Is(res.Cause, provider.ErrInvalidRequest) {
			t.Fatalf("%s: %+v", name, res)
		}
	}
	var n int64
	db.Model(&Charge{}).Count(&n)
	if n != 0 {
		t.Fatalf("invalid requests recorded %d charges", n)
	}
}

func TestLookupIsWorkspaceScoped(t *testing.T) {
	db := testDB(t)
	a := newAdapter(db, instant)
	a.Submit(context.Background(), request("scoped", ScenarioSuccess))
	other := lookup("scoped")
	other.WorkspaceID = "w2"
	if found, err := a.Lookup(context.Background(), other); err != nil || found.Found {
		t.Fatalf("cross-workspace lookup %+v %v", found, err)
	}
}

func TestChargesSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	first, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, first.DB, `INSERT INTO workspaces VALUES ('w1','One','sandbox',0); INSERT INTO providers VALUES ('prov1','w1','mock',1,0);`)
	if res := newAdapter(first.DB, instant).Submit(context.Background(), request("durable", ScenarioTimeoutAfterSuccess)); res.Outcome != provider.OutcomeAmbiguous {
		t.Fatalf("result %+v", res)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := openDB(t, path)
	b := newAdapter(second.DB, instant)
	found, err := b.Lookup(context.Background(), lookup("durable"))
	if err != nil || !found.Found || found.Result.Outcome != provider.OutcomeSucceeded {
		t.Fatalf("lookup after restart %+v %v", found, err)
	}
	if res := b.Submit(context.Background(), request("durable", ScenarioTimeoutAfterSuccess)); !res.Replayed || res.Outcome != provider.OutcomeSucceeded {
		t.Fatalf("replay after restart %+v", res)
	}
	if rows := charges(t, second.DB, "durable"); len(rows) != 1 {
		t.Fatalf("want one charge, got %d", len(rows))
	}
}

func TestCapabilities(t *testing.T) {
	c := New(nil, Options{}).Capabilities()
	if c.Code != "mock" || !c.IdempotentReplay || !c.LookupByIdempotencyKey {
		t.Fatalf("capabilities %+v", c)
	}
}
