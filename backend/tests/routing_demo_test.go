package tests

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/provider/mock"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

type altBody struct {
	ProviderCode string   `json:"provider_code"`
	Status       string   `json:"status"`
	Eligible     bool     `json:"eligible"`
	Reasons      []string `json:"reasons"`
}

type routingBody struct {
	ProviderCode    string    `json:"provider_code"`
	RuleID          string    `json:"rule_id"`
	Reason          string    `json:"reason"`
	FailoverBlocked bool      `json:"failover_blocked"`
	Alternatives    []altBody `json:"alternatives"`
}

// newRoutingDemoEnv wires all three simulated profiles with instant latency.
func newRoutingDemoEnv(t *testing.T) *dispatchEnv {
	t.Helper()
	extra := payment.ProviderSet{}
	d := &dispatchEnv{env: newEnv(t, payment.Options{})}
	for _, p := range mock.Profiles {
		extra[p.Code] = mock.NewProfile(d.store.DB, p, mock.Options{Sleep: instantSleep})
	}
	d.build(dispatchConfig{extra: extra})
	return d
}

func (d *dispatchEnv) evaluateDemo(method, amount, paymentID string) routingBody {
	d.t.Helper()
	body := `{"payment_method_id":"` + method + `","amount_minor":"` + amount + `","currency":"USD"`
	if paymentID != "" {
		body += `,"payment_id":"` + paymentID + `"`
	}
	response := d.do("POST", "/v1/routing/evaluate", credentials.OperatorKey, "", body+`}`)
	if response.Code != 200 {
		d.t.Fatalf("evaluate %s: %d %s", method, response.Code, response.Body.String())
	}
	var out routingBody
	if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
		d.t.Fatal(err)
	}
	return out
}

func alt(t *testing.T, b routingBody, code string) altBody {
	t.Helper()
	for _, a := range b.Alternatives {
		if a.ProviderCode == code {
			return a
		}
	}
	t.Fatalf("no alternative %s in %+v", code, b.Alternatives)
	return altBody{}
}

