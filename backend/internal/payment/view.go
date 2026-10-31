package payment

import (
	"strconv"
	"time"

	"github.com/railvanta/railvanta/backend/internal/storage"
)

// PaymentView is the API representation. Money is a decimal string so values
// beyond JavaScript's safe integer range survive JSON exactly.
type PaymentView struct {
	ID                string  `json:"id"`
	AmountMinor       string  `json:"amount_minor"`
	Currency          string  `json:"currency"`
	PaymentMethodID   string  `json:"payment_method_id"`
	MerchantReference *string `json:"merchant_reference"`
	Status            string  `json:"status"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

// AttemptView is the API representation of a provider attempt.
type AttemptView struct {
	ID                string  `json:"id"`
	PaymentID         string  `json:"payment_id"`
	ProviderID        string  `json:"provider_id"`
	AttemptNumber     int64   `json:"attempt_number"`
	Status            string  `json:"status"`
	RoutingRuleID     *string `json:"routing_rule_id"`
	RoutingReason     string  `json:"routing_reason"`
	ProviderReference *string `json:"provider_reference"`
	ErrorCode         *string `json:"error_code"`
	SubmittedAt       *string `json:"submitted_at"`
	CompletedAt       *string `json:"completed_at"`
	CreatedAt         string  `json:"created_at"`
}

// FormatTimestamp renders stored UTC Unix milliseconds as RFC 3339 with milliseconds.
func FormatTimestamp(millis int64) string {
	return time.UnixMilli(millis).UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func optionalTimestamp(millis *int64) *string {
	if millis == nil {
		return nil
	}
	value := FormatTimestamp(*millis)
	return &value
}

// NewPaymentView maps a stored payment. Stored amounts are positive by schema,
// so plain base-10 formatting is exact.
func NewPaymentView(p storage.Payment) PaymentView {
	return PaymentView{
		ID: p.ID, AmountMinor: strconv.FormatInt(p.AmountMinor, 10), Currency: p.Currency,
		PaymentMethodID: p.PaymentMethodID, MerchantReference: p.MerchantReference, Status: p.Status,
		CreatedAt: FormatTimestamp(p.CreatedAt), UpdatedAt: FormatTimestamp(p.UpdatedAt),
	}
}

// NewAttemptView maps a stored attempt without exposing the provider idempotency key.
func NewAttemptView(a storage.PaymentAttempt) AttemptView {
	return AttemptView{
		ID: a.ID, PaymentID: a.PaymentID, ProviderID: a.ProviderID, AttemptNumber: a.AttemptNumber, Status: a.Status,
		RoutingRuleID: a.RoutingRuleID, RoutingReason: a.RoutingReason, ProviderReference: a.ProviderReference,
		ErrorCode: a.ErrorCode, SubmittedAt: optionalTimestamp(a.SubmittedAt), CompletedAt: optionalTimestamp(a.CompletedAt),
		CreatedAt: FormatTimestamp(a.CreatedAt),
	}
}
