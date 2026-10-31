package ledger

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// Side is the debit or credit side of a ledger entry.
type Side string

const (
	Debit  Side = "debit"
	Credit Side = "credit"
)

const (
	statusDraft  = "draft"
	statusPosted = "posted"
)

var (
	// ErrNoTransaction means the handle is not inside a caller-owned transaction.
	ErrNoTransaction = errors.New("ledger: posting requires the caller's transaction handle")
	// ErrInvalidJournal covers missing keys, bad currency codes, sides or amounts.
	ErrInvalidJournal = errors.New("ledger: invalid journal")
	// ErrSingleEntry means a journal has fewer than two entries.
	ErrSingleEntry = errors.New("ledger: journal requires at least two entries")
	// ErrUnbalanced means debit and credit totals differ.
	ErrUnbalanced = errors.New("ledger: journal is not balanced")
	// ErrOverflow means a side total does not fit in int64.
	ErrOverflow = errors.New("ledger: amount total overflows int64")
	// ErrAccountNotFound means an entry references a missing account.
	ErrAccountNotFound = errors.New("ledger: account not found")
	// ErrAccountMismatch means an account belongs to another workspace or currency.
	ErrAccountMismatch = errors.New("ledger: account workspace or currency does not match journal")
	// ErrPostingMismatch means the posting key already exists for a different journal.
	ErrPostingMismatch = errors.New("ledger: posting key already used by a different journal")
	// ErrPaymentMismatch means the payment's workspace, currency or amount differ from the request.
	ErrPaymentMismatch = errors.New("ledger: payment does not match posting request")
	// ErrUnknownProvider means the provider does not belong to the workspace.
	ErrUnknownProvider = errors.New("ledger: provider not found in workspace")
	// ErrJournalNotFound means the journal to reverse does not exist in the workspace.
	ErrJournalNotFound = errors.New("ledger: journal not found")
	// ErrNotPosted means the journal to reverse is not posted.
	ErrNotPosted = errors.New("ledger: journal is not posted")
	// ErrAlreadyReversed means the journal already has a reversal.
	ErrAlreadyReversed = errors.New("ledger: journal already reversed")
	// ErrReversalMismatch means a reversal does not exactly offset its original.
	ErrReversalMismatch = errors.New("ledger: reversal does not exactly offset original")
)

// Line is one requested debit or credit; the entry inherits the journal currency.
type Line struct {
	AccountID   string
	Side        Side
	AmountMinor int64
}

// JournalSpec describes a journal to post atomically.
type JournalSpec struct {
	WorkspaceID string
	Currency    string
	PostingKey  string
	PaymentID   *string
	ReversalOf  *string
	Lines       []Line
}

// Posting is a posted journal with its entries. AlreadyPosted is true when the
// posting key existed and the stored journal matched the request exactly; no
// rows were written in that case.
type Posting struct {
	Journal       storage.LedgerJournal
	Entries       []storage.LedgerEntry
	AlreadyPosted bool
}

// afterEntriesHook lets tests force a failure after entries are inserted and
// before the journal is marked posted.
var afterEntriesHook func(tx *gorm.DB) error

