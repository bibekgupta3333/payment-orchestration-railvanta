package reconciliation

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// Discrepancy statuses.
const (
	StatusOpen          = "open"
	StatusInvestigating = "investigating"
	StatusResolved      = "resolved"
)

var (
	// ErrNotFound covers a missing discrepancy and another workspace's one.
	ErrNotFound = errors.New("reconciliation: discrepancy not found")
	// ErrNotOperator rejects a non-operator resolver.
	ErrNotOperator = errors.New("reconciliation: operator permission is required")
	// ErrInvalidResolution rejects an unknown status or a blank note or actor.
	ErrInvalidResolution = errors.New("reconciliation: status, note and actor are required")
	// ErrInvalidTransition rejects a status change that is not allowed.
	ErrInvalidTransition = errors.New("reconciliation: status change not allowed")
)

// MaxNoteLength bounds a resolution note in characters.
const MaxNoteLength = 2000

// Resolution is one requested status change.
type Resolution struct {
	WorkspaceID string
	Role        string // principal role; only "operator" may resolve
	ID          string // discrepancy ID
	ToStatus    string
	Note        string
	Actor       string // resolver reference, for example "session:operator"
}

// Resolve changes a discrepancy's status and appends a history row in one
// transaction. It records evidence only: it never touches payments, attempts,
// journals or the provider. A resolved discrepancy is final.
func (s *Service) Resolve(ctx context.Context, in Resolution) (storage.ReconciliationDiscrepancy, error) {
	var out storage.ReconciliationDiscrepancy
	if in.Role != "operator" {
		return out, ErrNotOperator
	}
	note := strings.TrimSpace(in.Note)
	if (in.ToStatus != StatusOpen && in.ToStatus != StatusInvestigating && in.ToStatus != StatusResolved) ||
		note == "" || utf8.RuneCountInString(note) > MaxNoteLength || strings.TrimSpace(in.Actor) == "" {
		return out, ErrInvalidResolution
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row storage.ReconciliationDiscrepancy
		err := tx.Where("id = ? AND workspace_id = ?", in.ID, in.WorkspaceID).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if row.Status == StatusResolved || row.Status == in.ToStatus {
			return ErrInvalidTransition
		}
		now := s.now().UnixMilli()
		updates := map[string]any{"status": in.ToStatus}
		if in.ToStatus == StatusResolved {
			updates["resolution_note"], updates["resolved_by"], updates["resolved_at"] = note, in.Actor, now
		}
		res := tx.Model(&storage.ReconciliationDiscrepancy{}).
			Where("id = ? AND workspace_id = ? AND status = ?", row.ID, row.WorkspaceID, row.Status).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrInvalidTransition // lost a race with another resolver
		}
		id, err := newID()
		if err != nil {
			return err
		}
		if err := tx.Create(&storage.ReconciliationResolutionEvent{
			ID: id, WorkspaceID: row.WorkspaceID, DiscrepancyID: row.ID, FromStatus: row.Status, ToStatus: in.ToStatus,
			Note: note, ActorReference: in.Actor, CreatedAt: now,
		}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", row.ID).Take(&out).Error
	})
	return out, err
}

// Timestamp formats Unix milliseconds the way the API does.
func Timestamp(millis int64) string {
	return time.UnixMilli(millis).UTC().Format("2006-01-02T15:04:05.000Z")
}
