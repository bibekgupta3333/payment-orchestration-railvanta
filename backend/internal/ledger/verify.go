package ledger

import (
	"errors"
	"fmt"
	"math"

	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// Violation kinds reported by Verify.
const (
	ViolationUnbalanced        = "unbalanced_journal"
	ViolationEntryMismatch     = "entry_journal_mismatch"
	ViolationDraftJournal      = "draft_journal"
	ViolationMissingSuccess    = "missing_success_journal"
	ViolationDuplicateSuccess  = "duplicate_success_journal"
	ViolationUnexpectedSuccess = "unexpected_success_journal"
	ViolationSuccessAmount     = "success_journal_amount_mismatch"
	ViolationReversalNotOffset = "reversal_not_offsetting"
	ViolationReversalOriginal  = "reversal_of_unposted_journal"
)

// Violation is one broken ledger invariant.
type Violation struct {
	Kind      string
	JournalID string
	PaymentID string
	Detail    string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s journal=%s payment=%s: %s", v.Kind, v.JournalID, v.PaymentID, v.Detail)
}

// ErrInvariantViolated is returned by Check when Verify finds violations.
var ErrInvariantViolated = errors.New("ledger: invariant violated")

// Check runs Verify and converts any violation into ErrInvariantViolated.
func Check(db *gorm.DB) error {
	violations, err := Verify(db)
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return fmt.Errorf("%w: %d violation(s), first: %s", ErrInvariantViolated, len(violations), violations[0])
	}
	return nil
}

// Verify reads committed ledger and payment rows and reports every journal that
// is unposted, unbalanced or inconsistent with its entries, every succeeded
// payment without exactly one matching success journal, every failed or
// canceled payment with a success journal, and every reversal that does not
// exactly offset its original. Totals use overflow-checked int64 arithmetic.
func Verify(db *gorm.DB) ([]Violation, error) {
	var journals []storage.LedgerJournal
	if err := db.Order("id").Find(&journals).Error; err != nil {
		return nil, err
	}
	var entries []storage.LedgerEntry
	if err := db.Order("journal_id, id").Find(&entries).Error; err != nil {
		return nil, err
	}
	var payments []paymentRow
	if err := db.Order("id").Find(&payments).Error; err != nil {
		return nil, err
	}
	byJournal := make(map[string][]storage.LedgerEntry, len(journals))
	for _, entry := range entries {
		byJournal[entry.JournalID] = append(byJournal[entry.JournalID], entry)
	}
	journalByID := make(map[string]storage.LedgerJournal, len(journals))
	successByPayment := map[string][]storage.LedgerJournal{}
	for _, journal := range journals {
		journalByID[journal.ID] = journal
		if journal.PaymentID != nil && journal.Status == statusPosted && journal.PostingKey == SuccessPostingKey(*journal.PaymentID) {
			successByPayment[*journal.PaymentID] = append(successByPayment[*journal.PaymentID], journal)
		}
	}
	var violations []Violation
	add := func(kind string, journal storage.LedgerJournal, detail string) {
		paymentID := ""
		if journal.PaymentID != nil {
			paymentID = *journal.PaymentID
		}
		violations = append(violations, Violation{Kind: kind, JournalID: journal.ID, PaymentID: paymentID, Detail: detail})
	}
	for _, journal := range journals {
		lines := byJournal[journal.ID]
		if journal.Status != statusPosted {
			add(ViolationDraftJournal, journal, "committed journal is not posted")
			continue
		}
		for _, entry := range lines {
			if entry.WorkspaceID != journal.WorkspaceID || entry.Currency != journal.Currency {
				add(ViolationEntryMismatch, journal, "entry "+entry.ID+" workspace or currency differs from journal")
			}
		}
		if err := checkBalance(entryLines(lines)); err != nil {
			add(ViolationUnbalanced, journal, err.Error())
		}
		if journal.ReversalOf != nil {
			original, ok := journalByID[*journal.ReversalOf]
			if !ok || original.Status != statusPosted {
				add(ViolationReversalOriginal, journal, "original journal missing or unposted")
			} else if !offsets(entryLines(byJournal[original.ID]), entryLines(lines)) {
				add(ViolationReversalNotOffset, journal, "reversal of "+original.ID+" does not mirror its entries")
			}
		}
	}
	for _, payment := range payments {
		success := successByPayment[payment.ID]
		switch payment.Status {
		case "succeeded":
			if len(success) == 0 {
				violations = append(violations, Violation{Kind: ViolationMissingSuccess, PaymentID: payment.ID, Detail: "succeeded payment has no success journal"})
				continue
			}
			if len(success) > 1 {
				add(ViolationDuplicateSuccess, success[1], fmt.Sprintf("%d success journals", len(success)))
			}
			journal := success[0]
			debits, credits := sideTotals(byJournal[journal.ID])
			if journal.Currency != payment.Currency || journal.WorkspaceID != payment.WorkspaceID || debits != payment.AmountMinor || credits != payment.AmountMinor {
				add(ViolationSuccessAmount, journal, "success journal does not match payment amount, currency or workspace")
			}
		case "failed", "canceled":
			for _, journal := range success {
				add(ViolationUnexpectedSuccess, journal, payment.Status+" payment has a success journal")
			}
		}
	}
	return violations, nil
}

// sideTotals sums each side, saturating to -1 on overflow so it never matches
// a positive payment amount.
func sideTotals(entries []storage.LedgerEntry) (debits, credits int64) {
	for _, entry := range entries {
		total := &credits
		if entry.Side == string(Debit) {
			total = &debits
		}
		if *total < 0 || entry.AmountMinor <= 0 || *total > math.MaxInt64-entry.AmountMinor {
			*total = -1
			continue
		}
		*total += entry.AmountMinor
	}
	return debits, credits
}
