package httpapi

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// Public sandbox access (opt-in, SANDBOX_PUBLIC_ACCESS=true).
//
//	GET    /v1/sandbox/status   public: whether one-click sessions are on
//	POST   /v1/sandbox/session  public: create a throwaway operator visitor
//	DELETE /v1/sandbox/session  cookie + CSRF: sign the visitor out
//
// Trust model: anyone who can reach the (loopback) proxy gets operator rights
// in the shared demo workspace. Nothing moves real funds. Every other route
// still requires credentials. Visitors are rows in users/workspace_members/
// sessions whose email ends with @sandbox.invalid; only those are ever cleaned.
const (
	maxSessionsPerIPPerHour = 30
	maxLiveVisitorSessions  = 500
	cleanupBatch            = 100
	sessionIPWindow         = time.Hour
)

type sessionWindow struct {
	count int
	until time.Time
}

func remoteHost(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return remote
	}
	return host
}

// allowSession enforces the per-IP hourly session limit and returns the
// seconds to wait when it is exhausted.
func (h *handler) allowSession(remote string) (bool, int) {
	host := remoteHost(remote)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessionWindows == nil {
		h.sessionWindows = make(map[string]sessionWindow)
	}
	now := time.Now()
	if len(h.sessionWindows) >= 1024 {
		for key, window := range h.sessionWindows {
			if now.After(window.until) {
				delete(h.sessionWindows, key)
			}
		}
	}
	window := h.sessionWindows[host]
	if now.After(window.until) {
		window = sessionWindow{until: now.Add(sessionIPWindow)}
	}
	if window.count >= maxSessionsPerIPPerHour {
		return false, int(time.Until(window.until).Seconds()) + 1
	}
	if _, known := h.sessionWindows[host]; !known && len(h.sessionWindows) >= 4096 {
		return false, 60 // table full of live windows: refuse rather than grow
	}
	window.count++
	h.sessionWindows[host] = window
	return true, 0
}

// servePublicSandbox answers the two unauthenticated routes.
func (h *handler) servePublicSandbox(w http.ResponseWriter, r *http.Request, id string) bool {
	switch {
	case r.URL.Path == "/v1/sandbox/status" && r.Method == http.MethodGet:
		h.sandboxStatus(w)
	case r.URL.Path == "/v1/sandbox/session" && r.Method == http.MethodPost:
		h.createVisitorSession(w, r, id)
	default:
		return false
	}
	return true
}

func (h *handler) sandboxStatus(w http.ResponseWriter) {
	body := map[string]any{"public_access": h.config.PublicSandboxAccess, "session_ttl_hours": int(auth.VisitorSessionTTL / time.Hour)}
	if h.config.PublicSandboxAccess {
		body["workspace_name"] = h.workspaceName()
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *handler) workspaceName() string {
	var name string
	if err := h.db.Table("workspaces").Select("name").Where("id = ?", auth.DemoWorkspaceID).Scan(&name).Error; err != nil || name == "" {
		return "Demo workspace"
	}
	return name
}

func (h *handler) createVisitorSession(w http.ResponseWriter, r *http.Request, id string) {
	if !h.config.PublicSandboxAccess {
		fail(w, 403, "sandbox_access_disabled", "Public sandbox access is not enabled on this server", id)
		return
	}
	// Same browser-origin rule as CSRF; the body is never read.
	if h.config.PublicOrigin == "" || r.Header.Get("Origin") != h.config.PublicOrigin {
		fail(w, 403, "csrf_rejected", "Valid origin and CSRF token are required", id)
		return
	}
	if ok, retry := h.allowSession(r.RemoteAddr); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		fail(w, 429, "rate_limited", "Too many sandbox sessions from this address; try again later", id)
		return
	}
	token, err1 := auth.RandomToken()
	csrf, err2 := auth.RandomToken()
	suffix, err3 := auth.RandomToken()
	visitor, err4 := storage.NewID()
	sessionID, err5 := storage.NewID()
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
		fail(w, 503, "unavailable", "Sandbox sessions unavailable", id)
		return
	}
	now := time.Now()
	expires := now.Add(auth.VisitorSessionTTL)
	busy := false
	err := h.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		cleanupVisitors(tx, now.UnixMilli())
		var live int64
		if err := tx.Table("sessions AS s").Joins("JOIN users AS u ON u.id = s.user_id").
			Where("u.email LIKE ? AND s.expires_at > ?", "%"+auth.VisitorEmailSuffix, now.UnixMilli()).Count(&live).Error; err != nil {
			return err
		}
		if live >= maxLiveVisitorSessions {
			busy = true
			return nil
		}
		for _, statement := range []struct {
			SQL  string
			Args []any
		}{
			{"INSERT INTO users(id,email,created_at) VALUES(?,?,?)", []any{visitor, "visitor-" + suffix[:16] + auth.VisitorEmailSuffix, now.UnixMilli()}},
			{"INSERT INTO workspace_members(user_id,workspace_id,role,created_at) VALUES(?,?,'operator',?)", []any{visitor, auth.DemoWorkspaceID, now.UnixMilli()}},
			{"INSERT INTO sessions(id,user_id,workspace_id,token_hash,csrf_hash,expires_at,created_at) VALUES(?,?,?,?,?,?,?)", []any{sessionID, visitor, auth.DemoWorkspaceID, auth.Hash(token), auth.Hash(csrf), expires.UnixMilli(), now.UnixMilli()}},
		} {
			if err := tx.Exec(statement.SQL, statement.Args...).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if busy {
		w.Header().Set("Retry-After", "60")
		fail(w, 503, "sandbox_busy", "The sandbox has reached its visitor session limit; try again shortly", id)
		return
	}
	if err != nil {
		fail(w, 503, "unavailable", "Sandbox sessions unavailable", id)
		return
	}
	// Log the visitor id only, never a token.
	h.config.Logger.Info("sandbox visitor session created", "request_id", id, "visitor_id", visitor)
	http.SetCookie(w, auth.SessionCookie(token, expires))
	http.SetCookie(w, auth.CSRFCookie(csrf, expires))
	writeJSON(w, http.StatusCreated, map[string]any{
		"workspace":  map[string]string{"id": auth.DemoWorkspaceID, "name": h.workspaceName(), "environment": "sandbox"},
		"role":       "operator",
		"expires_at": expires.UTC().Format(time.RFC3339),
	})
}

