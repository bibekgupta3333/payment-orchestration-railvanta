package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// servePayments routes /v1/payments*. It reports false for unmatched routes.
func (h *handler) servePayments(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) bool {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/payments")
	switch {
	case rest == "" && r.Method == http.MethodPost:
		h.createPayment(w, r, principal, id)
	case rest == "" && r.Method == http.MethodGet:
		h.listPayments(w, r, principal, id)
	case strings.HasPrefix(rest, "/") && r.Method == http.MethodPost:
		paymentID, suffix, _ := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
		if paymentID == "" || suffix != "cancel" {
			return false
		}
		h.cancelPayment(w, r, principal, id, paymentID)
	case strings.HasPrefix(rest, "/") && r.Method == http.MethodGet:
		paymentID, suffix, _ := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
		switch {
		case paymentID != "" && suffix == "recovery":
			h.getRecovery(w, r, principal, id, paymentID)
		case paymentID != "" && suffix == "" && !strings.HasSuffix(rest, "/"):
			h.getPayment(w, r, principal, id, paymentID)
		case paymentID != "" && suffix == "attempts":
			h.listAttempts(w, r, principal, id, paymentID)
		default:
			return false
		}
	default:
		return false
	}
	return true
}

func actorReference(principal auth.Principal) string {
	if principal.Cookie {
		return "session:" + principal.Role
	}
	return "api_key:" + principal.Role
}

func (h *handler) createPayment(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	// Every validation failure returns before the service runs, so validation
	// and authorization failures never create an idempotency record.
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 1 || len(key) > 255 {
		fail(w, 400, "validation_error", "A 1–255 character Idempotency-Key is required", id, FieldError{Field: "Idempotency-Key", Message: "Required header"})
		return
	}
	if r.Header.Get("Content-Type") != "application/json" && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json;") {
		fail(w, 415, "unsupported_media_type", "Use application/json", id)
		return
	}
	var input struct {
		AmountMinor       string  `json:"amount_minor"`
		Currency          string  `json:"currency"`
		PaymentMethodID   string  `json:"payment_method_id"`
		MerchantReference *string `json:"merchant_reference,omitempty"`
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
	reference := ""
	if input.MerchantReference != nil {
		reference = *input.MerchantReference
	}
	if len(reference) > 255 {
		fields = append(fields, FieldError{Field: "merchant_reference", Message: "At most 255 bytes"})
	}
	if len(fields) > 0 {
		fail(w, 400, "validation_error", "Invalid payment request", id, fields...)
		return
	}
	result, err := h.payments.Accept(r.Context(), payment.AcceptRequest{
		WorkspaceID: principal.WorkspaceID, ActorReference: actorReference(principal), IdempotencyKey: key,
		AmountMinor: amount, Currency: input.Currency, PaymentMethodID: input.PaymentMethodID, MerchantReference: reference,
	})
	switch {
	case err == nil:
	case errors.Is(err, payment.ErrIdempotencyConflict):
		fail(w, 409, "idempotency_conflict", "This Idempotency-Key was used with a different request", id)
		return
	case errors.Is(err, payment.ErrIdempotencyInProgress):
		w.Header().Set("Retry-After", "1")
		fail(w, 409, "idempotency_in_progress", "A request with this Idempotency-Key is still being processed; retry with the same key", id)
		return
	case errors.Is(err, payment.ErrPaymentMethodNotFound):
		fail(w, 404, "not_found", "Payment method not found", id)
		return
	case errors.Is(err, payment.ErrPaymentMethodUnusable):
		fail(w, 422, "payment_method_unusable", "Payment method has no provider token", id, FieldError{Field: "payment_method_id", Message: "No provider token"})
		return
	case errors.Is(err, payment.ErrBusy):
		w.Header().Set("Retry-After", "1")
		fail(w, 503, "unavailable", "Payment acceptance is temporarily unavailable; retry with the same key", id)
		return
	default:
		h.config.Logger.Error("payment acceptance failed", "request_id", id)
		fail(w, 500, "internal_error", "Payment acceptance failed; retry with the same key", id)
		return
	}
	if !result.Replayed && h.config.OnAccepted != nil {
		h.config.OnAccepted()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", "/v1/payments/"+result.PaymentID)
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	w.WriteHeader(result.StatusCode)
	_, _ = w.Write(result.Body)
}

func encodeCursor(cursor storage.PaymentCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(cursor.CreatedAt, 10) + "." + cursor.ID))
}

