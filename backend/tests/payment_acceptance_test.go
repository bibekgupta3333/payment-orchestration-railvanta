package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Gate: 50 concurrent same-key requests yield one payment, one idempotency
// record, one outbox command, one transition and identical 202 bodies.
func TestConcurrentSameKeyCreatesExactlyOnePayment(t *testing.T) {
	e := newEnv(t, payment.Options{})
	server := httptest.NewServer(e.handler)
	defer server.Close()
	body := intent("25000", "USD", auth.DemoMethodID)
	const n = 50
	type reply struct {
		status   int
		body     []byte
		location string
	}
	replies := make([]reply, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			request, _ := http.NewRequest("POST", server.URL+"/v1/payments", strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+credentials.OperatorKey)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "concurrent-50")
			response, err := server.Client().Do(request)
			if err != nil {
				t.Error(err)
				return
			}
			defer response.Body.Close()
			data, _ := io.ReadAll(response.Body)
			replies[i] = reply{response.StatusCode, data, response.Header.Get("Location")}
		}()
	}
	close(start)
	wg.Wait()
	for i, r := range replies {
		if r.status != 202 {
			t.Fatalf("request %d: %d %s", i, r.status, r.body)
		}
		if !bytes.Equal(r.body, replies[0].body) || r.location != replies[0].location {
			t.Fatalf("request %d body differs:\n%s\n%s", i, r.body, replies[0].body)
		}
	}
	e.requireCounts(1)
	var outbox storage.OutboxEvent
	if err := e.store.DB.Take(&outbox).Error; err != nil {
		t.Fatal(err)
	}
	view := decodePayment(t, replies[0].body)
	var command payment.DispatchCommand
	if err := json.Unmarshal([]byte(outbox.Payload), &command); err != nil || command.PaymentID != view.ID || command.WorkspaceID != auth.DemoWorkspaceID {
		t.Fatalf("outbox payload %s %v", outbox.Payload, err)
	}
	if outbox.EventType != payment.OutboxPaymentDispatch || outbox.Status != "pending" || outbox.DeduplicationKey != payment.DispatchDeduplicationKey(view.ID) {
		t.Fatalf("outbox row %+v", outbox)
	}
	if replies[0].location != "/v1/payments/"+view.ID {
		t.Fatalf("location %q", replies[0].location)
	}
	snapshot := e.metrics.Snapshot()
	if snapshot.Accepted != 1 || snapshot.Replayed != n-1 || snapshot.AcceptanceLatencyCount != n {
		t.Fatalf("metrics %+v", snapshot)
	}
}

// Gate: a changed amount, currency, method or reference returns 409 and the
// original payment is unchanged.
func TestChangedRequestWithSameKeyConflicts(t *testing.T) {
	e := newEnv(t, payment.Options{})
	const secondMethod = "00000000-0000-7000-8000-000000000111"
	e.addMethod(secondMethod, true)
	original := e.create("conflict-key", intent("1000", "USD", auth.DemoMethodID))
	if original.Code != 202 {
		t.Fatalf("create %d %s", original.Code, original.Body)
	}
	var before storage.Payment
	if err := e.store.DB.Take(&before).Error; err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"amount":    intent("1001", "USD", auth.DemoMethodID),
		"currency":  intent("1000", "EUR", auth.DemoMethodID),
		"method":    intent("1000", "USD", secondMethod),
		"reference": `{"amount_minor":"1000","currency":"USD","payment_method_id":"` + auth.DemoMethodID + `","merchant_reference":"x"}`,
		"foreign":   intent("1000", "USD", auth.OtherMethodID),
	} {
		response := e.create("conflict-key", body)
		if response.Code != 409 || !strings.Contains(response.Body.String(), "idempotency_conflict") {
			t.Errorf("%s: %d %s", name, response.Code, response.Body)
		}
	}
	var after storage.Payment
	if err := e.store.DB.Take(&after).Error; err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("original payment changed:\n%+v\n%+v", before, after)
	}
	e.requireCounts(1)
	// Field order and whitespace are not part of the canonical hash.
	reordered := e.create("conflict-key", `{ "payment_method_id":"`+auth.DemoMethodID+`", "currency":"USD", "amount_minor":"1000" }`)
	if reordered.Code != 202 || !bytes.Equal(reordered.Body.Bytes(), original.Body.Bytes()) || reordered.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("reordered replay %d %s", reordered.Code, reordered.Body)
	}
	if e.metrics.Snapshot().Conflicts != 5 {
		t.Fatalf("conflict metric %+v", e.metrics.Snapshot())
	}
}