func TestRoutingDemoProvidersListedAsSimulated(t *testing.T) {
	d := newRoutingDemoEnv(t)
	response := d.do("GET", "/v1/providers", credentials.OperatorKey, "", "")
	var list struct {
		Items []struct {
			ID, Code, Mode string
			DisplayName    string `json:"display_name"`
			Capabilities   *struct {
				Rails []string `json:"rails"`
			}
			Latency *int64 `json:"simulated_latency_ms"`
		}
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &list) != nil {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	found := map[string]bool{}
	for _, item := range list.Items {
		p, ok := mock.ProfileByCode(item.Code)
		if !ok {
			continue
		}
		found[item.Code] = true
		if item.ID != p.ID || item.Mode != "simulated" || item.DisplayName != p.Label || item.Capabilities == nil ||
			len(item.Capabilities.Rails) != len(p.Rails) || item.Latency == nil || *item.Latency != p.Latency.Milliseconds() {
			t.Fatalf("%s: %+v", item.Code, item)
		}
	}
	if len(found) != 3 {
		t.Fatalf("simulated providers listed: %v", found)
	}
	if code := d.do("GET", "/v1/providers/"+mock.ProfileEconomy.ID, credentials.OperatorKey, "", "").Code; code != 200 {
		t.Fatalf("provider detail: %d", code)
	}
}

func TestRoutingDemoCardPicksFastAndExplainsEconomy(t *testing.T) {
	d := newRoutingDemoEnv(t)
	before := d.dump()
	got := d.evaluateDemo(auth.RoutingCardMethodID, "2500", "")
	if got.ProviderCode != "mock_fast" || got.RuleID != auth.RoutingCardRuleFast {
		t.Fatalf("%+v", got)
	}
	if e := alt(t, got, "mock_economy"); e.Status != "eligible_lower_priority" || !e.Eligible {
		t.Fatalf("economy: %+v", e)
	}
	if ach := alt(t, got, "mock_ach"); ach.Status != "ineligible" || len(ach.Reasons) == 0 {
		t.Fatalf("ach: %+v", ach)
	}
	for table, sum := range before {
		if d.dump()[table] != sum {
			t.Fatalf("evaluate wrote table %s", table)
		}
	}
	id := d.accept(auth.RoutingCardMethodID, "2500", "USD")
	d.drain()
	attempts := d.attempts(id)
	if len(attempts) != 1 || attempts[0].ProviderID != mock.ProfileFast.ID || d.payment(id).Status != "succeeded" {
		t.Fatalf("%+v %s", attempts, d.payment(id).Status)
	}
	if got := d.evaluateDemo(auth.RoutingEconomyOnlyMethodID, "2500", ""); got.ProviderCode != "mock_economy" || alt(t, got, "mock_fast").Eligible {
		t.Fatalf("economy-only: %+v", got)
	}
	d.requireLedger()
}

func TestRoutingDemoACHPicksACHAndRespectsBounds(t *testing.T) {
	d := newRoutingDemoEnv(t)
	small := d.evaluateDemo(auth.RoutingACHMethodID, "5000", "")
	if small.ProviderCode != "mock_ach" || alt(t, small, "mock_fast").Status != "eligible_lower_priority" {
		t.Fatalf("%+v", small)
	}
	large := d.evaluateDemo(auth.RoutingACHMethodID, "250000", "")
	if large.ProviderCode != "mock_ach" || alt(t, large, "mock_fast").Eligible {
		t.Fatalf("large: %+v", large)
	}
	id := d.accept(auth.RoutingACHMethodID, "5000", "USD")
	d.drain()
	if a := d.attempts(id); len(a) != 1 || a[0].ProviderID != mock.ProfileACH.ID || d.payment(id).Status != "succeeded" {
		t.Fatalf("%+v", a)
	}
}

func TestRoutingDemoOutageEndsUnknownWithoutFailover(t *testing.T) {
	d := newRoutingDemoEnv(t)
	id := d.accept(auth.RoutingOutageMethodID, "4200", "USD")
	d.drain()
	if got := d.payment(id).Status; got != "unknown" {
		t.Fatalf("status %s", got)
	}
	attempts := d.attempts(id)
	if len(attempts) != 1 || attempts[0].ProviderID != mock.ProfileFast.ID {
		t.Fatalf("attempts %+v", attempts)
	}
	if n := d.count("payment_attempts", "payment_id = ? AND provider_id <> ?", id, mock.ProfileFast.ID); n != 0 {
		t.Fatalf("%d attempts at another provider", n)
	}
	if n := len(d.charges("provider_id = ?", mock.ProfileEconomy.ID)); n != 0 {
		t.Fatalf("economy received %d charges", n)
	}
	before := d.dump()
	got := d.evaluateDemo(auth.RoutingOutageMethodID, "4200", id)
	if !got.FailoverBlocked || got.ProviderCode != "mock_fast" {
		t.Fatalf("%+v", got)
	}
	if e := alt(t, got, "mock_economy"); e.Status != "blocked" || !e.Eligible || !containsReason(e.Reasons, "unsafe failover blocked: prior attempt unresolved") {
		t.Fatalf("economy: %+v", e)
	}
	if f := alt(t, got, "mock_fast"); f.Status != "pinned" {
		t.Fatalf("fast: %+v", f)
	}
	for table, sum := range before {
		if d.dump()[table] != sum {
			t.Fatalf("evaluate wrote table %s", table)
		}
	}
	d.requireLedger()
}

func containsReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

// Migration 0006 rebuilds providers; rows referenced by child tables must
// survive an upgrade of a database that already holds seeded data.
func TestSimulatedProvidersMigrationPreservesExistingRows(t *testing.T) {
	plan, err := storage.EmbeddedMigrations()
	if err != nil || len(plan) < 6 {
		t.Fatalf("plan %d %v", len(plan), err)
	}
	db, err := gorm.Open(sqlite.Open("file:"+filepath.Join(t.TempDir(), "old.sqlite")+"?_foreign_keys=on&_txlock=immediate"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx := context.Background()
	if err := storage.Migrate(ctx, db, plan[:5]); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"INSERT INTO workspaces(id,name,environment,created_at) VALUES('" + auth.DemoWorkspaceID + "','Demo','sandbox',1)",
		"INSERT INTO providers(id,workspace_id,code,enabled,created_at) VALUES('" + auth.DemoProviderID + "','" + auth.DemoWorkspaceID + "','mock',1,1)",
		"INSERT INTO payment_methods(id,workspace_id,rail,brand,last_four,created_at) VALUES('" + auth.DemoMethodID + "','" + auth.DemoWorkspaceID + "','mock','Mock','4242',1)",
		"INSERT INTO payment_method_tokens(id,workspace_id,method_id,provider_id,token_reference,created_at) VALUES('00000000-0000-7000-8000-000000000301','" + auth.DemoWorkspaceID + "','" + auth.DemoMethodID + "','" + auth.DemoProviderID + "','mock_reference',1)",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.Migrate(ctx, db, plan); err != nil {
		t.Fatal(err)
	}
	var violations []map[string]any
	if err := db.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil || len(violations) != 0 {
		t.Fatalf("foreign_key_check %v %v", violations, err)
	}
	var providers, tokens int64
	db.Table("providers").Count(&providers)
	db.Table("payment_method_tokens").Count(&tokens)
	if providers != 1 || tokens != 1 {
		t.Fatalf("providers %d tokens %d", providers, tokens)
	}
	if err := auth.SeedDemo(ctx, db, credentials); err != nil {
		t.Fatal(err)
	}
	db.Table("providers").Count(&providers)
	if providers != 5 {
		t.Fatalf("providers after reseed %d", providers)
	}
}