// Post builds a draft journal, inserts its entries, checks totals with
// overflow-safe arithmetic, and marks it posted, all on tx. tx must be inside a
// transaction owned by the caller, who must roll back when Post returns an
// error. A repeated posting key returns the existing journal when it matches.
func Post(tx *gorm.DB, spec JournalSpec) (*Posting, error) {
	if !inTransaction(tx) {
		return nil, ErrNoTransaction
	}
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	existing, err := findByPostingKey(tx, spec.WorkspaceID, spec.PostingKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return confirmExisting(tx, *existing, spec)
	}
	if err := checkAccounts(tx, spec); err != nil {
		return nil, err
	}
	journalID, err := storage.NewID()
	if err != nil {
		return nil, err
	}
	journal := storage.LedgerJournal{
		ID: journalID, WorkspaceID: spec.WorkspaceID, PaymentID: spec.PaymentID, Currency: spec.Currency,
		PostingKey: spec.PostingKey, Status: statusDraft, ReversalOf: spec.ReversalOf,
	}
	if err := tx.Create(&journal).Error; err != nil {
		if isPostingKeyConflict(err) {
			// Another writer committed the key first; confirm it instead.
			if existing, findErr := findByPostingKey(tx, spec.WorkspaceID, spec.PostingKey); findErr == nil && existing != nil {
				return confirmExisting(tx, *existing, spec)
			}
		}
		return nil, fmt.Errorf("ledger: insert draft journal: %w", err)
	}
	entries := make([]storage.LedgerEntry, 0, len(spec.Lines))
	for _, line := range spec.Lines {
		entryID, err := storage.NewID()
		if err != nil {
			return nil, err
		}
		entries = append(entries, storage.LedgerEntry{
			ID: entryID, WorkspaceID: spec.WorkspaceID, JournalID: journal.ID, AccountID: line.AccountID,
			Currency: spec.Currency, Side: string(line.Side), AmountMinor: line.AmountMinor,
		})
	}
	if err := tx.Create(&entries).Error; err != nil {
		return nil, fmt.Errorf("ledger: insert entries: %w", err)
	}
	if afterEntriesHook != nil {
		if err := afterEntriesHook(tx); err != nil {
			return nil, err
		}
	}
	// Re-check the persisted lines, not only the request, before posting.
	var stored []storage.LedgerEntry
	if err := tx.Where("journal_id = ?", journal.ID).Order("id").Find(&stored).Error; err != nil {
		return nil, err
	}
	if err := checkBalance(entryLines(stored)); err != nil {
		return nil, err
	}
	postedAt := time.Now().UnixMilli()
	result := tx.Model(&storage.LedgerJournal{}).Where("id = ? AND status = ?", journal.ID, statusDraft).
		Updates(map[string]any{"status": statusPosted, "posted_at": postedAt})
	if result.Error != nil {
		return nil, fmt.Errorf("ledger: post journal: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("ledger: post journal %s: draft not found", journal.ID)
	}
	journal.Status, journal.PostedAt = statusPosted, &postedAt
	return &Posting{Journal: journal, Entries: stored}, nil
}

// SuccessPostingKey is the deterministic posting key for a payment success.
func SuccessPostingKey(paymentID string) string { return "payment:" + paymentID + ":success" }

// ReversalPostingKey is the deterministic posting key for a journal reversal.
func ReversalPostingKey(journalID string) string { return "journal:" + journalID + ":reversal" }

// PostPaymentSuccess debits the executing provider's clearing account and
// credits merchant payable for the payment's exact currency and amount, using
// the posting key payment:{id}:success. Accounts are created lazily. A repeated
// call returns the existing matching journal with AlreadyPosted set.
func PostPaymentSuccess(tx *gorm.DB, workspaceID, paymentID, providerID, currency string, amountMinor int64) (*Posting, error) {
	if !inTransaction(tx) {
		return nil, ErrNoTransaction
	}
	if workspaceID == "" || paymentID == "" || providerID == "" || !validCurrency(currency) || amountMinor <= 0 {
		return nil, ErrInvalidJournal
	}
	var payment paymentRow
	err := tx.Where("id = ? AND workspace_id = ?", paymentID, workspaceID).Take(&payment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPaymentMismatch
	}
	if err != nil {
		return nil, err
	}
	if payment.Currency != currency || payment.AmountMinor != amountMinor {
		return nil, ErrPaymentMismatch
	}
	clearing, err := EnsureProviderClearingAccount(tx, workspaceID, providerID, currency)
	if err != nil {
		return nil, err
	}
	payable, err := EnsureMerchantPayableAccount(tx, workspaceID, currency)
	if err != nil {
		return nil, err
	}
	return Post(tx, JournalSpec{
		WorkspaceID: workspaceID, Currency: currency, PostingKey: SuccessPostingKey(paymentID), PaymentID: &paymentID,
		Lines: []Line{
			{AccountID: clearing.ID, Side: Debit, AmountMinor: amountMinor},
			{AccountID: payable.ID, Side: Credit, AmountMinor: amountMinor},
		},
	})
}

// Reverse posts a new journal with every entry of the posted original on the
// opposite side, linked by reversal_of, after checking it exactly offsets the
// original. A journal can be reversed only once.
func Reverse(tx *gorm.DB, workspaceID, journalID string) (*Posting, error) {
	if !inTransaction(tx) {
		return nil, ErrNoTransaction
	}
	var original storage.LedgerJournal
	err := tx.Where("id = ? AND workspace_id = ?", journalID, workspaceID).Take(&original).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrJournalNotFound
	}
	if err != nil {
		return nil, err
	}
	if original.Status != statusPosted {
		return nil, ErrNotPosted
	}
	var reversals int64
	if err := tx.Model(&storage.LedgerJournal{}).Where("reversal_of = ?", original.ID).Count(&reversals).Error; err != nil {
		return nil, err
	}
	if reversals > 0 {
		return nil, ErrAlreadyReversed
	}
	var originalEntries []storage.LedgerEntry
	if err := tx.Where("journal_id = ?", original.ID).Order("id").Find(&originalEntries).Error; err != nil {
		return nil, err
	}
	lines := make([]Line, 0, len(originalEntries))
	for _, entry := range originalEntries {
		lines = append(lines, Line{AccountID: entry.AccountID, Side: opposite(Side(entry.Side)), AmountMinor: entry.AmountMinor})
	}
	if !offsets(entryLines(originalEntries), lines) {
		return nil, ErrReversalMismatch
	}
	reversalOf := original.ID
	posting, err := Post(tx, JournalSpec{
		WorkspaceID: workspaceID, Currency: original.Currency, PostingKey: ReversalPostingKey(original.ID),
		PaymentID: original.PaymentID, ReversalOf: &reversalOf, Lines: lines,
	})
	if err != nil {
		return nil, err
	}
	if !offsets(entryLines(originalEntries), entryLines(posting.Entries)) {
		return nil, ErrReversalMismatch
	}
	return posting, nil
}

