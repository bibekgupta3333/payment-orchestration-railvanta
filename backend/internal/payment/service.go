package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// OutboxPaymentDispatch is the outbox command the dispatcher (WBS 2.2) claims
// to route and submit an accepted payment.
const OutboxPaymentDispatch = "payment.dispatch"

// DispatchCommand is the JSON payload of a payment.dispatch outbox row. The
// dispatcher re-reads the payment; the payload carries identity only.
type DispatchCommand struct {
	PaymentID   string `json:"payment_id"`
	WorkspaceID string `json:"workspace_id"`
}

// DispatchDeduplicationKey is unique per workspace, so one payment can never
// receive two dispatch commands.
func DispatchDeduplicationKey(paymentID string) string {
	return OutboxPaymentDispatch + ":" + paymentID
}

// AcceptStage names fault-injection points inside the acceptance transaction.
type AcceptStage string

const (
	StageAfterReservation   AcceptStage = "after_reservation"
	StageAfterPaymentInsert AcceptStage = "after_payment_insert"
	StageAfterTransition    AcceptStage = "after_transition"
	StageAfterOutbox        AcceptStage = "after_outbox"
	StageBeforeCommit       AcceptStage = "before_commit"
)

var (
	// ErrInvalidRequest means the service received an unvalidated request.
	ErrInvalidRequest = errors.New("invalid payment request")
	// ErrIdempotencyConflict means the key was used with a different request hash.
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
	// ErrIdempotencyInProgress means a pending reservation exists without a
	// stored response. The client should retry the same key later.
	ErrIdempotencyInProgress = errors.New("idempotency key is still being processed")
	// ErrPaymentMethodNotFound means no method with that ID exists in the workspace.
	ErrPaymentMethodNotFound = errors.New("payment method not found")
	// ErrPaymentMethodUnusable means the method has no provider token.
	ErrPaymentMethodUnusable = errors.New("payment method has no provider token")
	// ErrBusy means bounded SQLITE_BUSY retries were exhausted; nothing committed.
	ErrBusy = errors.New("database busy")

	errReservationRace = errors.New("idempotency reservation lost a race")
)

// Options configures a Service. Zero values select production defaults.
type Options struct {
	Metrics *Metrics
	// MaxBusyAttempts bounds whole-transaction retries on SQLITE_BUSY (default 4).
	MaxBusyAttempts int
	// BaseBackoff and MaxBackoff bound full-jitter exponential backoff.
	BaseBackoff, MaxBackoff time.Duration
	// Now overrides the clock in tests.
	Now func() time.Time
	// FaultInjector, when set, runs at each AcceptStage inside the transaction;
	// a returned error rolls the whole acceptance back. Tests only.
	FaultInjector func(AcceptStage) error
}

// Service owns the payment acceptance unit of work.
type Service struct {
	db      *gorm.DB
	options Options
}

