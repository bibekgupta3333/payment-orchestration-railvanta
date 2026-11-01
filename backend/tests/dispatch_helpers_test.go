package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/provider/mock"
	"github.com/railvanta/railvanta/backend/internal/storage"
	httpapi "github.com/railvanta/railvanta/backend/internal/transport/http"
	"gorm.io/gorm"
)

// Seeded scenario method IDs (auth.ScenarioMethods).
var scenarioMethod = func() map[string]string {
	m := map[string]string{}
	for _, s := range auth.ScenarioMethods {
		m[s.Scenario] = s.ID
	}
	return m
}()

func instantSleep(context.Context, mock.Scenario, time.Duration) error { return nil }

// dispatchEnv adds a mock adapter, executor and dispatcher to env.
type dispatchEnv struct {
	*env
	adapter    *mock.Adapter
	providers  payment.ProviderSet
	executor   *payment.Executor
	dispatcher *event.Dispatcher
}

type dispatchConfig struct {
	sleep  mock.SleepFunc
	lease  time.Duration
	hooks  payment.ExecutorHooks
	extra  payment.ProviderSet
	db     *gorm.DB // defaults to the env store
	drain  time.Duration
	poll   time.Duration
	nowFor func() time.Time
	// resolveDelay postpones the first lookup after an ambiguous outcome.
	resolveDelay time.Duration
}

func newDispatchEnv(t *testing.T, cfg dispatchConfig) *dispatchEnv {
	t.Helper()
	d := &dispatchEnv{env: newEnv(t, payment.Options{})}
	d.build(cfg)
	return d
}

func (d *dispatchEnv) build(cfg dispatchConfig) {
	if cfg.sleep == nil {
		cfg.sleep = instantSleep
	}
	db := cfg.db
	if db == nil {
		db = d.store.DB
	}
	d.adapter = mock.New(db, mock.Options{Sleep: cfg.sleep})
	d.providers = payment.ProviderSet{"mock": d.adapter}
	for code, p := range cfg.extra {
		d.providers[code] = p
	}
	d.executor = payment.NewExecutor(db, payment.ExecutorOptions{Providers: d.providers, ProviderTimeout: 5 * time.Second, Hooks: cfg.hooks, Now: cfg.nowFor, ResolveDelay: cfg.resolveDelay})
	d.dispatcher = event.NewDispatcher(db, event.Options{LeaseDuration: cfg.lease, DrainTimeout: cfg.drain, PollInterval: cfg.poll, Now: cfg.nowFor})
	d.executor.Register(d.dispatcher)
	d.handler = httpapi.NewHandler(d.store.DB, httpapi.Config{PublicOrigin: "http://localhost:3000", RequestsPerMinute: 100000, Payments: d.service, Router: d.executor.Router()})
}

// accept creates a payment through the HTTP API and returns its ID.
func (e *env) accept(method, amount, currency string) string {
	e.t.Helper()
	response := e.create("k-"+uuid.NewString(), intent(amount, currency, method))
	if response.Code != 202 {
		e.t.Fatalf("accept: %d %s", response.Code, response.Body.String())
	}
	return decodePayment(e.t, response.Body.Bytes()).ID
}

// drain runs dispatcher rounds until nothing is claimed.
func (d *dispatchEnv) drain() {
	d.t.Helper()
	for i := 0; i < 50; i++ {
		n, err := d.dispatcher.RunOnce(context.Background())
		if err != nil {
			d.t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
	d.t.Fatal("dispatcher did not settle")
}

func (e *env) payment(id string) storage.Payment {
	e.t.Helper()
	p, err := storage.GetPayment(e.store.DB, auth.DemoWorkspaceID, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *env) attempts(paymentID string) []storage.PaymentAttempt {
	e.t.Helper()
	attempts, err := storage.ListPaymentAttempts(e.store.DB, auth.DemoWorkspaceID, paymentID)
	if err != nil {
		e.t.Fatal(err)
	}
	return attempts
}

func (e *env) outbox(paymentID, eventType string) []storage.OutboxEvent {
	e.t.Helper()
	var rows []storage.OutboxEvent
	if err := e.store.DB.Where("payment_id = ? AND event_type = ?", paymentID, eventType).Find(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func (e *env) charges(where string, args ...any) []mock.Charge {
	e.t.Helper()
	var rows []mock.Charge
	if err := e.store.DB.Where(where, args...).Find(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func (e *env) journals(paymentID string) []storage.LedgerJournal {
	e.t.Helper()
	var rows []storage.LedgerJournal
	if err := e.store.DB.Where("payment_id = ?", paymentID).Find(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func (e *env) count(table, where string, args ...any) int64 {
	e.t.Helper()
	var n int64
	query := e.store.DB.Table(table)
	if where != "" {
		query = query.Where(where, args...)
	}
	if err := query.Count(&n).Error; err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if err := e.store.DB.Exec(sql, args...).Error; err != nil {
		e.t.Fatalf("%v\n%s", err, sql)
	}
}

func (e *env) requireLedger() {
	e.t.Helper()
	if err := ledger.Check(e.store.DB); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) statuses(paymentID string) []string {
	e.t.Helper()
	transitions, err := storage.ListStateTransitions(e.store.DB, auth.DemoWorkspaceID, paymentID)
	if err != nil {
		e.t.Fatal(err)
	}
	out := make([]string, 0, len(transitions))
	for _, tr := range transitions {
		out = append(out, tr.ToStatus)
	}
	return out
}

// dump hashes every user table's rows in a stable order; equal dumps mean no
// table changed.
func (e *env) dump() map[string]string {
	e.t.Helper()
	var tables []string
	if err := e.store.DB.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name").Scan(&tables).Error; err != nil {
		e.t.Fatal(err)
	}
	out := map[string]string{}
	for _, table := range tables {
		rows, err := e.store.DB.Raw(fmt.Sprintf("SELECT * FROM %q ORDER BY rowid", table)).Rows()
		if err != nil {
			rows, err = e.store.DB.Raw(fmt.Sprintf("SELECT * FROM %q", table)).Rows()
			if err != nil {
				e.t.Fatal(err)
			}
		}
		columns, _ := rows.Columns()
		hash := sha256.New()
		count := 0
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				e.t.Fatal(err)
			}
			encoded, _ := json.Marshal(values)
			hash.Write(encoded)
			count++
		}
		_ = rows.Close()
		out[table] = fmt.Sprintf("%d:%s", count, hex.EncodeToString(hash.Sum(nil)))
	}
	return out
}

func requireStatus(t *testing.T, p storage.Payment, want payment.Status) {
	t.Helper()
	if p.Status != string(want) {
		t.Fatalf("payment %s status %s, want %s", p.ID, p.Status, want)
	}
}

func joined(values []string) string { return strings.Join(values, ">") }