// paymentRow reads only the payment fields the ledger needs.
type paymentRow struct {
	ID          string `gorm:"column:id"`
	WorkspaceID string `gorm:"column:workspace_id"`
	AmountMinor int64  `gorm:"column:amount_minor"`
	Currency    string `gorm:"column:currency"`
	Status      string `gorm:"column:status"`
}

func (paymentRow) TableName() string { return "payments" }

func inTransaction(tx *gorm.DB) bool {
	if tx == nil || tx.Statement == nil {
		return false
	}
	_, ok := tx.Statement.ConnPool.(gorm.TxCommitter)
	return ok
}

func validCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, c := range currency {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

func validateSpec(spec JournalSpec) error {
	if spec.WorkspaceID == "" || spec.PostingKey == "" || !validCurrency(spec.Currency) {
		return ErrInvalidJournal
	}
	for _, line := range spec.Lines {
		if line.AccountID == "" || line.AmountMinor <= 0 || (line.Side != Debit && line.Side != Credit) {
			return ErrInvalidJournal
		}
	}
	return checkBalance(spec.Lines)
}

// checkBalance requires two or more lines and equal side totals, rejecting any
// int64 overflow instead of wrapping.
func checkBalance(lines []Line) error {
	if len(lines) < 2 {
		return ErrSingleEntry
	}
	var debits, credits int64
	for _, line := range lines {
		if line.AmountMinor <= 0 {
			return ErrInvalidJournal
		}
		total := &credits
		if line.Side == Debit {
			total = &debits
		}
		if *total > math.MaxInt64-line.AmountMinor {
			return ErrOverflow
		}
		*total += line.AmountMinor
	}
	if debits != credits {
		return ErrUnbalanced
	}
	return nil
}

func checkAccounts(tx *gorm.DB, spec JournalSpec) error {
	ids := make([]string, 0, len(spec.Lines))
	for _, line := range spec.Lines {
		ids = append(ids, line.AccountID)
	}
	var accounts []storage.LedgerAccount
	if err := tx.Where("id IN ?", ids).Find(&accounts).Error; err != nil {
		return err
	}
	byID := make(map[string]storage.LedgerAccount, len(accounts))
	for _, account := range accounts {
		byID[account.ID] = account
	}
	for _, id := range ids {
		account, ok := byID[id]
		if !ok {
			return ErrAccountNotFound
		}
		if account.WorkspaceID != spec.WorkspaceID || account.Currency != spec.Currency {
			return ErrAccountMismatch
		}
	}
	return nil
}

func findByPostingKey(tx *gorm.DB, workspaceID, postingKey string) (*storage.LedgerJournal, error) {
	var journals []storage.LedgerJournal
	if err := tx.Where("workspace_id = ? AND posting_key = ?", workspaceID, postingKey).Limit(1).Find(&journals).Error; err != nil {
		return nil, err
	}
	if len(journals) == 0 {
		return nil, nil
	}
	return &journals[0], nil
}

// confirmExisting accepts a duplicate posting only when the stored journal is
// posted and has the same currency, payment, reversal link and entry lines.
func confirmExisting(tx *gorm.DB, journal storage.LedgerJournal, spec JournalSpec) (*Posting, error) {
	if journal.Status != statusPosted || journal.Currency != spec.Currency ||
		!sameOptional(journal.PaymentID, spec.PaymentID) || !sameOptional(journal.ReversalOf, spec.ReversalOf) {
		return nil, ErrPostingMismatch
	}
	var entries []storage.LedgerEntry
	if err := tx.Where("journal_id = ?", journal.ID).Order("id").Find(&entries).Error; err != nil {
		return nil, err
	}
	if !sameLines(entryLines(entries), spec.Lines) {
		return nil, ErrPostingMismatch
	}
	return &Posting{Journal: journal, Entries: entries, AlreadyPosted: true}, nil
}

func sameOptional(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func entryLines(entries []storage.LedgerEntry) []Line {
	lines := make([]Line, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, Line{AccountID: entry.AccountID, Side: Side(entry.Side), AmountMinor: entry.AmountMinor})
	}
	return lines
}

func lineKeys(lines []Line) []string {
	keys := make([]string, 0, len(lines))
	for _, line := range lines {
		keys = append(keys, fmt.Sprintf("%s|%s|%d", line.AccountID, line.Side, line.AmountMinor))
	}
	sort.Strings(keys)
	return keys
}

// sameLines compares lines as multisets, ignoring order.
func sameLines(a, b []Line) bool {
	return strings.Join(lineKeys(a), "\n") == strings.Join(lineKeys(b), "\n") && len(a) == len(b)
}

func opposite(side Side) Side {
	if side == Debit {
		return Credit
	}
	return Debit
}

// offsets reports whether reversal contains exactly the original lines with
// each side flipped, so the pair nets every account to zero.
func offsets(original, reversal []Line) bool {
	flipped := make([]Line, 0, len(original))
	for _, line := range original {
		flipped = append(flipped, Line{AccountID: line.AccountID, Side: opposite(line.Side), AmountMinor: line.AmountMinor})
	}
	return len(original) > 0 && sameLines(flipped, reversal)
}

func isPostingKeyConflict(err error) bool {
	message := err.Error()
	return strings.Contains(message, "UNIQUE constraint failed") && strings.Contains(message, "posting_key")
}
