package tests

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

func entryOf(side string, amount int64) storage.LedgerEntry {
	return storage.LedgerEntry{Side: side, AmountMinor: amount}
}

type accountBody struct {
	ID           string  `json:"id"`
	Code         string  `json:"code"`
	Kind         string  `json:"kind"`
	Currency     string  `json:"currency"`
	ProviderID   *string `json:"provider_id"`
	ProviderCode *string `json:"provider_code"`
	Debit        *string `json:"debit_total_minor"`
	Credit       *string `json:"credit_total_minor"`
	Balance      *string `json:"balance_minor"`
	BalanceError *string `json:"balance_error"`
	EntryCount   int64   `json:"entry_count"`
}

type journalDetailBody struct {
	ID          string  `json:"id"`
	PaymentID   *string `json:"payment_id"`
	Currency    string  `json:"currency"`
	Status      string  `json:"status"`
	ReversalOf  *string `json:"reversal_of"`
	ReversedBy  *string `json:"reversed_by"`
	DebitTotal  string  `json:"debit_total_minor"`
	CreditTotal string  `json:"credit_total_minor"`
	Entries     []struct {
		Side        string `json:"side"`
		AmountMinor string `json:"amount_minor"`
	} `json:"entries"`
	Payment *struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		AmountMinor string `json:"amount_minor"`
		Currency    string `json:"currency"`
	} `json:"payment"`
}

