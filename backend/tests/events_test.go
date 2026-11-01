package tests

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// WBS 4.3: inbox duplicates, changed payloads, reordered events, audit
// immutability and the safe events API.

type webhookReceipt struct {
	EventID         string  `json:"event_id"`
	ProviderEventID string  `json:"provider_event_id"`
	Result          string  `json:"result"`
	Outcome         *string `json:"outcome"`
	OutcomeReason   *string `json:"outcome_reason"`
	DuplicateCount  int64   `json:"duplicate_count"`
	SignatureStatus string  `json:"signature_status"`
}

func webhookBody(eventID, eventType, paymentID string) string {
	return fmt.Sprintf(`{"event_id":%q,"type":%q,"payment_id":%q}`, eventID, eventType, paymentID)
}

func (e *env) webhook(body string) (int, webhookReceipt) {
	e.t.Helper()
	response := e.do("POST", "/v1/sandbox/webhooks/mock", credentials.OperatorKey, "", body)
	var receipt webhookReceipt
	if response.Code < 300 {
		if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil {
			e.t.Fatal(err)
		}
	}
	return response.Code, receipt
}

func (e *env) inboxRow(providerEventID string) storage.WebhookInbox {
	e.t.Helper()
	var row storage.WebhookInbox
	if err := e.store.DB.Where("provider_event_id = ?", providerEventID).Take(&row).Error; err != nil {
		e.t.Fatal(err)
	}
	return row
}

func (e *env) transitionsTo(paymentID, status string) int64 {
	return e.count("payment_state_transitions", "payment_id = ? AND to_status = ?", paymentID, status)
}

func TestDuplicateWebhooksCountOnceWithSingleEffect(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["outage"], "800", "USD")
	d.drain()
	if got := d.payment(id).Status; got != "unknown" {
		t.Fatalf("payment is %s, want unknown", got)
	}
	body := webhookBody("evt_dup_1", payment.WebhookSucceeded, id)
	code, first := d.webhook(body)
	if code != 202 || first.Result != "processed" || first.Outcome == nil || *first.Outcome != "applied" || first.SignatureStatus != "not_applicable" {
		t.Fatalf("first delivery: %d %+v", code, first)
	}
	for i := 1; i <= 3; i++ {
		code, again := d.webhook(body)
		if code != 200 || again.Result != "duplicate" || again.DuplicateCount != int64(i) || again.EventID != first.EventID {
			t.Fatalf("delivery %d: %d %+v", i+1, code, again)
		}
	}
	row := d.inboxRow("evt_dup_1")
	if row.DuplicateCount != 3 || row.LastReceivedAt < row.ReceivedAt || row.InvestigationFlag != 0 {
		t.Fatalf("inbox row %+v", row)
	}
	if n := d.count("webhook_inbox", "provider_event_id = ?", "evt_dup_1"); n != 1 {
		t.Fatalf("%d inbox rows", n)
	}
	if d.payment(id).Status != "succeeded" || d.transitionsTo(id, "succeeded") != 1 || d.count("ledger_journals", "payment_id = ?", id) != 1 {
		t.Fatal("duplicates must have exactly one effect")
	}
}