// cleanupVisitors lazily removes one bounded batch of expired visitor
// sessions with their users. Only @sandbox.invalid users are eligible, so the
// seeded demo user and session are never touched. Best effort.
func cleanupVisitors(tx *gorm.DB, now int64) {
	var users []string
	if err := tx.Table("sessions AS s").Select("DISTINCT s.user_id").Joins("JOIN users AS u ON u.id = s.user_id").
		Where("u.email LIKE ? AND s.expires_at <= ?", "%"+auth.VisitorEmailSuffix, now).Limit(cleanupBatch).Scan(&users).Error; err != nil || len(users) == 0 {
		return
	}
	deleteVisitors(tx, users)
}

func deleteVisitors(tx *gorm.DB, users []string) {
	_ = tx.Exec("DELETE FROM sessions WHERE user_id IN ?", users).Error
	_ = tx.Exec("DELETE FROM workspace_members WHERE user_id IN ?", users).Error
	_ = tx.Exec("DELETE FROM users WHERE id IN ? AND email LIKE ?", users, "%"+auth.VisitorEmailSuffix).Error
}

// serveSandboxSession handles DELETE /v1/sandbox/session for an authenticated
// caller (writes already passed the operator and CSRF checks).
func (h *handler) serveSandboxSession(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) bool {
	if r.URL.Path != "/v1/sandbox/session" || r.Method != http.MethodDelete {
		return false
	}
	cookie, err := r.Cookie(auth.SessionCookieName)
	if !principal.Cookie || err != nil {
		fail(w, 403, "forbidden", "Only a visitor session can be signed out", id)
		return true
	}
	var row struct{ UserID string }
	err = h.db.WithContext(r.Context()).Table("sessions AS s").Select("s.user_id").Joins("JOIN users AS u ON u.id = s.user_id").
		Where("s.token_hash = ? AND u.email LIKE ?", auth.Hash(cookie.Value), "%"+auth.VisitorEmailSuffix).Take(&row).Error
	if err != nil {
		// The seeded demo session (or any non-visitor session) is never deletable.
		fail(w, 403, "forbidden", "Only a visitor session can be signed out", id)
		return true
	}
	if err := h.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		deleteVisitors(tx, []string{row.UserID})
		return nil
	}); err != nil {
		fail(w, 503, "unavailable", "Sandbox sessions unavailable", id)
		return true
	}
	h.config.Logger.Info("sandbox visitor session deleted", "request_id", id, "visitor_id", row.UserID)
	for _, c := range auth.ClearedCookies() {
		http.SetCookie(w, c)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
	return true
}
