package tests

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/provider"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// submittedPayment leaves a payment processing with a submitted attempt whose
// mock charge succeeded: the provider answered but nothing was recorded.
func submittedPayment(t *testing.T, d *dispatchEnv, amount string) (string, storage.PaymentAttempt) {
	t.Helper()
	crash := errors.New("crash")
	d.build(dispatchConfig{hooks: payment.ExecutorHooks{BeforeRecord: func(context.Context, payment.PreparedAttempt, provider.Result) error { return crash }}})
	id := d.accept(scenarioMethod["success"], amount, "USD")
	claims, err := event.ClaimDue(context.Background(), d.store.DB, []string{payment.OutboxPaymentDispatch}, time.Now(), time.Minute, 1)
	if err != nil || len(claims) != 1 {
		t.Fatal(claims, err)
	}
	if err := d.executor.Handle(context.Background(), claims[0]); !errors.Is(err, crash) {
		t.Fatal(err)
	}
	d.build(dispatchConfig{})
	return id, d.attempts(id)[0]
}

func successOutcome(id string, attempt storage.PaymentAttempt) payment.Outcome {
	return payment.Outcome{
		WorkspaceID: auth.DemoWorkspaceID, PaymentID: id, AttemptID: attempt.ID, ProviderReference: "mock_ref_" + attempt.ID,
		ActorReference: "test:delivery", Now: time.Now().UnixMilli(),
	}
}

