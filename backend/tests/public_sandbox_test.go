package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	httpapi "github.com/railvanta/railvanta/backend/internal/transport/http"
)

const sandboxOrigin = "http://localhost:3000"

func (e *env) enablePublicAccess() {
	e.handler = httpapi.NewHandler(e.store.DB, httpapi.Config{PublicOrigin: sandboxOrigin, RequestsPerMinute: 100000, Payments: e.service, PublicSandboxAccess: true})
}

func (e *env) req(method, path, origin, remote string, cookies []*http.Cookie, csrf string) *httptest.ResponseRecorder {
	e.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(`{"ignored":true}`))
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if remote != "" {
		request.RemoteAddr = remote
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	if method == "POST" {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		request.AddCookie(c)
	}
	response := httptest.NewRecorder()
	e.handler.ServeHTTP(response, request)
	return response
}

func (e *env) visitorRows() (users, sessions int64) {
	e.t.Helper()
	e.store.DB.Table("users").Where("email LIKE ?", "%@sandbox.invalid").Count(&users)
	e.store.DB.Table("sessions AS s").Joins("JOIN users u ON u.id = s.user_id").Where("u.email LIKE ?", "%@sandbox.invalid").Count(&sessions)
	return
}

func cookieByName(response *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range response.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (e *env) newVisitor() (session, csrf *http.Cookie) {
	e.t.Helper()
	response := e.req("POST", "/v1/sandbox/session", sandboxOrigin, "", nil, "")
	if response.Code != http.StatusCreated {
		e.t.Fatalf("session: %d %s", response.Code, response.Body.String())
	}
	return cookieByName(response, auth.SessionCookieName), cookieByName(response, auth.CSRFCookieName)
}

func TestPublicSandboxDisabledByDefault(t *testing.T) {
	e := newEnv(t, payment.Options{})
	status := e.req("GET", "/v1/sandbox/status", "", "", nil, "")
	if status.Code != 200 || status.Header().Get("Cache-Control") != "no-store" || strings.Contains(status.Body.String(), "workspace_name") || !strings.Contains(status.Body.String(), `"public_access":false`) {
		t.Fatalf("status: %d %s", status.Code, status.Body.String())
	}
	response := e.req("POST", "/v1/sandbox/session", sandboxOrigin, "", nil, "")
	var envelope httpapi.ErrorEnvelope
	_ = json.Unmarshal(response.Body.Bytes(), &envelope)
	if response.Code != 403 || envelope.Code != "sandbox_access_disabled" || len(response.Result().Cookies()) != 0 {
		t.Fatalf("disabled: %d %s", response.Code, response.Body.String())
	}
	if users, sessions := e.visitorRows(); users != 0 || sessions != 0 {
		t.Fatalf("rows created while disabled: %d %d", users, sessions)
	}
}

func TestPublicSandboxSessionLifecycle(t *testing.T) {
	e := newEnv(t, payment.Options{})
	e.enablePublicAccess()

	status := e.req("GET", "/v1/sandbox/status", "", "", nil, "")
	var body map[string]any
	_ = json.Unmarshal(status.Body.Bytes(), &body)
	if body["public_access"] != true || body["workspace_name"] != "Demo workspace" || body["session_ttl_hours"] != float64(12) || len(body) != 3 {
		t.Fatalf("status leaks or is wrong: %s", status.Body.String())
	}

	response := e.req("POST", "/v1/sandbox/session", sandboxOrigin, "", nil, "")
	if response.Code != 201 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	session, csrf := cookieByName(response, auth.SessionCookieName), cookieByName(response, auth.CSRFCookieName)
	if session == nil || csrf == nil {
		t.Fatalf("missing cookies: %v", response.Result().Cookies())
	}
	if !session.Secure || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/" {
		t.Fatalf("session cookie attributes: %+v", session)
	}
	if !csrf.Secure || csrf.HttpOnly || csrf.SameSite != http.SameSiteStrictMode || csrf.Path != "/" || !csrf.Expires.Equal(session.Expires) {
		t.Fatalf("csrf cookie attributes: %+v", csrf)
	}
	var created struct {
		Workspace struct{ ID, Name, Environment string }
		Role      string
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	expires, err := time.Parse(time.RFC3339, created.ExpiresAt)
	if err != nil || created.Role != "operator" || created.Workspace.ID != auth.DemoWorkspaceID || created.Workspace.Environment != "sandbox" || time.Until(expires) < 11*time.Hour || time.Until(expires) > 12*time.Hour+time.Minute {
		t.Fatalf("response: %+v %v", created, err)
	}
	if strings.Contains(response.Body.String(), session.Value) || strings.Contains(response.Body.String(), csrf.Value) {
		t.Fatal("tokens must only travel in cookies")
	}
	var stored struct{ TokenHash, CSRFHash string }
	e.store.DB.Table("sessions").Where("token_hash = ?", auth.Hash(session.Value)).Take(&stored)
	if stored.TokenHash == session.Value || stored.CSRFHash != auth.Hash(csrf.Value) {
		t.Fatalf("only hashes may be stored: %+v", stored)
	}

	// The cookie authenticates reads.
	for _, path := range []string{"/v1/workspace", "/v1/payment-methods"} {
		if got := e.req("GET", path, "", "", []*http.Cookie{session}, ""); got.Code != 200 {
			t.Fatalf("GET %s: %d %s", path, got.Code, got.Body.String())
		}
	}
	// Writes need CSRF and Origin.
	mint := `{"name":"Visitor key","role":"read"}`
	post := func(origin, token string) int {
		request := httptest.NewRequest("POST", "/v1/api-keys", strings.NewReader(mint))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(session)
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		if token != "" {
			request.Header.Set("X-CSRF-Token", token)
		}
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, request)
		return rec.Code
	}
	if code := post("", ""); code != 403 {
		t.Fatalf("no csrf: %d", code)
	}
	if code := post(sandboxOrigin, ""); code != 403 {
		t.Fatalf("no csrf token: %d", code)
	}
	if code := post("http://evil.example", csrf.Value); code != 403 {
		t.Fatalf("wrong origin: %d", code)
	}
	if code := post(sandboxOrigin, csrf.Value); code != 201 {
		t.Fatalf("with csrf: %d", code)
	}

	// Sign out clears both cookies and kills the session.
	if got := e.req("DELETE", "/v1/sandbox/session", sandboxOrigin, "", []*http.Cookie{session}, ""); got.Code != 403 {
		t.Fatalf("delete without csrf: %d", got.Code)
	}
	out := e.req("DELETE", "/v1/sandbox/session", sandboxOrigin, "", []*http.Cookie{session}, csrf.Value)
	if out.Code != 204 {
		t.Fatalf("delete: %d %s", out.Code, out.Body.String())
	}
	for _, name := range []string{auth.SessionCookieName, auth.CSRFCookieName} {
		if c := cookieByName(out, name); c == nil || c.Value != "" || c.MaxAge >= 0 {
			t.Fatalf("%s not cleared: %+v", name, c)
		}
	}
	if got := e.req("GET", "/v1/workspace", "", "", []*http.Cookie{session}, ""); got.Code != 401 {
		t.Fatalf("after sign-out: %d", got.Code)
	}
	if users, sessions := e.visitorRows(); users != 0 || sessions != 0 {
		t.Fatalf("visitor rows remain: %d %d", users, sessions)
	}
}

