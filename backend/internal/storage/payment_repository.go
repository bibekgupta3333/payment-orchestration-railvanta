package storage

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// Repository functions in this file take the caller's handle: pass the tx from
// Store.WithTx so every write in a unit of work commits or rolls back together.
// Every query carries a workspace_id predicate; IDs alone never authorize.

var (
	// ErrNotFound means no row matched within the caller's workspace.
	ErrNotFound = errors.New("record not found")
	// ErrStaleVersion means a guarded update matched no row: the status or
	// version changed concurrently, or the row is in another workspace.
	ErrStaleVersion = errors.New("stale version or status")
)

// IsUniqueViolation reports a SQLite unique/primary-key constraint failure.
// The gorm sqlite driver returns raw driver errors, so match the stable text.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "UNIQUE constraint failed") || strings.Contains(message, "PRIMARY KEY constraint failed")
}

// IsBusy reports SQLITE_BUSY or SQLITE_LOCKED after busy_timeout expired.
func IsBusy(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked") ||
		strings.Contains(message, "SQLITE_BUSY") || strings.Contains(message, "SQLITE_LOCKED")
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// FindPaymentMethod reads one method owned by the workspace.
func FindPaymentMethod(tx *gorm.DB, workspaceID, methodID string) (PaymentMethod, error) {
	var method PaymentMethod
	err := tx.Where("workspace_id = ? AND id = ?", workspaceID, methodID).Take(&method).Error
	return method, notFound(err)
}

// CountPaymentMethodTokens counts provider tokens for a workspace-owned method.
func CountPaymentMethodTokens(tx *gorm.DB, workspaceID, methodID string) (int64, error) {
	var count int64
	err := tx.Model(&PaymentMethodToken{}).Where("workspace_id = ? AND method_id = ?", workspaceID, methodID).Count(&count).Error
	return count, err
}

// InsertPayment inserts a new payment row exactly as given.
func InsertPayment(tx *gorm.DB, payment *Payment) error {
	return tx.Create(payment).Error
}

// GetPayment reads one payment within the workspace.
func GetPayment(tx *gorm.DB, workspaceID, paymentID string) (Payment, error) {
	var payment Payment
	err := tx.Where("workspace_id = ? AND id = ?", workspaceID, paymentID).Take(&payment).Error
	return payment, notFound(err)
}

// PaymentCursor is the keyset position after the last returned row.
type PaymentCursor struct {
	CreatedAt int64
	ID        string
}

// PaymentListFilter selects a page. Every field except Limit is optional.
// Ascending switches the keyset order to oldest-first; cursors are directional.
type PaymentListFilter struct {
	Status      string
	After       *PaymentCursor
	Limit       int
	Query       string // exact payment ID or exact merchant_reference
	Currency    string
	Rail        string // payment method rail
	ProviderID  string // any attempt routed to this provider
	CreatedFrom *int64 // inclusive Unix milliseconds
	CreatedTo   *int64 // exclusive Unix milliseconds
	AmountMin   *int64 // inclusive minor units
	AmountMax   *int64 // inclusive minor units
	Ascending   bool
}

// ListPayments returns up to Limit payments ordered by (created_at, id),
// newest first unless Ascending, using keyset pagination so concurrent
// inserts do not shift pages.
func ListPayments(tx *gorm.DB, workspaceID string, filter PaymentListFilter) ([]Payment, error) {
	query := tx.Where("workspace_id = ?", workspaceID)
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Query != "" {
		query = query.Where("(id = ? OR merchant_reference = ?)", filter.Query, filter.Query)
	}
	if filter.Currency != "" {
		query = query.Where("currency = ?", filter.Currency)
	}
	if filter.Rail != "" {
		query = query.Where("payment_method_id IN (SELECT id FROM payment_methods WHERE workspace_id = ? AND rail = ?)", workspaceID, filter.Rail)
	}
	if filter.ProviderID != "" {
		query = query.Where("id IN (SELECT payment_id FROM payment_attempts WHERE workspace_id = ? AND provider_id = ?)", workspaceID, filter.ProviderID)
	}
	if filter.CreatedFrom != nil {
		query = query.Where("created_at >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query = query.Where("created_at < ?", *filter.CreatedTo)
	}
	if filter.AmountMin != nil {
		query = query.Where("amount_minor >= ?", *filter.AmountMin)
	}
	if filter.AmountMax != nil {
		query = query.Where("amount_minor <= ?", *filter.AmountMax)
	}
	order := "created_at DESC, id DESC"
	if filter.After != nil {
		if filter.Ascending {
			query = query.Where("(created_at > ? OR (created_at = ? AND id > ?))", filter.After.CreatedAt, filter.After.CreatedAt, filter.After.ID)
		} else {
			query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", filter.After.CreatedAt, filter.After.CreatedAt, filter.After.ID)
		}
	}
	if filter.Ascending {
		order = "created_at ASC, id ASC"
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	payments := []Payment{}
	err := query.Order(order).Limit(filter.Limit).Find(&payments).Error
	return payments, err
}

// UpdatePaymentStatus is a compare-and-swap on (status, version). It bumps the
// version and requires exactly one affected row; otherwise ErrStaleVersion.
// Callers validate the transition graph and write the audit row in the same tx.
func UpdatePaymentStatus(tx *gorm.DB, workspaceID, paymentID, fromStatus, toStatus string, version, now int64) error {
	result := tx.Model(&Payment{}).
		Where("workspace_id = ? AND id = ? AND status = ? AND version = ?", workspaceID, paymentID, fromStatus, version).
		Updates(map[string]any{"status": toStatus, "version": version + 1, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrStaleVersion
	}
	return nil
}

// InsertStateTransition appends one audit row; the table is append-only.
func InsertStateTransition(tx *gorm.DB, transition *PaymentStateTransition) error {
	return tx.Create(transition).Error
}

// ListStateTransitions returns a payment's audit timeline in order.
func ListStateTransitions(tx *gorm.DB, workspaceID, paymentID string) ([]PaymentStateTransition, error) {
	transitions := []PaymentStateTransition{}
	err := tx.Where("workspace_id = ? AND payment_id = ?", workspaceID, paymentID).Order("created_at, id").Find(&transitions).Error
	return transitions, err
}

// FindIdempotencyRecord reads the record for (workspace, operation, key).
func FindIdempotencyRecord(tx *gorm.DB, workspaceID, operation, key string) (IdempotencyRecord, error) {
	var record IdempotencyRecord
	err := tx.Where("workspace_id = ? AND operation = ? AND key = ?", workspaceID, operation, key).Take(&record).Error
	return record, notFound(err)
}

// InsertIdempotencyRecord reserves a key. A concurrent reservation of the same
// key fails with a unique violation (see IsUniqueViolation).
func InsertIdempotencyRecord(tx *gorm.DB, record *IdempotencyRecord) error {
	return tx.Create(record).Error
}

// CompleteIdempotencyRecord moves a pending reservation to completed with the
// stored response. Exactly one pending row must match.
func CompleteIdempotencyRecord(tx *gorm.DB, workspaceID, recordID, paymentID string, responseStatus int64, responseBody string, now int64) error {
	result := tx.Model(&IdempotencyRecord{}).
		Where("workspace_id = ? AND id = ? AND status = 'pending'", workspaceID, recordID).
		Updates(map[string]any{"status": "completed", "payment_id": paymentID, "response_status": responseStatus, "response_body": responseBody, "completed_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrStaleVersion
	}
	return nil
}

// InsertOutboxEvent appends a durable command or event.
func InsertOutboxEvent(tx *gorm.DB, event *OutboxEvent) error {
	return tx.Create(event).Error
}

// ListPaymentAttempts returns a payment's attempts in attempt order.
func ListPaymentAttempts(tx *gorm.DB, workspaceID, paymentID string) ([]PaymentAttempt, error) {
	attempts := []PaymentAttempt{}
	err := tx.Where("workspace_id = ? AND payment_id = ?", workspaceID, paymentID).Order("attempt_number").Find(&attempts).Error
	return attempts, err
}
