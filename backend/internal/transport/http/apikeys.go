package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// Sandbox API key management for the dashboard (portfolio demo).
//
//	POST   /v1/api-keys        mint a key for the caller's workspace (operator)
//	GET    /v1/api-keys        list key metadata, never secrets or hashes (operator)
//	DELETE /v1/api-keys/{id}   revoke a key of the caller's workspace (operator)
//
// The secret is generated here from crypto/rand, returned exactly once in the
// 201 response and stored only as a SHA-256 hash, like the seeded demo keys. A
// minted key can never exceed the caller's authority (the generic handler
// already requires the operator role for every write), is always workspace
// scoped, always expires, and the number of live keys per workspace is capped.
// This is sandbox-only convenience: it creates no provider credentials and
// moves no money.

const (
	maxActiveAPIKeys      = 20
	defaultKeyExpiryHours = 24
	maxKeyExpiryHours     = 24 * 30
)

var apiKeyNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`)

type apiKeyView struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Prefix    string  `json:"prefix"`
	Role      string  `json:"role"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt *string `json:"expires_at"`
	RevokedAt *string `json:"revoked_at"`
	Status    string  `json:"status"`
}

func toAPIKeyView(key storage.APIKey, now int64) apiKeyView {
	view := apiKeyView{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Role: key.Role, CreatedAt: millisTime(key.CreatedAt)}
	status := "active"
	if key.ExpiresAt != nil {
		text := millisTime(*key.ExpiresAt)
		view.ExpiresAt = &text
		if *key.ExpiresAt <= now {
			status = "expired"
		}
	}
	if key.RevokedAt != nil {
		text := millisTime(*key.RevokedAt)
		view.RevokedAt = &text
		status = "revoked"
	}
	view.Status = status
	return view
}

func millisTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
}

// newAPIKey returns a prefix.secret key: a 4-byte random prefix (public, unique)
// and a 24-byte (192-bit) random secret, both hex, matching auth.APIKeyPrefix.
func newAPIKey() (key, prefix string, err error) {
	raw := make([]byte, 28)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	prefix = "rvk_" + hex.EncodeToString(raw[:4])
	return prefix + "." + hex.EncodeToString(raw[4:]), prefix, nil
}

func (h *handler) serveAPIKeys(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) bool {
	path := r.URL.Path
	switch {
	case path == "/v1/api-keys" && r.Method == http.MethodPost:
		h.createAPIKey(w, r, principal, id)
	case path == "/v1/api-keys" && r.Method == http.MethodGet:
		h.listAPIKeys(w, r, principal, id)
	case strings.HasPrefix(path, "/v1/api-keys/") && r.Method == http.MethodDelete:
		h.revokeAPIKey(w, r, principal, id, strings.TrimPrefix(path, "/v1/api-keys/"))
	default:
		return false
	}
	return true
}