// NewService creates a payment service over the store's root handle.
func NewService(db *gorm.DB, options Options) *Service {
	if options.Metrics == nil {
		options.Metrics = NewMetrics()
	}
	if options.MaxBusyAttempts <= 0 {
		options.MaxBusyAttempts = 4
	}
	if options.BaseBackoff <= 0 {
		options.BaseBackoff = 10 * time.Millisecond
	}
	if options.MaxBackoff <= 0 {
		options.MaxBackoff = 250 * time.Millisecond
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{db: db, options: options}
}

// Metrics returns the service's acceptance metrics.
func (s *Service) Metrics() *Metrics { return s.options.Metrics }

// AcceptRequest is a validated create-payment request. WorkspaceID and
// ActorReference come from the authenticated principal, never the body.
type AcceptRequest struct {
	WorkspaceID       string
	ActorReference    string
	IdempotencyKey    string
	AmountMinor       int64
	Currency          string
	PaymentMethodID   string
	MerchantReference string // empty when absent
}

// AcceptResult is the stored (or freshly stored) acceptance response.
type AcceptResult struct {
	StatusCode int
	Body       []byte
	PaymentID  string
	Replayed   bool
}

// Canonical returns the hashing input for this request.
func (r AcceptRequest) Canonical() CanonicalRequest {
	return CanonicalRequest{Operation: OperationCreatePayment, AmountMinor: r.AmountMinor, Currency: r.Currency, PaymentMethodID: r.PaymentMethodID, MerchantReference: r.MerchantReference}
}

func (r AcceptRequest) validate() error {
	switch {
	case r.WorkspaceID == "" || r.ActorReference == "":
		return fmt.Errorf("%w: missing authenticated scope", ErrInvalidRequest)
	case len(r.IdempotencyKey) < 1 || len(r.IdempotencyKey) > 255:
		return fmt.Errorf("%w: idempotency key must be 1-255 bytes", ErrInvalidRequest)
	case r.AmountMinor <= 0:
		return fmt.Errorf("%w: amount must be positive", ErrInvalidRequest)
	case r.PaymentMethodID == "":
		return fmt.Errorf("%w: payment method is required", ErrInvalidRequest)
	case len(r.MerchantReference) > 255:
		return fmt.Errorf("%w: merchant reference exceeds 255 bytes", ErrInvalidRequest)
	}
	return ValidateCurrency(r.Currency)
}

// Accept runs the acceptance unit of work: one short transaction that reserves
// (workspace_id, operation, key), inserts the payment, writes the initial
// NULL -> created audit transition, appends a payment.dispatch outbox command
// and stores the 202 response. A reused key with the same hash replays the
// stored response; a different hash returns ErrIdempotencyConflict. Only this
// transaction is retried on SQLITE_BUSY: it has no external side effects.
func (s *Service) Accept(ctx context.Context, request AcceptRequest) (AcceptResult, error) {
	if err := request.validate(); err != nil {
		return AcceptResult{}, err
	}
	started := time.Now()
	defer func() { s.options.Metrics.observeLatency(time.Since(started)) }()
	hash := request.Canonical().Hash()
	busyAttempts, races := 0, 0
	for {
		result, err := s.acceptOnce(ctx, request, hash)
		switch {
		case err == nil:
			if result.Replayed {
				s.options.Metrics.replayed.Add(1)
			} else {
				s.options.Metrics.accepted.Add(1)
			}
			return result, nil
		case errors.Is(err, errReservationRace) && races < 3:
			// A concurrent writer committed the key first; re-read the winner.
			races++
			s.options.Metrics.reservationRace.Add(1)
			continue
		case storage.IsBusy(err):
			busyAttempts++
			s.options.Metrics.busyErrors.Add(1)
			if busyAttempts >= s.options.MaxBusyAttempts {
				s.options.Metrics.busyExhausted.Add(1)
				return AcceptResult{}, fmt.Errorf("%w: %d attempts", ErrBusy, busyAttempts)
			}
			if err := s.sleep(ctx, busyAttempts); err != nil {
				return AcceptResult{}, err
			}
			continue
		case errors.Is(err, ErrIdempotencyConflict):
			s.options.Metrics.conflicts.Add(1)
		}
		return AcceptResult{}, err
	}
}

// sleep waits a full-jitter backoff: uniform in [0, min(max, base*2^attempt)).
func (s *Service) sleep(ctx context.Context, attempt int) error {
	ceiling := s.options.BaseBackoff << min(attempt, 16)
	if ceiling > s.options.MaxBackoff || ceiling <= 0 {
		ceiling = s.options.MaxBackoff
	}
	timer := time.NewTimer(time.Duration(rand.Int64N(int64(ceiling)) + 1))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) fault(stage AcceptStage) error {
	if s.options.FaultInjector == nil {
		return nil
	}
	return s.options.FaultInjector(stage)
}

// replay maps an existing record to a stored response, conflict or in-progress.
func replay(record storage.IdempotencyRecord, hash string) (AcceptResult, error) {
	if record.RequestHash != hash {
		return AcceptResult{}, ErrIdempotencyConflict
	}
	if record.Status != "completed" || record.ResponseStatus == nil || record.ResponseBody == nil || record.PaymentID == nil {
		return AcceptResult{}, ErrIdempotencyInProgress
	}
	return AcceptResult{StatusCode: int(*record.ResponseStatus), Body: []byte(*record.ResponseBody), PaymentID: *record.PaymentID, Replayed: true}, nil
}

func (s *Service) acceptOnce(ctx context.Context, request AcceptRequest, hash string) (AcceptResult, error) {
	var result AcceptResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Replay is decided before method validation so a stored acceptance stays
		// replayable regardless of later configuration changes.
		existing, err := storage.FindIdempotencyRecord(tx, request.WorkspaceID, OperationCreatePayment, request.IdempotencyKey)
		if err == nil {
			result, err = replay(existing, hash)
			return err
		}
		if !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		if _, err := storage.FindPaymentMethod(tx, request.WorkspaceID, request.PaymentMethodID); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrPaymentMethodNotFound
			}
			return err
		}
		tokens, err := storage.CountPaymentMethodTokens(tx, request.WorkspaceID, request.PaymentMethodID)
		if err != nil {
			return err
		}
		if tokens == 0 {
			return ErrPaymentMethodUnusable
		}

		now := s.options.Now().UnixMilli()
		ids := make([]string, 4)
		for i := range ids {
			if ids[i], err = storage.NewID(); err != nil {
				return err
			}
		}
		recordID, paymentID, transitionID, outboxID := ids[0], ids[1], ids[2], ids[3]
		record := storage.IdempotencyRecord{ID: recordID, WorkspaceID: request.WorkspaceID, Operation: OperationCreatePayment, Key: request.IdempotencyKey, RequestHash: hash, Status: "pending", CreatedAt: now}
		if err := storage.InsertIdempotencyRecord(tx, &record); err != nil {
			if storage.IsUniqueViolation(err) {
				return errReservationRace
			}
			return err
		}
		if err := s.fault(StageAfterReservation); err != nil {
			return err
		}

		var merchantReference *string
		if request.MerchantReference != "" {
			reference := request.MerchantReference
			merchantReference = &reference
		}
		payment := storage.Payment{ID: paymentID, WorkspaceID: request.WorkspaceID, PaymentMethodID: request.PaymentMethodID, AmountMinor: request.AmountMinor, Currency: request.Currency, MerchantReference: merchantReference, Status: string(StatusCreated), Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := storage.InsertPayment(tx, &payment); err != nil {
			return err
		}
		if err := s.fault(StageAfterPaymentInsert); err != nil {
			return err
		}

		if err := ValidateTransition("", StatusCreated); err != nil {
			return err
		}
		transition := storage.PaymentStateTransition{ID: transitionID, WorkspaceID: request.WorkspaceID, PaymentID: paymentID, FromStatus: nil, ToStatus: string(StatusCreated), Reason: "payment accepted", ActorReference: request.ActorReference, CreatedAt: now}
		if err := storage.InsertStateTransition(tx, &transition); err != nil {
			return err
		}
		if err := s.fault(StageAfterTransition); err != nil {
			return err
		}

		payload, err := json.Marshal(DispatchCommand{PaymentID: paymentID, WorkspaceID: request.WorkspaceID})
		if err != nil {
			return err
		}
		event := storage.OutboxEvent{ID: outboxID, WorkspaceID: request.WorkspaceID, PaymentID: &paymentID, EventType: OutboxPaymentDispatch, DeduplicationKey: DispatchDeduplicationKey(paymentID), Payload: string(payload), Status: "pending", AvailableAt: now, CreatedAt: now}
		if err := storage.InsertOutboxEvent(tx, &event); err != nil {
			return err
		}
		if err := s.fault(StageAfterOutbox); err != nil {
			return err
		}

		body, err := json.Marshal(NewPaymentView(payment))
		if err != nil {
			return err
		}
		if err := storage.CompleteIdempotencyRecord(tx, request.WorkspaceID, recordID, paymentID, http.StatusAccepted, string(body), now); err != nil {
			return err
		}
		if err := s.fault(StageBeforeCommit); err != nil {
			return err
		}
		result = AcceptResult{StatusCode: http.StatusAccepted, Body: body, PaymentID: paymentID}
		return nil
	})
	if err != nil {
		return AcceptResult{}, err
	}
	return result, nil
}

// Get reads one payment within the workspace.
func (s *Service) Get(ctx context.Context, workspaceID, paymentID string) (storage.Payment, error) {
	return storage.GetPayment(s.db.WithContext(ctx), workspaceID, paymentID)
}

// List reads a newest-first keyset page within the workspace.
func (s *Service) List(ctx context.Context, workspaceID string, filter storage.PaymentListFilter) ([]storage.Payment, error) {
	return storage.ListPayments(s.db.WithContext(ctx), workspaceID, filter)
}

// Attempts reads a payment's attempts after confirming workspace ownership.
func (s *Service) Attempts(ctx context.Context, workspaceID, paymentID string) ([]storage.PaymentAttempt, error) {
	db := s.db.WithContext(ctx)
	if _, err := storage.GetPayment(db, workspaceID, paymentID); err != nil {
		return nil, err
	}
	return storage.ListPaymentAttempts(db, workspaceID, paymentID)
}
