package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/routing"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// RoutingDecisionView is the POST /v1/routing/evaluate response.
type RoutingDecisionView struct {
	ProviderID   string `json:"provider_id"`
	ProviderCode string `json:"provider_code"`
	// RuleID and Priority are null only for a blocked failover whose original
	// attempt recorded no rule.
	RuleID          *string                `json:"rule_id"`
	Priority        *int64                 `json:"priority"`
	Reason          string                 `json:"reason"`
	Rule            *RoutingRuleView       `json:"rule"`
	Checks          []RoutingCheckView     `json:"checks"`
	Candidates      []RoutingCandidateView `json:"candidates"`
	FailoverBlocked bool                   `json:"failover_blocked"`
	// Alternatives explains every provider in the workspace: why it was chosen,
	// is eligible but lower priority, is ineligible, or is blocked from failover.
	Alternatives []RoutingAlternativeView `json:"alternatives"`
}

// RoutingAlternativeView is one provider's routing verdict. Status is one of
// chosen, pinned (unresolved prior attempt: lookup only), eligible_lower_priority,
// ineligible or blocked (unsafe failover blocked: prior attempt unresolved).
type RoutingAlternativeView struct {
	ProviderID   string   `json:"provider_id"`
	ProviderCode string   `json:"provider_code"`
	Status       string   `json:"status"`
	Eligible     bool     `json:"eligible"`
	RuleID       *string  `json:"rule_id"`
	Priority     *int64   `json:"priority"`
	Reasons      []string `json:"reasons"`
}

// buildAlternatives explains each provider against the request without the
// caller's exclusions, so a provider blocked only by the failover guard still
// shows whether it would otherwise be eligible.
func buildAlternatives(req routing.Request, snap routing.Snapshot, chosenID string, blocked bool, hasAdapter func(string) bool) []RoutingAlternativeView {
	probe := req
	probe.Excluded = map[string]string{}
	for _, p := range snap.Providers {
		if !hasAdapter(p.ID) {
			probe.Excluded[p.ID] = "no adapter is configured for provider code " + p.Code
		}
	}
	eligible := map[string]routing.Candidate{}
	for _, c := range routing.Eligible(probe, snap) {
		if _, seen := eligible[c.ProviderID]; !seen {
			eligible[c.ProviderID] = c
		}
	}
	out := []RoutingAlternativeView{}
	for _, p := range snap.Providers {
		view := RoutingAlternativeView{ProviderID: p.ID, ProviderCode: p.Code, Reasons: []string{}}
		c, ok := eligible[p.ID]
		view.Eligible = ok
		if ok {
			rule, prio := c.RuleID, c.Priority
			view.RuleID, view.Priority = &rule, &prio
		} else {
			for _, why := range routing.Explain(probe, snap, p.ID) {
				view.Reasons = append(view.Reasons, why.Message)
			}
		}
		switch {
		case p.ID == chosenID && blocked:
			view.Status = "pinned"
			view.Reasons = append(view.Reasons, "prior attempt is unresolved: only a lookup at this same provider with the stored key is allowed")
		case blocked:
			view.Status = "blocked"
			view.Reasons = append(view.Reasons, "unsafe failover blocked: prior attempt unresolved")
		case p.ID == chosenID:
			view.Status = "chosen"
		case ok:
			view.Status = "eligible_lower_priority"
			view.Reasons = append(view.Reasons, fmt.Sprintf("eligible via rule %s (priority %d) but lower priority than the chosen provider", c.RuleID, c.Priority))
		default:
			view.Status = "ineligible"
		}
		out = append(out, view)
	}
	return out
}

