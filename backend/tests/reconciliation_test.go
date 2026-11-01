package tests

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"gorm.io/gorm"
)

// forged describes a settled payment inserted directly, so a mismatch that the
// real pipeline cannot produce can be exercised. Rows are inserted before any
// reconciliation runs; reconciliation itself never writes them.
type forged struct {
	status       string
	amount       int64
	chargeStatus string // empty: no charge recorded
	chargeAmount int64
}

func (e *env) forge(id string, f forged) {
	e.t.Helper()
	now := time.Now().UnixMilli()
	e.exec(`INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,status,version,created_at,updated_at) VALUES(?,?,?,?,?,?,3,?,?)`,
		id, auth.DemoWorkspaceID, scenarioMethod["success"], f.amount, "USD", f.status, now, now)
	e.exec(`INSERT INTO payment_attempts(id,workspace_id,payment_id,provider_id,routing_reason,attempt_number,provider_idempotency_key,provider_reference,status,submitted_at,created_at,updated_at) VALUES(?,?,?,?,?,1,?,?,?,?,?,?)`,
		"att-"+id, auth.DemoWorkspaceID, id, auth.DemoProviderID, "forged", "key-"+id, "ref-"+id, "submitted", now, now, now)
	if f.chargeStatus != "" {
		e.exec(`INSERT INTO mock_provider_charges(id,workspace_id,provider_id,provider_idempotency_key,provider_reference,amount_minor,currency,status,scenario,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			"chg-"+id, auth.DemoWorkspaceID, auth.DemoProviderID, "key-"+id, "ref-"+id, f.chargeAmount, "USD", f.chargeStatus, "success", now, now)
	}
}

type runBody struct {
	ID                    string `json:"id"`
	RunKey                string `json:"run_key"`
	Status                string `json:"status"`
	EvidenceConsumedCount int64  `json:"evidence_consumed_count"`
	Counts                struct {
		Compared, Matched, Unmatched, Mismatch int64
	} `json:"counts"`
}

type discrepancyBody struct {
	ID               string          `json:"id"`
	RunID            string          `json:"run_id"`
	PaymentID        string          `json:"payment_id"`
	Kind             string          `json:"kind"`
	Status           string          `json:"status"`
	Details          json.RawMessage `json:"details"`
	ResolutionNote   *string         `json:"resolution_note"`
	ResolvedBy       *string         `json:"resolved_by"`
	ResolvedAt       *string         `json:"resolved_at"`
	RelatedJournalID *string         `json:"related_journal_id"`
	History          []struct {
		FromStatus string `json:"from_status"`
		ToStatus   string `json:"to_status"`
		Note       string `json:"note"`
		Actor      string `json:"actor"`
	} `json:"history"`
}

func window() string {
	now := time.Now().UTC()
	return fmt.Sprintf(`{"window_start":%q,"window_end":%q}`, now.Add(-time.Hour).Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339))
}

func (e *env) startRun(key string) (int, runBody) {
	e.t.Helper()
	response := e.do("POST", "/v1/reconciliation/runs", credentials.OperatorKey, key, window())
	var run runBody
	if response.Code == 200 || response.Code == 201 {
		if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
			e.t.Fatal(err)
		}
	} else {
		e.t.Fatalf("run %d %s", response.Code, response.Body.String())
	}
	return response.Code, run
}

func (e *env) discrepancies(query, key string) []discrepancyBody {
	e.t.Helper()
	response := e.do("GET", "/v1/reconciliation/discrepancies"+query, key, "", "")
	if response.Code != 200 {
		e.t.Fatalf("list discrepancies %d %s", response.Code, response.Body.String())
	}
	var page struct {
		Items []discrepancyBody `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		e.t.Fatal(err)
	}
	return page.Items
}

func kindsFor(items []discrepancyBody, paymentID string) []string {
	var kinds []string
	for _, item := range items {
		if item.PaymentID == paymentID {
			kinds = append(kinds, item.Kind)
		}
	}
	return kinds
}

func TestReconciliationMatchesCleanPipelinePayments(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	d.accept(scenarioMethod["success"], "500", "USD")
	d.accept(scenarioMethod["decline"], "700", "USD")
	d.drain()
	code, run := d.startRun("clean")
	if code != 201 || run.Status != "completed" || run.Counts.Compared != 2 || run.Counts.Matched != 2 || run.Counts.Unmatched != 0 || run.Counts.Mismatch != 0 {
		t.Fatalf("run %d %+v", code, run)
	}
	if items := d.discrepancies("?run_id="+run.ID, credentials.ReaderKey); len(items) != 0 {
		t.Fatalf("unexpected discrepancies %+v", items)
	}
	d.requireLedger()
}

func TestReconciliationReportsEachDiscrepancyKind(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	// unknown_outcome through the real pipeline: the outage method leaves the payment unknown.
	unknown := d.accept(scenarioMethod["outage"], "500", "USD")
	d.drain()
	requireStatus(t, d.payment(unknown), payment.StatusUnknown)
	// A matched success, to prove it is not reported.
	good := d.accept(scenarioMethod["success"], "100", "USD")
	d.drain()
	d.forge("forged-amount", forged{status: "failed", amount: 1000, chargeStatus: "declined", chargeAmount: 900})
	d.forge("forged-status", forged{status: "failed", amount: 1000, chargeStatus: "succeeded", chargeAmount: 1000})
	d.forge("forged-posting", forged{status: "succeeded", amount: 1000, chargeStatus: "succeeded", chargeAmount: 1000})
	d.forge("forged-missing", forged{status: "succeeded", amount: 1000})

	_, run := d.startRun("kinds")
	items := d.discrepancies("?run_id="+run.ID, credentials.ReaderKey)
	want := map[string][]string{
		unknown:          {"unknown_outcome"},
		"forged-amount":  {"amount_mismatch"},
		"forged-status":  {"status_mismatch"},
		"forged-posting": {"missing_posting"},
		"forged-missing": {"missing_at_provider", "missing_posting"},
		good:             nil,
	}
	for id, kinds := range want {
		got := kindsFor(items, id)
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(kinds, ",") {
			t.Errorf("payment %s kinds %v want %v", id, got, kinds)
		}
	}
	for _, item := range items {
		if item.Status != "open" || len(item.History) != 0 || !strings.Contains(string(item.Details), `"simulated":true`) {
			t.Errorf("discrepancy %+v", item)
		}
	}
	if run.Counts.Compared != 6 || run.Counts.Unmatched != 4 || run.Counts.Mismatch != 2 || run.Counts.Matched != 1 {
		t.Errorf("counts %+v", run.Counts)
	}
}

func TestReconciliationRerunsAreIdempotent(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	d.accept(scenarioMethod["outage"], "500", "USD")
	d.drain()
	d.forge("forged-amount", forged{status: "failed", amount: 1000, chargeStatus: "declined", chargeAmount: 900})

	code, first := d.startRun("same-key")
	if code != 201 {
		t.Fatal(code)
	}
	code, second := d.startRun("same-key")
	if code != 200 || second.ID != first.ID || second.Counts != first.Counts {
		t.Fatalf("rerun %d %+v vs %+v", code, second, first)
	}
	if n := d.count("reconciliation_discrepancies", "run_id = ?", first.ID); n != 2 {
		t.Fatalf("discrepancies after rerun %d", n)
	}
	if n := d.count("reconciliation_runs", ""); n != 1 {
		t.Fatalf("runs %d", n)
	}
	// A new key is a new run with its own rows; a fingerprint never repeats inside a run.
	code, third := d.startRun("other-key")
	if code != 201 || third.ID == first.ID || d.count("reconciliation_discrepancies", "run_id = ?", third.ID) != 2 {
		t.Fatalf("new key %d %+v", code, third)
	}
	var duplicates int64
	if err := d.store.DB.Raw(`SELECT count(*) FROM (SELECT run_id, fingerprint FROM reconciliation_discrepancies GROUP BY run_id, fingerprint HAVING count(*) > 1)`).Scan(&duplicates).Error; err != nil || duplicates != 0 {
		t.Fatalf("duplicate fingerprints %d %v", duplicates, err)
	}
	// Re-using a key for another window is rejected.
	other := fmt.Sprintf(`{"window_start":%q,"window_end":%q}`, time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339), time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339))
	if response := d.do("POST", "/v1/reconciliation/runs", credentials.OperatorKey, "same-key", other); response.Code != 409 {
		t.Fatalf("conflicting window %d", response.Code)
	}
}

