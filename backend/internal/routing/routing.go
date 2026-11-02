// Package routing chooses a provider for a payment before submission. Decide and
// Explain are pure functions over a Snapshot; Load reads a Snapshot through a
// caller-supplied *gorm.DB with workspace predicates and never writes.
//
// Eligibility: a provider is eligible when it is enabled, holds a token for the
// payment method, and has at least one enabled rule matching the request rail,
// currency and amount bounds (min <= amount <= max, max NULL meaning unbounded).
// Candidates are ordered by rule priority ascending (0 is tried first), then rule
// ID for a stable tie-break. Routing never decides failover after an ambiguous
// outcome: callers exclude providers via Request.Excluded and the exclusion is
// reported as an ineligibility reason.
package routing

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrNoEligibleProvider is returned when no candidate passes every check.
var ErrNoEligibleProvider = errors.New("no eligible provider")

// Request is the input to routing; AmountMinor is integer minor units.
type Request struct {
	WorkspaceID string
	MethodID    string
	Rail        string
	Currency    string
	AmountMinor int64
	// Excluded maps provider IDs the caller must not use to a human reason,
	// e.g. an unresolved ambiguous attempt that blocks unsafe failover.
	Excluded map[string]string
}

// Provider is the routing view of a providers row.
type Provider struct {
	ID      string
	Code    string
	Enabled bool
}

// Rule is the routing view of a routing_rules row.
type Rule struct {
	ID             string
	ProviderID     string
	Rail           string
	Currency       string
	Priority       int64
	MinAmountMinor int64
	MaxAmountMinor *int64
	Enabled        bool
}

// Snapshot is everything routing needs for one method in one workspace.
type Snapshot struct {
	Providers []Provider
	Rules     []Rule
	// TokenProviderIDs are the providers holding a token for the method.
	TokenProviderIDs []string
}

// Candidate is an eligible (provider, rule) pair.
type Candidate struct {
	ProviderID   string
	ProviderCode string
	RuleID       string
	Priority     int64
}

// Decision is the routing result persisted on the attempt.
type Decision struct {
	ProviderID   string
	ProviderCode string
	RuleID       string
	Priority     int64
	Reason       string
	// Candidates lists every eligible pair in the order they would be tried.
	Candidates []Candidate
}

// Ineligibility reason codes.
const (
	CodeProviderUnknown  = "provider_unknown"
	CodeProviderDisabled = "provider_disabled"
	CodeNoToken          = "no_token_for_method"
	CodeExcluded         = "excluded"
	CodeNoRule           = "no_rule"
	CodeRuleDisabled     = "rule_disabled"
	CodeRailMismatch     = "rail_mismatch"
	CodeCurrencyMismatch = "currency_mismatch"
	CodeBelowMinimum     = "amount_below_minimum"
	CodeAboveMaximum     = "amount_above_maximum"
)

// Ineligibility explains one reason a provider (or one of its rules) fails.
type Ineligibility struct {
	ProviderID string
	RuleID     string // empty for provider-level reasons
	Code       string
	Message    string
}

// Decide returns the highest-priority eligible provider with a readable reason.
func Decide(req Request, snap Snapshot) (Decision, error) {
	candidates := Eligible(req, snap)
	if len(candidates) == 0 {
		return Decision{}, fmt.Errorf("%w for rail %s, currency %s, amount %d", ErrNoEligibleProvider, req.Rail, req.Currency, req.AmountMinor)
	}
	best := candidates[0]
	rule := ruleByID(snap.Rules, best.RuleID)
	reason := fmt.Sprintf("Rule %s (priority %d) matches rail %s, currency %s and amount %d within %s; provider %s is enabled and holds a token for the payment method",
		best.RuleID, best.Priority, req.Rail, req.Currency, req.AmountMinor, bounds(rule), providerLabel(best.ProviderCode, best.ProviderID))
	if others := len(candidates) - 1; others > 0 {
		reason += fmt.Sprintf("; %d lower-priority candidate(s) remain", others)
		var names []string
		for _, c := range candidates[1:] {
			names = append(names, fmt.Sprintf("%s (rule %s, priority %d)", providerLabel(c.ProviderCode, c.ProviderID), c.RuleID, c.Priority))
		}
		reason += ": " + strings.Join(names, ", ") + " eligible but lower priority"
	}
	return Decision{
		ProviderID: best.ProviderID, ProviderCode: best.ProviderCode, RuleID: best.RuleID,
		Priority: best.Priority, Reason: reason, Candidates: candidates,
	}, nil
}