func TestPublicSandboxRequiresOrigin(t *testing.T) {
	e := newEnv(t, payment.Options{})
	e.enablePublicAccess()
	for _, origin := range []string{"", "http://evil.example", "http://localhost:3001"} {
		response := e.req("POST", "/v1/sandbox/session", origin, "", nil, "")
		var envelope httpapi.ErrorEnvelope
		_ = json.Unmarshal(response.Body.Bytes(), &envelope)
		if response.Code != 403 || envelope.Code != "csrf_rejected" || len(response.Result().Cookies()) != 0 {
			t.Fatalf("origin %q: %d %s", origin, response.Code, response.Body.String())
		}
	}
	if users, _ := e.visitorRows(); users != 0 {
		t.Fatal("rejected requests created visitors")
	}
}

func TestPublicSandboxPerIPLimit(t *testing.T) {
	e := newEnv(t, payment.Options{})
	e.enablePublicAccess()
	for i := 0; i < 30; i++ {
		if got := e.req("POST", "/v1/sandbox/session", sandboxOrigin, "203.0.113.7:4000", nil, ""); got.Code != 201 {
			t.Fatalf("request %d: %d %s", i+1, got.Code, got.Body.String())
		}
	}
	got := e.req("POST", "/v1/sandbox/session", sandboxOrigin, "203.0.113.7:4001", nil, "")
	if got.Code != 429 || got.Header().Get("Retry-After") == "" {
		t.Fatalf("31st: %d retry-after=%q", got.Code, got.Header().Get("Retry-After"))
	}
	if other := e.req("POST", "/v1/sandbox/session", sandboxOrigin, "203.0.113.8:4000", nil, ""); other.Code != 201 {
		t.Fatalf("other IP: %d", other.Code)
	}
}

func (e *env) insertVisitor(n int, expires int64) {
	e.t.Helper()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("fake-%d-%d-%d", time.Now().UnixNano(), n, i)
		stmts := []string{
			"INSERT INTO users(id,email,created_at) VALUES(?,?,1)",
			"INSERT INTO workspace_members(user_id,workspace_id,role,created_at) VALUES(?,'" + auth.DemoWorkspaceID + "','operator',1)",
			"INSERT INTO sessions(id,user_id,workspace_id,token_hash,csrf_hash,expires_at,created_at) VALUES(?,?,'" + auth.DemoWorkspaceID + "',?,?,?,1)",
		}
		args := [][]any{{id, id + "@sandbox.invalid"}, {id}, {id, id, auth.Hash("t" + id), auth.Hash("c" + id), expires}}
		for j, s := range stmts {
			if err := e.store.DB.Exec(s, args[j]...).Error; err != nil {
				e.t.Fatal(err)
			}
		}
	}
}

