package auth

import (
	"context"
	"errors"
	"time"

	"github.com/railvanta/railvanta/backend/internal/provider/mock"
	"gorm.io/gorm"
)

const (
	DemoWorkspaceID  = "00000000-0000-7000-8000-000000000001"
	OtherWorkspaceID = "00000000-0000-7000-8000-000000000002"
	DemoMethodID     = "00000000-0000-7000-8000-000000000101"
	OtherMethodID    = "00000000-0000-7000-8000-000000000102"
	DemoProviderID   = "00000000-0000-7000-8000-000000000201"
	OtherProviderID  = "00000000-0000-7000-8000-000000000202"
)

// ScenarioMethod is a seeded demo-workspace payment method whose mock token
// reference selects one deterministic mock scenario (see provider/mock).
type ScenarioMethod struct {
	ID, TokenID, Scenario, LastFour string
}

// ScenarioMethods are seeded in the demo workspace so tests and E2E can pick a
// mock outcome by method ID. IDs are stable; token references are
// "mock_<scenario>_<method id>".
var ScenarioMethods = []ScenarioMethod{
	{"00000000-0000-7000-8000-000000000121", "00000000-0000-7000-8000-000000000321", "success", "1111"},
	{"00000000-0000-7000-8000-000000000122", "00000000-0000-7000-8000-000000000322", "decline", "0002"},
	{"00000000-0000-7000-8000-000000000123", "00000000-0000-7000-8000-000000000323", "delayed_success", "0003"},
	{"00000000-0000-7000-8000-000000000124", "00000000-0000-7000-8000-000000000324", "timeout_before_execution", "0004"},
	{"00000000-0000-7000-8000-000000000125", "00000000-0000-7000-8000-000000000325", "timeout_after_success", "0005"},
	{"00000000-0000-7000-8000-000000000126", "00000000-0000-7000-8000-000000000326", "outage", "0006"},
}

// Demo routing story (Demo 4). All three providers are SIMULATED mock profiles
// seeded in the demo workspace only (see provider/mock profiles). Methods hold a
// token at each listed provider; the token reference selects the mock scenario.
const (
	// RoutingCardMethodID: card; tokens at mock_fast and mock_economy (both
	// success). Routes to mock_fast (priority 0); mock_economy is eligible but
	// lower priority (priority 1).
	RoutingCardMethodID = "00000000-0000-7000-8000-000000000131"
	// RoutingACHMethodID: ach; tokens at mock_ach and mock_fast. USD amounts up
	// to 100000 minor units match both (mock_ach priority 0 wins); larger amounts
	// only match mock_ach because the mock_fast ach rule is bounded.
	RoutingACHMethodID = "00000000-0000-7000-8000-000000000132"
	// RoutingOutageMethodID: card; the token at mock_fast selects the outage
	// scenario (payment ends unknown) while mock_economy also holds a success
	// token. The unknown payment is never retried at mock_economy.
	RoutingOutageMethodID = "00000000-0000-7000-8000-000000000133"
	// RoutingEconomyOnlyMethodID: card; token only at mock_economy, so the router
	// reports mock_fast as ineligible (no token) and picks mock_economy.
	RoutingEconomyOnlyMethodID = "00000000-0000-7000-8000-000000000134"

	RoutingCardRuleFast    = "00000000-0000-7000-8000-000000000731"
	RoutingCardRuleEconomy = "00000000-0000-7000-8000-000000000732"
	RoutingACHRuleACH      = "00000000-0000-7000-8000-000000000733"
	RoutingACHRuleFast     = "00000000-0000-7000-8000-000000000734"
	RoutingACHFastMaxMinor = 100000
	routingDemoCurrency    = "USD"
)

type routingDemoMethod struct {
	ID, Rail, Brand, LastFour string
	Tokens                    []routingDemoToken
}

type routingDemoToken struct {
	ID       string
	Profile  mock.Profile
	Scenario string
}

var routingDemoMethods = []routingDemoMethod{
	{RoutingCardMethodID, "card", "Routing demo card", "4131", []routingDemoToken{
		{"00000000-0000-7000-8000-000000000331", mock.ProfileFast, "success"},
		{"00000000-0000-7000-8000-000000000332", mock.ProfileEconomy, "success"}}},
	{RoutingACHMethodID, "ach", "Routing demo ACH", "4132", []routingDemoToken{
		{"00000000-0000-7000-8000-000000000333", mock.ProfileACH, "success"},
		{"00000000-0000-7000-8000-000000000334", mock.ProfileFast, "success"}}},
	{RoutingOutageMethodID, "card", "Routing demo outage at preferred", "4133", []routingDemoToken{
		{"00000000-0000-7000-8000-000000000335", mock.ProfileFast, "outage"},
		{"00000000-0000-7000-8000-000000000336", mock.ProfileEconomy, "success"}}},
	{RoutingEconomyOnlyMethodID, "card", "Routing demo economy only", "4134", []routingDemoToken{
		{"00000000-0000-7000-8000-000000000337", mock.ProfileEconomy, "success"}}},
}

