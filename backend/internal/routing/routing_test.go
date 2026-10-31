package routing

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/railvanta/railvanta/backend/internal/storage"
)

func bound(v int64) *int64 { return &v }

func baseSnapshot() Snapshot {
	return Snapshot{
		Providers: []Provider{{ID: "pa", Code: "mock", Enabled: true}, {ID: "pb", Code: "stripe", Enabled: true}, {ID: "pc", Code: "plaid", Enabled: true}},
		Rules: []Rule{
			{ID: "r-b", ProviderID: "pb", Rail: "card", Currency: "USD", Priority: 1, MinAmountMinor: 1, MaxAmountMinor: bound(100000), Enabled: true},
			{ID: "r-a", ProviderID: "pa", Rail: "card", Currency: "USD", Priority: 2, MinAmountMinor: 1, Enabled: true},
			{ID: "r-c", ProviderID: "pc", Rail: "card", Currency: "USD", Priority: 0, MinAmountMinor: 500, MaxAmountMinor: bound(1000), Enabled: true},
		},
		TokenProviderIDs: []string{"pa", "pb", "pc"},
	}
}

func req(amount int64) Request {
	return Request{WorkspaceID: "w1", MethodID: "m1", Rail: "card", Currency: "USD", AmountMinor: amount}
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*Request, *Snapshot)
		amount   int64
		provider string
		rule     string
		order    []string
		err      error
	}{
		{name: "lowest priority number wins", amount: 700, provider: "pc", rule: "r-c", order: []string{"r-c", "r-b", "r-a"}},
		{name: "below min skips rule", amount: 499, provider: "pb", rule: "r-b", order: []string{"r-b", "r-a"}},
		{name: "above max skips rule", amount: 1001, provider: "pb", rule: "r-b", order: []string{"r-b", "r-a"}},
		{name: "min bound inclusive", amount: 500, provider: "pc", rule: "r-c", order: []string{"r-c", "r-b", "r-a"}},
		{name: "max bound inclusive", amount: 100000, provider: "pb", rule: "r-b", order: []string{"r-b", "r-a"}},
		{name: "unbounded max", amount: 1 << 40, provider: "pa", rule: "r-a", order: []string{"r-a"}},
		{name: "no token excludes provider", amount: 700, provider: "pb", rule: "r-b", order: []string{"r-b", "r-a"},
			mutate: func(_ *Request, s *Snapshot) { s.TokenProviderIDs = []string{"pa", "pb"} }},
		{name: "disabled provider", amount: 700, provider: "pb", rule: "r-b", order: []string{"r-b", "r-a"},
			mutate: func(_ *Request, s *Snapshot) { s.Providers[2].Enabled = false }},
		{name: "disabled rule", amount: 700, provider: "pb", rule: "r-b", order: []string{"r-b", "r-a"},
			mutate: func(_ *Request, s *Snapshot) { s.Rules[2].Enabled = false }},
		{name: "excluded provider", amount: 700, provider: "pb", rule: "r-b", order: []string{"r-b", "r-a"},
			mutate: func(r *Request, _ *Snapshot) { r.Excluded = map[string]string{"pc": "prior attempt unknown"} }},
		{name: "currency mismatch", amount: 700, err: ErrNoEligibleProvider,
			mutate: func(r *Request, _ *Snapshot) { r.Currency = "EUR" }},
		{name: "rail mismatch", amount: 700, err: ErrNoEligibleProvider,
			mutate: func(r *Request, _ *Snapshot) { r.Rail = "ach" }},
		{name: "rule for unknown provider ignored", amount: 700, provider: "pb", rule: "r-b", order: []string{"r-b", "r-a"},
			mutate: func(_ *Request, s *Snapshot) { s.Providers = s.Providers[:2] }},
		{name: "no tokens at all", amount: 700, err: ErrNoEligibleProvider,
			mutate: func(_ *Request, s *Snapshot) { s.TokenProviderIDs = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, s := req(tc.amount), baseSnapshot()
			if tc.mutate != nil {
				tc.mutate(&r, &s)
			}
			d, err := Decide(r, s)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("want %v, got %v (%+v)", tc.err, err, d)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if d.ProviderID != tc.provider || d.RuleID != tc.rule {
				t.Fatalf("decision %+v", d)
			}
			var order []string
			for _, c := range d.Candidates {
				order = append(order, c.RuleID)
			}
			if !reflect.DeepEqual(order, tc.order) {
				t.Fatalf("order %v, want %v", order, tc.order)
			}
			if !strings.Contains(d.Reason, "Rule "+tc.rule) || !strings.Contains(d.Reason, tc.provider) {
				t.Fatalf("reason %q", d.Reason)
			}
		})
	}
}

