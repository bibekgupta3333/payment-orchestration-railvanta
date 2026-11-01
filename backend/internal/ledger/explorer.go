package ledger

import (
	"errors"
	"math"

	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// Read-only explorer queries (WBS 4.1). Every query is scoped to one workspace,
// reads posted journals only, and sums with overflow-checked int64 arithmetic:
// a total that does not fit returns ErrOverflow, never a rounded value.
// Amounts are never combined across currencies.

// AddChecked returns a+b or ErrOverflow.
func AddChecked(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, ErrOverflow
	}
	return a + b, nil
}

// SubChecked returns a-b or ErrOverflow.
func SubChecked(a, b int64) (int64, error) {
	if b < 0 && a > math.MaxInt64+b || b > 0 && a < math.MinInt64+b {
		return 0, ErrOverflow
	}
	return a - b, nil
}

// Totals returns the checked debit and credit sums of entries.
func Totals(entries []EntryView) (debit, credit int64, err error) {
	for _, e := range entries {
		switch e.Side {
		case "debit":
			debit, err = AddChecked(debit, e.AmountMinor)
		case "credit":
			credit, err = AddChecked(credit, e.AmountMinor)
		default:
			err = ErrInvalidJournal
		}
		if err != nil {
			return 0, 0, err
		}
	}
	return debit, credit, nil
}

// AccountBalance is one account with debit and credit totals from posted
// entries. Balance is signed in the account's normal direction: debits minus
// credits for asset and expense accounts, credits minus debits otherwise.
type AccountBalance struct {
	Account       storage.LedgerAccount
	ProviderCode  *string
	DebitTotal    int64
	CreditTotal   int64
	Balance       int64
	EntryCount    int64
	NormalBalance string // "debit" or "credit"
	// Err is ErrOverflow when a total or the balance of this account does not
	// fit in int64. The totals are then meaningless and must not be shown; other
	// accounts are unaffected.
	Err error
}

// ListAccounts returns every account of the workspace, optionally for one
// currency, ordered by currency then code, with balances computed from posted
// entries only. An account whose totals overflow int64 carries Err =
// ErrOverflow instead of a rounded number; the query itself still succeeds.
func ListAccounts(db *gorm.DB, workspaceID, currency string) ([]AccountBalance, error) {
	query := db.Table("ledger_accounts AS a").
		Select("a.id, a.workspace_id, a.code, a.currency, a.provider_id, a.kind, a.created_at, p.code AS provider_code").
		Joins("LEFT JOIN providers AS p ON p.id = a.provider_id AND p.workspace_id = a.workspace_id").
		Where("a.workspace_id = ?", workspaceID)
	if currency != "" {
		query = query.Where("a.currency = ?", currency)
	}
	var rows []struct {
		storage.LedgerAccount
		ProviderCode *string `gorm:"column:provider_code"`
	}
	if err := query.Order("a.currency, a.code").Scan(&rows).Error; err != nil {
		return nil, err
	}
	accounts := make([]AccountBalance, len(rows))
	index := make(map[string]int, len(rows))
	for i, row := range rows {
		normal := "credit"
		if row.Kind == "asset" || row.Kind == "expense" {
			normal = "debit"
		}
		accounts[i] = AccountBalance{Account: row.LedgerAccount, ProviderCode: row.ProviderCode, NormalBalance: normal}
		index[row.ID] = i
	}
	if len(accounts) == 0 {
		return accounts, nil
	}
	entries, err := db.Table("ledger_entries AS e").
		Select("e.account_id, e.side, e.amount_minor").
		Joins("JOIN ledger_journals AS j ON j.id = e.journal_id AND j.workspace_id = e.workspace_id").
		Where("e.workspace_id = ? AND j.status = ?", workspaceID, statusPosted).
		Rows()
	if err != nil {
		return nil, err
	}
	defer entries.Close()
	for entries.Next() {
		var accountID, side string
		var amount int64
		if err := entries.Scan(&accountID, &side, &amount); err != nil {
			return nil, err
		}
		i, ok := index[accountID]
		if !ok {
			continue // excluded by the currency filter
		}
		a := &accounts[i]
		a.EntryCount++
		if a.Err != nil {
			continue
		}
		if side == "debit" {
			a.DebitTotal, err = AddChecked(a.DebitTotal, amount)
		} else {
			a.CreditTotal, err = AddChecked(a.CreditTotal, amount)
		}
		if err != nil {
			a.Err = err
		}
	}
	if err := entries.Err(); err != nil {
		return nil, err
	}
	for i := range accounts {
		a := &accounts[i]
		if a.Err != nil {
			continue
		}
		if a.NormalBalance == "debit" {
			a.Balance, a.Err = SubChecked(a.DebitTotal, a.CreditTotal)
		} else {
			a.Balance, a.Err = SubChecked(a.CreditTotal, a.DebitTotal)
		}
	}
	return accounts, nil
}

// JournalDetail is one posted journal with checked totals and its reversal link.
type JournalDetail struct {
	JournalView
	DebitTotal  int64
	CreditTotal int64
}

// GetPosted returns one posted journal of the workspace. A missing journal, a
// draft, or another workspace's journal all report ErrJournalNotFound.
func GetPosted(db *gorm.DB, workspaceID, journalID string) (*JournalDetail, error) {
	var journal storage.LedgerJournal
	err := db.Where("id = ? AND workspace_id = ? AND status = ?", journalID, workspaceID, statusPosted).Take(&journal).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrJournalNotFound
	}
	if err != nil {
		return nil, err
	}
	var rows []struct {
		storage.LedgerEntry
		AccountCode       string  `gorm:"column:account_code"`
		AccountProviderID *string `gorm:"column:account_provider_id"`
	}
	err = db.Table("ledger_entries AS e").
		Select("e.*, a.code AS account_code, a.provider_id AS account_provider_id").
		Joins("JOIN ledger_accounts AS a ON a.id = e.account_id AND a.workspace_id = e.workspace_id").
		Where("e.workspace_id = ? AND e.journal_id = ?", workspaceID, journal.ID).
		Order("CASE e.side WHEN 'debit' THEN 0 ELSE 1 END, e.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	view := JournalView{Journal: journal, Entries: make([]EntryView, 0, len(rows))}
	for _, row := range rows {
		view.Entries = append(view.Entries, EntryView{LedgerEntry: row.LedgerEntry, AccountCode: row.AccountCode, AccountProviderID: row.AccountProviderID})
	}
	reversedBy, err := reversedByMap(db, workspaceID, []string{journal.ID})
	if err != nil {
		return nil, err
	}
	if id, ok := reversedBy[journal.ID]; ok {
		view.ReversedBy = &id
	}
	debit, credit, err := Totals(view.Entries)
	if err != nil {
		return nil, err
	}
	return &JournalDetail{JournalView: view, DebitTotal: debit, CreditTotal: credit}, nil
}
