package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
)

type mintedKey struct {
	Key struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		Prefix    string  `json:"prefix"`
		Role      string  `json:"role"`
		Status    string  `json:"status"`
		ExpiresAt *string `json:"expires_at"`
	} `json:"key"`
	Secret string `json:"secret"`
}

var generatedKeyPattern = regexp.MustCompile(`^rvk_[0-9a-f]{8}\.[0-9a-f]{48}$`)

func (e *env) mint(apiKey, body string) (*httptest.ResponseRecorder, mintedKey) {
	e.t.Helper()
	response := e.do("POST", "/v1/api-keys", apiKey, "", body)
	var minted mintedKey
	if response.Code == http.StatusCreated {
		if err := json.Unmarshal(response.Body.Bytes(), &minted); err != nil {
			e.t.Fatalf("decode minted key: %v: %s", err, response.Body.String())
		}
	}
	return response, minted
}

func (e *env) apiKeyRows(workspace string) int64 {
	e.t.Helper()
	var n int64
	if err := e.store.DB.Table("api_keys").Where("workspace_id = ?", workspace).Count(&n).Error; err != nil {
		e.t.Fatal(err)
	}
	return n
}

// The generator returns the secret exactly once, stores only its hash, and the
// minted key authenticates with the role it was given.
func TestMintedAPIKeyWorksAndStoresOnlyAHash(t *testing.T) {
	e := newEnv(t, payment.Options{})
	response, minted := e.mint(credentials.OperatorKey, `{"name":"Demo laptop","role":"operator","expires_in_hours":2}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("mint: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a response carrying a secret must not be cacheable: %q", response.Header().Get("Cache-Control"))
	}
	if !generatedKeyPattern.MatchString(minted.Secret) {
		t.Fatalf("unexpected key format: %q", minted.Secret)
	}
	if prefix, ok := auth.APIKeyPrefix(minted.Secret); !ok || prefix != minted.Key.Prefix {
		t.Fatalf("returned prefix %q does not match the key", minted.Key.Prefix)
	}
	if minted.Key.Role != "operator" || minted.Key.Name != "Demo laptop" || minted.Key.Status != "active" || minted.Key.ExpiresAt == nil {
		t.Fatalf("unexpected key view: %+v", minted.Key)
	}

	// Only the SHA-256 hash is stored; the secret appears nowhere in the table.
	var stored struct{ KeyHash, Name, Prefix string }
	if err := e.store.DB.Table("api_keys").Where("id = ?", minted.Key.ID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.KeyHash != auth.Hash(minted.Secret) || len(stored.KeyHash) != 64 {
		t.Fatalf("stored hash does not match: %q", stored.KeyHash)
	}
	secretPart := strings.SplitN(minted.Secret, ".", 2)[1]
	var leaks int64
	e.store.DB.Raw("SELECT count(*) FROM api_keys WHERE key_hash LIKE ? OR name LIKE ? OR prefix LIKE ?", "%"+secretPart+"%", "%"+secretPart+"%", "%"+secretPart+"%").Scan(&leaks)
	if leaks != 0 {
		t.Fatalf("the plaintext secret was stored")
	}

	// The key authenticates, and an operator key can itself mint further keys.
	if got := e.do("GET", "/v1/workspace", minted.Secret, "", "").Code; got != 200 {
		t.Fatalf("minted key rejected: %d", got)
	}
	if got, _ := e.mint(minted.Secret, `{"name":"Second","role":"read"}`); got.Code != http.StatusCreated {
		t.Fatalf("operator-minted key could not mint: %d", got.Code)
	}
}

// A read key can read but can neither write, nor mint, nor list keys.
func TestReadKeyCannotEscalate(t *testing.T) {
	e := newEnv(t, payment.Options{})
	_, minted := e.mint(credentials.OperatorKey, `{"name":"Viewer","role":"read"}`)
	if minted.Secret == "" {
		t.Fatal("mint failed")
	}
	if got := e.do("GET", "/v1/workspace", minted.Secret, "", "").Code; got != 200 {
		t.Fatalf("read key cannot read: %d", got)
	}
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/v1/api-keys", `{"name":"Escalate","role":"operator"}`},
		{"GET", "/v1/api-keys", ""},
		{"DELETE", "/v1/api-keys/" + minted.Key.ID, ""},
		{"POST", "/v1/payments", intent("1000", "USD", "00000000-0000-7000-8000-000000000101")},
	} {
		key := ""
		if c.path == "/v1/payments" {
			key = "k-escalate"
		}
		if got := e.do(c.method, c.path, minted.Secret, key, c.body).Code; got != http.StatusForbidden {
			t.Fatalf("%s %s with a read key: want 403, got %d", c.method, c.path, got)
		}
	}
	if got := e.do("GET", "/v1/api-keys", credentials.ReaderKey, "", "").Code; got != http.StatusForbidden {
		t.Fatalf("the seeded reader must not list keys: %d", got)
	}
}

// Listing never reveals a secret or a hash, and is scoped to the workspace.
func TestListShowsMetadataOnlyAndStaysInWorkspace(t *testing.T) {
	e := newEnv(t, payment.Options{})
	_, minted := e.mint(credentials.OperatorKey, `{"name":"Listed","role":"read"}`)
	list := e.do("GET", "/v1/api-keys", credentials.OperatorKey, "", "")
	if list.Code != 200 {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	body := list.Body.String()
	secretPart := strings.SplitN(minted.Secret, ".", 2)[1]
	for _, forbidden := range []string{minted.Secret, secretPart, auth.Hash(minted.Secret), "key_hash", "secret"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("list leaks %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, minted.Key.Prefix) || !strings.Contains(body, "Listed") {
		t.Fatalf("list lacks the key metadata: %s", body)
	}
	if other := e.do("GET", "/v1/api-keys", credentials.OtherWorkspaceKey, "", ""); strings.Contains(other.Body.String(), minted.Key.Prefix) {
		t.Fatalf("another workspace can see this workspace's keys: %s", other.Body.String())
	}
}

func TestMintValidationCreatesNothing(t *testing.T) {
	e := newEnv(t, payment.Options{})
	before := e.apiKeyRows(auth.DemoWorkspaceID)
	for name, body := range map[string]string{
		"bad name":        `{"name":"<script>","role":"read"}`,
		"empty name":      `{"name":"  ","role":"read"}`,
		"long name":       `{"name":"` + strings.Repeat("a", 65) + `","role":"read"}`,
		"unknown role":    `{"name":"x1","role":"admin"}`,
		"missing role":    `{"name":"x1"}`,
		"zero expiry":     `{"name":"x1","role":"read","expires_in_hours":0}`,
		"excess expiry":   `{"name":"x1","role":"read","expires_in_hours":721}`,
		"negative expiry": `{"name":"x1","role":"read","expires_in_hours":-1}`,
	} {
		response := e.do("POST", "/v1/api-keys", credentials.OperatorKey, "", body)
		if response.Code != 422 {
			t.Fatalf("%s: want 422, got %d %s", name, response.Code, response.Body.String())
		}
	}
	for name, body := range map[string]string{
		"unknown field": `{"name":"x1","role":"read","workspace_id":"` + auth.OtherWorkspaceID + `"}`,
		"not json":      `nope`,
		"trailing":      `{"name":"x1","role":"read"} {}`,
	} {
		if got := e.do("POST", "/v1/api-keys", credentials.OperatorKey, "", body).Code; got != 400 {
			t.Fatalf("%s: want 400, got %d", name, got)
		}
	}
	if after := e.apiKeyRows(auth.DemoWorkspaceID); after != before {
		t.Fatalf("invalid requests created %d row(s)", after-before)
	}
	// The workspace is derived from the credential: it can't be forged.
	if got := e.apiKeyRows(auth.OtherWorkspaceID); got != 1 {
		t.Fatalf("other workspace key count changed: %d", got)
	}
}

func TestActiveKeyCapIsEnforced(t *testing.T) {
	e := newEnv(t, payment.Options{})
	for i := 0; i < 60; i++ {
		response, _ := e.mint(credentials.OperatorKey, `{"name":"Bulk","role":"read"}`)
		if response.Code == http.StatusConflict {
			var envelope struct{ Code string }
			_ = json.Unmarshal(response.Body.Bytes(), &envelope)
			if envelope.Code != "api_key_limit" {
				t.Fatalf("unexpected conflict code %q", envelope.Code)
			}
			var active int64
			e.store.DB.Table("api_keys").Where("workspace_id = ? AND revoked_at IS NULL", auth.DemoWorkspaceID).Count(&active)
			if active != 20 {
				t.Fatalf("the cap should stop at 20 active keys, found %d", active)
			}
			// Revoking one frees a slot.
			list := e.do("GET", "/v1/api-keys", credentials.OperatorKey, "", "")
			var page struct{ Items []struct{ ID, Name string } }
			_ = json.Unmarshal(list.Body.Bytes(), &page)
			for _, item := range page.Items {
				if item.Name == "Bulk" {
					if got := e.do("DELETE", "/v1/api-keys/"+item.ID, credentials.OperatorKey, "", "").Code; got != 200 {
						t.Fatalf("revoke: %d", got)
					}
					break
				}
			}
			if again, _ := e.mint(credentials.OperatorKey, `{"name":"After","role":"read"}`); again.Code != http.StatusCreated {
				t.Fatalf("a freed slot should allow a new key: %d", again.Code)
			}
			return
		}
		if response.Code != http.StatusCreated {
			t.Fatalf("mint %d: %d %s", i, response.Code, response.Body.String())
		}
	}
	t.Fatal("the cap was never reached")
}

func TestRevokedAndExpiredKeysAreRejected(t *testing.T) {
	e := newEnv(t, payment.Options{})
	_, revoked := e.mint(credentials.OperatorKey, `{"name":"To revoke","role":"read"}`)
	if got := e.do("DELETE", "/v1/api-keys/"+revoked.Key.ID, credentials.OperatorKey, "", "").Code; got != 200 {
		t.Fatalf("revoke: %d", got)
	}
	if got := e.do("GET", "/v1/workspace", revoked.Secret, "", "").Code; got != 401 {
		t.Fatalf("a revoked key must be rejected: %d", got)
	}
	// Idempotent: a second revoke still answers 200 and shows revoked.
	again := e.do("DELETE", "/v1/api-keys/"+revoked.Key.ID, credentials.OperatorKey, "", "")
	if again.Code != 200 || !strings.Contains(again.Body.String(), `"status":"revoked"`) {
		t.Fatalf("second revoke: %d %s", again.Code, again.Body.String())
	}

	_, expiring := e.mint(credentials.OperatorKey, `{"name":"To expire","role":"read","expires_in_hours":1}`)
	e.exec("UPDATE api_keys SET expires_at = 1 WHERE id = ?", expiring.Key.ID)
	if got := e.do("GET", "/v1/workspace", expiring.Secret, "", "").Code; got != 401 {
		t.Fatalf("an expired key must be rejected: %d", got)
	}
	if list := e.do("GET", "/v1/api-keys", credentials.OperatorKey, "", ""); !strings.Contains(list.Body.String(), `"status":"expired"`) {
		t.Fatalf("an expired key should be listed as expired: %s", list.Body.String())
	}
}

// Another workspace can neither see nor revoke these keys; unknown IDs are 404.
func TestRevocationIsWorkspaceScoped(t *testing.T) {
	e := newEnv(t, payment.Options{})
	_, minted := e.mint(credentials.OperatorKey, `{"name":"Mine","role":"read"}`)
	if got := e.do("DELETE", "/v1/api-keys/"+minted.Key.ID, credentials.OtherWorkspaceKey, "", "").Code; got != 404 {
		t.Fatalf("a foreign workspace revoking this key: want 404, got %d", got)
	}
	if got := e.do("GET", "/v1/workspace", minted.Secret, "", "").Code; got != 200 {
		t.Fatalf("the key must still work: %d", got)
	}
	for _, id := range []string{"00000000-0000-7000-8000-00000000dead", "nope", "a/b"} {
		if got := e.do("DELETE", "/v1/api-keys/"+id, credentials.OperatorKey, "", "").Code; got != 404 {
			t.Fatalf("revoking %q: want 404, got %d", id, got)
		}
	}
}

// Cookie sessions need the CSRF token to mint; the generator adds no bypass.
func TestMintWithSessionRequiresCSRF(t *testing.T) {
	e := newEnv(t, payment.Options{})
	request := httptest.NewRequest("POST", "/v1/api-keys", strings.NewReader(`{"name":"Browser","role":"read"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: credentials.SessionToken})
	response := httptest.NewRecorder()
	e.handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("a session write without CSRF must be rejected, got %d", response.Code)
	}
	var created int64
	e.store.DB.Table("api_keys").Where("name = ?", "Browser").Count(&created)
	if created != 0 {
		t.Fatal("a rejected request created a key")
	}
}

// The cap is enforced atomically: concurrent mints can never exceed it.
func TestConcurrentMintsNeverExceedTheCap(t *testing.T) {
	e := newEnv(t, payment.Options{})
	const attempts = 40
	codes := make([]int, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			request := httptest.NewRequest("POST", "/v1/api-keys", strings.NewReader(`{"name":"Racer","role":"read"}`))
			request.Header.Set("Authorization", "Bearer "+credentials.OperatorKey)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			e.handler.ServeHTTP(response, request)
			codes[i] = response.Code
		}(i)
	}
	wg.Wait()
	created, limited := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			limited++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	var active int64
	e.store.DB.Table("api_keys").Where("workspace_id = ? AND revoked_at IS NULL", auth.DemoWorkspaceID).Count(&active)
	if active != 20 {
		t.Fatalf("active keys = %d, want exactly the cap of 20 (created %d, limited %d)", active, created, limited)
	}
	if created+limited != attempts {
		t.Fatalf("lost responses: created %d limited %d", created, limited)
	}
}
