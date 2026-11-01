package event

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Outbox row statuses (outbox_events.status).
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDelivered  = "delivered"
	StatusFailed     = "failed"
)

var (
	// ErrLeaseLost means the row is no longer processing under the caller's
	// token: it was completed, released, or reclaimed by another owner after the
	// caller's lease expired. The caller must not acknowledge the work.
	ErrLeaseLost = errors.New("outbox lease lost")
	// ErrInvalidLease rejects a non-positive lease duration.
	ErrInvalidLease = errors.New("outbox lease duration must be positive")
)

// Claim is one outbox row owned by the caller until LeaseUntil. Token is the
// fresh unpredictable value stored in lease_owner for this acquisition only;
// renewals and completion compare it, so an expired owner cannot acknowledge
// work that another owner reclaimed.
type Claim struct {
	EventID     string
	WorkspaceID string
	PaymentID   *string
	EventType   string
	Payload     string
	// Attempts counts acquisitions, including this one.
	Attempts int64
	// AvailableAt is when the row became due (Unix ms); ClaimedAt-AvailableAt is lag.
	AvailableAt int64
	ClaimedAt   int64
	Token       string
	LeaseUntil  int64
	// Reclaimed is true when a previous owner's lease had expired.
	Reclaimed bool
}

// NewLeaseToken returns 32 bytes from crypto/rand, hex encoded.
func NewLeaseToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("lease token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Enqueue appends a pending command on the caller's transaction. A row with the
// same (workspace_id, deduplication_key) already present is kept unchanged and
// Enqueue reports inserted=false; commands are therefore at most once per key.
func Enqueue(tx *gorm.DB, row storage.OutboxEvent) (bool, error) {
	if row.WorkspaceID == "" || row.EventType == "" || row.DeduplicationKey == "" || row.Payload == "" {
		return false, errors.New("outbox: workspace, type, deduplication key and payload are required")
	}
	if row.ID == "" {
		id, err := storage.NewID()
		if err != nil {
			return false, err
		}
		row.ID = id
	}
	row.Status, row.Attempts, row.LeaseOwner, row.LeaseUntil, row.DeliveredAt = StatusPending, 0, nil, nil, nil
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "deduplication_key"}},
		DoNothing: true,
	}).Create(&row)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// ClaimDue claims up to limit due rows of the given types. A row is due when it
// is pending with available_at <= now, or processing with an expired lease.
// Each claim is a compare-and-swap UPDATE that also matches the status and
// lease_owner observed by the candidate read, so two racing claimers can never
// both win the same row. No transaction spans more than one statement.
func ClaimDue(ctx context.Context, db *gorm.DB, eventTypes []string, now time.Time, lease time.Duration, limit int) ([]Claim, error) {
	if lease <= 0 {
		return nil, ErrInvalidLease
	}
	if len(eventTypes) == 0 || limit <= 0 {
		return nil, nil
	}
	nowMS := now.UnixMilli()
	var candidates []storage.OutboxEvent
	err := db.WithContext(ctx).
		Where("event_type IN ? AND ((status = ? AND available_at <= ?) OR (status = ? AND lease_until < ?))", eventTypes, StatusPending, nowMS, StatusProcessing, nowMS).
		Order("available_at, id").Limit(limit).Find(&candidates).Error
	if err != nil {
		return nil, err
	}
	claims := make([]Claim, 0, len(candidates))
	for _, row := range candidates {
		token, err := NewLeaseToken()
		if err != nil {
			return claims, err
		}
		until := now.Add(lease).UnixMilli()
		query := db.WithContext(ctx).Model(&storage.OutboxEvent{}).Where("id = ? AND status = ?", row.ID, row.Status)
		if row.Status == StatusPending {
			query = query.Where("lease_owner IS NULL AND available_at <= ?", nowMS)
		} else {
			query = query.Where("lease_owner = ? AND lease_until < ?", *row.LeaseOwner, nowMS)
		}
		result := query.Updates(map[string]any{"status": StatusProcessing, "lease_owner": token, "lease_until": until, "attempts": gorm.Expr("attempts + 1")})
		if result.Error != nil {
			return claims, result.Error
		}
		if result.RowsAffected != 1 {
			continue // another claimer won the compare-and-swap
		}
		claims = append(claims, Claim{
			EventID: row.ID, WorkspaceID: row.WorkspaceID, PaymentID: row.PaymentID, EventType: row.EventType, Payload: row.Payload,
			Attempts: row.Attempts + 1, AvailableAt: row.AvailableAt, ClaimedAt: nowMS, Token: token, LeaseUntil: until,
			Reclaimed: row.Status == StatusProcessing,
		})
	}
	return claims, nil
}

func owned(tx *gorm.DB, claim Claim) *gorm.DB {
	return tx.Model(&storage.OutboxEvent{}).Where("id = ? AND status = ? AND lease_owner = ?", claim.EventID, StatusProcessing, claim.Token)
}

func exactlyOne(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

// Renew extends the lease to now+lease only while the caller's token still owns
// the row. It returns ErrLeaseLost after another owner reclaimed it.
func Renew(ctx context.Context, db *gorm.DB, claim *Claim, now time.Time, lease time.Duration) error {
	if lease <= 0 {
		return ErrInvalidLease
	}
	until := now.Add(lease).UnixMilli()
	if err := exactlyOne(owned(db.WithContext(ctx), *claim).Update("lease_until", until)); err != nil {
		return err
	}
	claim.LeaseUntil = until
	return nil
}

// Complete marks the row delivered on the caller's handle (normally the same
// transaction that applies the command's effects) only while the token owns it.
// On ErrLeaseLost the caller must roll back its transaction.
func Complete(tx *gorm.DB, claim Claim, now int64) error {
	return exactlyOne(owned(tx, claim).Updates(map[string]any{"status": StatusDelivered, "delivered_at": now, "lease_owner": nil, "lease_until": nil}))
}

// SkipPending marks a still-pending, unclaimed command delivered on the
// caller's transaction, for example when cancellation makes a dispatch
// pointless. A row that a dispatcher already claimed is left alone: its handler
// re-reads state and completes it. It reports whether a row was skipped.
func SkipPending(tx *gorm.DB, workspaceID, deduplicationKey, reason string, now int64) (bool, error) {
	result := tx.Model(&storage.OutboxEvent{}).
		Where("workspace_id = ? AND deduplication_key = ? AND status = ?", workspaceID, deduplicationKey, StatusPending).
		Updates(map[string]any{"status": StatusDelivered, "delivered_at": now, "last_error": truncate(reason)})
	return result.RowsAffected == 1, result.Error
}

// Release returns an owned row to pending, due again at retryAt, recording a
// sanitized error message.
func Release(ctx context.Context, db *gorm.DB, claim Claim, retryAt time.Time, lastError string) error {
	return exactlyOne(owned(db.WithContext(ctx), claim).Updates(map[string]any{
		"status": StatusPending, "available_at": retryAt.UnixMilli(), "lease_owner": nil, "lease_until": nil, "last_error": truncate(lastError),
	}))
}

// Fail parks an owned row as failed after its handler exhausted retries. A
// failed row is never claimed again; an operator must inspect it.
func Fail(ctx context.Context, db *gorm.DB, claim Claim, lastError string) error {
	return exactlyOne(owned(db.WithContext(ctx), claim).Updates(map[string]any{
		"status": StatusFailed, "lease_owner": nil, "lease_until": nil, "last_error": truncate(lastError),
	}))
}

func truncate(message string) string {
	if len(message) > 500 {
		return message[:500]
	}
	return message
}