// Gate: restarting the process between requests still replays the stored response.
func TestReplayAfterReopeningDatabase(t *testing.T) {
	e := newEnv(t, payment.Options{})
	body := intent("4200", "JPY", auth.DemoMethodID)
	first := e.create("restart-key", body)
	if first.Code != 202 {
		t.Fatalf("create %d %s", first.Code, first.Body)
	}
	e.reopen()
	replayed := e.create("restart-key", body)
	if replayed.Code != 202 || !bytes.Equal(first.Body.Bytes(), replayed.Body.Bytes()) {
		t.Fatalf("replay after reopen %d\n%s\n%s", replayed.Code, first.Body, replayed.Body)
	}
	if replayed.Header().Get("Location") != first.Header().Get("Location") || replayed.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay headers %v", replayed.Header())
	}
	if response := e.create("restart-key", intent("4201", "JPY", auth.DemoMethodID)); response.Code != 409 {
		t.Fatalf("conflict after reopen %d", response.Code)
	}
	e.requireCounts(1)
}

// Gate: int64 max round-trips exactly; out-of-range and non-canonical amounts
// return 400 and create nothing.
func TestAmountBoundaries(t *testing.T) {
	e := newEnv(t, payment.Options{})
	response := e.create("max-amount", intent("9223372036854775807", "KWD", auth.DemoMethodID))
	if response.Code != 202 {
		t.Fatalf("max %d %s", response.Code, response.Body)
	}
	view := decodePayment(t, response.Body.Bytes())
	if view.AmountMinor != "9223372036854775807" || !strings.Contains(response.Body.String(), `"amount_minor":"9223372036854775807"`) {
		t.Fatalf("accept body %s", response.Body)
	}
	read := e.do("GET", "/v1/payments/"+view.ID, credentials.OperatorKey, "", "")
	if read.Code != 200 || decodePayment(t, read.Body.Bytes()).AmountMinor != "9223372036854775807" {
		t.Fatalf("read %d %s", read.Code, read.Body)
	}
	var stored int64
	if err := e.store.DB.Raw("SELECT amount_minor FROM payments WHERE id = ?", view.ID).Scan(&stored).Error; err != nil || stored != 9223372036854775807 {
		t.Fatalf("stored %d %v", stored, err)
	}
	before := e.counts()
	for i, amount := range []string{`"9223372036854775808"`, `"0"`, `"-1"`, `"1.5"`, `"+1"`, `" 1"`, `""`, `"01"`, `1`, `null`} {
		body := `{"amount_minor":` + amount + `,"currency":"USD","payment_method_id":"` + auth.DemoMethodID + `"}`
		response := e.create("bad-amount-"+strconv.Itoa(i), body)
		if response.Code != 400 {
			t.Errorf("amount %s: %d %s", amount, response.Code, response.Body)
		}
	}
	if response := e.create("bad-currency", intent("1", "usd", auth.DemoMethodID)); response.Code != 400 || !strings.Contains(response.Body.String(), `"field":"currency"`) {
		t.Errorf("currency %d %s", response.Code, response.Body)
	}
	if after := e.counts(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("validation failures wrote rows: %v -> %v", before, after)
	}
}

// Gate: another workspace's payment or method ID returns 404 and writes nothing.
func TestCrossWorkspaceAccessReturnsNotFound(t *testing.T) {
	e := newEnv(t, payment.Options{})
	foreign := e.do("POST", "/v1/payments", credentials.OtherWorkspaceKey, "shared-key", intent("700", "USD", auth.OtherMethodID))
	if foreign.Code != 202 {
		t.Fatalf("other workspace create %d %s", foreign.Code, foreign.Body)
	}
	foreignID := decodePayment(t, foreign.Body.Bytes()).ID
	before := e.counts()
	for _, path := range []string{"/v1/payments/" + foreignID, "/v1/payments/" + foreignID + "/attempts"} {
		if response := e.do("GET", path, credentials.OperatorKey, "", ""); response.Code != 404 || strings.Contains(response.Body.String(), foreignID) {
			t.Errorf("%s leaked: %d %s", path, response.Code, response.Body)
		}
	}
	list := e.do("GET", "/v1/payments", credentials.OperatorKey, "", "")
	if list.Code != 200 || strings.Contains(list.Body.String(), foreignID) {
		t.Fatalf("list leaked %s", list.Body)
	}
	if response := e.create("foreign-method", intent("1", "USD", auth.OtherMethodID)); response.Code != 404 {
		t.Fatalf("foreign method %d %s", response.Code, response.Body)
	}
	if after := e.counts(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("cross-workspace request wrote rows: %v -> %v", before, after)
	}
	// Keys are workspace scoped: the same key in the demo workspace is independent.
	if response := e.create("shared-key", intent("700", "USD", auth.DemoMethodID)); response.Code != 202 || decodePayment(t, response.Body.Bytes()).ID == foreignID {
		t.Fatalf("workspace-scoped key %d %s", response.Code, response.Body)
	}
}

