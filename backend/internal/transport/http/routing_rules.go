package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/routing"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// RoutingRuleView is one routing rule. Money is a decimal string of minor units.
type RoutingRuleView struct {
	ID             string  `json:"id"`
	ProviderID     string  `json:"provider_id"`
	ProviderCode   string  `json:"provider_code"`
	Rail           string  `json:"rail"`
	Currency       string  `json:"currency"`
	Priority       int64   `json:"priority"`
	MinAmountMinor string  `json:"min_amount_minor"`
	MaxAmountMinor *string `json:"max_amount_minor"`
	Enabled        bool    `json:"enabled"`
}

func newRuleView(rule storage.RoutingRule, providerCode string) RoutingRuleView {
	view := RoutingRuleView{
		ID: rule.ID, ProviderID: rule.ProviderID, ProviderCode: providerCode, Rail: rule.Rail, Currency: rule.Currency,
		Priority: rule.Priority, MinAmountMinor: strconv.FormatInt(rule.MinAmountMinor, 10), Enabled: rule.Enabled == 1,
	}
	if rule.MaxAmountMinor != nil {
		max := strconv.FormatInt(*rule.MaxAmountMinor, 10)
		view.MaxAmountMinor = &max
	}
	return view
}

// listRoutingRules serves GET /v1/routing/rules: every rule of the workspace in
// dispatch order (priority, then ID). It only reads; rule mutation has no
// contract yet.
func (h *handler) listRoutingRules(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	rules, codes, err := h.loadRules(r, principal.WorkspaceID)
	if err != nil {
		fail(w, 503, "unavailable", "Routing rules unavailable", id)
		return
	}
	items := make([]RoutingRuleView, 0, len(rules))
	for _, rule := range rules {
		items = append(items, newRuleView(rule, codes[rule.ProviderID]))
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
}

func (h *handler) loadRules(r *http.Request, workspaceID string) ([]storage.RoutingRule, map[string]string, error) {
	db := h.db.WithContext(r.Context())
	var rules []storage.RoutingRule
	if err := db.Where("workspace_id = ?", workspaceID).Order("priority, id").Find(&rules).Error; err != nil {
		return nil, nil, err
	}
	var providers []storage.Provider
	if err := db.Where("workspace_id = ?", workspaceID).Find(&providers).Error; err != nil {
		return nil, nil, err
	}
	codes := make(map[string]string, len(providers))
	for _, p := range providers {
		codes[p.ID] = p.Code
	}
	return rules, codes, nil
}

// RoutingCheckView is one eligibility check of the evaluation path.
type RoutingCheckView struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// RoutingCandidateView is one eligible (provider, rule) pair in try order.
type RoutingCandidateView struct {
	ProviderID   string `json:"provider_id"`
	ProviderCode string `json:"provider_code"`
	RuleID       string `json:"rule_id"`
	Priority     int64  `json:"priority"`
}

// buildChecks lists the capability and eligibility checks for one provider
// against one rule, from the same snapshot routing decided on.
func buildChecks(req routing.Request, snap routing.Snapshot, providerID string, rule *routing.Rule, adapter bool, blockedReason string) []RoutingCheckView {
	var provider routing.Provider
	for _, p := range snap.Providers {
		if p.ID == providerID {
			provider = p
		}
	}
	token := false
	for _, id := range snap.TokenProviderIDs {
		token = token || id == providerID
	}
	label := provider.Code
	if label == "" {
		label = providerID
	}
	checks := []RoutingCheckView{
		{ID: "method", Label: "Payment method found", Passed: true, Detail: fmt.Sprintf("method %s uses rail %s", req.MethodID, req.Rail)},
		{ID: "provider_enabled", Label: "Provider enabled", Passed: provider.Enabled, Detail: fmt.Sprintf("provider %s is %s", label, map[bool]string{true: "enabled", false: "disabled"}[provider.Enabled])},
		{ID: "token", Label: "Provider holds a token for the method", Passed: token, Detail: fmt.Sprintf("provider %s %s a token for method %s", label, map[bool]string{true: "holds", false: "holds no"}[token], req.MethodID)},
		{ID: "adapter", Label: "Execution adapter configured", Passed: adapter, Detail: fmt.Sprintf("an adapter for provider code %s is %s", label, map[bool]string{true: "configured", false: "not configured"}[adapter])},
	}
	if rule == nil {
		checks = append(checks, RoutingCheckView{ID: "rule", Label: "Routing rule found", Passed: false, Detail: "no rule applies"})
	} else {
		checks = append(checks,
			RoutingCheckView{ID: "rule_enabled", Label: "Rule enabled", Passed: rule.Enabled, Detail: fmt.Sprintf("rule %s is %s", rule.ID, map[bool]string{true: "enabled", false: "disabled"}[rule.Enabled])},
			RoutingCheckView{ID: "rail", Label: "Rail matches", Passed: rule.Rail == req.Rail, Detail: fmt.Sprintf("rule rail %s, payment rail %s", rule.Rail, req.Rail)},
			RoutingCheckView{ID: "currency", Label: "Currency matches", Passed: rule.Currency == req.Currency, Detail: fmt.Sprintf("rule currency %s, payment currency %s", rule.Currency, req.Currency)},
			RoutingCheckView{ID: "amount", Label: "Amount within bounds", Passed: req.AmountMinor >= rule.MinAmountMinor && (rule.MaxAmountMinor == nil || req.AmountMinor <= *rule.MaxAmountMinor),
				Detail: fmt.Sprintf("amount %d against %s", req.AmountMinor, boundsText(*rule))},
		)
	}
	failoverDetail := "no unresolved prior attempt, so a first routing decision is allowed"
	if blockedReason != "" {
		failoverDetail = blockedReason
	}
	return append(checks, RoutingCheckView{ID: "failover", Label: "No unresolved prior attempt", Passed: blockedReason == "", Detail: failoverDetail})
}

func boundsText(r routing.Rule) string {
	if r.MaxAmountMinor == nil {
		return fmt.Sprintf("[%d, unbounded]", r.MinAmountMinor)
	}
	return fmt.Sprintf("[%d, %d]", r.MinAmountMinor, *r.MaxAmountMinor)
}

func ruleViewFromRouting(rule routing.Rule, providerCode string) RoutingRuleView {
	view := RoutingRuleView{
		ID: rule.ID, ProviderID: rule.ProviderID, ProviderCode: providerCode, Rail: rule.Rail, Currency: rule.Currency,
		Priority: rule.Priority, MinAmountMinor: strconv.FormatInt(rule.MinAmountMinor, 10), Enabled: rule.Enabled,
	}
	if rule.MaxAmountMinor != nil {
		max := strconv.FormatInt(*rule.MaxAmountMinor, 10)
		view.MaxAmountMinor = &max
	}
	return view
}