func TestConcurrentIdenticalWebhooksHaveOneEffect(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["outage"], "800", "USD")
	d.drain()
	body := webhookBody("evt_race", payment.WebhookSucceeded, id)
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response := d.do("POST", "/v1/sandbox/webhooks/mock", credentials.OperatorKey, "", body)
			codes <- response.Code
		}()
	}
	wg.Wait()
	close(codes)
	accepted := 0
	for code := range codes {
		switch code {
		case 202:
			accepted++
		case 200:
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	if accepted != 1 {
		t.Fatalf("%d deliveries processed, want 1", accepted)
	}
	if row := d.inboxRow("evt_race"); row.DuplicateCount != 7 {
		t.Fatalf("duplicate_count %d, want 7", row.DuplicateCount)
	}
	if d.transitionsTo(id, "succeeded") != 1 || d.count("ledger_journals", "payment_id = ?", id) != 1 {
		t.Fatal("concurrent duplicates must have exactly one effect")
	}
}

func TestChangedPayloadHashIsFlaggedNotOverwritten(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["outage"], "800", "USD")
	d.drain()
	first := webhookBody("evt_changed", payment.WebhookProcessing, id)
	if code, _ := d.webhook(first); code != 202 {
		t.Fatalf("first delivery %d", code)
	}
	before := d.inboxRow("evt_changed")
	changed := webhookBody("evt_changed", payment.WebhookSucceeded, id)
	response := d.do("POST", "/v1/sandbox/webhooks/mock", credentials.OperatorKey, "", changed)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "event_payload_conflict") {
		t.Fatalf("changed payload: %d %s", response.Code, response.Body.String())
	}
	after := d.inboxRow("evt_changed")
	if after.InvestigationFlag != 1 || after.ConflictCount != 1 || after.ConflictingPayloadHash == nil || *after.ConflictingPayloadHash != payment.PayloadHash([]byte(changed)) {
		t.Fatalf("conflict not recorded: %+v", after)
	}
	if after.PayloadHash != before.PayloadHash || after.Payload != before.Payload || after.EventType != before.EventType || after.DuplicateCount != 0 {
		t.Fatal("the first payload must never be overwritten and a conflict is not a duplicate")
	}
	if d.payment(id).Status != "unknown" {
		t.Fatal("a changed payload must not be applied")
	}
	// The same changed payload again is still not applied; the first stays intact.
	if r := d.do("POST", "/v1/sandbox/webhooks/mock", credentials.OperatorKey, "", changed); r.Code != 409 {
		t.Fatalf("repeat conflict: %d", r.Code)
	}
	if again := d.inboxRow("evt_changed"); again.ConflictCount != 2 || again.PayloadHash != before.PayloadHash {
		t.Fatalf("conflict count %d", again.ConflictCount)
	}
}

func TestReorderedWebhooksNeverRegressTerminalState(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["success"], "500", "USD")
	d.drain()
	if d.payment(id).Status != "succeeded" {
		t.Fatalf("payment is %s", d.payment(id).Status)
	}
	cases := []struct{ event, kind, reason string }{
		{"evt_late_processing", payment.WebhookProcessing, "terminal_state"},
		{"evt_late_failed", payment.WebhookFailed, "terminal_state"},
		{"evt_late_success", payment.WebhookSucceeded, "already_succeeded"},
	}
	for _, c := range cases {
		code, receipt := d.webhook(webhookBody(c.event, c.kind, id))
		if code != 202 || receipt.Outcome == nil || *receipt.Outcome != "ignored" || receipt.OutcomeReason == nil || *receipt.OutcomeReason != c.reason {
			t.Fatalf("%s: %d %+v", c.event, code, receipt)
		}
	}
	if d.payment(id).Status != "succeeded" || d.transitionsTo(id, "failed") != 0 || d.count("ledger_journals", "payment_id = ?", id) != 1 {
		t.Fatal("a reordered event regressed the payment")
	}
	if late := d.late(id); len(late) != 1 {
		t.Fatalf("%d late outcomes, want one for the conflicting failure", len(late))
	}

	failed := d.accept(scenarioMethod["decline"], "500", "USD")
	d.drain()
	if code, receipt := d.webhook(webhookBody("evt_success_after_decline", payment.WebhookSucceeded, failed)); code != 202 || *receipt.Outcome != "ignored" {
		t.Fatalf("success after decline: %d %+v", code, receipt)
	}
	if d.payment(failed).Status != "failed" || d.count("ledger_journals", "payment_id = ?", failed) != 0 {
		t.Fatal("a late success must not revive a failed payment")
	}
	if code, receipt := d.webhook(webhookBody("evt_unknown_payment", payment.WebhookSucceeded, "missing")); code != 202 || *receipt.OutcomeReason != "unknown_payment" {
		t.Fatalf("unknown payment: %d %+v", code, receipt)
	}
}

