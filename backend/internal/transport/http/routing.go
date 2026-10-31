package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/routing"
)

// RoutingDecisionView is the POST /v1/routing/evaluate response.
type RoutingDecisionView struct {
	ProviderID   string `json:"provider_id"`
	ProviderCode string `json:"provider_code"`
	RuleID       string `json:"rule_id"`
	Priority     int64  `json:"priority"`
	Reason       string `json:"reason"`
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
	decision, err := h.config.Router.Decide(r.Context(), h.db, payment.RouteInput{
		WorkspaceID: principal.WorkspaceID, PaymentMethodID: input.PaymentMethodID, Currency: input.Currency, AmountMinor: amount,
	})
	switch {
	case err == nil:
	case errors.Is(err, routing.ErrMethodNotFound):
		fail(w, 404, "not_found", "Payment method not found", id)
		return
	case errors.Is(err, routing.ErrNoEligibleProvider):
		fail(w, 422, "no_eligible_provider", "No enabled provider and routing rule match this payment method, currency and amount", id)
		return
	default:
		fail(w, 503, "unavailable", "Routing evaluation unavailable", id)
		return
	}
	writeJSON(w, 200, RoutingDecisionView{
		ProviderID: decision.ProviderID, ProviderCode: decision.ProviderCode, RuleID: decision.RuleID, Priority: decision.Priority, Reason: decision.Reason,
	})
}
