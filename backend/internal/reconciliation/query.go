package reconciliation

import (
	"errors"

	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// Cursor is the keyset position after the last returned row.
type Cursor struct {
	CreatedAt int64
	ID        string
}

// RunFilter selects a newest-first page of runs.
type RunFilter struct {
	After *Cursor
	Limit int
}

// ListRuns reads the workspace's runs, newest first. Read-only.
func ListRuns(db *gorm.DB, workspaceID string, f RunFilter) ([]storage.ReconciliationRun, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	query := db.Where("workspace_id = ?", workspaceID)
	if f.After != nil {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", f.After.CreatedAt, f.After.CreatedAt, f.After.ID)
	}
	var runs []storage.ReconciliationRun
	err := query.Order("created_at DESC, id DESC").Limit(f.Limit).Find(&runs).Error
	return runs, err
}

// GetRun reads one run of the workspace.
func GetRun(db *gorm.DB, workspaceID, id string) (storage.ReconciliationRun, error) {
	var run storage.ReconciliationRun
	err := db.Where("id = ? AND workspace_id = ?", id, workspaceID).Take(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return run, ErrNotFound
	}
	return run, err
}

// DiscrepancyFilter selects a newest-first page of discrepancies.
type DiscrepancyFilter struct {
	RunID  string
	Kind   string
	Status string
	After  *Cursor
	Limit  int
}

// DiscrepancyView is a discrepancy with its resolution history and the posted
// success journal of its payment, if one exists.
type DiscrepancyView struct {
	storage.ReconciliationDiscrepancy
	History          []storage.ReconciliationResolutionEvent
	RelatedJournalID *string
}

// ListDiscrepancies reads the workspace's discrepancies, newest first.
func ListDiscrepancies(db *gorm.DB, workspaceID string, f DiscrepancyFilter) ([]DiscrepancyView, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	query := db.Where("workspace_id = ?", workspaceID)
	if f.RunID != "" {
		query = query.Where("run_id = ?", f.RunID)
	}
	if f.Kind != "" {
		query = query.Where("kind = ?", f.Kind)
	}
	if f.Status != "" {
		query = query.Where("status = ?", f.Status)
	}
	if f.After != nil {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", f.After.CreatedAt, f.After.CreatedAt, f.After.ID)
	}
	var rows []storage.ReconciliationDiscrepancy
	if err := query.Order("created_at DESC, id DESC").Limit(f.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return attach(db, workspaceID, rows)
}

// GetDiscrepancy reads one discrepancy of the workspace.
func GetDiscrepancy(db *gorm.DB, workspaceID, id string) (DiscrepancyView, error) {
	var row storage.ReconciliationDiscrepancy
	err := db.Where("id = ? AND workspace_id = ?", id, workspaceID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DiscrepancyView{}, ErrNotFound
	}
	if err != nil {
		return DiscrepancyView{}, err
	}
	views, err := attach(db, workspaceID, []storage.ReconciliationDiscrepancy{row})
	if err != nil {
		return DiscrepancyView{}, err
	}
	return views[0], nil
}

func attach(db *gorm.DB, workspaceID string, rows []storage.ReconciliationDiscrepancy) ([]DiscrepancyView, error) {
	views := make([]DiscrepancyView, len(rows))
	if len(rows) == 0 {
		return views, nil
	}
	ids := make([]string, len(rows))
	paymentIDs := make([]string, 0, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		if r.PaymentID != nil {
			paymentIDs = append(paymentIDs, *r.PaymentID)
		}
	}
	var events []storage.ReconciliationResolutionEvent
	if err := db.Where("workspace_id = ? AND discrepancy_id IN ?", workspaceID, ids).Order("created_at, id").Find(&events).Error; err != nil {
		return nil, err
	}
	history := map[string][]storage.ReconciliationResolutionEvent{}
	for _, e := range events {
		history[e.DiscrepancyID] = append(history[e.DiscrepancyID], e)
	}
	journalByPayment := map[string]string{}
	if len(paymentIDs) > 0 {
		var journals []storage.LedgerJournal
		if err := db.Where("workspace_id = ? AND status = 'posted' AND reversal_of IS NULL AND payment_id IN ?", workspaceID, paymentIDs).Find(&journals).Error; err != nil {
			return nil, err
		}
		for _, j := range journals {
			if j.PaymentID != nil && j.PostingKey == ledger.SuccessPostingKey(*j.PaymentID) {
				journalByPayment[*j.PaymentID] = j.ID
			}
		}
	}
	for i, r := range rows {
		view := DiscrepancyView{ReconciliationDiscrepancy: r, History: history[r.ID]}
		if view.History == nil {
			view.History = []storage.ReconciliationResolutionEvent{}
		}
		if r.PaymentID != nil {
			if id, ok := journalByPayment[*r.PaymentID]; ok {
				view.RelatedJournalID = &id
			}
		}
		views[i] = view
	}
	return views, nil
}
