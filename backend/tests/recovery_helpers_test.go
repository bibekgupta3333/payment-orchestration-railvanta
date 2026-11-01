package tests

import (
	"context"
	"errors"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/provider"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// addRecovery builds a recovery worker over the env's providers, registers its
// lookup handler on the current dispatcher, and returns it. Backoff is 1ms so
// tests can drive retries quickly.
func (d *dispatchEnv) addRecovery(hooks payment.RecoveryHooks, now func() time.Time) *payment.Recovery {
	r := payment.NewRecovery(d.store.DB, payment.RecoveryOptions{
		Providers: d.providers, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond, Hooks: hooks, Now: now,
	})
	r.Register(d.dispatcher)
	return r
}

// settle runs dispatcher rounds, sleeping briefly so backed-off lookups fall due,
// until a round claims nothing or rounds run out.
func (d *dispatchEnv) settle(rounds int) {
	d.t.Helper()
	for i := 0; i < rounds; i++ {
		n, err := d.dispatcher.RunOnce(context.Background())
		if err != nil {
			d.t.Fatal(err)
		}
		if n == 0 {
			return
		}
		time.Sleep(3 * time.Millisecond)
	}
}

func (e *env) late(paymentID string) []storage.OutboxEvent {
	return e.outbox(paymentID, payment.OutboxPaymentLateOutcome)
}

// requireNoOtherProvider asserts every attempt of the payment ran at the demo
// provider and nothing was charged anywhere else.
func (e *env) requireNoOtherProvider(paymentID string) {
	e.t.Helper()
	if n := e.count("payment_attempts", "payment_id = ? AND provider_id <> ?", paymentID, auth.DemoProviderID); n != 0 {
		e.t.Fatalf("%d attempts at another provider", n)
	}
	if n := e.count("mock_provider_charges", "provider_id <> ?", auth.DemoProviderID); n != 0 {
		e.t.Fatalf("%d charges at another provider", n)
	}
}

// claimDispatch claims the one due dispatch command of a fresh payment.
func (d *dispatchEnv) claimDispatch(now time.Time, lease time.Duration) event.Claim {
	d.t.Helper()
	claims, err := event.ClaimDue(context.Background(), d.store.DB, []string{payment.OutboxPaymentDispatch}, now, lease, 1)
	if err != nil || len(claims) != 1 {
		d.t.Fatal(claims, err)
	}
	return claims[0]
}

var errCrash = errors.New("simulated crash")

// noReplay wraps a provider but declares no idempotent-replay contract and
// counts Submit calls.
type noReplay struct {
	provider.Provider
	submits int
}

func (n *noReplay) Capabilities() provider.Capabilities {
	c := n.Provider.Capabilities()
	c.IdempotentReplay = false
	return c
}

func (n *noReplay) Submit(ctx context.Context, req provider.SubmitRequest) provider.Result {
	n.submits++
	return n.Provider.Submit(ctx, req)
}