func TestAuditAndInboxRowsAreAppendOnly(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["success"], "500", "USD")
	d.drain()
	d.webhook(webhookBody("evt_audit", payment.WebhookProcessing, id))
	row := d.inboxRow("evt_audit")
	for _, statement := range []struct{ name, sql string }{
		{"delete transition", "DELETE FROM payment_state_transitions WHERE payment_id = '" + id + "'"},
		{"rewrite transition", "UPDATE payment_state_transitions SET reason = 'x' WHERE payment_id = '" + id + "'"},
		{"delete inbox row", "DELETE FROM webhook_inbox WHERE id = '" + row.ID + "'"},
		{"overwrite payload", "UPDATE webhook_inbox SET payload = '{}' WHERE id = '" + row.ID + "'"},
		{"overwrite hash", "UPDATE webhook_inbox SET payload_hash = '" + strings.Repeat("0", 64) + "' WHERE id = '" + row.ID + "'"},
		{"reset duplicate count", "UPDATE webhook_inbox SET duplicate_count = duplicate_count - 1 WHERE id = '" + row.ID + "'"},
		{"invalid signature status", "UPDATE webhook_inbox SET signature_status = 'trusted' WHERE id = '" + row.ID + "'"},
	} {
		if err := d.store.DB.Exec(statement.sql).Error; err == nil {
			t.Errorf("%s was accepted", statement.name)
		}
	}
	if d.transitionsTo(id, "succeeded") != 1 {
		t.Fatal("audit rows changed")
	}
	// The timeline is readable per payment and ordered.
	response := d.do("GET", "/v1/payments/"+id+"/history", credentials.OperatorKey, "", "")
	var history struct {
		Transitions []struct {
			ToStatus string `json:"to_status"`
		} `json:"transitions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &history); err != nil || len(history.Transitions) < 3 || history.Transitions[0].ToStatus != "created" {
		t.Fatalf("history: %s", response.Body.String())
	}
}

type eventPage struct {
	Items []map[string]any `json:"items"`
	Next  *string          `json:"next_cursor"`
}

func (e *env) events(query string) eventPage {
	e.t.Helper()
	response := e.do("GET", "/v1/events"+query, credentials.OperatorKey, "", "")
	if response.Code != 200 {
		e.t.Fatalf("events%s: %d %s", query, response.Code, response.Body.String())
	}
	var page eventPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		e.t.Fatal(err)
	}
	return page
}

func TestEventsAPIIsSafeFilteredAndPaginated(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["outage"], "800", "USD")
	d.drain()
	secretReference := "sk_live_do_not_show"
	body := fmt.Sprintf(`{"event_id":"evt_api","type":"payment.succeeded","payment_id":%q,"provider_reference":%q}`, id, secretReference)
	d.webhook(body)
	d.webhook(body)
	d.do("POST", "/v1/sandbox/webhooks/mock", credentials.OperatorKey, "", strings.Replace(body, "payment.succeeded", "payment.failed", 1))

	all := d.events("?limit=100")
	if len(all.Items) < 3 {
		t.Fatalf("%d events", len(all.Items))
	}
	var inboxID string
	for _, item := range all.Items {
		if item["source"] == "inbox" {
			inboxID = item["id"].(string)
			if item["signature_status"] != "not_applicable" || item["duplicate_count"].(float64) != 1 || item["needs_investigation"] != true || item["provider_event_id"] != "evt_api" {
				t.Fatalf("inbox item %+v", item)
			}
		}
	}
	if inboxID == "" {
		t.Fatal("inbox event missing from the combined feed")
	}
	detail := d.do("GET", "/v1/events/"+inboxID, credentials.OperatorKey, "", "")
	if detail.Code != 200 {
		t.Fatalf("detail %d %s", detail.Code, detail.Body.String())
	}
	for _, forbidden := range []string{secretReference, credentials.OperatorKey, "Idempotency", `"payload":`, "lease_owner", "last_error"} {
		if strings.Contains(detail.Body.String(), forbidden) || strings.Contains(fmt.Sprint(all.Items), forbidden) {
			t.Fatalf("events API leaked %q", forbidden)
		}
	}
	var view struct {
		PayloadSummary struct {
			Fields        []map[string]string `json:"fields"`
			RedactedCount int                 `json:"redacted_count"`
		} `json:"payload_summary"`
		ConflictingHash *string `json:"conflicting_payload_hash"`
		PayloadHash     *string `json:"payload_hash"`
		Attempts        []any   `json:"attempts"`
		PaymentStatus   string  `json:"payment_status"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.PayloadSummary.RedactedCount != 1 || view.ConflictingHash == nil || view.PayloadHash == nil || len(view.Attempts) != 1 || view.PaymentStatus != "succeeded" {
		t.Fatalf("detail %s", detail.Body.String())
	}

	// Filters: payment, source, state.
	byPayment := d.events("?limit=100&q=" + id)
	for _, item := range byPayment.Items {
		if item["payment_id"] != id {
			t.Fatalf("q filter returned %+v", item)
		}
	}
	if n := len(d.events("?source=inbox").Items); n != 1 {
		t.Fatalf("%d inbox events", n)
	}
	if n := len(d.events("?state=investigation").Items); n != 1 {
		t.Fatalf("%d investigation events", n)
	}
	for _, item := range d.events("?state=delivered&limit=100").Items {
		if item["source"] != "outbox" || item["status"] != "delivered" {
			t.Fatalf("state filter returned %+v", item)
		}
	}
	// Cursor pagination visits every event once, newest first.
	seen := map[string]bool{}
	cursor, pages := "", 0
	for {
		page := d.events("?limit=1" + cursor)
		pages++
		for _, item := range page.Items {
			key := item["id"].(string)
			if seen[key] {
				t.Fatalf("duplicate %s across pages", key)
			}
			seen[key] = true
		}
		if page.Next == nil {
			break
		}
		cursor = "&cursor=" + *page.Next
		if pages > 100 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != len(all.Items) {
		t.Fatalf("paged %d events, want %d", len(seen), len(all.Items))
	}
	for _, query := range []string{"?limit=0", "?state=bogus", "?cursor=!!", "?created_from=nope", "?source=x", "?q=a%20b"} {
		if response := d.do("GET", "/v1/events"+query, credentials.OperatorKey, "", ""); response.Code != 400 {
			t.Errorf("%s: %d", query, response.Code)
		}
	}
}

func TestEventsAndWebhooksAreWorkspaceScopedAndAuthorized(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	id := d.accept(scenarioMethod["success"], "500", "USD")
	d.drain()
	d.webhook(webhookBody("evt_scoped", payment.WebhookProcessing, id))
	row := d.inboxRow("evt_scoped")
	if response := d.do("GET", "/v1/events/whk_"+row.ID, credentials.OtherWorkspaceKey, "", ""); response.Code != 404 {
		t.Fatalf("other workspace read: %d", response.Code)
	}
	if items := (func() int {
		response := d.do("GET", "/v1/events?limit=100", credentials.OtherWorkspaceKey, "", "")
		var page eventPage
		_ = json.Unmarshal(response.Body.Bytes(), &page)
		return len(page.Items)
	})(); items != 0 {
		t.Fatalf("other workspace sees %d events", items)
	}
	if response := d.do("GET", "/v1/events/whk_missing", credentials.OperatorKey, "", ""); response.Code != 404 {
		t.Fatalf("missing event: %d", response.Code)
	}
	if response := d.do("GET", "/v1/events/bogus", credentials.OperatorKey, "", ""); response.Code != 404 {
		t.Fatalf("malformed id: %d", response.Code)
	}
	if response := d.do("POST", "/v1/sandbox/webhooks/mock", credentials.ReaderKey, "", webhookBody("evt_reader", payment.WebhookProcessing, id)); response.Code != 403 {
		t.Fatalf("read-only webhook: %d", response.Code)
	}
	if response := d.do("POST", "/v1/sandbox/webhooks/mock", "", "", webhookBody("evt_anon", payment.WebhookProcessing, id)); response.Code != 401 {
		t.Fatalf("anonymous webhook: %d", response.Code)
	}
	// The other workspace cannot affect this payment, even by ID.
	response := d.do("POST", "/v1/sandbox/webhooks/mock", credentials.OtherWorkspaceKey, "", webhookBody("evt_cross", payment.WebhookFailed, id))
	if response.Code != 202 || !strings.Contains(response.Body.String(), "unknown_payment") || d.payment(id).Status != "succeeded" {
		t.Fatalf("cross-workspace webhook: %d %s", response.Code, response.Body.String())
	}
	for _, body := range []string{`{}`, `{"event_id":"e","type":"payment.refunded","payment_id":"p"}`, `{"event_id":"e","type":"payment.failed","payment_id":"p","extra":1}`, `not json`} {
		if response := d.do("POST", "/v1/sandbox/webhooks/mock", credentials.OperatorKey, "", body); response.Code != 400 {
			t.Errorf("%q: %d", body, response.Code)
		}
	}
}

func TestSettingsEnforceMembershipAndHideSecrets(t *testing.T) {
	e := newEnv(t, payment.Options{})
	response := e.do("GET", "/v1/settings", credentials.ReaderKey, "", "")
	if response.Code != 200 {
		t.Fatalf("settings: %d %s", response.Code, response.Body.String())
	}
	var settings struct {
		Workspace struct{ ID, Name, Environment string } `json:"workspace"`
		Providers []struct{ Code, Status string }        `json:"providers"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Workspace.ID != auth.DemoWorkspaceID || settings.Workspace.Environment != "sandbox" {
		t.Fatalf("workspace %+v", settings.Workspace)
	}
	status := map[string]string{}
	for _, p := range settings.Providers {
		status[p.Code] = p.Status
	}
	if status["mock"] != "enabled" || status["stripe"] != "not_configured" || status["plaid"] != "not_configured" {
		t.Fatalf("providers %v", status)
	}
	for _, secret := range []string{credentials.OperatorKey, credentials.ReaderKey, credentials.SessionToken, credentials.CSRFToken, "key_hash", "token"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("settings leaked %q", secret)
		}
	}
	other := e.do("GET", "/v1/settings", credentials.OtherWorkspaceKey, "", "")
	if !strings.Contains(other.Body.String(), auth.OtherWorkspaceID) || strings.Contains(other.Body.String(), auth.DemoWorkspaceID) {
		t.Fatalf("other workspace settings: %s", other.Body.String())
	}
	if r := e.do("GET", "/v1/settings", "", "", ""); r.Code != 401 {
		t.Fatalf("anonymous: %d", r.Code)
	}
	if r := e.do("GET", "/v1/settings", "reader."+strings.Repeat("z", 32), "", ""); r.Code != 401 {
		t.Fatalf("wrong key: %d", r.Code)
	}
	// A revoked key loses access immediately (session membership is joined in auth.Authenticate).
	if err := e.store.DB.Exec("UPDATE api_keys SET revoked_at = 1 WHERE prefix = 'reader'").Error; err != nil {
		t.Fatal(err)
	}
	if r := e.do("GET", "/v1/settings", credentials.ReaderKey, "", ""); r.Code != 401 {
		t.Fatalf("revoked key: %d", r.Code)
	}
}