func decodeCursor(value string) (*storage.PaymentCursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, false
	}
	created, paymentID, ok := strings.Cut(string(raw), ".")
	at, err := strconv.ParseInt(created, 10, 64)
	if !ok || err != nil || at < 0 || paymentID == "" || len(paymentID) > 64 {
		return nil, false
	}
	return &storage.PaymentCursor{CreatedAt: at, ID: paymentID}, true
}

func (h *handler) listPayments(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	query := r.URL.Query()
	filter := storage.PaymentListFilter{Limit: defaultPageSize}
	var fields []FieldError
	if value := query.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > maxPageSize {
			fields = append(fields, FieldError{Field: "limit", Message: "Use an integer from 1 to 100"})
		}
		filter.Limit = limit
	}
	if value := query.Get("status"); value != "" {
		status, err := payment.ParseStatus(value)
		if err != nil {
			fields = append(fields, FieldError{Field: "status", Message: "Use a documented payment status"})
		}
		filter.Status = string(status)
	}
	if value := query.Get("cursor"); value != "" {
		cursor, ok := decodeCursor(value)
		if !ok {
			fields = append(fields, FieldError{Field: "cursor", Message: "Use next_cursor from a previous page"})
		}
		filter.After = cursor
	}
	if len(fields) > 0 {
		fail(w, 400, "validation_error", "Invalid list parameters", id, fields...)
		return
	}
	pageSize := filter.Limit
	filter.Limit++ // read one extra row to learn whether another page exists
	payments, err := h.payments.List(r.Context(), principal.WorkspaceID, filter)
	if err != nil {
		fail(w, 503, "unavailable", "Payments unavailable", id)
		return
	}
	var next *string
	if len(payments) > pageSize {
		payments = payments[:pageSize]
		last := payments[len(payments)-1]
		cursor := encodeCursor(storage.PaymentCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		next = &cursor
	}
	items := make([]payment.PaymentView, 0, len(payments))
	for _, p := range payments {
		items = append(items, payment.NewPaymentView(p))
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
}

func (h *handler) getPayment(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, paymentID string) {
	p, err := h.payments.Get(r.Context(), principal.WorkspaceID, paymentID)
	if errors.Is(err, storage.ErrNotFound) {
		fail(w, 404, "not_found", "Payment not found", id)
		return
	}
	if err != nil {
		fail(w, 503, "unavailable", "Payment unavailable", id)
		return
	}
	writeJSON(w, 200, payment.NewPaymentView(p))
}

func (h *handler) listAttempts(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, paymentID string) {
	attempts, err := h.payments.Attempts(r.Context(), principal.WorkspaceID, paymentID)
	if errors.Is(err, storage.ErrNotFound) {
		fail(w, 404, "not_found", "Payment not found", id)
		return
	}
	if err != nil {
		fail(w, 503, "unavailable", "Attempts unavailable", id)
		return
	}
	items := make([]payment.AttemptView, 0, len(attempts))
	for _, a := range attempts {
		items = append(items, payment.NewAttemptView(a))
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
}

// cancelPayment serves POST /v1/payments/{id}/cancel (WBS 2.4.5): 200 with the
// canceled payment (also when it already was), 409 once dispatch has claimed it.
func (h *handler) cancelPayment(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, paymentID string) {
	result, err := h.payments.Cancel(r.Context(), payment.CancelRequest{WorkspaceID: principal.WorkspaceID, PaymentID: paymentID, ActorReference: actorReference(principal)})
	switch {
	case err == nil:
		writeJSON(w, 200, payment.NewPaymentView(result.Payment))
	case errors.Is(err, storage.ErrNotFound):
		fail(w, 404, "not_found", "Payment not found", id)
	case errors.Is(err, payment.ErrNotCancelable):
		fail(w, 409, "not_cancelable", "Payment is "+result.Payment.Status+"; only a created payment can be canceled", id)
	case errors.Is(err, payment.ErrBusy):
		w.Header().Set("Retry-After", "1")
		fail(w, 503, "unavailable", "Cancellation is temporarily unavailable; retry", id)
	default:
		h.config.Logger.Error("payment cancellation failed", "request_id", id)
		fail(w, 500, "internal_error", "Cancellation failed", id)
	}
}

// getRecovery serves GET /v1/payments/{id}/recovery: whether the payment is
// awaiting a provider lookup, how many were made, and late provider results
// recorded for reconciliation. It exposes no provider key.
func (h *handler) getRecovery(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, paymentID string) {
	view, err := h.payments.Recovery(r.Context(), principal.WorkspaceID, paymentID)
	if errors.Is(err, storage.ErrNotFound) {
		fail(w, 404, "not_found", "Payment not found", id)
		return
	}
	if err != nil {
		fail(w, 503, "unavailable", "Recovery state unavailable", id)
		return
	}
	writeJSON(w, 200, view)
}
