package auth_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

func TestAuthenticationBoundaries(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "auth.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	credentials := auth.DemoCredentials{OperatorKey: "operator." + strings.Repeat("a", 32), ReaderKey: "reader." + strings.Repeat("b", 32), OtherWorkspaceKey: "other." + strings.Repeat("c", 32), SessionToken: strings.Repeat("s", 32), CSRFToken: strings.Repeat("x", 32)}
	if err := auth.SeedDemo(ctx, store.DB, credentials); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/v1/workspace", nil)
	if _, err := auth.Authenticate(ctx, store.DB, request); err == nil {
		t.Fatal("missing credentials accepted")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.ReaderKey)
	principal, err := auth.Authenticate(ctx, store.DB, request)
	if err != nil || principal.Role != "read" || principal.WorkspaceID != auth.DemoWorkspaceID {
		t.Fatalf("reader: %+v %v", principal, err)
	}
	if err := store.DB.Exec("UPDATE api_keys SET revoked_at = ? WHERE prefix = 'reader'", time.Now().UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if err := auth.SeedDemo(ctx, store.DB, credentials); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ctx, store.DB, request); err == nil {
		t.Fatal("revoked key revived by restart seed")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.OtherWorkspaceKey)
	principal, err = auth.Authenticate(ctx, store.DB, request)
	if err != nil || principal.WorkspaceID != auth.OtherWorkspaceID {
		t.Fatalf("workspace mismatch: %+v %v", principal, err)
	}
	if err := store.DB.Exec("UPDATE api_keys SET expires_at = ? WHERE prefix = 'other'", time.Now().Add(-time.Minute).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ctx, store.DB, request); err == nil {
		t.Fatal("expired key accepted")
	}
	request.Header.Del("Authorization")
	cookie := auth.SessionCookie(credentials.SessionToken, time.Now().Add(time.Hour))
	if !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatal("unsafe session cookie")
	}
	request.AddCookie(cookie)
	principal, err = auth.Authenticate(ctx, store.DB, request)
	if err != nil || !principal.Cookie || principal.Role != "operator" {
		t.Fatalf("session: %+v %v", principal, err)
	}
	request.Header.Set("Origin", "http://localhost:3000")
	if auth.CheckCSRF(request, principal, "http://localhost:3000") {
		t.Fatal("missing CSRF accepted")
	}
	request.Header.Set("X-CSRF-Token", credentials.CSRFToken)
	if !auth.CheckCSRF(request, principal, "http://localhost:3000") {
		t.Fatal("valid CSRF rejected")
	}
	request.Header.Set("Origin", "https://attacker.example")
	if auth.CheckCSRF(request, principal, "http://localhost:3000") {
		t.Fatal("wrong origin accepted")
	}
	request.Header.Set("Authorization", "Bearer broken")
	if _, err := auth.Authenticate(ctx, store.DB, request); err == nil {
		t.Fatal("invalid bearer fell back to cookie")
	}
	request.Header.Del("Authorization")
	if err := store.DB.Exec("DELETE FROM workspace_members WHERE workspace_id = ?", auth.DemoWorkspaceID).Error; err == nil {
		t.Fatal("referenced membership deletion unexpectedly succeeded")
	}
	if err := store.DB.Exec("UPDATE sessions SET expires_at = ?", time.Now().Add(-time.Minute).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ctx, store.DB, request); err == nil {
		t.Fatal("expired session accepted")
	}
	var hash string
	if err := store.DB.Table("api_keys").Select("key_hash").Where("prefix = ?", "operator").Scan(&hash).Error; err != nil {
		t.Fatal(err)
	}
	if hash == credentials.OperatorKey || len(hash) != 64 {
		t.Fatal("credential stored unhashed")
	}
	changed := credentials
	changed.OperatorKey = "operator." + strings.Repeat("z", 32)
	if err := auth.SeedDemo(ctx, store.DB, changed); err == nil {
		t.Fatal("changed bootstrap credentials were silently ignored")
	}
}

func TestDemoRejectsSharedCredentialPrefixes(t *testing.T) {
	credentials := auth.DemoCredentials{
		OperatorKey:       "shared." + strings.Repeat("a", 32),
		ReaderKey:         "shared." + strings.Repeat("b", 32),
		OtherWorkspaceKey: "other." + strings.Repeat("c", 32),
	}
	if err := auth.SeedDemo(context.Background(), nil, credentials); err == nil {
		t.Fatal("a shared prefix can accidentally grant the reader operator authority")
	}
}
