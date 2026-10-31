package ledger

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

func openStore(t *testing.T, path string) *storage.Store {
	t.Helper()
	s, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func testStore(t *testing.T) (*storage.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.db")
	s := openStore(t, path)
	execSQL(t, s.DB, `INSERT INTO workspaces VALUES ('w1','One','sandbox',0),('w2','Two','sandbox',0);
 INSERT INTO providers VALUES ('prov1','w1','mock',1,0),('prov2','w1','stripe',1,0),('prov3','w2','mock',1,0);
 INSERT INTO payment_methods(id,workspace_id,rail,brand,last_four,created_at) VALUES ('m1','w1','mock','mock','0000',0),('m2','w2','mock','mock','0000',0);
 INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,status,version,created_at,updated_at) VALUES
  ('p1','w1','m1',25000,'USD','processing',2,0,0),
  ('p2','w1','m1',100,'USD','processing',2,0,0),
  ('p3','w1','m1',700,'EUR','processing',2,0,0),
  ('pfail','w1','m1',300,'USD','processing',2,0,0),
  ('pw2','w2','m2',500,'USD','processing',2,0,0);`)
	return s, path
}

func execSQL(t *testing.T, db *gorm.DB, sql string) {
	t.Helper()
	if err := db.Exec(sql).Error; err != nil {
		t.Fatalf("SQL failed: %v\n%s", err, sql)
	}
}

func postSuccess(t *testing.T, s *storage.Store, paymentID, providerID, currency string, amount int64) *Posting {
	t.Helper()
	var posting *Posting
	err := s.WithTx(context.Background(), func(tx *gorm.DB) error {
		var err error
		posting, err = PostPaymentSuccess(tx, "w1", paymentID, providerID, currency, amount)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return posting
}

func counts(t *testing.T, db *gorm.DB) (journals, entries int64) {
	t.Helper()
	if err := db.Model(&storage.LedgerJournal{}).Count(&journals).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&storage.LedgerEntry{}).Count(&entries).Error; err != nil {
		t.Fatal(err)
	}
	return journals, entries
}

func requireClean(t *testing.T, db *gorm.DB) {
	t.Helper()
	violations, err := Verify(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("invariant violations: %v", violations)
	}
	if err := Check(db); err != nil {
		t.Fatal(err)
	}
}

func accountByID(t *testing.T, db *gorm.DB, id string) storage.LedgerAccount {
	t.Helper()
	var account storage.LedgerAccount
	if err := db.Take(&account, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return account
}

func TestPaymentSuccessPostsBalancedJournal(t *testing.T) {
	s, _ := testStore(t)
	posting := postSuccess(t, s, "p1", "prov1", "USD", 25000)
	if posting.AlreadyPosted || posting.Journal.Status != "posted" || posting.Journal.PostedAt == nil {
		t.Fatalf("unexpected journal %+v", posting.Journal)
	}
	if posting.Journal.PostingKey != "payment:p1:success" || *posting.Journal.PaymentID != "p1" || posting.Journal.Currency != "USD" {
		t.Fatalf("unexpected journal identity %+v", posting.Journal)
	}
	if len(posting.Entries) != 2 {
		t.Fatalf("got %d entries", len(posting.Entries))
	}
	for _, entry := range posting.Entries {
		account := accountByID(t, s.DB, entry.AccountID)
		if entry.AmountMinor != 25000 || entry.Currency != "USD" || account.Currency != "USD" || account.WorkspaceID != "w1" {
			t.Fatalf("entry %+v account %+v", entry, account)
		}
		switch entry.Side {
		case "debit":
			if account.Code != "provider_clearing:prov1" || account.Kind != "asset" || account.ProviderID == nil || *account.ProviderID != "prov1" {
				t.Fatalf("debit account %+v", account)
			}
		case "credit":
			if account.Code != MerchantPayableCode || account.Kind != "liability" || account.ProviderID != nil {
				t.Fatalf("credit account %+v", account)
			}
		}
	}
	// A different provider gets its own clearing account; merchant payable is shared.
	other := postSuccess(t, s, "p2", "prov2", "USD", 100)
	var accounts int64
	s.DB.Model(&storage.LedgerAccount{}).Where("workspace_id = 'w1' AND currency = 'USD'").Count(&accounts)
	if accounts != 3 {
		t.Fatalf("expected two clearing accounts and one payable, got %d", accounts)
	}
	for _, entry := range other.Entries {
		if entry.Side == "debit" && accountByID(t, s.DB, entry.AccountID).Code != "provider_clearing:prov2" {
			t.Fatal("debit must use the executing provider's clearing account")
		}
	}
	// Per-currency accounts are distinct.
	postSuccess(t, s, "p3", "prov1", "EUR", 700)
	s.DB.Model(&storage.LedgerAccount{}).Where("workspace_id = 'w1'").Count(&accounts)
	if accounts != 5 {
		t.Fatalf("expected EUR accounts to be created separately, got %d accounts", accounts)
	}
	execSQL(t, s.DB, `UPDATE payments SET status='succeeded' WHERE id IN ('p1','p2','p3')`)
	requireClean(t, s.DB)
}

func TestDuplicateSuccessDeliveryPostsOnce(t *testing.T) {
	s, path := testStore(t)
	first := postSuccess(t, s, "p1", "prov1", "USD", 25000)
	for i := 0; i < 2; i++ {
		again := postSuccess(t, s, "p1", "prov1", "USD", 25000)
		if !again.AlreadyPosted || again.Journal.ID != first.Journal.ID || len(again.Entries) != 2 {
			t.Fatalf("repeat %d: %+v", i, again)
		}
	}
	if journals, entries := counts(t, s.DB); journals != 1 || entries != 2 {
		t.Fatalf("sequential repeats: %d journals %d entries", journals, entries)
	}

	// Concurrent deliveries through independent connections to the same file.
	stores := []*storage.Store{s, openStore(t, path), openStore(t, path)}
	var wg sync.WaitGroup
	results := make([]*Posting, len(stores))
	errs := make([]error, len(stores))
	for i, store := range stores {
		wg.Add(1)
		go func(i int, store *storage.Store) {
			defer wg.Done()
			errs[i] = store.WithTx(context.Background(), func(tx *gorm.DB) error {
				var err error
				results[i], err = PostPaymentSuccess(tx, "w1", "p2", "prov1", "USD", 100)
				return err
			})
		}(i, store)
	}
	wg.Wait()
	created := 0
	for i := range stores {
		if errs[i] != nil {
			t.Fatalf("concurrent delivery %d: %v", i, errs[i])
		}
		if !results[i].AlreadyPosted {
			created++
		}
		if results[i].Journal.ID != results[0].Journal.ID {
			t.Fatal("concurrent deliveries returned different journals")
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly one creating delivery, got %d", created)
	}
	if journals, entries := counts(t, s.DB); journals != 2 || entries != 4 {
		t.Fatalf("after concurrent repeats: %d journals %d entries", journals, entries)
	}
	execSQL(t, s.DB, `UPDATE payments SET status='succeeded' WHERE id IN ('p1','p2')`)
	requireClean(t, s.DB)
}

func TestDuplicateKeyMismatchIsRejected(t *testing.T) {
	s, _ := testStore(t)
	postSuccess(t, s, "p1", "prov1", "USD", 25000)
	err := s.WithTx(context.Background(), func(tx *gorm.DB) error {
		_, err := PostPaymentSuccess(tx, "w1", "p1", "prov2", "USD", 25000)
		return err
	})
	if !errors.Is(err, ErrPostingMismatch) {
		t.Fatalf("different provider for an existing key: %v", err)
	}
	err = s.WithTx(context.Background(), func(tx *gorm.DB) error {
		clearing, err := EnsureProviderClearingAccount(tx, "w1", "prov1", "USD")
		if err != nil {
			return err
		}
		payable, err := EnsureMerchantPayableAccount(tx, "w1", "USD")
		if err != nil {
			return err
		}
		_, err = Post(tx, JournalSpec{WorkspaceID: "w1", Currency: "USD", PostingKey: SuccessPostingKey("p1"), Lines: []Line{
			{AccountID: clearing.ID, Side: Debit, AmountMinor: 1}, {AccountID: payable.ID, Side: Credit, AmountMinor: 1},
		}})
		return err
	})
	if !errors.Is(err, ErrPostingMismatch) {
		t.Fatalf("different amount for an existing key: %v", err)
	}
	for _, c := range []struct {
		payment, currency string
		amount            int64
	}{{"p1", "USD", 1}, {"p1", "EUR", 25000}, {"pw2", "USD", 500}, {"missing", "USD", 1}} {
		err = s.WithTx(context.Background(), func(tx *gorm.DB) error {
			_, err := PostPaymentSuccess(tx, "w1", c.payment, "prov1", c.currency, c.amount)
			return err
		})
		if !errors.Is(err, ErrPaymentMismatch) {
			t.Fatalf("%+v: %v", c, err)
		}
	}
	err = s.WithTx(context.Background(), func(tx *gorm.DB) error {
		_, err := PostPaymentSuccess(tx, "w1", "p2", "prov3", "USD", 100)
		return err
	})
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("cross-workspace provider: %v", err)
	}
	if journals, entries := counts(t, s.DB); journals != 1 || entries != 2 {
		t.Fatalf("rejected postings wrote rows: %d %d", journals, entries)
	}
}

func TestRollbackMidPostingLeavesNothing(t *testing.T) {
	s, _ := testStore(t)
	injected := errors.New("injected failure after entries")
	afterEntriesHook = func(tx *gorm.DB) error {
		var entries int64
		if err := tx.Model(&storage.LedgerEntry{}).Count(&entries).Error; err != nil {
			return err
		}
		if entries != 2 {
			return errors.New("entries were not inserted before the hook")
		}
		return injected
	}
	t.Cleanup(func() { afterEntriesHook = nil })
	err := s.WithTx(context.Background(), func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE payments SET status='succeeded', version=version+1 WHERE id='p1'`).Error; err != nil {
			return err
		}
		_, err := PostPaymentSuccess(tx, "w1", "p1", "prov1", "USD", 25000)
		return err
	})
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
	if journals, entries := counts(t, s.DB); journals != 0 || entries != 0 {
		t.Fatalf("rollback left %d journals %d entries", journals, entries)
	}
	var accounts int64
	s.DB.Model(&storage.LedgerAccount{}).Count(&accounts)
	var payment paymentRow
	if err := s.DB.Take(&payment, "id = 'p1'").Error; err != nil {
		t.Fatal(err)
	}
	var version int64
	s.DB.Raw(`SELECT version FROM payments WHERE id='p1'`).Scan(&version)
	if accounts != 0 || payment.Status != "processing" || version != 2 {
		t.Fatalf("rollback left accounts=%d status=%s version=%d", accounts, payment.Status, version)
	}
	afterEntriesHook = nil
	postSuccess(t, s, "p1", "prov1", "USD", 25000)
	if journals, entries := counts(t, s.DB); journals != 1 || entries != 2 {
		t.Fatalf("retry after rollback: %d %d", journals, entries)
	}
}

func TestServiceRejectsInvalidJournals(t *testing.T) {
	s, _ := testStore(t)
	var usd, usd2, eur, w2 *storage.LedgerAccount
	err := s.WithTx(context.Background(), func(tx *gorm.DB) error {
		var err error
		if usd, err = EnsureProviderClearingAccount(tx, "w1", "prov1", "USD"); err != nil {
			return err
		}
		if usd2, err = EnsureMerchantPayableAccount(tx, "w1", "USD"); err != nil {
			return err
		}
		if eur, err = EnsureMerchantPayableAccount(tx, "w1", "EUR"); err != nil {
			return err
		}
		w2, err = EnsureMerchantPayableAccount(tx, "w2", "USD")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		lines []Line
		want  error
	}{
		{"unbalanced", []Line{{usd.ID, Debit, 100}, {usd2.ID, Credit, 99}}, ErrUnbalanced},
		{"single entry", []Line{{usd.ID, Debit, 100}}, ErrSingleEntry},
		{"no entries", nil, ErrSingleEntry},
		{"mixed currency", []Line{{usd.ID, Debit, 100}, {eur.ID, Credit, 100}}, ErrAccountMismatch},
		{"cross workspace", []Line{{usd.ID, Debit, 100}, {w2.ID, Credit, 100}}, ErrAccountMismatch},
		{"missing account", []Line{{usd.ID, Debit, 100}, {"nope", Credit, 100}}, ErrAccountNotFound},
		{"overflow sum", []Line{{usd.ID, Debit, math.MaxInt64}, {usd.ID, Debit, 1}, {usd2.ID, Credit, math.MaxInt64}, {usd2.ID, Credit, 1}}, ErrOverflow},
		{"zero amount", []Line{{usd.ID, Debit, 0}, {usd2.ID, Credit, 0}}, ErrInvalidJournal},
		{"negative amount", []Line{{usd.ID, Debit, -5}, {usd2.ID, Credit, -5}}, ErrInvalidJournal},
		{"bad side", []Line{{usd.ID, "sideways", 5}, {usd2.ID, Credit, 5}}, ErrInvalidJournal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.WithTx(context.Background(), func(tx *gorm.DB) error {
				_, err := Post(tx, JournalSpec{WorkspaceID: "w1", Currency: "USD", PostingKey: "test:" + c.name, Lines: c.lines})
				return err
			})
			if !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
	if _, err := Post(s.DB, JournalSpec{WorkspaceID: "w1", Currency: "USD", PostingKey: "outside", Lines: []Line{{usd.ID, Debit, 1}, {usd2.ID, Credit, 1}}}); !errors.Is(err, ErrNoTransaction) {
		t.Fatalf("posting outside a transaction: %v", err)
	}
	if journals, entries := counts(t, s.DB); journals != 0 || entries != 0 {
		t.Fatalf("rejected postings wrote %d journals %d entries", journals, entries)
	}
}

// postRaw inserts a draft journal and entries with SQL, then marks it posted,
// bypassing the service so only database constraints and triggers apply.
func postRaw(db *gorm.DB, journalID, currency string, entries [][4]string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO ledger_journals(id,workspace_id,currency,posting_key,status,created_at) VALUES(?,?,?,?,?,0)`,
			journalID, "w1", currency, "raw:"+journalID, "draft").Error; err != nil {
			return err
		}
		for i, e := range entries {
			if err := tx.Exec(`INSERT INTO ledger_entries(id,workspace_id,journal_id,account_id,currency,side,amount_minor,created_at) VALUES(?,?,?,?,?,?,CAST(? AS INTEGER),0)`,
				journalID+"-"+string(rune('a'+i)), "w1", journalID, e[0], e[1], e[2], e[3]).Error; err != nil {
				return err
			}
		}
		return tx.Exec(`UPDATE ledger_journals SET status='posted', posted_at=1 WHERE id=?`, journalID).Error
	})
}

func TestDatabaseTriggersRejectInvalidJournals(t *testing.T) {
	s, _ := testStore(t)
	execSQL(t, s.DB, `INSERT INTO ledger_accounts(id,workspace_id,provider_id,code,currency,kind,created_at) VALUES
  ('clear','w1','prov1','provider_clearing:prov1','USD','asset',0),
  ('pay','w1',NULL,'merchant_payable','USD','liability',0),
  ('payeur','w1',NULL,'merchant_payable','EUR','liability',0),
  ('payw2','w2',NULL,'merchant_payable','USD','liability',0)`)
	max := "9223372036854775807"
	cases := []struct {
		name    string
		entries [][4]string
		message string
	}{
		{"unbalanced", [][4]string{{"clear", "USD", "debit", "100"}, {"pay", "USD", "credit", "99"}}, "not balanced"},
		{"single entry", [][4]string{{"clear", "USD", "debit", "100"}}, "at least two entries"},
		{"mixed currency entry", [][4]string{{"clear", "USD", "debit", "100"}, {"payeur", "EUR", "credit", "100"}}, "FOREIGN KEY"},
		{"mixed currency account", [][4]string{{"clear", "USD", "debit", "100"}, {"payeur", "USD", "credit", "100"}}, "FOREIGN KEY"},
		{"cross workspace account", [][4]string{{"clear", "USD", "debit", "100"}, {"payw2", "USD", "credit", "100"}}, "FOREIGN KEY"},
		{"overflow sum", [][4]string{{"clear", "USD", "debit", max}, {"clear", "USD", "debit", "1"}, {"pay", "USD", "credit", max}, {"pay", "USD", "credit", "1"}}, "overflow"},
		{"zero amount", [][4]string{{"clear", "USD", "debit", "0"}, {"pay", "USD", "credit", "0"}}, "CHECK"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := postRaw(s.DB, "j-"+strings.ReplaceAll(c.name, " ", "-"), "USD", c.entries)
			if err == nil || !strings.Contains(err.Error(), c.message) {
				t.Fatalf("expected %q rejection, got %v", c.message, err)
			}
		})
	}
	if err := s.DB.Exec(`INSERT INTO ledger_journals(id,workspace_id,currency,posting_key,status,posted_at,created_at) VALUES('direct','w1','USD','direct','posted',1,0)`).Error; err == nil {
		t.Fatal("journal inserted directly as posted")
	}
	if journals, entries := counts(t, s.DB); journals != 0 || entries != 0 {
		t.Fatalf("rejected raw postings left %d journals %d entries", journals, entries)
	}
	if err := postRaw(s.DB, "valid", "USD", [][4]string{{"clear", "USD", "debit", "100"}, {"pay", "USD", "credit", "100"}}); err != nil {
		t.Fatalf("valid raw posting rejected: %v", err)
	}
}