// One journal with two balanced entries after the same success result is
// delivered three times sequentially and three times concurrently.
func TestDuplicateSuccessDeliveryPostsOneJournal(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id, attempt := submittedPayment(t, d, "123456789")
	changed := 0
	for i := 0; i < 3; i++ {
		var applied payment.Applied
		if err := d.store.DB.Transaction(func(tx *gorm.DB) error {
			var err error
			applied, err = payment.ApplySuccess(tx, successOutcome(id, attempt))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if applied.Changed {
			changed++
		}
		if applied.Posting == nil || len(applied.Posting.Entries) != 2 || applied.Posting.AlreadyPosted == applied.Changed {
			t.Fatalf("delivery %d: %+v", i, applied.Posting)
		}
	}
	// Concurrent deliveries over three separate connections.
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < 3; i++ {
		store, err := storage.Open(context.Background(), d.path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := store.DB.Transaction(func(tx *gorm.DB) error {
				applied, err := payment.ApplySuccess(tx, successOutcome(id, attempt))
				if applied.Changed {
					mu.Lock()
					changed++
					mu.Unlock()
				}
				return err
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if changed != 1 {
		t.Fatalf("success applied %d times", changed)
	}
	journals := d.journals(id)
	if len(journals) != 1 || journals[0].PostingKey != ledger.SuccessPostingKey(id) {
		t.Fatalf("journals %+v", journals)
	}
	var entries []storage.LedgerEntry
	d.store.DB.Where("journal_id = ?", journals[0].ID).Find(&entries)
	if len(entries) != 2 || entries[0].AmountMinor != 123456789 || entries[1].AmountMinor != 123456789 || entries[0].Side == entries[1].Side {
		t.Fatalf("entries %+v", entries)
	}
	if got := joined(d.statuses(id)); got != "created>processing>succeeded" {
		t.Fatalf("audit %s", got)
	}
	d.requireLedger()
}

// Concurrent deliveries racing a fresh (not yet applied) success.
func TestConcurrentFirstSuccessDeliveryPostsOnce(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id, attempt := submittedPayment(t, d, "500")
	var wg sync.WaitGroup
	var mu sync.Mutex
	changed := 0
	for i := 0; i < 3; i++ {
		store, err := storage.Open(context.Background(), d.path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.DB.Transaction(func(tx *gorm.DB) error {
				applied, err := payment.ApplySuccess(tx, successOutcome(id, attempt))
				if applied.Changed {
					mu.Lock()
					changed++
					mu.Unlock()
				}
				return err
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if changed != 1 || len(d.journals(id)) != 1 {
		t.Fatalf("changed %d journals %d", changed, len(d.journals(id)))
	}
	d.requireLedger()
}

// An error after entries are inserted, before commit, leaves no journal, no
// entries, the payment and attempt unchanged and the outbox row still owned.
func TestRollbackMidPostingLeavesNothing(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id, attempt := submittedPayment(t, d, "2500")
	before := d.dump()
	forced := errors.New("forced failure before commit")
	err := d.store.DB.Transaction(func(tx *gorm.DB) error {
		applied, err := payment.ApplySuccess(tx, successOutcome(id, attempt))
		if err != nil {
			return err
		}
		var entries int64
		tx.Table("ledger_entries").Where("journal_id = ?", applied.Posting.Journal.ID).Count(&entries)
		if entries != 2 {
			t.Errorf("entries inside tx %d", entries)
		}
		return forced
	})
	if !errors.Is(err, forced) {
		t.Fatal(err)
	}
	after := d.dump()
	for table, sum := range before {
		if after[table] != sum {
			t.Fatalf("rollback left changes in %s", table)
		}
	}
	if n := d.count("ledger_journals", ""); n != 0 {
		t.Fatalf("journals %d", n)
	}
	requireStatus(t, d.payment(id), payment.StatusProcessing)
	d.requireLedger()
}

// A success result for a payment already failed is rejected and posts nothing;
// decline and unknown results are idempotent.
func TestLateAndRepeatedOutcomes(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["decline"], "300", "USD")
	d.drain()
	attempt := d.attempts(id)[0]
	err := d.store.DB.Transaction(func(tx *gorm.DB) error {
		_, err := payment.ApplySuccess(tx, successOutcome(id, attempt))
		return err
	})
	if !errors.Is(err, payment.ErrLateOutcome) {
		t.Fatalf("late success: %v", err)
	}
	if err := d.store.DB.Transaction(func(tx *gorm.DB) error {
		applied, err := payment.ApplyDecline(tx, successOutcome(id, attempt))
		if applied.Changed {
			t.Error("repeat decline changed state")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, d.payment(id), payment.StatusFailed)
	if n := len(d.journals(id)); n != 0 {
		t.Fatalf("journals %d", n)
	}
	d.requireLedger()
}

type ledgerPage struct {
	Items []struct {
		ID        string  `json:"id"`
		PaymentID *string `json:"payment_id"`
		Currency  string  `json:"currency"`
		Status    string  `json:"status"`
		Entries   []struct {
			AccountCode string  `json:"account_code"`
			ProviderID  *string `json:"provider_id"`
			Side        string  `json:"side"`
			AmountMinor string  `json:"amount_minor"`
		} `json:"entries"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func (e *env) ledgerPage(query, key string) ledgerPage {
	e.t.Helper()
	response := e.do("GET", "/v1/ledger/transactions"+query, key, "", "")
	if response.Code != 200 {
		e.t.Fatalf("ledger list %d %s", response.Code, response.Body.String())
	}
	var page ledgerPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		e.t.Fatal(err)
	}
	return page
}

func TestLedgerTransactionsEndpoint(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	ids := []string{
		d.accept(scenarioMethod["success"], "9223372036854775807", "KWD"),
		d.accept(scenarioMethod["success"], "100", "USD"),
		d.accept(scenarioMethod["success"], "200", "USD"),
	}
	declined := d.accept(scenarioMethod["decline"], "300", "USD")
	d.drain()
	page := d.ledgerPage("?payment_id="+ids[0], credentials.ReaderKey)
	if len(page.Items) != 1 || page.NextCursor != nil {
		t.Fatalf("page %+v", page)
	}
	j := page.Items[0]
	if j.Status != "posted" || j.Currency != "KWD" || len(j.Entries) != 2 ||
		j.Entries[0].Side != "debit" || j.Entries[0].AccountCode != ledger.ProviderClearingCode(auth.DemoProviderID) || j.Entries[0].ProviderID == nil ||
		j.Entries[1].Side != "credit" || j.Entries[1].AccountCode != ledger.MerchantPayableCode ||
		j.Entries[0].AmountMinor != "9223372036854775807" || j.Entries[1].AmountMinor != "9223372036854775807" {
		t.Fatalf("journal %+v", j)
	}
	if page := d.ledgerPage("?payment_id="+declined, credentials.ReaderKey); len(page.Items) != 0 {
		t.Fatal("declined payment has a journal")
	}
	if page := d.ledgerPage("", credentials.OtherWorkspaceKey); len(page.Items) != 0 {
		t.Fatal("ledger leaked across workspaces")
	}
	first := d.ledgerPage("?limit=2", credentials.ReaderKey)
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first page %+v", first)
	}
	second := d.ledgerPage("?limit=2&cursor="+*first.NextCursor, credentials.ReaderKey)
	if len(second.Items) != 1 || second.NextCursor != nil {
		t.Fatalf("second page %+v", second)
	}
	seen := map[string]bool{}
	for _, item := range append(first.Items, second.Items...) {
		seen[item.ID] = true
	}
	if len(seen) != 3 {
		t.Fatalf("pages overlap: %v", seen)
	}
	if response := d.do("GET", "/v1/ledger/transactions?limit=0", credentials.ReaderKey, "", ""); response.Code != 400 {
		t.Fatalf("limit=0: %d", response.Code)
	}
	if response := d.do("GET", "/v1/ledger/transactions?cursor=not-a-cursor!", credentials.ReaderKey, "", ""); response.Code != 400 {
		t.Fatalf("bad cursor: %d", response.Code)
	}
	d.requireLedger()
}
