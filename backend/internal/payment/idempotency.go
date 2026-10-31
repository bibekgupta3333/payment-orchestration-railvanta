package payment

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// OperationCreatePayment scopes idempotency keys for POST /v1/payments. Keys
// are unique per (workspace_id, operation, key), so another operation may
// reuse the same client key without colliding.
const OperationCreatePayment = "payment.create"

// canonicalVersion is part of the hashed bytes so a future change to the
// canonical form cannot silently match hashes produced by this one.
const canonicalVersion = "railvanta.idempotency.v1"

// CanonicalRequest is the already-validated business content of a request.
// Hashing parsed values rather than raw JSON makes the hash independent of
// field order, whitespace and other encoding choices.
type CanonicalRequest struct {
	Operation         string
	AmountMinor       int64
	Currency          string
	PaymentMethodID   string
	MerchantReference string // empty when absent; absent and "" are equivalent
}

// Canonical returns the exact bytes that RequestHash digests.
//
// Field order is fixed: version, operation, amount_minor, currency,
// payment_method_id, merchant_reference. Normalization:
//   - amount_minor is the base-10 int64 without sign or leading zeros;
//   - currency must already be validated (exact upper-case ISO code);
//   - payment_method_id and merchant_reference are compared byte-for-byte,
//     with no trimming or case folding (whitespace is significant);
//   - an absent merchant_reference is the empty string.
//
// Each field is written as "<byte length>:<value>\n" so no value can be
// crafted to shift a boundary and collide with a different field set.
func (r CanonicalRequest) Canonical() string {
	var builder strings.Builder
	for _, field := range []string{
		canonicalVersion,
		r.Operation,
		strconv.FormatInt(r.AmountMinor, 10),
		r.Currency,
		r.PaymentMethodID,
		r.MerchantReference,
	} {
		builder.WriteString(strconv.Itoa(len(field)))
		builder.WriteByte(':')
		builder.WriteString(field)
		builder.WriteByte('\n')
	}
	return builder.String()
}

// Hash returns the lower-case hex SHA-256 of Canonical (64 characters).
func (r CanonicalRequest) Hash() string {
	sum := sha256.Sum256([]byte(r.Canonical()))
	return hex.EncodeToString(sum[:])
}