func (e *env) accounts(query, key string) []accountBody {
	e.t.Helper()
	response := e.do("GET", "/v1/ledger/accounts"+query, key, "", "")
	if response.Code != 200 {
		e.t.Fatalf("accounts %d %s", response.Code, response.Body.String())
	}
	var page struct {
		Items []accountBody `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		e.t.Fatal(err)
	}
	return page.Items
}

func (e *env) journalDetail(id, key string) (int, journalDetailBody) {
	e.t.Helper()
	response := e.do("GET", "/v1/ledger/transactions/"+id, key, "", "")
	var detail journalDetailBody
	if response.Code == 200 {
		if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
			e.t.Fatal(err)
		}
	}
	return response.Code, detail
}

func TestLedgerDetailTotalsMatchDatabaseAndLinkPayment(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["success"], "12345", "USD")
	d.drain()
	journal := d.journals(id)[0]
	code, detail := d.journalDetail(journal.ID, credentials.ReaderKey)
	if code != 200 || detail.Status != "posted" || detail.Currency != "USD" || len(detail.Entries) != 2 {
		t.Fatalf("detail %d %+v", code, detail)
	}
	var debit, credit int64
	if err := d.store.DB.Raw(`SELECT coalesce(sum(CASE WHEN side='debit' THEN amount_minor END),0), coalesce(sum(CASE WHEN side='credit' THEN amount_minor END),0) FROM ledger_entries WHERE journal_id=?`, journal.ID).Row().Scan(&debit, &credit); err != nil {
		t.Fatal(err)
	}
	if detail.DebitTotal != strconv.FormatInt(debit, 10) || detail.CreditTotal != strconv.FormatInt(credit, 10) || debit != 12345 || debit != credit {
		t.Fatalf("totals %s %s vs db %d %d", detail.DebitTotal, detail.CreditTotal, debit, credit)
	}
	if detail.Payment == nil || detail.Payment.ID != id || detail.Payment.Status != "succeeded" || detail.Payment.AmountMinor != "12345" {
		t.Fatalf("payment link %+v", detail.Payment)
	}
	if detail.ReversalOf != nil || detail.ReversedBy != nil {
		t.Fatal("unexpected reversal link")
	}
	d.requireLedger()
}

func TestLedgerAccountBalancesMatchEntries(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	d.accept(scenarioMethod["success"], "100", "USD")
	d.accept(scenarioMethod["success"], "250", "USD")
	d.accept(scenarioMethod["success"], "7", "EUR")
	d.accept(scenarioMethod["decline"], "999", "USD")
	d.drain()
	accounts := d.accounts("", credentials.ReaderKey)
	if len(accounts) != 4 {
		t.Fatalf("accounts %+v", accounts)
	}
	seenUSD := false
	for _, a := range accounts {
		var debit, credit, count int64
		if err := d.store.DB.Raw(`SELECT coalesce(sum(CASE WHEN e.side='debit' THEN e.amount_minor END),0), coalesce(sum(CASE WHEN e.side='credit' THEN e.amount_minor END),0), count(*) FROM ledger_entries e JOIN ledger_journals j ON j.id=e.journal_id WHERE e.account_id=? AND j.status='posted'`, a.ID).Row().Scan(&debit, &credit, &count); err != nil {
			t.Fatal(err)
		}
		want := debit - credit
		if a.Kind == "liability" {
			want = credit - debit
		}
		if a.BalanceError != nil || *a.Debit != strconv.FormatInt(debit, 10) || *a.Credit != strconv.FormatInt(credit, 10) || *a.Balance != strconv.FormatInt(want, 10) || a.EntryCount != count {
			t.Errorf("account %+v vs db debit=%d credit=%d", a, debit, credit)
		}
		if a.Currency == "USD" && a.Code == ledger.MerchantPayableCode {
			seenUSD = true
			if *a.Balance != "350" {
				t.Errorf("USD payable %s", *a.Balance)
			}
		}
		if a.Code != ledger.MerchantPayableCode && (a.ProviderID == nil || a.ProviderCode == nil || *a.ProviderCode != "mock") {
			t.Errorf("clearing account provider %+v", a)
		}
	}
	if !seenUSD {
		t.Fatal("no USD merchant payable account")
	}
	// The currency filter never mixes currencies, and balances are never added across them.
	for _, a := range d.accounts("?currency=EUR", credentials.ReaderKey) {
		if a.Currency != "EUR" {
			t.Fatalf("filter leaked %s", a.Currency)
		}
	}
	if r := d.do("GET", "/v1/ledger/accounts?currency=usd", credentials.ReaderKey, "", ""); r.Code != 400 {
		t.Fatalf("bad currency %d", r.Code)
	}
	if page := d.ledgerPage("?currency=EUR", credentials.ReaderKey); len(page.Items) != 1 || page.Items[0].Currency != "EUR" {
		t.Fatalf("journal currency filter %+v", page.Items)
	}
}

func TestLedgerNeverReturnsDraftJournals(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["success"], "100", "USD")
	d.drain()
	posted := d.journals(id)[0]
	var accountID string
	if err := d.store.DB.Raw(`SELECT account_id FROM ledger_entries WHERE journal_id=? AND side='debit'`, posted.ID).Scan(&accountID).Error; err != nil {
		t.Fatal(err)
	}
	// A draft that has entries but is not posted: it must be invisible everywhere.
	d.exec(`INSERT INTO ledger_journals(id,workspace_id,currency,posting_key,status,created_at) VALUES('draft-1',?,'USD','draft-key','draft',?)`, auth.DemoWorkspaceID, posted.CreatedAt+10)
	d.exec(`INSERT INTO ledger_entries(id,workspace_id,journal_id,account_id,currency,side,amount_minor,created_at) VALUES('draft-e1',?,'draft-1',?,'USD','debit',5000,1)`, auth.DemoWorkspaceID, accountID)
	if page := d.ledgerPage("", credentials.ReaderKey); len(page.Items) != 1 || page.Items[0].ID != posted.ID {
		t.Fatalf("draft listed: %+v", page.Items)
	}
	if code, _ := d.journalDetail("draft-1", credentials.OperatorKey); code != 404 {
		t.Fatalf("draft detail %d", code)
	}
	for _, a := range d.accounts("", credentials.ReaderKey) {
		if a.ID == accountID && (*a.Debit != "100" || a.EntryCount != 1) {
			t.Fatalf("draft counted in balance: %+v", a)
		}
	}
}

func TestLedgerWorkspaceIsolationAndReadOnly(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["success"], "100", "USD")
	d.drain()
	journal := d.journals(id)[0]
	if code, _ := d.journalDetail(journal.ID, credentials.OtherWorkspaceKey); code != 404 {
		t.Fatalf("cross-workspace detail %d", code)
	}
	if accounts := d.accounts("", credentials.OtherWorkspaceKey); len(accounts) != 0 {
		t.Fatalf("accounts leaked: %+v", accounts)
	}
	if code, _ := d.journalDetail("no-such-journal", credentials.ReaderKey); code != 404 {
		t.Fatalf("missing detail %d", code)
	}
	before := d.dump()
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, path := range []string{"/v1/ledger/transactions/" + journal.ID, "/v1/ledger/transactions", "/v1/ledger/accounts"} {
			if r := d.do(method, path, credentials.OperatorKey, "", "{}"); r.Code < 400 {
				t.Errorf("%s %s succeeded with %d", method, path, r.Code)
			}
		}
	}
	after := d.dump()
	for table := range before {
		if before[table] != after[table] {
			t.Errorf("%s changed", table)
		}
	}
}

func TestLedgerReversalLinksBothWays(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["success"], "100", "USD")
	d.drain()
	original := d.journals(id)[0]
	var reversalID string
	err := d.store.WithTx(t.Context(), func(tx *gorm.DB) error {
		posting, err := ledger.Reverse(tx, auth.DemoWorkspaceID, original.ID)
		if posting != nil {
			reversalID = posting.Journal.ID
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, forward := d.journalDetail(original.ID, credentials.ReaderKey)
	_, backward := d.journalDetail(reversalID, credentials.ReaderKey)
	if forward.ReversedBy == nil || *forward.ReversedBy != reversalID || forward.ReversalOf != nil ||
		backward.ReversalOf == nil || *backward.ReversalOf != original.ID || backward.ReversedBy != nil {
		t.Fatalf("links %+v %+v", forward, backward)
	}
	if page := d.ledgerPage("?payment_id="+id, credentials.ReaderKey); len(page.Items) != 2 {
		t.Fatalf("journals %d", len(page.Items))
	}
	for _, a := range d.accounts("", credentials.ReaderKey) {
		if *a.Balance != "0" {
			t.Errorf("reversed balance %+v", a)
		}
	}
}

func TestLedgerOverflowIsExplicitNeverRounded(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	// Two max-int64 KWD successes: each journal fits, the account total cannot.
	d.accept(scenarioMethod["success"], strconv.FormatInt(math.MaxInt64, 10), "KWD")
	d.accept(scenarioMethod["success"], strconv.FormatInt(math.MaxInt64, 10), "KWD")
	d.accept(scenarioMethod["success"], "5", "USD")
	d.drain()
	overflowed := 0
	for _, a := range d.accounts("", credentials.ReaderKey) {
		switch {
		case a.Currency == "KWD":
			overflowed++
			if a.BalanceError == nil || *a.BalanceError != "amount_overflow" || a.Balance != nil || a.Debit != nil || a.Credit != nil {
				t.Errorf("KWD account must report overflow, not a number: %+v", a)
			}
		case a.BalanceError != nil || a.Balance == nil:
			t.Errorf("overflow leaked into %+v", a)
		}
	}
	if overflowed != 2 {
		t.Fatalf("overflowed accounts %d", overflowed)
	}
	// Journal-level checked arithmetic.
	if _, err := ledger.AddChecked(math.MaxInt64, 1); err != ledger.ErrOverflow {
		t.Fatalf("AddChecked %v", err)
	}
	if _, err := ledger.SubChecked(math.MinInt64, 1); err != ledger.ErrOverflow {
		t.Fatalf("SubChecked %v", err)
	}
	if _, _, err := ledger.Totals([]ledger.EntryView{{LedgerEntry: entryOf("debit", math.MaxInt64)}, {LedgerEntry: entryOf("debit", 1)}}); err != ledger.ErrOverflow {
		t.Fatalf("Totals %v", err)
	}
	sum, err := ledger.AddChecked(math.MaxInt64-1, 1)
	if err != nil || sum != math.MaxInt64 {
		t.Fatalf("exact max %d %v", sum, err)
	}
}
