package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/provider"
)

// crashPoint is one failure point of the docs/backend/flows.md recovery
// checklist at which the "process" dies. A crash is simulated by a hook
// returning an error, which aborts the handler with nothing further recorded
// (the same state a SIGKILL leaves); the "restart" builds a fresh executor,
// recovery worker and dispatcher over the same file-backed database.
type crashPoint string

const (
	// crashAfterPrepare: the submitted attempt is durable; the provider was
	// never called, so it has no charge.
	crashAfterPrepare crashPoint = "after_prepare"
	// crashBeforeRecord: the provider executed; the result was not recorded.
	crashBeforeRecord crashPoint = "before_record"
	// crashBeforeResolve: the lookup confirmed the outcome; the resolving
	// transaction never ran.
	crashBeforeResolve crashPoint = "before_resolve"
)

// crashRun is the state after the crash and restart.
type crashRun struct {
	d          *dispatchEnv
	paymentID  string
	key        string
	restartNow func() time.Time
}

// crashAt accepts a payment on the method's scenario, runs the dispatcher until
// the crash point, then restarts: the clock jumps past every lease, the startup
// scan runs, and a fresh dispatcher with the lookup worker settles everything.
func crashAt(t *testing.T, scenario string, point crashPoint) crashRun {
	t.Helper()
	d := newDispatchEnv(t, dispatchConfig{})
	var armed bool
	hooks := payment.ExecutorHooks{}
	switch point {
	case crashAfterPrepare:
		hooks.AfterPrepare = func(context.Context, payment.PreparedAttempt) error { return errCrash }
	case crashBeforeRecord:
		hooks.BeforeRecord = func(context.Context, payment.PreparedAttempt, provider.Result) error { return errCrash }
	case crashBeforeResolve:
		armed = true
	}
	d.build(dispatchConfig{hooks: hooks})
	id := d.accept(scenarioMethod[scenario], "7700", "USD")
	claim := d.claimDispatch(time.Now(), time.Minute)
	err := d.executor.Handle(context.Background(), claim)
	if armed {
		if err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, errCrash) {
		t.Fatalf("expected simulated crash, got %v", err)
	}
	run := crashRun{d: d, paymentID: id, restartNow: func() time.Time { return time.Now().Add(time.Hour) }}
	if atts := d.attempts(id); len(atts) == 1 {
		run.key = atts[0].ProviderIdempotencyKey
	}

	// Restart.
	d.build(dispatchConfig{nowFor: run.restartNow})
	crashed := false
	hook := payment.RecoveryHooks{}
	if point == crashBeforeResolve {
		hook.BeforeResolve = func(context.Context, payment.LookupCommand, provider.Result) error {
			if !crashed {
				crashed = true
				return errCrash
			}
			return nil
		}
	}
	recovery := d.addRecovery(hook, run.restartNow)
	if _, err := recovery.RecoverStale(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A crashed handler's row is released with the dispatcher's own jittered
	// backoff, so keep polling until the payment settles.
	for i := 0; i < 40; i++ {
		d.settle(40)
		if payment.Status(d.payment(id).Status).Terminal() {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if point == crashBeforeResolve && !crashed {
		t.Fatal("the before-resolve crash point was never reached")
	}
	return run
}

// A crash at each point of the checklist resolves to one succeeded payment,
// one journal and at most one charge, under the original key and provider.
func TestCrashInjectionResolvesSuccessAtEveryPoint(t *testing.T) {
	for _, point := range []crashPoint{crashAfterPrepare, crashBeforeRecord, crashBeforeResolve} {
		t.Run(string(point), func(t *testing.T) {
			scenario := "success"
			if point == crashBeforeResolve {
				scenario = "timeout_after_success"
			}
			run := crashAt(t, scenario, point)
			d, id := run.d, run.paymentID
			requireStatus(t, d.payment(id), payment.StatusSucceeded)
			atts := d.attempts(id)
			if len(atts) != 1 || atts[0].ProviderIdempotencyKey != run.key || atts[0].Status != payment.AttemptSucceeded || atts[0].CompletedAt == nil {
				t.Fatalf("attempts %+v (key %s)", atts, run.key)
			}
			if n := len(d.charges("provider_idempotency_key = ?", run.key)); n != 1 {
				t.Fatalf("charges %d, want exactly 1", n)
			}
			if n := len(d.journals(id)); n != 1 {
				t.Fatalf("journals %d", n)
			}
			if n := d.count("payment_attempts", ""); n != 1 {
				t.Fatalf("attempts total %d", n)
			}
			d.requireNoOtherProvider(id)
			d.requireLedger()
			if rows := d.outbox(id, payment.OutboxPaymentLookup); len(rows) != 1 || rows[0].Status != event.StatusDelivered {
				t.Fatalf("lookup rows %+v", rows)
			}
			if rows := d.outbox(id, payment.OutboxPaymentDispatch); len(rows) != 1 || rows[0].Status != event.StatusDelivered {
				t.Fatalf("dispatch rows %+v", rows)
			}
			if got := joined(d.statuses(id)); got != "created>processing>unknown>succeeded" {
				t.Fatalf("audit %s", got)
			}
		})
	}
}

// Crash after the provider declined and before the decline was recorded: the
// lookup finds the recorded decline (proof of no effect) and fails the payment.
func TestCrashAfterProviderDeclineResolvesFailed(t *testing.T) {
	run := crashAt(t, "decline", crashBeforeRecord)
	d, id := run.d, run.paymentID
	requireStatus(t, d.payment(id), payment.StatusFailed)
	atts := d.attempts(id)
	if len(atts) != 1 || atts[0].Status != payment.AttemptFailed || atts[0].ErrorCode == nil || *atts[0].ErrorCode != "declined" {
		t.Fatalf("attempts %+v", atts)
	}
	if n := len(d.journals(id)); n != 0 {
		t.Fatalf("journals %d", n)
	}
	d.requireNoOtherProvider(id)
	d.requireLedger()
}
