package httpapi

import (
	"net/http"
	"strconv"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// LedgerEntryView is one posted entry; money is a decimal string.
type LedgerEntryView struct {
	ID          string  `json:"id"`
	AccountID   string  `json:"account_id"`
	AccountCode string  `json:"account_code"`
	ProviderID  *string `json:"provider_id"`
	Side        string  `json:"side"`
	AmountMinor string  `json:"amount_minor"`
}

// LedgerJournalView is one posted journal.
type LedgerJournalView struct {
	ID         string            `json:"id"`
	PaymentID  *string           `json:"payment_id"`
	Currency   string            `json:"currency"`
	Status     string            `json:"status"`
	PostingKey string            `json:"posting_key"`
	ReversalOf *string           `json:"reversal_of"`
	CreatedAt  string            `json:"created_at"`
	PostedAt   string            `json:"posted_at"`
	Entries    []LedgerEntryView `json:"entries"`
}

// listLedgerTransactions serves GET /v1/ledger/transactions: posted journals
// only, newest first, keyset cursor, optional payment_id filter, workspace scoped.
func (h *handler) listLedgerTransactions(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	query := r.URL.Query()
	filter := ledger.JournalFilter{Limit: defaultPageSize}
	var fields []FieldError
	if value := query.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > maxPageSize {
			fields = append(fields, FieldError{Field: "limit", Message: "Use an integer from 1 to 100"})
		}
		filter.Limit = limit
	}
	if value := query.Get("cursor"); value != "" {
		cursor, ok := decodeCursor(value)
		if !ok {
			fields = append(fields, FieldError{Field: "cursor", Message: "Use next_cursor from a previous page"})
		} else {
			filter.After = &ledger.JournalCursor{CreatedAt: cursor.CreatedAt, ID: cursor.ID}
		}
	}
	if value := query.Get("payment_id"); value != "" {
		if len(value) > 64 {
			fields = append(fields, FieldError{Field: "payment_id", Message: "Use a payment ID"})
		}
		filter.PaymentID = value
	}
	if len(fields) > 0 {
		fail(w, 400, "validation_error", "Invalid list parameters", id, fields...)
		return
	}
	pageSize := filter.Limit
	filter.Limit++
	journals, err := ledger.ListPosted(h.db.WithContext(r.Context()), principal.WorkspaceID, filter)
	if err != nil {
		fail(w, 503, "unavailable", "Ledger unavailable", id)
		return
	}
	var next *string
	if len(journals) > pageSize {
		journals = journals[:pageSize]
		last := journals[len(journals)-1].Journal
		cursor := encodeCursor(storage.PaymentCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		next = &cursor
	}
	items := make([]LedgerJournalView, 0, len(journals))
	for _, view := range journals {
		j := view.Journal
		postedAt := ""
		if j.PostedAt != nil {
			postedAt = payment.FormatTimestamp(*j.PostedAt)
		}
		entries := make([]LedgerEntryView, 0, len(view.Entries))
		for _, e := range view.Entries {
			entries = append(entries, LedgerEntryView{
				ID: e.ID, AccountID: e.AccountID, AccountCode: e.AccountCode, ProviderID: e.AccountProviderID,
				Side: e.Side, AmountMinor: strconv.FormatInt(e.AmountMinor, 10),
			})
		}
		items = append(items, LedgerJournalView{
			ID: j.ID, PaymentID: j.PaymentID, Currency: j.Currency, Status: j.Status, PostingKey: j.PostingKey, ReversalOf: j.ReversalOf,
			CreatedAt: payment.FormatTimestamp(j.CreatedAt), PostedAt: postedAt, Entries: entries,
		})
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
}
