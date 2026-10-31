package ledger

import (
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// JournalCursor is the keyset position after the last returned journal.
type JournalCursor struct {
	CreatedAt int64
	ID        string
}

// JournalFilter selects a newest-first page of posted journals.
type JournalFilter struct {
	PaymentID string // optional
	After     *JournalCursor
	Limit     int
}

// EntryView is one posted entry with its account's code and provider.
type EntryView struct {
	storage.LedgerEntry
	AccountCode       string
	AccountProviderID *string
}

// JournalView is a posted journal with its entries in stable order.
type JournalView struct {
	Journal storage.LedgerJournal
	Entries []EntryView
}

// ListPosted reads posted journals for one workspace, ordered by (created_at,
// id) descending with keyset pagination. Drafts are never returned. Read-only.
func ListPosted(db *gorm.DB, workspaceID string, filter JournalFilter) ([]JournalView, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	query := db.Where("workspace_id = ? AND status = ?", workspaceID, statusPosted)
	if filter.PaymentID != "" {
		query = query.Where("payment_id = ?", filter.PaymentID)
	}
	if filter.After != nil {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", filter.After.CreatedAt, filter.After.CreatedAt, filter.After.ID)
	}
	var journals []storage.LedgerJournal
	if err := query.Order("created_at DESC, id DESC").Limit(filter.Limit).Find(&journals).Error; err != nil {
		return nil, err
	}
	views := make([]JournalView, 0, len(journals))
	if len(journals) == 0 {
		return views, nil
	}
	ids := make([]string, len(journals))
	for i, j := range journals {
		ids[i] = j.ID
	}
	var rows []struct {
		storage.LedgerEntry
		AccountCode       string  `gorm:"column:account_code"`
		AccountProviderID *string `gorm:"column:account_provider_id"`
	}
	err := db.Table("ledger_entries AS e").
		Select("e.*, a.code AS account_code, a.provider_id AS account_provider_id").
		Joins("JOIN ledger_accounts AS a ON a.id = e.account_id AND a.workspace_id = e.workspace_id").
		Where("e.workspace_id = ? AND e.journal_id IN ?", workspaceID, ids).
		Order("e.journal_id, CASE e.side WHEN 'debit' THEN 0 ELSE 1 END, e.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	byJournal := make(map[string][]EntryView, len(journals))
	for _, row := range rows {
		byJournal[row.JournalID] = append(byJournal[row.JournalID], EntryView{LedgerEntry: row.LedgerEntry, AccountCode: row.AccountCode, AccountProviderID: row.AccountProviderID})
	}
	for _, j := range journals {
		views = append(views, JournalView{Journal: j, Entries: byJournal[j.ID]})
	}
	return views, nil
}
