package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
)

// otherSession is a browser session bound to the other workspace.
const otherSessionToken = "o0o0o0o0o0o0o0o0o0o0o0o0o0o0o0o0"

func (e *env) addOtherWorkspaceSession() {
	e.t.Helper()
	now := time.Now().UnixMilli()
	e.exec("INSERT INTO users(id,email,created_at) VALUES('00000000-0000-7000-8000-000000000502','other@example.invalid',?)", now)
	e.exec("INSERT INTO workspace_members(user_id,workspace_id,role,created_at) VALUES('00000000-0000-7000-8000-000000000502',?,'operator',?)", auth.OtherWorkspaceID, now)
	e.exec("INSERT INTO sessions(id,user_id,workspace_id,token_hash,csrf_hash,expires_at,created_at) VALUES('00000000-0000-7000-8000-000000000602','00000000-0000-7000-8000-000000000502',?,?,?,?,?)",
		auth.OtherWorkspaceID, auth.Hash(otherSessionToken), auth.Hash(strings.Repeat("y", 32)), time.Now().Add(time.Hour).UnixMilli(), now)
}

func (e *env) getWithSession(path, token string) *httptest.ResponseRecorder {
	e.t.Helper()
	request := httptest.NewRequest("GET", path, nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	response := httptest.NewRecorder()
	e.handler.ServeHTTP(response, request)
	return response
}

// Matrix "Forged workspace IDs": the workspace comes only from the credential;
// foreign IDs in bodies, paths, headers or queries are rejected or ignored and
// nothing is written.
func TestForgedWorkspaceIDsAreRejectedOrIgnored(t *testing.T) {
	e := newEnv(t, payment.Options{})
	before := e.dump()
	for name, body := range map[string]string{
		"body workspace_id": `{"amount_minor":"100","currency":"USD","payment_method_id":"` + auth.DemoMethodID + `","workspace_id":"` + auth.DemoWorkspaceID + `"}`,
		"body camelCase":    `{"amount_minor":"100","currency":"USD","payment_method_id":"` + auth.DemoMethodID + `","workspaceId":"` + auth.DemoWorkspaceID + `"}`,
	} {
		if response := e.do("POST", "/v1/payments", credentials.OtherWorkspaceKey, "forge-"+name, body); response.Code != 400 {
			t.Errorf("%s: %d %s", name, response.Code, response.Body)
		}
	}
	if after := e.dump(); strings.Join(flat(after), ",") != strings.Join(flat(before), ",") {
		t.Fatalf("forged bodies wrote rows")
	}
	// Headers and query parameters naming another workspace are ignored: the
	// other workspace's key still sees only its own (empty) payment list, and
	// the demo workspace's payment is not created in it.
	id := e.accept(auth.DemoMethodID, "100", "USD")
	request := httptest.NewRequest("GET", "/v1/payments?workspace_id="+auth.DemoWorkspaceID, nil)
	request.Header.Set("Authorization", "Bearer "+credentials.OtherWorkspaceKey)
	request.Header.Set("X-Workspace-ID", auth.DemoWorkspaceID)
	response := httptest.NewRecorder()
	e.handler.ServeHTTP(response, request)
	if response.Code != 200 || strings.Contains(response.Body.String(), id) {
		t.Fatalf("forged header/query: %d %s", response.Code, response.Body)
	}
	// The workspace in the path must equal the credential's workspace.
	if r := e.do("GET", "/v1/workspaces/"+auth.DemoWorkspaceID, credentials.OtherWorkspaceKey, "", ""); r.Code != 404 {
		t.Errorf("foreign workspace path: %d %s", r.Code, r.Body)
	}
	var own struct {
		ID string `json:"id"`
	}
	if r := e.do("GET", "/v1/workspace", credentials.OtherWorkspaceKey, "", ""); r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &own) != nil || own.ID != auth.OtherWorkspaceID {
		t.Errorf("own workspace: %d %s", r.Code, r.Body)
	}
}