// Eligible lists eligible candidates in priority order.
func Eligible(req Request, snap Snapshot) []Candidate {
	providers := providerIndex(snap.Providers)
	tokens := tokenSet(snap.TokenProviderIDs)
	var out []Candidate
	for _, rule := range snap.Rules {
		p, ok := providers[rule.ProviderID]
		if !ok || !p.Enabled || !tokens[rule.ProviderID] {
			continue
		}
		if _, excluded := req.Excluded[rule.ProviderID]; excluded {
			continue
		}
		if len(ruleMismatches(req, rule)) > 0 {
			continue
		}
		out = append(out, Candidate{ProviderID: p.ID, ProviderCode: p.Code, RuleID: rule.ID, Priority: rule.Priority})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].RuleID < out[j].RuleID
	})
	return out
}

// Explain returns why providerID is ineligible for req; an empty result means it
// is eligible. Rule-level reasons are reported for every rule of that provider.
func Explain(req Request, snap Snapshot, providerID string) []Ineligibility {
	var out []Ineligibility
	add := func(ruleID, code, msg string) {
		out = append(out, Ineligibility{ProviderID: providerID, RuleID: ruleID, Code: code, Message: msg})
	}
	p, ok := providerIndex(snap.Providers)[providerID]
	if !ok {
		add("", CodeProviderUnknown, fmt.Sprintf("provider %s does not exist in this workspace", providerID))
		return out
	}
	label := providerLabel(p.Code, p.ID)
	if !p.Enabled {
		add("", CodeProviderDisabled, fmt.Sprintf("provider %s is disabled", label))
	}
	if !tokenSet(snap.TokenProviderIDs)[providerID] {
		add("", CodeNoToken, fmt.Sprintf("provider %s holds no token for payment method %s", label, req.MethodID))
	}
	if why, excluded := req.Excluded[providerID]; excluded {
		add("", CodeExcluded, fmt.Sprintf("provider %s is excluded: %s", label, why))
	}
	var rules []Rule
	for _, r := range snap.Rules {
		if r.ProviderID == providerID {
			rules = append(rules, r)
		}
	}
	if len(rules) == 0 {
		add("", CodeNoRule, fmt.Sprintf("no routing rule targets provider %s", label))
		return out
	}
	providerLevel := len(out)
	matched := false
	var ruleReasons []Ineligibility
	for _, r := range rules {
		mismatches := ruleMismatches(req, r)
		if len(mismatches) == 0 {
			matched = true
			continue
		}
		for _, m := range mismatches {
			m.ProviderID = providerID
			ruleReasons = append(ruleReasons, m)
		}
	}
	if !matched {
		out = append(out, ruleReasons...)
	}
	if providerLevel == 0 && matched {
		return nil
	}
	return out
}

func ruleMismatches(req Request, r Rule) []Ineligibility {
	var out []Ineligibility
	add := func(code, msg string) {
		out = append(out, Ineligibility{ProviderID: r.ProviderID, RuleID: r.ID, Code: code, Message: msg})
	}
	if !r.Enabled {
		add(CodeRuleDisabled, fmt.Sprintf("rule %s is disabled", r.ID))
	}
	if r.Rail != req.Rail {
		add(CodeRailMismatch, fmt.Sprintf("rule %s applies to rail %s, not %s", r.ID, r.Rail, req.Rail))
	}
	if r.Currency != req.Currency {
		add(CodeCurrencyMismatch, fmt.Sprintf("rule %s applies to currency %s, not %s", r.ID, r.Currency, req.Currency))
	}
	if req.AmountMinor < r.MinAmountMinor {
		add(CodeBelowMinimum, fmt.Sprintf("rule %s requires at least %d minor units; amount is %d", r.ID, r.MinAmountMinor, req.AmountMinor))
	}
	if r.MaxAmountMinor != nil && req.AmountMinor > *r.MaxAmountMinor {
		add(CodeAboveMaximum, fmt.Sprintf("rule %s allows at most %d minor units; amount is %d", r.ID, *r.MaxAmountMinor, req.AmountMinor))
	}
	return out
}

func providerIndex(ps []Provider) map[string]Provider {
	m := make(map[string]Provider, len(ps))
	for _, p := range ps {
		m[p.ID] = p
	}
	return m
}

func tokenSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func ruleByID(rules []Rule, id string) Rule {
	for _, r := range rules {
		if r.ID == id {
			return r
		}
	}
	return Rule{}
}

func bounds(r Rule) string {
	if r.MaxAmountMinor == nil {
		return fmt.Sprintf("[%d, unbounded]", r.MinAmountMinor)
	}
	return fmt.Sprintf("[%d, %d]", r.MinAmountMinor, *r.MaxAmountMinor)
}

func providerLabel(code, id string) string {
	if code == "" {
		return id
	}
	return fmt.Sprintf("%s (%s)", code, id)
}