// SeedCurrencies receive one priority-0 mock-rail routing rule per workspace.
var SeedCurrencies = []string{"USD", "EUR", "GBP", "JPY", "KWD"}

// RoutingRuleID returns the stable seeded rule ID for a workspace index (0
// demo, 1 other) and currency index in SeedCurrencies.
func RoutingRuleID(workspaceIndex, currencyIndex int) string {
	return "00000000-0000-7000-8000-0000000007" + string(rune('0'+workspaceIndex)) + string(rune('1'+currencyIndex))
}

// DemoCredentials must be supplied explicitly by a loopback-only bootstrap.
// Empty session fields skip session creation; API keys are required.
type DemoCredentials struct{ OperatorKey, ReaderKey, OtherWorkspaceKey, SessionToken, CSRFToken string }

// SeedDemo persists deterministic public fixture IDs and hashed credentials.
// Existing keys are never reset, so revocation/expiry survive an API restart.
func SeedDemo(ctx context.Context, db *gorm.DB, credentials DemoCredentials) error {
	seenPrefixes := make(map[string]bool)
	for _, key := range []string{credentials.OperatorKey, credentials.ReaderKey, credentials.OtherWorkspaceKey} {
		prefix, ok := APIKeyPrefix(key)
		if !ok {
			return errors.New("demo bootstrap requires three prefix.secret API keys with high-entropy secrets")
		}
		if seenPrefixes[prefix] {
			return errors.New("demo API key prefixes must be distinct across roles and workspaces")
		}
		seenPrefixes[prefix] = true
	}
	if (credentials.SessionToken == "") != (credentials.CSRFToken == "") || credentials.SessionToken != "" && (len(credentials.SessionToken) < 24 || len(credentials.CSRFToken) < 24) {
		return errors.New("demo session and CSRF tokens must both be high entropy")
	}
	now := time.Now().UnixMilli()
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, workspace := range []string{DemoWorkspaceID, OtherWorkspaceID} {
			name, method := "Demo workspace", DemoMethodID
			if i == 1 {
				name, method = "Other workspace", OtherMethodID
			}
			provider := "00000000-0000-7000-8000-00000000020" + string(rune('1'+i))
			statements := []struct {
				SQL  string
				Args []any
			}{
				{"INSERT INTO workspaces(id,name,environment,created_at) VALUES(?,?,'sandbox',?) ON CONFLICT(id) DO NOTHING", []any{workspace, name, now}},
				{"INSERT INTO providers(id,workspace_id,code,enabled,created_at) VALUES(?,?,'mock',1,?) ON CONFLICT(id) DO NOTHING", []any{provider, workspace, now}},
				{"INSERT INTO payment_methods(id,workspace_id,rail,brand,last_four,created_at) VALUES(?,?,'mock','Mock','4242',?) ON CONFLICT(id) DO NOTHING", []any{method, workspace, now}},
				{"INSERT INTO payment_method_tokens(id,workspace_id,method_id,provider_id,token_reference,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", []any{"00000000-0000-7000-8000-00000000030" + string(rune('1'+i)), workspace, method, provider, "mock_reference_" + method, now}},
			}
			for c, currency := range SeedCurrencies {
				statements = append(statements, struct {
					SQL  string
					Args []any
				}{"INSERT INTO routing_rules(id,workspace_id,provider_id,rail,currency,priority,min_amount_minor,max_amount_minor,enabled,version,created_at,updated_at) VALUES(?,?,?,'mock',?,0,1,NULL,1,1,?,?) ON CONFLICT DO NOTHING", []any{RoutingRuleID(i, c), workspace, provider, currency, now, now}})
			}
			if i == 0 {
				for _, m := range ScenarioMethods {
					statements = append(statements,
						struct {
							SQL  string
							Args []any
						}{"INSERT INTO payment_methods(id,workspace_id,rail,brand,last_four,created_at) VALUES(?,?,'mock',?,?,?) ON CONFLICT(id) DO NOTHING", []any{m.ID, workspace, "Mock " + m.Scenario, m.LastFour, now}},
						struct {
							SQL  string
							Args []any
						}{"INSERT INTO payment_method_tokens(id,workspace_id,method_id,provider_id,token_reference,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", []any{m.TokenID, workspace, m.ID, provider, "mock_" + m.Scenario + "_" + m.ID, now}},
					)
				}
			}
			if i == 0 {
				statements = append(statements, routingDemoStatements(workspace, now)...)
			}
			for _, statement := range statements {
				if err := tx.Exec(statement.SQL, statement.Args...).Error; err != nil {
					return err
				}
			}
		}
		for i, key := range []string{credentials.OperatorKey, credentials.ReaderKey, credentials.OtherWorkspaceKey} {
			prefix, _ := APIKeyPrefix(key)
			var existing struct{ KeyHash string }
			lookup := tx.Table("api_keys").Where("prefix = ?", prefix).Take(&existing)
			if lookup.Error != nil && !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
				return lookup.Error
			}
			if lookup.Error == nil && !equalHash(key, existing.KeyHash) {
				return errors.New("demo credentials changed: reuse the original credentials or use a fresh sandbox database")
			}
			workspace, role := DemoWorkspaceID, "operator"
			if i == 1 {
				role = "read"
			}
			if i == 2 {
				workspace = OtherWorkspaceID
			}
			if err := tx.Exec("INSERT INTO api_keys(id,workspace_id,name,prefix,key_hash,role,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(prefix) DO NOTHING", "00000000-0000-7000-8000-00000000040"+string(rune('1'+i)), workspace, "Demo fixture", prefix, Hash(key), role, now).Error; err != nil {
				return err
			}
		}
		if credentials.SessionToken != "" {
			var existing struct{ TokenHash, CSRFHash string }
			lookup := tx.Table("sessions").Where("id = ?", "00000000-0000-7000-8000-000000000601").Take(&existing)
			if lookup.Error != nil && !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
				return lookup.Error
			}
			if lookup.Error == nil && (!equalHash(credentials.SessionToken, existing.TokenHash) || !equalHash(credentials.CSRFToken, existing.CSRFHash)) {
				return errors.New("demo session credentials changed: reuse the original credentials or use a fresh sandbox database")
			}
			user := "00000000-0000-7000-8000-000000000501"
			if err := tx.Exec("INSERT INTO users(id,email,created_at) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING", user, "demo@example.invalid", now).Error; err != nil {
				return err
			}
			if err := tx.Exec("INSERT INTO workspace_members(user_id,workspace_id,role,created_at) VALUES(?,?,'operator',?) ON CONFLICT(user_id,workspace_id) DO NOTHING", user, DemoWorkspaceID, now).Error; err != nil {
				return err
			}
			if err := tx.Exec("INSERT INTO sessions(id,user_id,workspace_id,token_hash,csrf_hash,expires_at,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", "00000000-0000-7000-8000-000000000601", user, DemoWorkspaceID, Hash(credentials.SessionToken), Hash(credentials.CSRFToken), time.Now().Add(24*time.Hour).UnixMilli(), now).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