func TestDecideIsDeterministic(t *testing.T) {
	s := baseSnapshot()
	first, _ := Decide(req(700), s)
	s.Rules[0], s.Rules[2] = s.Rules[2], s.Rules[0]
	second, _ := Decide(req(700), s)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("input order changed the decision:\n%+v\n%+v", first, second)
	}
}

func codes(in []Ineligibility) []string {
	var out []string
	for _, i := range in {
		out = append(out, i.Code)
	}
	return out
}

func TestExplain(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		mutate   func(*Request, *Snapshot)
		amount   int64
		want     []string
	}{
		{name: "eligible", provider: "pc", amount: 700},
		{name: "unknown provider", provider: "zz", amount: 700, want: []string{CodeProviderUnknown}},
		{name: "below min", provider: "pc", amount: 10, want: []string{CodeBelowMinimum}},
		{name: "above max", provider: "pc", amount: 5000, want: []string{CodeAboveMaximum}},
		{name: "no token", provider: "pc", amount: 700, want: []string{CodeNoToken},
			mutate: func(_ *Request, s *Snapshot) { s.TokenProviderIDs = []string{"pa"} }},
		{name: "disabled provider and rule", provider: "pc", amount: 700, want: []string{CodeProviderDisabled, CodeRuleDisabled},
			mutate: func(_ *Request, s *Snapshot) { s.Providers[2].Enabled = false; s.Rules[2].Enabled = false }},
		{name: "unsafe failover blocked", provider: "pc", amount: 700, want: []string{CodeExcluded},
			mutate: func(r *Request, _ *Snapshot) {
				r.Excluded = map[string]string{"pc": "attempt outcome is unknown; only the original provider may be queried"}
			}},
		{name: "currency and rail mismatch", provider: "pa", amount: 700, want: []string{CodeRailMismatch, CodeCurrencyMismatch},
			mutate: func(r *Request, _ *Snapshot) { r.Rail = "ach"; r.Currency = "EUR" }},
		{name: "no rule", provider: "pd", amount: 700, want: []string{CodeNoRule},
			mutate: func(_ *Request, s *Snapshot) {
				s.Providers = append(s.Providers, Provider{ID: "pd", Code: "mock", Enabled: true})
				s.TokenProviderIDs = append(s.TokenProviderIDs, "pd")
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, s := req(tc.amount), baseSnapshot()
			if tc.mutate != nil {
				tc.mutate(&r, &s)
			}
			got := Explain(r, s, tc.provider)
			if !reflect.DeepEqual(codes(got), tc.want) {
				t.Fatalf("codes %v, want %v (%+v)", codes(got), tc.want, got)
			}
			for _, g := range got {
				if g.Message == "" || g.ProviderID != tc.provider {
					t.Fatalf("incomplete explanation %+v", g)
				}
			}
			// Explain and Eligible must agree.
			eligible := false
			for _, c := range Eligible(r, s) {
				eligible = eligible || c.ProviderID == tc.provider
			}
			if eligible != (len(got) == 0) {
				t.Fatalf("Explain/Eligible disagree: eligible=%v explanation=%v", eligible, got)
			}
		})
	}
}

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	s, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "routing.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.DB.Exec(`INSERT INTO workspaces VALUES ('w1','One','sandbox',0),('w2','Two','sandbox',0);
 INSERT INTO providers VALUES ('pa','w1','mock',1,0),('pb','w1','stripe',0,0),('pc','w1','plaid',1,0),('px','w2','mock',1,0);
 INSERT INTO payment_methods(id,workspace_id,rail,created_at) VALUES ('m1','w1','card',0),('m2','w2','card',0);
 INSERT INTO payment_method_tokens VALUES ('t1','w1','m1','pa','mock_success_m1',0),('t2','w1','m1','pb','tok_b',0),('t3','w2','m2','px','mock_success_m2',0);
 INSERT INTO routing_rules(id,workspace_id,provider_id,rail,currency,priority,min_amount_minor,max_amount_minor,enabled,version,created_at,updated_at) VALUES
  ('r1','w1','pb','card','USD',0,1,NULL,1,1,0,0),
  ('r2','w1','pc','card','USD',1,1,NULL,1,1,0,0),
  ('r3','w1','pa','card','USD',2,1,50000,1,1,0,0),
  ('r4','w1','pa','card','USD',3,50001,NULL,0,1,0,0),
  ('rx','w2','px','card','USD',0,1,NULL,1,1,0,0);`).Error; err != nil {
		t.Fatal(err)
	}
	return s.DB
}

func TestLoadAndDecideFromSQLite(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	counts := func() (n int64) {
		db.Raw(`SELECT (SELECT count(*) FROM providers)+(SELECT count(*) FROM routing_rules)+(SELECT count(*) FROM payment_method_tokens)+(SELECT count(*) FROM payment_attempts)+(SELECT total_changes())`).Scan(&n)
		return n
	}
	before := counts()
	snap, rail, err := Load(ctx, db, "w1", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if rail != "card" || len(snap.Providers) != 3 || len(snap.Rules) != 4 || !reflect.DeepEqual(snap.TokenProviderIDs, []string{"pa", "pb"}) {
		t.Fatalf("snapshot %+v rail %s", snap, rail)
	}
	r := Request{WorkspaceID: "w1", MethodID: "m1", Rail: rail, Currency: "USD", AmountMinor: 25000}
	d, err := Decide(r, snap)
	if err != nil {
		t.Fatal(err)
	}
	// pb is disabled, pc holds no token, so the priority-2 rule for pa wins.
	if d.ProviderID != "pa" || d.RuleID != "r3" || len(d.Candidates) != 1 {
		t.Fatalf("decision %+v", d)
	}
	if got := codes(Explain(r, snap, "pb")); !reflect.DeepEqual(got, []string{CodeProviderDisabled}) {
		t.Fatalf("pb %v", got)
	}
	if got := codes(Explain(r, snap, "pc")); !reflect.DeepEqual(got, []string{CodeNoToken}) {
		t.Fatalf("pc %v", got)
	}
	if got := codes(Explain(r, snap, "px")); !reflect.DeepEqual(got, []string{CodeProviderUnknown}) {
		t.Fatalf("cross-workspace provider must be unknown: %v", got)
	}
	r.AmountMinor = 60000
	if _, err := Decide(r, snap); !errors.Is(err, ErrNoEligibleProvider) {
		t.Fatalf("want no eligible provider above r3 max with r4 disabled, got %v", err)
	}
	if after := counts(); after != before {
		t.Fatalf("routing wrote to the database: %d -> %d", before, after)
	}
}

func TestLoadIsWorkspaceScoped(t *testing.T) {
	db := testDB(t)
	if _, _, err := Load(context.Background(), db, "w1", "m2"); !errors.Is(err, ErrMethodNotFound) {
		t.Fatalf("want ErrMethodNotFound for other workspace's method, got %v", err)
	}
	snap, _, err := Load(context.Background(), db, "w2", "m2")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Providers) != 1 || snap.Providers[0].ID != "px" || len(snap.Rules) != 1 || snap.Rules[0].ID != "rx" {
		t.Fatalf("snapshot leaked across workspaces: %+v", snap)
	}
}