func TestPostedRowsAreImmutableInDatabase(t *testing.T) {
	s, _ := testStore(t)
	posting := postSuccess(t, s, "p1", "prov1", "USD", 25000)
	j, e := posting.Journal.ID, posting.Entries[0].ID
	statements := []string{
		`UPDATE ledger_journals SET posting_key='changed' WHERE id='` + j + `'`,
		`UPDATE ledger_journals SET status='draft', posted_at=NULL WHERE id='` + j + `'`,
		`DELETE FROM ledger_journals WHERE id='` + j + `'`,
		`UPDATE ledger_entries SET amount_minor=1 WHERE id='` + e + `'`,
		`UPDATE ledger_entries SET side='credit' WHERE id='` + e + `'`,
		`DELETE FROM ledger_entries WHERE id='` + e + `'`,
		`INSERT INTO ledger_entries(id,workspace_id,journal_id,account_id,currency,side,amount_minor,created_at) SELECT 'extra',workspace_id,journal_id,account_id,currency,side,1,0 FROM ledger_entries WHERE id='` + e + `'`,
	}
	for _, statement := range statements {
		if err := s.DB.Exec(statement).Error; err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("posted rows changed by %q: %v", statement, err)
		}
	}
	if journals, entries := counts(t, s.DB); journals != 1 || entries != 2 {
		t.Fatalf("posted rows changed: %d %d", journals, entries)
	}
}