type seedStatement = struct {
	SQL  string
	Args []any
}

// routingDemoStatements seeds the simulated providers, their card/ach rules,
// and the demo methods. Everything is additive (ON CONFLICT DO NOTHING).
func routingDemoStatements(workspace string, now int64) []seedStatement {
	var out []seedStatement
	for _, p := range mock.Profiles {
		out = append(out, seedStatement{"INSERT INTO providers(id,workspace_id,code,enabled,created_at) VALUES(?,?,?,1,?) ON CONFLICT(id) DO NOTHING", []any{p.ID, workspace, p.Code, now}})
	}
	rule := func(id string, p mock.Profile, rail string, priority int64, max any) seedStatement {
		return seedStatement{"INSERT INTO routing_rules(id,workspace_id,provider_id,rail,currency,priority,min_amount_minor,max_amount_minor,enabled,version,created_at,updated_at) VALUES(?,?,?,?,?,?,1,?,1,1,?,?) ON CONFLICT DO NOTHING",
			[]any{id, workspace, p.ID, rail, routingDemoCurrency, priority, max, now, now}}
	}
	out = append(out,
		rule(RoutingCardRuleFast, mock.ProfileFast, "card", 0, nil),
		rule(RoutingCardRuleEconomy, mock.ProfileEconomy, "card", 1, nil),
		rule(RoutingACHRuleACH, mock.ProfileACH, "ach", 0, nil),
		rule(RoutingACHRuleFast, mock.ProfileFast, "ach", 1, RoutingACHFastMaxMinor),
	)
	for _, m := range routingDemoMethods {
		out = append(out, seedStatement{"INSERT INTO payment_methods(id,workspace_id,rail,brand,last_four,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", []any{m.ID, workspace, m.Rail, m.Brand, m.LastFour, now}})
		for _, t := range m.Tokens {
			out = append(out, seedStatement{"INSERT INTO payment_method_tokens(id,workspace_id,method_id,provider_id,token_reference,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING",
				[]any{t.ID, workspace, m.ID, t.Profile.ID, "mock_" + t.Scenario + "_" + m.ID + "_" + t.Profile.Code, now}})
		}
	}
	return out
}