func flat(m map[string]string) []string {
	var out []string
	for _, k := range sortedKeys(m) {
		out = append(out, k+"="+m[k])
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// Matrix "Forged workspace IDs" / WBS 5.1.5: every list and detail read, and
// cancel, with another workspace's API key or browser session reveals nothing
// of the demo workspace and changes nothing.
func TestOtherWorkspaceCredentialsNeverReadOrChangeForeignRows(t *testing.T) {
	d := newDispatchEnv(t, dispatchConfig{})
	d.addOtherWorkspaceSession()
	paymentID := d.accept(scenarioMethod["success"], "500", "USD")
	d.drain()
	d.webhook(webhookBody("evt_iso", payment.WebhookProcessing, paymentID))
	cancelID := d.accept(scenarioMethod["success"], "600", "USD") // stays created: not dispatched
	d.forge("iso-forged", forged{status: "succeeded", amount: 300, chargeStatus: "succeeded", chargeAmount: 999})
	_, run := d.startRun("iso-run")
	if code, _ := d.simulate("success", "iso-seed"); code != 202 {
		t.Fatalf("simulate %d", code)
	}
	var sim struct{ ID string }
	d.store.DB.Table("simulation_runs").Order("rowid desc").Limit(1).Select("id").Scan(&sim)
	// Collect every ID the demo workspace can see, then prove none of them is
	// visible with the other workspace's credentials.
	ids := map[string]bool{}
	lists := []string{"/v1/payments?limit=100", "/v1/ledger/transactions?limit=100", "/v1/ledger/accounts", "/v1/events?limit=100",
		"/v1/reconciliation/runs", "/v1/reconciliation/discrepancies", "/v1/providers", "/v1/payment-methods", "/v1/routing/rules"}
	for _, path := range lists {
		response := d.do("GET", path, credentials.OperatorKey, "", "")
		if response.Code != 200 {
			t.Fatalf("demo %s: %d %s", path, response.Code, response.Body)
		}
		collectIDs(t, response.Body.Bytes(), ids)
	}
	if len(ids) < 8 || !ids[paymentID] || !ids[run.ID] {
		t.Fatalf("demo fixture too small: %d ids", len(ids))
	}
	delete(ids, auth.OtherWorkspaceID)
	reads := append([]string{"/v1/dashboard/summary", "/v1/dashboard/payment-volume", "/v1/settings", "/v1/sandbox/scenarios"}, lists...)
	for _, path := range reads {
		for name, response := range map[string]*httptest.ResponseRecorder{
			"api key": d.do("GET", path, credentials.OtherWorkspaceKey, "", ""),
			"session": d.getWithSession(path, otherSessionToken),
		} {
			if response.Code >= 500 {
				t.Errorf("%s %s: %d %s", name, path, response.Code, response.Body)
			}
			assertNoForeignIDs(t, name+" "+path, response.Body.String(), ids, auth.DemoWorkspaceID)
		}
	}
	var journalID string
	for id := range ids {
		if response := d.do("GET", "/v1/ledger/transactions/"+id, credentials.OperatorKey, "", ""); response.Code == 200 {
			journalID = id
		}
	}
	if journalID == "" {
		t.Fatal("no ledger journal found for the demo workspace")
	}
	var discrepancyID string
	for _, item := range d.discrepancies("", credentials.OperatorKey) {
		discrepancyID = item.ID
	}
	if discrepancyID == "" {
		t.Fatal("no discrepancy found for the demo workspace")
	}
	details := []string{
		"/v1/payments/" + paymentID, "/v1/payments/" + paymentID + "/attempts", "/v1/payments/" + paymentID + "/history",
		"/v1/ledger/transactions/" + journalID, "/v1/reconciliation/discrepancies/" + discrepancyID,
		"/v1/sandbox/simulations/" + sim.ID, "/v1/providers/" + auth.DemoProviderID, "/v1/payment-methods/" + auth.DemoMethodID,
	}
	for _, path := range details {
		if response := d.do("GET", path, credentials.OperatorKey, "", ""); response.Code == 404 {
			t.Fatalf("demo workspace cannot read its own %s", path)
		}
		for name, response := range map[string]*httptest.ResponseRecorder{
			"api key": d.do("GET", path, credentials.OtherWorkspaceKey, "", ""),
			"session": d.getWithSession(path, otherSessionToken),
		} {
			if response.Code != 404 || strings.Contains(response.Body.String(), paymentID) {
				t.Errorf("%s %s: want 404 without leak, got %d %s", name, path, response.Code, response.Body)
			}
		}
	}
	// Writes against foreign rows: not found, and no row changes.
	before := d.dump()
	writes := []struct{ method, path, body string }{
		{"POST", "/v1/payments/" + cancelID + "/cancel", ""},
		{"POST", "/v1/reconciliation/discrepancies/" + discrepancyID + "/resolve", `{"note":"forged"}`},
		{"POST", "/v1/sandbox/webhooks/mock", webhookBody("evt_forged_foreign", payment.WebhookFailed, paymentID)},
	}
	for _, w := range writes {
		response := d.do(w.method, w.path, credentials.OtherWorkspaceKey, "forge-"+w.path, w.body)
		if response.Code >= 500 || response.Code == 200 && !strings.Contains(response.Body.String(), "unknown_payment") {
			t.Errorf("%s %s: %d %s", w.method, w.path, response.Code, response.Body)
		}
	}
	after := d.dump()
	for table, hash := range before {
		// A webhook from another workspace may append its own inbox/audit rows;
		// no other table, and no payment, may change.
		if table == "webhook_inbox" || table == "audit_log" || table == "audit_events" {
			continue
		}
		if after[table] != hash {
			t.Errorf("table %s changed through a foreign-workspace write", table)
		}
	}
	requireStatus(t, d.payment(cancelID), payment.StatusCreated)
	requireStatus(t, d.payment(paymentID), payment.StatusSucceeded)
}

func collectIDs(t *testing.T, body []byte, ids map[string]bool) {
	t.Helper()
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for key, child := range x {
				if s, ok := child.(string); ok && (key == "id" || strings.HasSuffix(key, "_id")) && len(s) >= 8 {
					ids[s] = true
				}
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(value)
}

func assertNoForeignIDs(t *testing.T, label, body string, ids map[string]bool, extra ...string) {
	t.Helper()
	for id := range ids {
		if strings.Contains(body, id) {
			t.Errorf("%s leaked foreign ID %s: %.300s", label, id, body)
			return
		}
	}
	for _, id := range extra {
		if strings.Contains(body, id) {
			t.Errorf("%s leaked %s", label, id)
		}
	}
}
