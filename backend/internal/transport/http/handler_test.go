package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/storage"
	httpapi "github.com/railvanta/railvanta/backend/internal/transport/http"
)

func TestHTTPAuthorityValidationAndSafeErrors(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "transport.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	credentials := auth.DemoCredentials{OperatorKey: "operator." + strings.Repeat("a", 32), ReaderKey: "reader." + strings.Repeat("b", 32), OtherWorkspaceKey: "other." + strings.Repeat("c", 32), SessionToken: strings.Repeat("s", 32), CSRFToken: strings.Repeat("x", 32)}
	if err := auth.SeedDemo(context.Background(), store.DB, credentials); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	handler := httpapi.NewHandler(store.DB, httpapi.Config{PublicOrigin: "http://localhost:3000", Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	call := func(method, path, key, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "never-log-this-idempotency-key")
		if key != "" {
			request.Header.Set("Authorization", "Bearer "+key)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing request ID")
		}
		if response.Code >= 400 {
			var envelope httpapi.ErrorEnvelope
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Code == "" || envelope.RequestID == "" || envelope.FieldErrors == nil {
				t.Fatalf("invalid error: %s", response.Body.String())
			}
		}
		return response
	}
	if response := call("GET", "/v1/workspace", "", ""); response.Code != 401 {
		t.Fatalf("unauthorized %d", response.Code)
	}
	if response := call("GET", "/v1/workspace", credentials.OperatorKey, ""); response.Code != 200 || !strings.Contains(response.Body.String(), auth.DemoWorkspaceID) {
		t.Fatalf("workspace %d %s", response.Code, response.Body.String())
	}
	for _, path := range []string{"/v1/workspaces/" + auth.OtherWorkspaceID, "/v1/payment-methods/" + auth.OtherMethodID} {
		if response := call("GET", path, credentials.OperatorKey, ""); response.Code != 404 {
			t.Fatalf("tenant leak %s %d", path, response.Code)
		}
	}
	valid := `{"amount_minor":"9223372036854775807","currency":"USD","payment_method_id":"` + auth.DemoMethodID + `"}`
	if response := call("POST", "/v1/payments", credentials.ReaderKey, valid); response.Code != 403 {
		t.Fatalf("reader wrote %d", response.Code)
	}
	if response := call("POST", "/v1/payments", credentials.OperatorKey, valid); response.Code != 501 {
		t.Fatalf("valid intent should honestly remain unavailable: %d %s", response.Code, response.Body.String())
	}
	for _, amount := range []string{`123`, `"9223372036854775808"`, `"1.5"`, `"-1"`, `"0"`, `" 1"`, `"+1"`} {
		body := `{"amount_minor":` + amount + `,"currency":"USD","payment_method_id":"` + auth.DemoMethodID + `"}`
		if response := call("POST", "/v1/payments", credentials.OperatorKey, body); response.Code != 400 {
			t.Errorf("accepted %s: %d", amount, response.Code)
		}
	}
	if response := call("POST", "/v1/payments", credentials.OperatorKey, `{"amount_minor":"1","currency":"USD","payment_method_id":"`+auth.OtherMethodID+`"}`); response.Code != 404 {
		t.Fatalf("foreign method %d", response.Code)
	}
	if response := call("POST", "/v1/payments", credentials.OperatorKey, `{"amount_minor":"1","currency":"USD","payment_method_id":"`+auth.DemoMethodID+`","workspace_id":"`+auth.OtherWorkspaceID+`"}`); response.Code != 400 {
		t.Fatalf("workspace body accepted %d", response.Code)
	}
	if response := call("POST", "/v1/payments", credentials.OperatorKey, `{"merchant_reference":"`+strings.Repeat("x", 70000)+`"}`); response.Code != 413 {
		t.Fatalf("body limit %d", response.Code)
	}
	request := httptest.NewRequest("POST", "/v1/payments", strings.NewReader(valid))
	request.AddCookie(auth.SessionCookie(credentials.SessionToken, time.Now().Add(time.Hour)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatalf("cookie missing CSRF %d", response.Code)
	}
	request = httptest.NewRequest("POST", "/v1/payments", strings.NewReader(valid))
	request.AddCookie(auth.SessionCookie(credentials.SessionToken, time.Now().Add(time.Hour)))
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("X-CSRF-Token", credentials.CSRFToken)
	request.Header.Set("Idempotency-Key", "intent")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 501 {
		t.Fatalf("valid cookie intent %d %s", response.Code, response.Body.String())
	}
	for _, secret := range []string{credentials.OperatorKey, credentials.ReaderKey, credentials.SessionToken, credentials.CSRFToken, "never-log-this-idempotency-key", "merchant_reference"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("log leaked secret/body %s", secret)
		}
	}
	if response := call("DELETE", "/v1/api-keys/current", credentials.OtherWorkspaceKey, ""); response.Code != 204 {
		t.Fatalf("self revocation failed: %d %s", response.Code, response.Body.String())
	}
	if response := call("GET", "/v1/workspace", credentials.OtherWorkspaceKey, ""); response.Code != 401 {
		t.Fatalf("revoked credential accepted: %d", response.Code)
	}
	if response := call("GET", "/v1/workspace", credentials.OperatorKey, ""); response.Code != 200 {
		t.Fatalf("self revocation affected another workspace: %d", response.Code)
	}
	limited := httpapi.NewHandler(store.DB, httpapi.Config{RequestsPerMinute: 1})
	for i := 0; i < 2; i++ {
		response := httptest.NewRecorder()
		limited.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/workspace", nil))
		if i == 1 && response.Code != 429 {
			t.Fatalf("rate limit %d", response.Code)
		}
	}
}
