package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
	httpapi "github.com/railvanta/railvanta/backend/internal/transport/http"
)

var credentials = auth.DemoCredentials{
	OperatorKey:       "operator." + strings.Repeat("a", 32),
	ReaderKey:         "reader." + strings.Repeat("b", 32),
	OtherWorkspaceKey: "other." + strings.Repeat("c", 32),
	SessionToken:      strings.Repeat("s", 32),
	CSRFToken:         strings.Repeat("x", 32),
}

// env is one file-backed WAL database with the demo seed and an HTTP handler.
type env struct {
	t       *testing.T
	path    string
	store   *storage.Store
	service *payment.Service
	handler http.Handler
	metrics *payment.Metrics
}

func newEnv(t *testing.T, options payment.Options) *env {
	t.Helper()
	e := &env{t: t, path: filepath.Join(t.TempDir(), "payments.sqlite")}
	e.open(options)
	if err := auth.SeedDemo(context.Background(), e.store.DB, credentials); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.store.Close() })
	return e
}

// openTestStore is the shared WBS 5.1.1 helper: it opens a file-backed SQLite
// database through storage.Open (production connection string, all embedded
// migrations) and independently asserts every production PRAGMA, the
// single-connection pool, foreign-key integrity and the migration history.
func openTestStore(t testing.TB, path string) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.VerifySettings(context.Background(), store.DB); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := store.DB.Raw("PRAGMA journal_mode").Scan(&mode).Error; err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q %v", mode, err)
	}
	plan, err := storage.EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var applied int64
	if err := store.DB.Raw("SELECT count(*) FROM schema_migrations").Scan(&applied).Error; err != nil || applied != int64(len(plan)) {
		t.Fatalf("applied %d of %d migrations: %v", applied, len(plan), err)
	}
	var violations []map[string]any
	if err := store.DB.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil || len(violations) != 0 {
		t.Fatalf("foreign_key_check %v %v", violations, err)
	}
	if handle, err := store.DB.DB(); err != nil || handle.Stats().MaxOpenConnections != 1 {
		t.Fatalf("pool must have one connection: %v", err)
	}
	return store
}

func (e *env) open(options payment.Options) {
	e.t.Helper()
	store := openTestStore(e.t, e.path)
	if options.Metrics == nil {
		options.Metrics = payment.NewMetrics()
	}
	e.store, e.metrics = store, options.Metrics
	e.service = payment.NewService(store.DB, options)
	e.handler = httpapi.NewHandler(store.DB, httpapi.Config{PublicOrigin: "http://localhost:3000", RequestsPerMinute: 100000, Payments: e.service})
}

// reopen simulates a process restart: close every connection, then reopen.
func (e *env) reopen() {
	e.t.Helper()
	if err := e.store.Close(); err != nil {
		e.t.Fatal(err)
	}
	e.open(payment.Options{})
}

func (e *env) do(method, path, apiKey, idempotencyKey, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	response := httptest.NewRecorder()
	e.handler.ServeHTTP(response, request)
	if response.Code >= 400 {
		var envelope httpapi.ErrorEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Code == "" || envelope.RequestID == "" || envelope.FieldErrors == nil {
			e.t.Fatalf("invalid error envelope: %s", response.Body.String())
		}
	}
	return response
}

func (e *env) create(key, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do("POST", "/v1/payments", credentials.OperatorKey, key, body)
}

// counts returns row counts for every table the acceptance unit of work writes.
func (e *env) counts() map[string]int64 {
	e.t.Helper()
	result := map[string]int64{}
	for _, table := range []string{"payments", "idempotency_records", "payment_state_transitions", "outbox_events"} {
		var n int64
		if err := e.store.DB.Table(table).Count(&n).Error; err != nil {
			e.t.Fatal(err)
		}
		result[table] = n
	}
	return result
}

func (e *env) requireCounts(want int64) {
	e.t.Helper()
	for table, n := range e.counts() {
		if n != want {
			e.t.Errorf("%s has %d rows, want %d", table, n, want)
		}
	}
}

// addMethod inserts a demo-workspace method, optionally tokenized for the demo mock provider.
func (e *env) addMethod(id string, tokenized bool) {
	e.t.Helper()
	db := e.store.DB
	if err := db.Exec("INSERT INTO payment_methods(id,workspace_id,rail,brand,last_four,created_at) VALUES(?,?,'mock','Mock','1111',1)", id, auth.DemoWorkspaceID).Error; err != nil {
		e.t.Fatal(err)
	}
	if tokenized {
		if err := db.Exec("INSERT INTO payment_method_tokens(id,workspace_id,method_id,provider_id,token_reference,created_at) VALUES(?,?,?,?,?,1)", id+"-token", auth.DemoWorkspaceID, id, "00000000-0000-7000-8000-000000000201", "mock_reference_"+id).Error; err != nil {
			e.t.Fatal(err)
		}
	}
}

func intent(amount, currency, method string) string {
	return `{"amount_minor":"` + amount + `","currency":"` + currency + `","payment_method_id":"` + method + `"}`
}

func decodePayment(t *testing.T, body []byte) payment.PaymentView {
	t.Helper()
	var view payment.PaymentView
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return view
}
