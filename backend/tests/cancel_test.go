package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

func (e *env) cancel(id, key string) (int, payment.PaymentView, string) {
	e.t.Helper()
	response := e.do("POST", "/v1/payments/"+id+"/cancel", key, "", "")
	var view payment.PaymentView
	if response.Code == 200 {
		_ = json.Unmarshal(response.Body.Bytes(), &view)
	}
	return response.Code, view, response.Body.String()
}

func TestCancelCreatedPayment(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["success"], "500", "USD")
	code, view, body := d.cancel(id, credentials.OperatorKey)
	if code != 200 || view.Status != "canceled" {
		t.Fatalf("cancel %d %s", code, body)
	}
	// Repeating is a no-op, not an error.
	if code, view, _ := d.cancel(id, credentials.OperatorKey); code != 200 || view.Status != "canceled" {
		t.Fatalf("repeat cancel %d", code)
	}
	d.drain()
	requireStatus(t, d.payment(id), payment.StatusCanceled)
	if n := len(d.attempts(id)); n != 0 {
		t.Fatalf("attempts %d", n)
	}
	if got := joined(d.statuses(id)); got != "created>canceled" {
		t.Fatalf("audit %s", got)
	}
	if rows := d.outbox(id, payment.OutboxPaymentDispatch); len(rows) != 1 || rows[0].Status != event.StatusDelivered {
		t.Fatalf("dispatch rows %+v", rows)
	}
	if n := d.count("mock_provider_charges", ""); n != 0 {
		t.Fatalf("charges %d", n)
	}
	if n := len(d.journals(id)); n != 0 {
		t.Fatalf("journals %d", n)
	}
	d.requireLedger()
}

func TestCancelRejectedOnceProcessingStartedAndOnBadRequests(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	succeeded := d.accept(scenarioMethod["success"], "500", "USD")
	failed := d.accept(scenarioMethod["decline"], "500", "USD")
	unknown := d.accept(scenarioMethod["timeout_after_success"], "500", "USD")
	d.drain()
	for name, id := range map[string]string{"succeeded": succeeded, "failed": failed, "unknown": unknown} {
		before := d.payment(id)
		if code, _, body := d.cancel(id, credentials.OperatorKey); code != 409 {
			t.Fatalf("%s: cancel %d %s", name, code, body)
		}
		after := d.payment(id)
		if after.Status != before.Status || after.Version != before.Version {
			t.Fatalf("%s changed by a rejected cancel", name)
		}
	}
	processing := d.accept(scenarioMethod["success"], "500", "USD")
	d.exec("UPDATE payments SET status = 'processing', version = version + 1 WHERE id = ?", processing)
	if code, _, _ := d.cancel(processing, credentials.OperatorKey); code != 409 {
		t.Fatalf("processing cancel %d", code)
	}
	if code, _, _ := d.cancel("00000000-0000-7000-8000-00000000dead", credentials.OperatorKey); code != 404 {
		t.Fatalf("missing %d", code)
	}
	if code, _, _ := d.cancel(succeeded, credentials.OtherWorkspaceKey); code != 404 {
		t.Fatalf("other workspace %d", code)
	}
	if code, _, _ := d.cancel(succeeded, credentials.ReaderKey); code != 403 {
		t.Fatalf("reader %d", code)
	}
	d.requireLedger()
}

// 100 cancel-vs-dispatch races, with the canceler and the dispatcher on
// separate database connections. Each ends with exactly one winner and a
// payment, attempt and audit history consistent with it.
func TestCancelVersusDispatchRaces(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	canceler, err := storage.Open(context.Background(), d.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = canceler.Close() })
	cancelService := payment.NewService(canceler.DB, payment.Options{})
	const races = 100
	ids := make([]string, races)
	for i := range ids {
		ids[i] = d.accept(scenarioMethod["success"], fmt.Sprintf("%d", 100+i), "USD")
	}
	var canceled [races]bool
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			result, err := cancelService.Cancel(context.Background(), payment.CancelRequest{WorkspaceID: auth.DemoWorkspaceID, PaymentID: ids[i], ActorReference: "test:cancel"})
			switch {
			case err == nil:
				canceled[i] = true
				if result.Payment.Status != string(payment.StatusCanceled) {
					t.Errorf("race %d: cancel returned %s", i, result.Payment.Status)
				}
			case errorsIsNotCancelable(err):
			default:
				t.Errorf("race %d: %v", i, err)
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for round := 0; round < 60; round++ {
			if _, err := d.dispatcher.RunOnce(context.Background()); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	d.drain()
	won := 0
	for i, id := range ids {
		p := d.payment(id)
		dispatch := d.outbox(id, payment.OutboxPaymentDispatch)
		if len(dispatch) != 1 || dispatch[0].Status != event.StatusDelivered {
			t.Fatalf("race %d: dispatch rows %+v", i, dispatch)
		}
		if canceled[i] {
			won++
			if p.Status != string(payment.StatusCanceled) || len(d.attempts(id)) != 0 || joined(d.statuses(id)) != "created>canceled" ||
				len(d.journals(id)) != 0 || d.count("mock_provider_charges", "provider_idempotency_key = ?", payment.ProviderIdempotencyKey(id, 1)) != 0 {
				t.Fatalf("race %d: canceler won but history is inconsistent: %s attempts=%d audit=%s", i, p.Status, len(d.attempts(id)), joined(d.statuses(id)))
			}
			continue
		}
		atts := d.attempts(id)
		if p.Status != string(payment.StatusSucceeded) || len(atts) != 1 || atts[0].Status != payment.AttemptSucceeded ||
			joined(d.statuses(id)) != "created>processing>succeeded" || len(d.journals(id)) != 1 ||
			d.count("mock_provider_charges", "provider_idempotency_key = ?", atts[0].ProviderIdempotencyKey) != 1 {
			t.Fatalf("race %d: dispatch won but history is inconsistent: %s attempts=%+v audit=%s", i, p.Status, atts, joined(d.statuses(id)))
		}
	}
	t.Logf("cancel won %d, dispatch won %d", won, races-won)
	d.requireLedger()
}

func errorsIsNotCancelable(err error) bool {
	for err != nil {
		if err == payment.ErrNotCancelable {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
