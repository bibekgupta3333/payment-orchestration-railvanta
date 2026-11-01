// Package httpapi provides the versioned sandbox HTTP boundary.
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/simulation"
	"gorm.io/gorm"
)

type Config struct {
	Logger            *slog.Logger
	PublicOrigin      string
	RequestsPerMinute int
	// Payments overrides the payment service (tests inject metrics or faults).
	Payments *payment.Service
	// Router answers POST /v1/routing/evaluate. Pass the dispatcher's
	// Executor.Router() so evaluation matches real dispatch.
	Router payment.Router
	// OnAccepted, when set, runs after a new payment is accepted (for example
	// to wake the dispatcher). It must not block.
	OnAccepted func()
}
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}
type ErrorEnvelope struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	RequestID   string       `json:"request_id"`
	FieldErrors []FieldError `json:"field_errors"`
}
type requestWindow struct {
	count int
	until time.Time
}
type handler struct {
	db       *gorm.DB
	config   Config
	payments *payment.Service
	sims     *simulation.Runner
	mu       sync.Mutex
	windows  map[string]requestWindow
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func NewHandler(db *gorm.DB, config Config) http.Handler {
	if config.Logger == nil {
		config.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if config.RequestsPerMinute <= 0 {
		config.RequestsPerMinute = 120
	}
	payments := config.Payments
	if payments == nil {
		payments = payment.NewService(db, payment.Options{})
	}
	sims := simulation.NewRunner(db, payments, simulation.Options{Notify: config.OnAccepted})
	return &handler{db: db, config: config, payments: payments, sims: sims, windows: make(map[string]requestWindow)}
}

func requestID(value string) string {
	if safeID.MatchString(value) {
		return value
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "request_unavailable"
	}
	return hex.EncodeToString(bytes[:])
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, code, message, id string, fields ...FieldError) {
	if fields == nil {
		fields = []FieldError{}
	}
	writeJSON(w, status, ErrorEnvelope{Code: code, Message: message, RequestID: id, FieldErrors: fields})
}

func (h *handler) allow(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if len(h.windows) >= 1024 {
		for key, window := range h.windows {
			if now.After(window.until) {
				delete(h.windows, key)
			}
		}
	}
	window := h.windows[host]
	if now.After(window.until) {
		window = requestWindow{until: now.Add(time.Minute)}
	}
	if window.count >= h.config.RequestsPerMinute || len(h.windows) >= 1024 && window.count == 0 {
		return false
	}
	window.count++
	h.windows[host] = window
	return true
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := requestID(r.Header.Get("X-Request-ID"))
	correlation := requestID(r.Header.Get("X-Correlation-ID"))
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("X-Correlation-ID", correlation)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Log no URL/query, body, cookies, authorization, or idempotency material.
	h.config.Logger.Info("api request", "request_id", id, "correlation_id", correlation, "method", r.Method)
	if !h.allow(r.RemoteAddr) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "rate_limited", "Request limit exceeded", id)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	principal, err := auth.Authenticate(r.Context(), h.db, r)
	if err != nil {
		fail(w, 401, "unauthorized", "Valid credentials are required", id)
		return
	}
	write := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS"
	if write && principal.Role != "operator" {
		fail(w, 403, "forbidden", "Operator permission is required", id)
		return
	}
	if write && !auth.CheckCSRF(r, principal, h.config.PublicOrigin) {
		fail(w, 403, "csrf_rejected", "Valid origin and CSRF token are required", id)
		return
	}
	if r.Method == "DELETE" && r.URL.Path == "/v1/api-keys/current" {
		prefix, ok := auth.APIKeyPrefix(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if !ok || principal.Cookie {
			fail(w, 400, "validation_error", "An API key is required to revoke itself", id)
			return
		}
		result := h.db.WithContext(r.Context()).Table("api_keys").Where("prefix = ? AND workspace_id = ? AND revoked_at IS NULL", prefix, principal.WorkspaceID).Update("revoked_at", time.Now().UnixMilli())
		if result.Error != nil {
			fail(w, 503, "unavailable", "Credential revocation unavailable", id)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == "GET" && (r.URL.Path == "/v1/workspace" || strings.HasPrefix(r.URL.Path, "/v1/workspaces/")) {
		if r.URL.Path != "/v1/workspace" && strings.TrimPrefix(r.URL.Path, "/v1/workspaces/") != principal.WorkspaceID {
			fail(w, 404, "not_found", "Workspace not found", id)
			return
		}
		var workspace struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Environment string `json:"environment"`
		}
		if err := h.db.WithContext(r.Context()).Table("workspaces").Where("id = ?", principal.WorkspaceID).Take(&workspace).Error; err != nil {
			fail(w, 404, "not_found", "Workspace not found", id)
			return
		}
		writeJSON(w, 200, workspace)
		return
	}
	if r.Method == "GET" && (r.URL.Path == "/v1/payment-methods" || strings.HasPrefix(r.URL.Path, "/v1/payment-methods/")) {
		type method struct {
			ID       string `json:"id"`
			Rail     string `json:"rail"`
			Brand    string `json:"brand"`
			LastFour string `json:"last_four"`
		}
		query := h.db.WithContext(r.Context()).Table("payment_methods").Select("id, rail, brand, last_four").Where("workspace_id = ?", principal.WorkspaceID)
		if r.URL.Path != "/v1/payment-methods" {
			var item method
			if err := query.Where("id = ?", strings.TrimPrefix(r.URL.Path, "/v1/payment-methods/")).Take(&item).Error; err != nil {
				fail(w, 404, "not_found", "Payment method not found", id)
				return
			}
			writeJSON(w, 200, item)
			return
		}
		items := []method{}
		if err := query.Order("id").Limit(100).Find(&items).Error; err != nil {
			fail(w, 503, "unavailable", "Payment methods unavailable", id)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
		return
	}
	if h.serveReadModels(w, r, principal, id) || h.serveRoutingAndSimulation(w, r, principal, id) {
		return
	}
	if h.serveEvents(w, r, principal, id) || h.serveWebhooks(w, r, principal, id) || h.serveSettings(w, r, principal, id) {
		return
	}
	if r.URL.Path == "/v1/payments" || strings.HasPrefix(r.URL.Path, "/v1/payments/") {
		if h.servePayments(w, r, principal, id) {
			return
		}
	}
	if r.Method == "POST" && r.URL.Path == "/v1/routing/evaluate" {
		h.evaluateRouting(w, r, principal, id)
		return
	}
	if h.serveLedger(w, r, principal, id) || h.serveReconciliation(w, r, principal, id) {
		return
	}
	if r.Method == "GET" && r.URL.Path == "/v1/session" {
		writeJSON(w, 200, map[string]string{"workspace_id": principal.WorkspaceID, "role": principal.Role})
		return
	}
	if plannedRoute(r.Method, r.URL.Path) {
		fail(w, 501, "not_implemented", "This capability is unavailable until its work package is implemented", id)
		return
	}
	fail(w, 404, "not_found", "Route not found", id)
}

func plannedRoute(method, path string) bool {
	if method == "GET" {
		for _, prefix := range []string{"/v1/dashboard/", "/v1/ledger/", "/v1/reconciliation/", "/v1/events"} {
			if path == prefix || strings.HasPrefix(path, prefix+"/") || strings.HasSuffix(prefix, "/") && strings.HasPrefix(path, prefix) {
				return true
			}
		}
	}
	return false
}