// Gate: a failure after any write rolls back the whole acceptance transaction.
func TestInjectedFailureRollsBackAcceptance(t *testing.T) {
	for _, stage := range []payment.AcceptStage{payment.StageAfterReservation, payment.StageAfterPaymentInsert, payment.StageAfterTransition, payment.StageAfterOutbox, payment.StageBeforeCommit} {
		t.Run(string(stage), func(t *testing.T) {
			injected := errors.New("injected failure")
			var armed sync.Mutex
			failing := true
			e := newEnv(t, payment.Options{FaultInjector: func(at payment.AcceptStage) error {
				armed.Lock()
				defer armed.Unlock()
				if failing && at == stage {
					return injected
				}
				return nil
			}})
			response := e.create("fault-key", intent("100", "USD", auth.DemoMethodID))
			if response.Code != 500 {
				t.Fatalf("fault %d %s", response.Code, response.Body)
			}
			e.requireCounts(0)
			_, err := e.service.Accept(context.Background(), payment.AcceptRequest{WorkspaceID: auth.DemoWorkspaceID, ActorReference: "test", IdempotencyKey: "fault-key-2", AmountMinor: 1, Currency: "USD", PaymentMethodID: auth.DemoMethodID})
			if !errors.Is(err, injected) {
				t.Fatalf("service error %v", err)
			}
			e.requireCounts(0)
			armed.Lock()
			failing = false
			armed.Unlock()
			// The key was not burnt: a retry with the same key now succeeds.
			if response := e.create("fault-key", intent("100", "USD", auth.DemoMethodID)); response.Code != 202 || response.Header().Get("Idempotent-Replayed") != "" {
				t.Fatalf("retry %d %s", response.Code, response.Body)
			}
			e.requireCounts(1)
		})
	}
}

// Gate: every created payment has exactly one initial audit transition.
func TestEveryPaymentHasExactlyOneInitialTransition(t *testing.T) {
	e := newEnv(t, payment.Options{})
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := "distinct-" + strconv.Itoa(i%10) // 10 keys, each sent twice
			request := httptest.NewRequest("POST", "/v1/payments", strings.NewReader(intent(strconv.Itoa(100+i%10), "USD", auth.DemoMethodID)))
			request.Header.Set("Authorization", "Bearer "+credentials.OperatorKey)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", key)
			response := httptest.NewRecorder()
			e.handler.ServeHTTP(response, request)
			if response.Code != 202 {
				t.Errorf("%s: %d %s", key, response.Code, response.Body)
			}
		}()
	}
	wg.Wait()
	var payments, bad int64
	e.store.DB.Table("payments").Count(&payments)
	if payments != 10 {
		t.Fatalf("payments %d", payments)
	}
	err := e.store.DB.Raw(`SELECT count(*) FROM payments p WHERE
		(SELECT count(*) FROM payment_state_transitions t WHERE t.payment_id = p.id AND t.workspace_id = p.workspace_id) <> 1
		OR NOT EXISTS (SELECT 1 FROM payment_state_transitions t WHERE t.payment_id = p.id AND t.from_status IS NULL AND t.to_status = 'created')`).Scan(&bad).Error
	if err != nil || bad != 0 {
		t.Fatalf("%d payments lack exactly one initial transition (%v)", bad, err)
	}
	var outbox int64
	e.store.DB.Table("outbox_events").Where("event_type = ?", payment.OutboxPaymentDispatch).Count(&outbox)
	if outbox != 10 {
		t.Fatalf("outbox %d", outbox)
	}
}

