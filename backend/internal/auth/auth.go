// Package auth authenticates workspace-scoped sessions and API keys.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

const SessionCookieName = "__Host-railvanta_session"

// Principal carries authority derived from the database, never request payloads.
type Principal struct {
	WorkspaceID, Role, CSRFHash string
	Cookie                      bool
}

// Hash stores only one-way fingerprints of high-entropy credentials.
func Hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func equalHash(value, expected string) bool {
	actual := Hash(value)
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

// APIKeyPrefix validates prefix.secret keys without exposing their secret.
func APIKeyPrefix(key string) (string, bool) {
	prefix, secret, ok := strings.Cut(key, ".")
	if !ok || len(prefix) < 4 || len(prefix) > 32 || len(secret) < 24 || len(secret) > 128 {
		return "", false
	}
	for _, c := range prefix {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return "", false
		}
	}
	return prefix, true
}

// Authenticate prefers an explicitly supplied Authorization header and never
// falls back to a session when that header is malformed or revoked.
func Authenticate(ctx context.Context, db *gorm.DB, request *http.Request) (Principal, error) {
	denied := errors.New("invalid or expired credentials")
	now := time.Now().UnixMilli()
	if header := request.Header.Get("Authorization"); header != "" {
		if !strings.HasPrefix(header, "Bearer ") {
			return Principal{}, denied
		}
		key := strings.TrimPrefix(header, "Bearer ")
		prefix, ok := APIKeyPrefix(key)
		if !ok {
			return Principal{}, denied
		}
		var row struct {
			WorkspaceID, Role, KeyHash string
			ExpiresAt, RevokedAt       *int64
		}
		result := db.WithContext(ctx).Table("api_keys").Where("prefix = ?", prefix).Take(&row)
		if result.Error != nil || !equalHash(key, row.KeyHash) || row.RevokedAt != nil || (row.ExpiresAt != nil && *row.ExpiresAt <= now) {
			return Principal{}, denied
		}
		return Principal{WorkspaceID: row.WorkspaceID, Role: row.Role}, nil
	}
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil || len(cookie.Value) < 24 || len(cookie.Value) > 256 {
		return Principal{}, denied
	}
	var row struct {
		WorkspaceID, Role, TokenHash, CSRFHash string
		ExpiresAt                              int64
	}
	result := db.WithContext(ctx).Table("sessions AS s").Select("s.workspace_id, m.role, s.token_hash, s.csrf_hash, s.expires_at").Joins("JOIN workspace_members AS m ON m.user_id = s.user_id AND m.workspace_id = s.workspace_id").Where("s.token_hash = ?", Hash(cookie.Value)).Take(&row)
	if result.Error != nil || !equalHash(cookie.Value, row.TokenHash) || row.ExpiresAt <= now {
		return Principal{}, denied
	}
	return Principal{WorkspaceID: row.WorkspaceID, Role: row.Role, CSRFHash: row.CSRFHash, Cookie: true}, nil
}

// CheckCSRF binds a write to its session and the configured browser origin.
func CheckCSRF(request *http.Request, principal Principal, origin string) bool {
	return !principal.Cookie || (origin != "" && request.Header.Get("Origin") == origin && equalHash(request.Header.Get("X-CSRF-Token"), principal.CSRFHash))
}

// SessionCookie returns a host-only, HTTPS-only, HttpOnly session cookie.
func SessionCookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: SessionCookieName, Value: token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: expires}
}
