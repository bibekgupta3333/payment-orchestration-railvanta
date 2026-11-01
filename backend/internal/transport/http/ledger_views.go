package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// Ledger explorer routes (WBS 4.1). All are GET-only: there is no route that
// edits or deletes a journal, an entry or an account.

// LedgerAccountView is one ledger account with balances from posted entries.
type LedgerAccountView struct {
	ID           string  `json:"id"`
	Code         string  `json:"code"`
	Kind         string  `json:"kind"`
	Currency     string  `json:"currency"`
	ProviderID   *string `json:"provider_id"`
	ProviderCode *string `json:"provider_code"`
	// The three amounts are null, with BalanceError "amount_overflow", when a
	// total does not fit in int64: an explicit error, never a rounded value.
	DebitTotal    *string `json:"debit_total_minor"`
	CreditTotal   *string `json:"credit_total_minor"`
	Balance       *string `json:"balance_minor"`
	BalanceError  *string `json:"balance_error"`
	NormalBalance string  `json:"normal_balance"`
	EntryCount    int64   `json:"entry_count"`
}

// LedgerPaymentRef is the payment a journal belongs to.
type LedgerPaymentRef struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	AmountMinor string `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// LedgerJournalDetailView extends a journal with totals and its payment.
type LedgerJournalDetailView struct {
	LedgerJournalView
	DebitTotal  string            `json:"debit_total_minor"`
	CreditTotal string            `json:"credit_total_minor"`
	Payment     *LedgerPaymentRef `json:"payment"`
}

func (h *handler) serveLedger(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch {
	case r.URL.Path == "/v1/ledger/accounts":
		h.listLedgerAccounts(w, r, principal, id)
	case r.URL.Path == "/v1/ledger/transactions":
		h.listLedgerTransactions(w, r, principal, id)
	case strings.HasPrefix(r.URL.Path, "/v1/ledger/transactions/"):
		journalID := strings.TrimPrefix(r.URL.Path, "/v1/ledger/transactions/")
		if journalID == "" || strings.Contains(journalID, "/") {
			return false
		}
		h.getLedgerTransaction(w, r, principal, id, journalID)
	default:
		return false
	}
	return true
}

func ledgerReadFailure(w http.ResponseWriter, err error, id string) {
	if errors.Is(err, ledger.ErrOverflow) {
		// An exact answer does not exist in int64; never return a rounded one.
		fail(w, 500, "amount_overflow", "A ledger total exceeds the supported range", id)
		return
	}
	fail(w, 503, "unavailable", "Ledger unavailable", id)
}

func (h *handler) listLedgerAccounts(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	currency := r.URL.Query().Get("currency")
	if currency != "" && payment.ValidateCurrency(currency) != nil {
		fail(w, 400, "validation_error", "Invalid list parameters", id, FieldError{Field: "currency", Message: "Use a three-letter uppercase currency code"})
		return
	}
	accounts, err := ledger.ListAccounts(h.db.WithContext(r.Context()), principal.WorkspaceID, currency)
	if err != nil {
		ledgerReadFailure(w, err, id)
		return
	}
	items := make([]LedgerAccountView, 0, len(accounts))
	for _, a := range accounts {
		view := LedgerAccountView{
			ID: a.Account.ID, Code: a.Account.Code, Kind: a.Account.Kind, Currency: a.Account.Currency,
			ProviderID: a.Account.ProviderID, ProviderCode: a.ProviderCode,
			NormalBalance: a.NormalBalance, EntryCount: a.EntryCount,
		}
		if a.Err != nil {
			code := "amount_overflow"
			view.BalanceError = &code
		} else {
			debit, credit, balance := strconv.FormatInt(a.DebitTotal, 10), strconv.FormatInt(a.CreditTotal, 10), strconv.FormatInt(a.Balance, 10)
			view.DebitTotal, view.CreditTotal, view.Balance = &debit, &credit, &balance
		}
		items = append(items, view)
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (h *handler) getLedgerTransaction(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, journalID string) {
	if len(journalID) > 64 {
		fail(w, 404, "not_found", "Journal not found", id)
		return
	}
	db := h.db.WithContext(r.Context())
	detail, err := ledger.GetPosted(db, principal.WorkspaceID, journalID)
	if errors.Is(err, ledger.ErrJournalNotFound) {
		fail(w, 404, "not_found", "Journal not found", id)
		return
	}
	if err != nil {
		ledgerReadFailure(w, err, id)
		return
	}
	view := LedgerJournalDetailView{
		LedgerJournalView: newLedgerJournalView(detail.JournalView),
		DebitTotal:        strconv.FormatInt(detail.DebitTotal, 10),
		CreditTotal:       strconv.FormatInt(detail.CreditTotal, 10),
	}
	if detail.Journal.PaymentID != nil {
		var p storage.Payment
		if err := db.Where("id = ? AND workspace_id = ?", *detail.Journal.PaymentID, principal.WorkspaceID).Take(&p).Error; err == nil {
			view.Payment = &LedgerPaymentRef{ID: p.ID, Status: p.Status, AmountMinor: strconv.FormatInt(p.AmountMinor, 10), Currency: p.Currency}
		}
	}
	writeJSON(w, 200, view)
}