func TestMethodWithoutProviderTokenIsRejected(t *testing.T) {
	e := newEnv(t, payment.Options{})
	const untokenized = "00000000-0000-7000-8000-000000000112"
	e.addMethod(untokenized, false)
	response := e.create("untokenized", intent("1", "USD", untokenized))
	if response.Code != 422 || !strings.Contains(response.Body.String(), "payment_method_unusable") {
		t.Fatalf("untokenized %d %s", response.Code, response.Body)
	}
	if response := e.create("missing-method", intent("1", "USD", "00000000-0000-7000-8000-000000000999")); response.Code != 404 {
		t.Fatalf("missing method %d", response.Code)
	}
	e.requireCounts(0)
}

func TestAuthAndHeaderFailuresCreateNothing(t *testing.T) {
	e := newEnv(t, payment.Options{})
	body := intent("1", "USD", auth.DemoMethodID)
	cases := []struct {
		name, apiKey, key, body string
		status                  int
	}{
		{"no credentials", "", "k1", body, 401},
		{"reader role", credentials.ReaderKey, "k2", body, 403},
		{"missing key", credentials.OperatorKey, "", body, 400},
		{"long key", credentials.OperatorKey, strings.Repeat("k", 256), body, 400},
		{"unknown field", credentials.OperatorKey, "k3", `{"amount_minor":"1","currency":"USD","payment_method_id":"` + auth.DemoMethodID + `","workspace_id":"` + auth.OtherWorkspaceID + `"}`, 400},
		{"two values", credentials.OperatorKey, "k4", body + body, 400},
	}
	for _, c := range cases {
		if response := e.do("POST", "/v1/payments", c.apiKey, c.key, c.body); response.Code != c.status {
			t.Errorf("%s: %d %s", c.name, response.Code, response.Body)
		}
	}
	e.requireCounts(0)
	if response := e.create(strings.Repeat("k", 255), body); response.Code != 202 {
		t.Fatalf("255-char key %d", response.Code)
	}
}

func TestListPaginationAndStatusFilter(t *testing.T) {
	e := newEnv(t, payment.Options{})
	created := map[string]bool{}
	for i := range 5 {
		response := e.create("page-"+strconv.Itoa(i), intent(strconv.Itoa(i+1), "USD", auth.DemoMethodID))
		if response.Code != 202 {
			t.Fatal(response.Body.String())
		}
		created[decodePayment(t, response.Body.Bytes()).ID] = true
	}
	type page struct {
		Items      []payment.PaymentView `json:"items"`
		NextCursor *string               `json:"next_cursor"`
	}
	seen := map[string]bool{}
	path := "/v1/payments?limit=2"
	for pages := 0; ; pages++ {
		response := e.do("GET", path, credentials.OperatorKey, "", "")
		var p page
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &p) != nil {
			t.Fatalf("page %d %s", response.Code, response.Body)
		}
		for _, item := range p.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate %s", item.ID)
			}
			seen[item.ID] = true
		}
		if p.NextCursor == nil {
			if pages != 2 {
				t.Fatalf("expected 3 pages, got %d", pages+1)
			}
			break
		}
		path = "/v1/payments?limit=2&cursor=" + *p.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("saw %d of %d", len(seen), len(created))
	}
	for status, want := range map[string]int{"created": 5, "succeeded": 0} {
		var p page
		response := e.do("GET", "/v1/payments?status="+status, credentials.OperatorKey, "", "")
		if json.Unmarshal(response.Body.Bytes(), &p) != nil || len(p.Items) != want {
			t.Errorf("status %s: %s", status, response.Body)
		}
	}
	for _, query := range []string{"status=settled", "limit=0", "limit=101", "limit=x", "cursor=%25%25"} {
		if response := e.do("GET", "/v1/payments?"+query, credentials.OperatorKey, "", ""); response.Code != 400 {
			t.Errorf("%s: %d", query, response.Code)
		}
	}
	if response := e.do("GET", "/v1/payments?limit=1", credentials.ReaderKey, "", ""); response.Code != 200 {
		t.Fatalf("reader list %d", response.Code)
	}
	var some string
	for id := range created {
		some = id
		break
	}
	attempts := e.do("GET", "/v1/payments/"+some+"/attempts", credentials.OperatorKey, "", "")
	if attempts.Code != 200 || attempts.Body.String() != "{\"items\":[],\"next_cursor\":null}\n" {
		t.Fatalf("attempts %d %s", attempts.Code, attempts.Body)
	}
}