func TestReversalOffsetsOnce(t *testing.T) {
	s, _ := testStore(t)
	original := postSuccess(t, s, "p1", "prov1", "USD", 25000)
	var reversal *Posting
	err := s.WithTx(context.Background(), func(tx *gorm.DB) error {
		var err error
		reversal, err = Reverse(tx, "w1", original.Journal.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if reversal.Journal.ReversalOf == nil || *reversal.Journal.ReversalOf != original.Journal.ID || reversal.Journal.Status != "posted" ||
		reversal.Journal.PostingKey != "journal:"+original.Journal.ID+":reversal" || *reversal.Journal.PaymentID != "p1" {
		t.Fatalf("unexpected reversal %+v", reversal.Journal)
	}
	if !offsets(entryLines(original.Entries), entryLines(reversal.Entries)) {
		t.Fatal("reversal does not offset original")
	}
	balances := map[string]int64{}
	var all []storage.LedgerEntry
	s.DB.Find(&all)
	for _, entry := range all {
		if entry.Side == "debit" {
			balances[entry.AccountID] += entry.AmountMinor
		} else {
			balances[entry.AccountID] -= entry.AmountMinor
		}
	}
	for account, balance := range balances {
		if balance != 0 {
			t.Fatalf("account %s nets to %d after reversal", account, balance)
		}
	}
	execSQL(t, s.DB, `UPDATE payments SET status='succeeded' WHERE id='p1'`)
	requireClean(t, s.DB)

	err = s.WithTx(context.Background(), func(tx *gorm.DB) error {
		_, err := Reverse(tx, "w1", original.Journal.ID)
		return err
	})
	if !errors.Is(err, ErrAlreadyReversed) {
		t.Fatalf("second reversal: %v", err)
	}
	// The database also rejects a second reversal row for the same original.
	err = s.DB.Exec(`INSERT INTO ledger_journals(id,workspace_id,payment_id,currency,posting_key,status,reversal_of,created_at) VALUES('again','w1','p1','USD','again','draft',?,0)`, original.Journal.ID).Error
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("database accepted a second reversal: %v", err)
	}
	err = s.WithTx(context.Background(), func(tx *gorm.DB) error {
		_, err := Reverse(tx, "w1", "missing")
		return err
	})
	if !errors.Is(err, ErrJournalNotFound) {
		t.Fatalf("reverse missing journal: %v", err)
	}
	err = s.WithTx(context.Background(), func(tx *gorm.DB) error {
		_, err := Reverse(tx, "w2", original.Journal.ID)
		return err
	})
	if !errors.Is(err, ErrJournalNotFound) {
		t.Fatalf("cross-workspace reversal: %v", err)
	}
	if journals, entries := counts(t, s.DB); journals != 2 || entries != 4 {
		t.Fatalf("after reversal: %d journals %d entries", journals, entries)
	}
}

func TestVerifyDetectsViolations(t *testing.T) {
	s, _ := testStore(t)
	requireClean(t, s.DB)
	posting := postSuccess(t, s, "pfail", "prov1", "USD", 300)
	execSQL(t, s.DB, `UPDATE payments SET status='failed' WHERE id='pfail'`)
	execSQL(t, s.DB, `UPDATE payments SET status='succeeded' WHERE id='p1'`)
	execSQL(t, s.DB, `UPDATE payments SET status='canceled' WHERE id='p2'`)
	execSQL(t, s.DB, `INSERT INTO ledger_journals(id,workspace_id,currency,posting_key,status,created_at) VALUES('stale','w1','USD','stale','draft',0)`)
	violations, err := Verify(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, v := range violations {
		kinds[v.Kind] = true
	}
	for _, want := range []string{ViolationUnexpectedSuccess, ViolationMissingSuccess, ViolationDraftJournal} {
		if !kinds[want] {
			t.Fatalf("missing %s in %v", want, violations)
		}
	}
	if len(violations) != 3 {
		t.Fatalf("unexpected violations %v", violations)
	}
	if err := Check(s.DB); !errors.Is(err, ErrInvariantViolated) {
		t.Fatalf("check: %v", err)
	}
	if posting.Journal.PaymentID == nil {
		t.Fatal("success journal must reference its payment")
	}
}