func (h *handler) createAPIKey(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	// Writes already require the operator role and CSRF in ServeHTTP.
	var input struct {
		Name           string `json:"name"`
		Role           string `json:"role"`
		ExpiresInHours *int   `json:"expires_in_hours"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			fail(w, 413, "body_too_large", "Request body exceeds the limit", id)
			return
		}
		fail(w, 400, "validation_error", "Malformed request or invalid field types", id, FieldError{Field: "body", Message: "Use name, role and optional expires_in_hours"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		fail(w, 400, "validation_error", "Exactly one JSON value is required", id, FieldError{Field: "body", Message: "Trailing content after the JSON object"})
		return
	}
	var fields []FieldError
	name := strings.TrimSpace(input.Name)
	if !apiKeyNamePattern.MatchString(name) {
		fields = append(fields, FieldError{Field: "name", Message: "Use 1 to 64 letters, digits, spaces, dots, dashes or underscores, starting with a letter or digit"})
	}
	if input.Role != "read" && input.Role != "operator" {
		fields = append(fields, FieldError{Field: "role", Message: "Use read or operator"})
	}
	hours := defaultKeyExpiryHours
	if input.ExpiresInHours != nil {
		hours = *input.ExpiresInHours
	}
	if hours < 1 || hours > maxKeyExpiryHours {
		fields = append(fields, FieldError{Field: "expires_in_hours", Message: "Use a whole number from 1 to 720"})
	}
	if len(fields) > 0 {
		fail(w, 422, "validation_error", "The request is invalid", id, fields...)
		return
	}
	db := h.db.WithContext(r.Context())
	now := time.Now().UnixMilli()
	expires := now + int64(hours)*int64(time.Hour/time.Millisecond)
	errLimit := errors.New("api key limit reached")
	for attempt := 0; attempt < 3; attempt++ {
		secretKey, prefix, err := newAPIKey()
		if err != nil {
			fail(w, 503, "unavailable", "API key management unavailable", id)
			return
		}
		keyID, err := storage.NewID()
		if err != nil {
			fail(w, 503, "unavailable", "API key management unavailable", id)
			return
		}
		row := storage.APIKey{ID: keyID, WorkspaceID: principal.WorkspaceID, Name: name, Prefix: prefix, KeyHash: auth.Hash(secretKey), Role: input.Role, ExpiresAt: &expires, CreatedAt: now}
		// The cap is checked and the key inserted in one transaction, so
		// concurrent requests cannot both pass the check and exceed the limit.
		err = db.Transaction(func(tx *gorm.DB) error {
			var active int64
			if err := tx.Table("api_keys").
				Where("workspace_id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", principal.WorkspaceID, now).
				Count(&active).Error; err != nil {
				return err
			}
			if active >= maxActiveAPIKeys {
				return errLimit
			}
			return tx.Create(&row).Error
		})
		switch {
		case err == nil:
			// Log identifiers only: never the secret, hash or request body.
			h.config.Logger.Info("api key created", "request_id", id, "key_id", keyID, "prefix", prefix, "role", input.Role)
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Location", "/v1/api-keys/"+keyID)
			writeJSON(w, http.StatusCreated, map[string]any{
				"key": toAPIKeyView(row, now),
				// The only time the secret is ever returned.
				"secret": secretKey,
			})
			return
		case errors.Is(err, errLimit):
			fail(w, 409, "api_key_limit", "This workspace already has the maximum number of active API keys; revoke one first", id)
			return
		case storage.IsUniqueViolation(err):
			continue // a prefix collision; draw a new key
		default:
			fail(w, 503, "unavailable", "API key management unavailable", id)
			return
		}
	}
	fail(w, 503, "unavailable", "API key management unavailable", id)
}

func (h *handler) listAPIKeys(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	if principal.Role != "operator" {
		fail(w, 403, "forbidden", "Operator permission is required", id)
		return
	}
	var rows []storage.APIKey
	if err := h.db.WithContext(r.Context()).
		Where("workspace_id = ?", principal.WorkspaceID).
		Order("created_at DESC, id DESC").Limit(100).Find(&rows).Error; err != nil {
		fail(w, 503, "unavailable", "API key management unavailable", id)
		return
	}
	now := time.Now().UnixMilli()
	items := make([]apiKeyView, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAPIKeyView(row, now))
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": items, "limit": maxActiveAPIKeys})
}

func (h *handler) revokeAPIKey(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, keyID string) {
	if keyID == "" || strings.Contains(keyID, "/") || keyID == "current" {
		fail(w, 404, "not_found", "API key not found", id)
		return
	}
	db := h.db.WithContext(r.Context())
	var row storage.APIKey
	if err := db.Where("id = ? AND workspace_id = ?", keyID, principal.WorkspaceID).Take(&row).Error; err != nil {
		fail(w, 404, "not_found", "API key not found", id)
		return
	}
	if row.RevokedAt == nil {
		now := time.Now().UnixMilli()
		if err := db.Model(&storage.APIKey{}).
			Where("id = ? AND workspace_id = ? AND revoked_at IS NULL", keyID, principal.WorkspaceID).
			Update("revoked_at", now).Error; err != nil {
			fail(w, 503, "unavailable", "API key management unavailable", id)
			return
		}
		row.RevokedAt = &now
		h.config.Logger.Info("api key revoked", "request_id", id, "key_id", keyID, "prefix", row.Prefix)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, toAPIKeyView(row, time.Now().UnixMilli()))
}