func TestReconciliationNeverInventsAdjustments(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	d.accept(scenarioMethod["success"], "500", "USD")
	unknown := d.accept(scenarioMethod["outage"], "500", "USD")
	d.drain()
	d.forge("forged-posting", forged{status: "succeeded", amount: 1000, chargeStatus: "succeeded", chargeAmount: 1000})
	d.forge("forged-status", forged{status: "failed", amount: 1000, chargeStatus: "succeeded", chargeAmount: 1000})
	protected := []string{"payments", "payment_attempts", "payment_state_transitions", "ledger_journals", "ledger_entries", "ledger_accounts", "mock_provider_charges", "idempotency_records"}
	before := d.dump()

	_, run := d.startRun("no-adjust")
	items := d.discrepancies("?run_id="+run.ID, credentials.ReaderKey)
	if len(items) != 3 {
		t.Fatalf("discrepancies %d", len(items))
	}
	for _, item := range items {
		response := d.do("POST", "/v1/reconciliation/discrepancies/"+item.ID+"/resolve", credentials.OperatorKey, "", `{"status":"resolved","note":"Reviewed; accepted as simulated evidence."}`)
		if response.Code != 200 {
			t.Fatalf("resolve %d %s", response.Code, response.Body.String())
		}
	}
	after := d.dump()
	for _, table := range protected {
		if before[table] != after[table] {
			t.Errorf("reconciliation changed %s", table)
		}
	}
	requireStatus(t, d.payment(unknown), payment.StatusUnknown)
	requireStatus(t, d.payment("forged-posting"), payment.StatusSucceeded)
	if n := len(d.journals("forged-posting")); n != 0 {
		t.Fatalf("journal invented for forged-posting: %d", n)
	}
}