func TestApplyTransitionIsGuardedAndAudited(t *testing.T) {
	e := newEnv(t, payment.Options{})
	result, err := e.service.Accept(context.Background(), payment.AcceptRequest{WorkspaceID: auth.DemoWorkspaceID, ActorReference: "test", IdempotencyKey: "transition", AmountMinor: 5, Currency: "USD", PaymentMethodID: auth.DemoMethodID})
	if err != nil {
		t.Fatal(err)
	}
	apply := func(from, to payment.Status, version int64) error {
		return e.store.WithTx(context.Background(), func(tx *gorm.DB) error {
			return payment.ApplyTransition(tx, payment.Transition{WorkspaceID: auth.DemoWorkspaceID, PaymentID: result.PaymentID, From: from, To: to, Version: version, Reason: "test", ActorReference: "test", Now: time.Now().UnixMilli()})
		})
	}
	if err := apply(payment.StatusCreated, payment.StatusSucceeded, 1); !errors.Is(err, payment.ErrIllegalTransition) {
		t.Fatalf("illegal transition %v", err)
	}
	if err := apply(payment.StatusCreated, payment.StatusProcessing, 1); err != nil {
		t.Fatal(err)
	}
	if err := apply(payment.StatusCreated, payment.StatusProcessing, 1); !errors.Is(err, storage.ErrStaleVersion) {
		t.Fatalf("stale CAS %v", err)
	}
	cross := e.store.WithTx(context.Background(), func(tx *gorm.DB) error {
		return payment.ApplyTransition(tx, payment.Transition{WorkspaceID: auth.OtherWorkspaceID, PaymentID: result.PaymentID, From: payment.StatusProcessing, To: payment.StatusUnknown, Version: 2, Reason: "test", ActorReference: "test", Now: 1})
	})
	if !errors.Is(cross, storage.ErrStaleVersion) {
		t.Fatalf("cross-workspace transition %v", cross)
	}
	transitions, err := storage.ListStateTransitions(e.store.DB, auth.DemoWorkspaceID, result.PaymentID)
	if err != nil || len(transitions) != 2 || transitions[0].FromStatus != nil || *transitions[1].FromStatus != "created" || transitions[1].ToStatus != "processing" {
		t.Fatalf("audit %+v %v", transitions, err)
	}
	read := e.do("GET", "/v1/payments/"+result.PaymentID, credentials.OperatorKey, "", "")
	if decodePayment(t, read.Body.Bytes()).Status != "processing" {
		t.Fatalf("GET is not current state: %s", read.Body)
	}
	// Replay still returns the original acceptance snapshot.
	replay := e.create("transition", intent("5", "USD", auth.DemoMethodID))
	if !bytes.Equal(replay.Body.Bytes(), result.Body) || decodePayment(t, replay.Body.Bytes()).Status != "created" {
		t.Fatalf("replay changed %s", replay.Body)
	}
}

// holdWriteLock takes SQLite's write lock from an independent connection.
func holdWriteLock(t *testing.T, path string) (release func()) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+path+"?_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
			_ = conn.Close()
			_ = sqlDB.Close()
		})
	}
}

// SQLITE_BUSY after busy_timeout is retried as a whole transaction, bounded,
// and exhaustion returns 503 without committing anything.
func TestBusyRetryIsBoundedAndRecovers(t *testing.T) {
	t.Run("exhausted", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, payment.Options{MaxBusyAttempts: 1})
		release := holdWriteLock(t, e.path)
		defer release()
		response := e.create("busy-exhausted", intent("1", "USD", auth.DemoMethodID))
		if response.Code != 503 || response.Header().Get("Retry-After") == "" {
			t.Fatalf("busy %d %s", response.Code, response.Body)
		}
		release()
		snapshot := e.metrics.Snapshot()
		if snapshot.BusyErrors != 1 || snapshot.BusyRetriesExhausted != 1 {
			t.Fatalf("metrics %+v", snapshot)
		}
		e.requireCounts(0)
	})
	t.Run("recovers", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, payment.Options{})
		release := holdWriteLock(t, e.path)
		defer release()
		timer := time.AfterFunc(5500*time.Millisecond, release)
		defer timer.Stop()
		response := e.create("busy-recovers", intent("1", "USD", auth.DemoMethodID))
		if response.Code != 202 {
			t.Fatalf("retry %d %s", response.Code, response.Body)
		}
		if snapshot := e.metrics.Snapshot(); snapshot.BusyErrors < 1 || snapshot.Accepted != 1 {
			t.Fatalf("metrics %+v", snapshot)
		}
		e.requireCounts(1)
	})
}