// evaluateRouting answers which provider a payment with this input would be
// dispatched to, through the same payment.Router the dispatcher uses. It only
// reads: no payment, attempt, idempotency record or outbox row is written.
func (h *handler) evaluateRouting(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	if r.Header.Get("Content-Type") != "application/json" && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json;") {
		fail(w, 415, "unsupported_media_type", "Use application/json", id)
		return
	}
	var input struct {
		PaymentMethodID string `json:"payment_method_id"`
		AmountMinor     string `json:"amount_minor"`
		Currency        string `json:"currency"`
		// PaymentID optionally names an existing payment; an unresolved prior
		// attempt then blocks routing to any other provider.
		PaymentID string `json:"payment_id"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			fail(w, 413, "body_too_large", "Request body exceeds the limit", id)
			return
		}
		fail(w, 400, "validation_error", "Malformed request or invalid field types", id, FieldError{Field: "body", Message: "Use the documented request schema; amount_minor is a decimal string"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		fail(w, 400, "validation_error", "Exactly one JSON value is required", id, FieldError{Field: "body", Message: "Trailing content after the JSON object"})
		return
	}
	var fields []FieldError
	if err := payment.ValidateCurrency(input.Currency); err != nil {
		fields = append(fields, FieldError{Field: "currency", Message: "Use a supported upper-case currency code"})
	}
	amount, err := payment.ParseAmount(input.AmountMinor)
	if err != nil {
		fields = append(fields, FieldError{Field: "amount_minor", Message: "Use a positive integer decimal string no greater than 9223372036854775807"})
	}
	if input.PaymentMethodID == "" || len(input.PaymentMethodID) > 64 {
		fields = append(fields, FieldError{Field: "payment_method_id", Message: "Required payment method ID"})
	}
	if len(fields) > 0 {
		fail(w, 400, "validation_error", "Invalid routing evaluation request", id, fields...)
		return
	}
	if len(input.PaymentID) > 64 {
		fail(w, 400, "validation_error", "Invalid routing evaluation request", id, FieldError{Field: "payment_id", Message: "Use a payment ID"})
		return
	}
	ctx := r.Context()
	snapshot, rail, loadErr := routing.Load(ctx, h.db, principal.WorkspaceID, input.PaymentMethodID)
	switch {
	case loadErr == nil:
	case errors.Is(loadErr, routing.ErrMethodNotFound):
		fail(w, 404, "not_found", "Payment method not found", id)
		return
	default:
		fail(w, 503, "unavailable", "Routing evaluation unavailable", id)
		return
	}
	req := routing.Request{WorkspaceID: principal.WorkspaceID, MethodID: input.PaymentMethodID, Rail: rail, Currency: input.Currency, AmountMinor: amount, Excluded: map[string]string{}}
	hasAdapter := func(providerID string) bool {
		for _, p := range snapshot.Providers {
			if p.ID == providerID {
				_, ok := h.config.Router.Providers[p.Code]
				return h.config.Router.Providers == nil || ok
			}
		}
		return false
	}
	codes := map[string]string{}
	for _, p := range snapshot.Providers {
		codes[p.ID] = p.Code
	}

	// An unresolved prior attempt means the payment may already have executed:
	// the only safe next step is a lookup at that same provider, never routing
	// to another one.
	if input.PaymentID != "" {
		if _, err := storage.GetPayment(h.db.WithContext(ctx), principal.WorkspaceID, input.PaymentID); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				fail(w, 404, "not_found", "Payment not found", id)
			} else {
				fail(w, 503, "unavailable", "Routing evaluation unavailable", id)
			}
			return
		}
		attempts, err := storage.ListPaymentAttempts(h.db.WithContext(ctx), principal.WorkspaceID, input.PaymentID)
		if err != nil {
			fail(w, 503, "unavailable", "Routing evaluation unavailable", id)
			return
		}
		for _, attempt := range attempts {
			if attempt.Status != payment.AttemptUnknown && attempt.Status != payment.AttemptSubmitted && attempt.Status != payment.AttemptCreated {
				continue
			}
			reason := fmt.Sprintf("Unsafe failover blocked: attempt %d at provider %s is %s, so the payment may already have executed. It is resolved by a lookup at that same provider with the stored key; it is never routed to another provider.",
				attempt.AttemptNumber, providerLabelFor(codes, attempt.ProviderID), attempt.Status)
			req.Excluded[attempt.ProviderID] = "unresolved prior attempt"
			var rule *routing.Rule
			var ruleView *RoutingRuleView
			var ruleID *string
			var priority *int64
			for i := range snapshot.Rules {
				if attempt.RoutingRuleID != nil && snapshot.Rules[i].ID == *attempt.RoutingRuleID {
					rule = &snapshot.Rules[i]
					view := ruleViewFromRouting(*rule, codes[rule.ProviderID])
					ruleView, ruleID, priority = &view, &rule.ID, &rule.Priority
				}
			}
			writeJSON(w, 200, RoutingDecisionView{
				ProviderID: attempt.ProviderID, ProviderCode: codes[attempt.ProviderID], RuleID: ruleID, Priority: priority, Reason: reason,
				Rule: ruleView, Checks: buildChecks(req, snapshot, attempt.ProviderID, rule, hasAdapter(attempt.ProviderID), reason),
				Candidates: []RoutingCandidateView{}, FailoverBlocked: true,
				Alternatives: buildAlternatives(req, snapshot, attempt.ProviderID, true, hasAdapter),
			})
			return
		}
	}

	decision, err := h.config.Router.Decide(ctx, h.db, payment.RouteInput{
		WorkspaceID: principal.WorkspaceID, PaymentMethodID: input.PaymentMethodID, Currency: input.Currency, AmountMinor: amount,
	})
	switch {
	case err == nil:
	case errors.Is(err, routing.ErrMethodNotFound):
		fail(w, 404, "not_found", "Payment method not found", id)
		return
	case errors.Is(err, routing.ErrNoEligibleProvider):
		// Explain each provider through field_errors so the dashboard can show
		// why nothing matched.
		var fields []FieldError
		explainReq := req
		explainReq.Excluded = map[string]string{}
		for _, p := range snapshot.Providers {
			if !hasAdapter(p.ID) {
				explainReq.Excluded[p.ID] = "no adapter is configured for provider code " + p.Code
			}
		}
		for _, p := range snapshot.Providers {
			for _, why := range routing.Explain(explainReq, snapshot, p.ID) {
				fields = append(fields, FieldError{Field: "provider:" + providerLabelFor(codes, p.ID), Message: why.Message})
			}
		}
		fail(w, 422, "no_eligible_provider", "No enabled provider and routing rule match this payment method, currency and amount", id, fields...)
		return
	default:
		fail(w, 503, "unavailable", "Routing evaluation unavailable", id)
		return
	}
	var matched *routing.Rule
	var ruleView *RoutingRuleView
	for i := range snapshot.Rules {
		if snapshot.Rules[i].ID == decision.RuleID {
			matched = &snapshot.Rules[i]
			view := ruleViewFromRouting(*matched, decision.ProviderCode)
			ruleView = &view
		}
	}
	candidates := make([]RoutingCandidateView, 0, len(decision.Candidates))
	for _, c := range decision.Candidates {
		candidates = append(candidates, RoutingCandidateView{ProviderID: c.ProviderID, ProviderCode: c.ProviderCode, RuleID: c.RuleID, Priority: c.Priority})
	}
	ruleID, priority := decision.RuleID, decision.Priority
	writeJSON(w, 200, RoutingDecisionView{
		ProviderID: decision.ProviderID, ProviderCode: decision.ProviderCode, RuleID: &ruleID, Priority: &priority, Reason: decision.Reason,
		Rule: ruleView, Checks: buildChecks(req, snapshot, decision.ProviderID, matched, hasAdapter(decision.ProviderID), ""), Candidates: candidates,
		Alternatives: buildAlternatives(req, snapshot, decision.ProviderID, false, hasAdapter),
	})
}

func providerLabelFor(codes map[string]string, id string) string {
	if code := codes[id]; code != "" {
		return code
	}
	return id
}
