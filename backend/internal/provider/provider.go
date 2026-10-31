// Package provider defines the boundary between payment execution and payment
// providers. It must not import payment-domain packages: adapters see only the
// provider-neutral request below and report one of three typed outcomes.
//
// Outcome contract:
//   - Succeeded: the provider executed the charge; ProviderReference is set.
//   - Declined: the provider proved the request had no money-moving effect.
//   - Ambiguous: anything else (timeouts, outages, storage or transport errors,
//     cancelled contexts). Callers must resolve it with Lookup against the SAME
//     provider and the SAME idempotency key, never by submitting elsewhere.
package provider

import (
	"context"
	"errors"
)

// Outcome is the only information a caller may branch on after Submit.
type Outcome string

const (
	// OutcomeSucceeded means the charge executed at the provider.
	OutcomeSucceeded Outcome = "succeeded"
	// OutcomeDeclined means the provider proved the request had no effect.
	OutcomeDeclined Outcome = "declined"
	// OutcomeAmbiguous means the effect is unknown and must be looked up.
	OutcomeAmbiguous Outcome = "ambiguous"
)

// Stable result codes shared by adapters. Adapters may add provider-specific codes.
const (
	CodeDeclined            = "declined"
	CodeTimeout             = "timeout"
	CodeDeadlineExceeded    = "deadline_exceeded"
	CodeProviderUnavailable = "provider_unavailable"
	CodeIdempotencyConflict = "idempotency_key_conflict"
	CodeStorageError        = "provider_storage_error"
	CodeInvalidRequest      = "invalid_request"
)

// ErrUnavailable is returned by Lookup when the provider cannot answer; it is
// distinct from a successful lookup that finds no record (LookupResult.Found false).
var ErrUnavailable = errors.New("provider unavailable")

// ErrInvalidRequest marks a request rejected locally before reaching the provider.
var ErrInvalidRequest = errors.New("invalid provider request")

// SubmitRequest is provider-neutral; IdempotencyKey must be stable for the attempt.
type SubmitRequest struct {
	WorkspaceID    string
	ProviderID     string
	IdempotencyKey string
	AmountMinor    int64
	Currency       string
	// TokenReference is the provider-specific opaque token for the payment method.
	TokenReference string
}

// Validate rejects requests that can never be sent.
func (r SubmitRequest) Validate() error {
	switch {
	case r.WorkspaceID == "", r.ProviderID == "", r.IdempotencyKey == "":
		return errors.Join(ErrInvalidRequest, errors.New("workspace, provider and idempotency key are required"))
	case r.AmountMinor <= 0:
		return errors.Join(ErrInvalidRequest, errors.New("amount_minor must be positive"))
	case !validCurrency(r.Currency):
		return errors.Join(ErrInvalidRequest, errors.New("currency must be three upper-case letters"))
	case r.TokenReference == "":
		return errors.Join(ErrInvalidRequest, errors.New("token reference is required"))
	}
	return nil
}

func validCurrency(c string) bool {
	if len(c) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		if c[i] < 'A' || c[i] > 'Z' {
			return false
		}
	}
	return true
}

// Result is the typed outcome of a Submit or of a found Lookup.
type Result struct {
	Outcome Outcome
	// ProviderReference is set whenever the provider recorded the request.
	ProviderReference string
	// Code is a stable machine-readable reason; empty for plain success.
	Code string
	// Cause carries the underlying error for Ambiguous results (logging only).
	Cause error
	// Replayed is true when the provider returned an existing charge for the key.
	Replayed bool
}

// LookupRequest identifies an earlier submission by its provider idempotency key.
type LookupRequest struct {
	WorkspaceID    string
	ProviderID     string
	IdempotencyKey string
}

// LookupResult distinguishes "the provider has no record" (Found false) from a
// recorded outcome. Provider unavailability is reported as ErrUnavailable instead.
type LookupResult struct {
	Found  bool
	Result Result
}

// Capabilities describes what the caller may rely on.
type Capabilities struct {
	Code string
	// IdempotentReplay: resubmitting the same key returns the original charge.
	IdempotentReplay bool
	// LookupByIdempotencyKey: Lookup can resolve Ambiguous outcomes.
	LookupByIdempotencyKey bool
	Rails                  []string
}

// Provider is implemented by every adapter. Implementations must honour context
// deadlines, must be safe for concurrent use, and are called outside the
// caller's database transactions.
type Provider interface {
	Capabilities() Capabilities
	// Submit never returns an error: every failure maps to a typed Result.
	Submit(ctx context.Context, req SubmitRequest) Result
	// Lookup returns (LookupResult{Found:false}, nil) when the provider has no
	// record of the key, and an error wrapping ErrUnavailable when it cannot answer.
	Lookup(ctx context.Context, req LookupRequest) (LookupResult, error)
}

// Ambiguous builds an Ambiguous result with a code and cause.
func Ambiguous(code string, cause error) Result {
	return Result{Outcome: OutcomeAmbiguous, Code: code, Cause: cause}
}

// ContextCode maps a context error to a result code.
func ContextCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeDeadlineExceeded
	}
	return CodeTimeout
}