func TestReconciliationConsumesLateOutcomesAsEvidence(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	// The payment is failed while the provider reports success: a late outcome was recorded.
	d.forge("late-pay", forged{status: "failed", amount: 1000, chargeStatus: "succeeded", chargeAmount: 1000})
	err := d.store.WithTx(t.Context(), func(tx *gorm.DB) error {
		_, err := payment.RecordLateOutcome(tx, payment.LateOutcome{
			PaymentID: "late-pay", WorkspaceID: auth.DemoWorkspaceID, AttemptID: "att-late-pay", ProviderID: auth.DemoProviderID,
			Outcome: "succeeded", Source: "lookup", ObservedAt: time.Now().UnixMilli(),
		}, nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if rows := d.outbox("late-pay", payment.OutboxPaymentLateOutcome); len(rows) != 1 || rows[0].Status != event.StatusPending {
		t.Fatalf("late rows %+v", rows)
	}
	paymentBefore := d.payment("late-pay")
	_, run := d.startRun("late")
	if run.EvidenceConsumedCount != 1 {
		t.Fatalf("consumed %d", run.EvidenceConsumedCount)
	}
	rows := d.outbox("late-pay", payment.OutboxPaymentLateOutcome)
	if len(rows) != 1 || rows[0].Status != event.StatusDelivered || rows[0].DeliveredAt == nil {
		t.Fatalf("late rows after %+v", rows)
	}
	items := d.discrepancies("?run_id="+run.ID, credentials.ReaderKey)
	if len(items) != 1 || items[0].Kind != "status_mismatch" || !strings.Contains(string(items[0].Details), `"outcome":"succeeded"`) {
		t.Fatalf("items %+v", items)
	}
	if after := d.payment("late-pay"); after.Status != paymentBefore.Status || after.Version != paymentBefore.Version {
		t.Fatal("late outcome changed the payment")
	}
	// A rerun consumes nothing more and adds no discrepancy.
	_, again := d.startRun("late")
	if again.EvidenceConsumedCount != 1 || d.count("reconciliation_discrepancies", "") != 1 {
		t.Fatalf("rerun %+v", again)
	}
}

func TestDiscrepancyResolutionPermissionsAndHistory(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	d.forge("forged-status", forged{status: "failed", amount: 1000, chargeStatus: "succeeded", chargeAmount: 1000})
	_, run := d.startRun("resolve")
	item := d.discrepancies("?run_id="+run.ID, credentials.ReaderKey)[0]
	path := "/v1/reconciliation/discrepancies/" + item.ID + "/resolve"
	note := `{"status":"resolved","note":"Provider confirmed the charge was voided."}`

	if r := d.do("POST", path, credentials.ReaderKey, "", note); r.Code != 403 {
		t.Fatalf("reader resolve %d", r.Code)
	}
	if r := d.do("POST", "/v1/reconciliation/runs", credentials.ReaderKey, "reader-run", window()); r.Code != 403 {
		t.Fatalf("reader run %d", r.Code)
	}
	if r := d.do("POST", path, "", "", note); r.Code != 401 {
		t.Fatalf("anonymous resolve %d", r.Code)
	}
	if r := d.do("POST", path, credentials.OtherWorkspaceKey, "", note); r.Code != 403 && r.Code != 404 {
		t.Fatalf("other workspace resolve %d", r.Code)
	}
	for _, body := range []string{`{"status":"resolved","note":"   "}`, `{"status":"resolved"}`, `{"status":"bogus","note":"x"}`, `{"status":"resolved","note":"x","extra":1}`, `{}`} {
		if r := d.do("POST", path, credentials.OperatorKey, "", body); r.Code != 400 {
			t.Fatalf("body %s -> %d", body, r.Code)
		}
	}
	if stored := d.discrepancies("?run_id="+run.ID, credentials.ReaderKey)[0]; stored.Status != "open" || len(stored.History) != 0 {
		t.Fatal("rejected requests changed the discrepancy")
	}

	r := d.do("POST", path, credentials.OperatorKey, "", `{"status":"investigating","note":"Checking with the provider."}`)
	if r.Code != 200 {
		t.Fatalf("investigate %d %s", r.Code, r.Body.String())
	}
	r = d.do("POST", path, credentials.OperatorKey, "", note)
	var resolved discrepancyBody
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &resolved) != nil {
		t.Fatalf("resolve %d %s", r.Code, r.Body.String())
	}
	if resolved.Status != "resolved" || resolved.ResolvedBy == nil || *resolved.ResolvedBy != "api_key:operator" || resolved.ResolvedAt == nil ||
		resolved.ResolutionNote == nil || len(resolved.History) != 2 || resolved.History[0].ToStatus != "investigating" || resolved.History[1].Actor != "api_key:operator" {
		t.Fatalf("resolved %+v", resolved)
	}
	if r := d.do("POST", path, credentials.OperatorKey, "", `{"status":"open","note":"reopen"}`); r.Code != 409 {
		t.Fatalf("resolved is final: %d", r.Code)
	}
	if got := d.discrepancies("?status=resolved&kind=status_mismatch", credentials.ReaderKey); len(got) != 1 {
		t.Fatalf("filtered list %d", len(got))
	}
	if got := d.discrepancies("", credentials.OtherWorkspaceKey); len(got) != 0 {
		t.Fatal("discrepancies leaked across workspaces")
	}
	if r := d.do("GET", "/v1/reconciliation/discrepancies/"+item.ID, credentials.OtherWorkspaceKey, "", ""); r.Code != 404 {
		t.Fatalf("cross-workspace get %d", r.Code)
	}

	// The database enforces the same rules without the API.
	d.exec(`INSERT INTO reconciliation_discrepancies(id,workspace_id,run_id,fingerprint,kind,details,created_at) VALUES('open-row','` + auth.DemoWorkspaceID + `','` + run.ID + `','open-fp','amount_mismatch','{}',1)`)
	for name, sql := range map[string]string{
		"resolve without note":    `UPDATE reconciliation_discrepancies SET status='resolved', resolved_by='x', resolved_at=1 WHERE id='open-row'`,
		"resolve with blank note": `UPDATE reconciliation_discrepancies SET status='resolved', resolution_note='  ', resolved_by='x', resolved_at=1 WHERE id='open-row'`,
		"edit history":            `UPDATE reconciliation_resolution_events SET note='changed'`,
		"delete history":          `DELETE FROM reconciliation_resolution_events`,
		"edit resolved":           `UPDATE reconciliation_discrepancies SET resolution_note='changed' WHERE id='` + item.ID + `'`,
		"edit evidence":           `UPDATE reconciliation_discrepancies SET details='{"x":1}' WHERE id='open-row'`,
		"delete discrepancy":      `DELETE FROM reconciliation_discrepancies`,
		"unknown kind":            `INSERT INTO reconciliation_discrepancies(id,workspace_id,run_id,fingerprint,kind,details,created_at) VALUES('k','` + auth.DemoWorkspaceID + `','` + run.ID + `','f','invented','{}',1)`,
		"insert resolved":         `INSERT INTO reconciliation_discrepancies(id,workspace_id,run_id,fingerprint,kind,details,status,resolution_note,resolved_by,resolved_at,created_at) VALUES('k','` + auth.DemoWorkspaceID + `','` + run.ID + `','f','amount_mismatch','{}','resolved','n','b',1,1)`,
		"blank history note":      `INSERT INTO reconciliation_resolution_events(id,workspace_id,discrepancy_id,from_status,to_status,note,actor_reference,created_at) VALUES('e','` + auth.DemoWorkspaceID + `','` + item.ID + `','open','investigating','  ','a',1)`,
		"delete run":              `DELETE FROM reconciliation_runs`,
	} {
		if err := d.store.DB.Exec(sql).Error; err == nil {
			t.Errorf("database accepted: %s", name)
		}
	}
}

func TestReconciliationRunValidationAndScoping(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	do := func(key, body string) int {
		return d.do("POST", "/v1/reconciliation/runs", credentials.OperatorKey, key, body).Code
	}
	if c := do("", window()); c != 400 {
		t.Fatalf("missing key %d", c)
	}
	if c := do("k", `{"window_start":"2026-01-02T00:00:00Z","window_end":"2026-01-01T00:00:00Z"}`); c != 400 {
		t.Fatalf("reversed window %d", c)
	}
	if c := do("k", `{"window_start":"2020-01-01T00:00:00Z","window_end":"2026-01-01T00:00:00Z"}`); c != 400 {
		t.Fatalf("oversized window %d", c)
	}
	if c := do("k", `{"window_start":"x"}`); c != 400 {
		t.Fatalf("bad timestamps %d", c)
	}
	if n := d.count("reconciliation_runs", ""); n != 0 {
		t.Fatalf("invalid requests created %d runs", n)
	}
	for _, q := range []string{"?kind=bogus", "?status=bogus", "?limit=0", "?cursor=!!"} {
		if r := d.do("GET", "/v1/reconciliation/discrepancies"+q, credentials.ReaderKey, "", ""); r.Code != 400 {
			t.Fatalf("%s -> %d", q, r.Code)
		}
	}
	d.startRun("one")
	response := d.do("GET", "/v1/reconciliation/runs", credentials.OtherWorkspaceKey, "", "")
	var page struct {
		Items []runBody `json:"items"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &page)
	if response.Code != 200 || len(page.Items) != 0 {
		t.Fatal("runs leaked across workspaces")
	}
	response = d.do("GET", "/v1/reconciliation/runs", credentials.ReaderKey, "", "")
	_ = json.Unmarshal(response.Body.Bytes(), &page)
	if response.Code != 200 || len(page.Items) != 1 {
		t.Fatalf("run list %d %s", response.Code, response.Body.String())
	}
}