func TestPublicSandboxGlobalCap(t *testing.T) {
	e := newEnv(t, payment.Options{})
	e.enablePublicAccess()
	e.insertVisitor(500, time.Now().Add(time.Hour).UnixMilli())
	got := e.req("POST", "/v1/sandbox/session", sandboxOrigin, "", nil, "")
	var envelope httpapi.ErrorEnvelope
	_ = json.Unmarshal(got.Body.Bytes(), &envelope)
	if got.Code != 503 || envelope.Code != "sandbox_busy" || got.Header().Get("Retry-After") == "" {
		t.Fatalf("cap: %d %s", got.Code, got.Body.String())
	}
	if _, sessions := e.visitorRows(); sessions != 500 {
		t.Fatalf("sessions %d", sessions)
	}
}

func TestPublicSandboxCleansExpiredVisitorsOnly(t *testing.T) {
	e := newEnv(t, payment.Options{})
	e.enablePublicAccess()
	e.insertVisitor(3, time.Now().Add(-time.Hour).UnixMilli())
	// Fill the cap with expired rows: cleanup must free the room.
	e.insertVisitor(1, time.Now().Add(time.Hour).UnixMilli())
	var expiredToken string
	e.store.DB.Table("sessions").Select("token_hash").Where("expires_at < ?", time.Now().UnixMilli()).Limit(1).Scan(&expiredToken)
	e.store.DB.Exec("UPDATE sessions SET token_hash = ? WHERE token_hash = ?", auth.Hash(strings.Repeat("e", 40)), expiredToken)
	expired := &http.Cookie{Name: auth.SessionCookieName, Value: strings.Repeat("e", 40)}
	if got := e.req("GET", "/v1/workspace", "", "", []*http.Cookie{expired}, ""); got.Code != 401 {
		t.Fatalf("expired visitor session must be rejected: %d", got.Code)
	}
	e.newVisitor() // triggers lazy cleanup
	if users, sessions := e.visitorRows(); users != 2 || sessions != 2 {
		t.Fatalf("after cleanup users=%d sessions=%d, want the live fake plus the new visitor", users, sessions)
	}
	// The seeded demo session and user survive and keep working.
	seeded := &http.Cookie{Name: auth.SessionCookieName, Value: credentials.SessionToken}
	if got := e.req("GET", "/v1/workspace", "", "", []*http.Cookie{seeded}, ""); got.Code != 200 {
		t.Fatalf("seeded session: %d", got.Code)
	}
	var demoUsers int64
	e.store.DB.Table("users").Where("email = ?", "demo@example.invalid").Count(&demoUsers)
	if demoUsers != 1 {
		t.Fatal("seeded user was removed")
	}
}

func TestPublicSandboxSeededSessionCannotBeSignedOut(t *testing.T) {
	e := newEnv(t, payment.Options{})
	e.enablePublicAccess()
	seeded := &http.Cookie{Name: auth.SessionCookieName, Value: credentials.SessionToken}
	got := e.req("DELETE", "/v1/sandbox/session", sandboxOrigin, "", []*http.Cookie{seeded}, credentials.CSRFToken)
	if got.Code != 403 {
		t.Fatalf("seeded delete: %d %s", got.Code, got.Body.String())
	}
	if got := e.req("GET", "/v1/workspace", "", "", []*http.Cookie{seeded}, ""); got.Code != 200 {
		t.Fatalf("seeded session after attempt: %d", got.Code)
	}
	// An API key cannot sign anything out either.
	if got := e.do("DELETE", "/v1/sandbox/session", credentials.OperatorKey, "", ""); got.Code != 403 {
		t.Fatalf("api key delete: %d", got.Code)
	}
}

func TestPublicSandboxLeavesOtherRoutesProtected(t *testing.T) {
	e := newEnv(t, payment.Options{})
	e.enablePublicAccess()
	for _, path := range []string{"/v1/payments", "/v1/workspace", "/v1/api-keys", "/v1/session"} {
		if got := e.do("GET", path, "", "", ""); got.Code != 401 {
			t.Fatalf("GET %s without credentials: %d", path, got.Code)
		}
	}
	if got := e.do("POST", "/v1/payments", "", "k1", intent("100", "USD", auth.DemoMethodID)); got.Code != 401 {
		t.Fatalf("POST /v1/payments: %d", got.Code)
	}
	if got := e.do("DELETE", "/v1/sandbox/session", "", "", ""); got.Code != 401 {
		t.Fatalf("unauthenticated DELETE: %d", got.Code)
	}
}
