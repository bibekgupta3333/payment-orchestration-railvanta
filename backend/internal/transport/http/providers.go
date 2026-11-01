package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/provider"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// ProviderCapabilitiesView reports what a configured adapter declares.
type ProviderCapabilitiesView struct {
	IdempotentReplay       bool     `json:"idempotent_replay"`
	LookupByIdempotencyKey bool     `json:"lookup_by_idempotency_key"`
	Rails                  []string `json:"rails"`
}

// ProviderView is one provider as configured in this deployment.
type ProviderView struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	DisplayName string `json:"display_name"`
	// Mode is "simulated" for the mock adapter and "not_configured" for a
	// provider with no adapter in this build.
	Mode         string                    `json:"mode"`
	Enabled      bool                      `json:"enabled"`
	Configured   bool                      `json:"configured"`
	Capabilities *ProviderCapabilitiesView `json:"capabilities"`
	RuleCount    int                       `json:"rule_count"`
}

// ProviderWindowView is health inside one window.
type ProviderWindowView struct {
	Window             string           `json:"window"`
	Observations       int64            `json:"observations"`
	Succeeded          int64            `json:"succeeded"`
	Failed             int64            `json:"failed"`
	Unknown            int64            `json:"unknown"`
	SuccessRatePercent *string          `json:"success_rate_percent"`
	P95Millis          *int64           `json:"p95_ms"`
	LatencySamples     int64            `json:"latency_samples"`
	ErrorCounts        map[string]int64 `json:"error_counts"`
}

// ProviderHealthView is computed from attempt submitted_at/completed_at. With
// no attempts it reports no_observations, never zero rates.
type ProviderHealthView struct {
	ID               string               `json:"id"`
	Code             string               `json:"code"`
	Configured       bool                 `json:"configured"`
	DataSource       string               `json:"data_source"`
	ObservationCount int64                `json:"observation_count"`
	Status           string               `json:"status"`
	Windows          []ProviderWindowView `json:"windows"`
}

var providerNames = map[string]string{"mock": "Mock provider", "stripe": "Stripe", "plaid": "Plaid"}

// knownProviderCodes lists every code the schema allows, in display order.
var knownProviderCodes = []string{"mock", "stripe", "plaid"}

// serveProviders routes GET /v1/providers*. It reports false for other paths.
func (h *handler) serveProviders(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) bool {
	if r.Method != http.MethodGet || (r.URL.Path != "/v1/providers" && !strings.HasPrefix(r.URL.Path, "/v1/providers/")) {
		return false
	}
	switch rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/v1/providers"), "/"); {
	case rest == "":
		views, err := h.providerViews(r, principal.WorkspaceID)
		if err != nil {
			fail(w, 503, "unavailable", "Providers unavailable", id)
			return true
		}
		writeJSON(w, 200, map[string]any{"items": views, "next_cursor": nil})
	case rest == "health":
		health, err := h.providerHealth(r, principal.WorkspaceID)
		if err != nil {
			fail(w, 503, "unavailable", "Provider health unavailable", id)
			return true
		}
		writeJSON(w, 200, map[string]any{"items": health, "next_cursor": nil})
	case !strings.Contains(rest, "/") && safeID.MatchString(rest):
		h.getProvider(w, r, principal, id, rest)
	default:
		return false
	}
	return true
}

// providerViews lists stored providers plus every known code with no row, which
// is shown as not configured under its code as the identifier.
func (h *handler) providerViews(r *http.Request, workspaceID string) ([]ProviderView, error) {
	db := h.db.WithContext(r.Context())
	var rows []storage.Provider
	if err := db.Where("workspace_id = ?", workspaceID).Order("code").Find(&rows).Error; err != nil {
		return nil, err
	}
	rules, _, err := h.loadRules(r, workspaceID)
	if err != nil {
		return nil, err
	}
	ruleCount := map[string]int{}
	for _, rule := range rules {
		ruleCount[rule.ProviderID]++
	}
	byCode := map[string]ProviderView{}
	for _, row := range rows {
		view := h.describeProvider(row.ID, row.Code, row.Enabled == 1)
		view.RuleCount = ruleCount[row.ID]
		byCode[row.Code] = view
	}
	views := make([]ProviderView, 0, len(knownProviderCodes))
	for _, code := range knownProviderCodes {
		if view, ok := byCode[code]; ok {
			views = append(views, view)
		} else {
			views = append(views, h.describeProvider(code, code, false))
		}
	}
	return views, nil
}

func (h *handler) describeProvider(id, code string, enabled bool) ProviderView {
	view := ProviderView{ID: id, Code: code, DisplayName: providerNames[code], Mode: "not_configured", Enabled: enabled}
	if adapter, ok := h.config.Router.Providers[code]; ok {
		caps := adapter.Capabilities()
		view.Configured = true
		if code == "mock" {
			view.Mode = "simulated"
		} else {
			view.Mode = "configured"
		}
		rails := caps.Rails
		if rails == nil {
			rails = []string{}
		}
		view.Capabilities = &ProviderCapabilitiesView{IdempotentReplay: caps.IdempotentReplay, LookupByIdempotencyKey: caps.LookupByIdempotencyKey, Rails: rails}
	}
	return view
}

func (h *handler) getProvider(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, key string) {
	views, err := h.providerViews(r, principal.WorkspaceID)
	if err != nil {
		fail(w, 503, "unavailable", "Provider unavailable", id)
		return
	}
	for _, view := range views {
		if view.ID != key && view.Code != key {
			continue
		}
		health, err := h.providerHealth(r, principal.WorkspaceID)
		if err != nil {
			fail(w, 503, "unavailable", "Provider unavailable", id)
			return
		}
		var summary ProviderHealthView
		for _, item := range health {
			if item.Code == view.Code {
				summary = item
			}
		}
		rules, codes, err := h.loadRules(r, principal.WorkspaceID)
		if err != nil {
			fail(w, 503, "unavailable", "Provider unavailable", id)
			return
		}
		ruleViews := []RoutingRuleView{}
		for _, rule := range rules {
			if rule.ProviderID == view.ID {
				ruleViews = append(ruleViews, newRuleView(rule, codes[rule.ProviderID]))
			}
		}
		writeJSON(w, 200, map[string]any{"provider": view, "health": summary, "rules": ruleViews})
		return
	}
	fail(w, 404, "not_found", "Provider not found", id)
}

var healthWindows = []struct {
	name   string
	length time.Duration
}{{"1h", time.Hour}, {"24h", 24 * time.Hour}, {"7d", 7 * 24 * time.Hour}}

// providerHealth computes per-provider health from stored attempts.
func (h *handler) providerHealth(r *http.Request, workspaceID string) ([]ProviderHealthView, error) {
	views, err := h.providerViews(r, workspaceID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var rows []struct {
		ProviderID  string
		Status      string
		ErrorCode   *string
		SubmittedAt int64
		CompletedAt *int64
	}
	err = h.db.WithContext(r.Context()).Table("payment_attempts").
		Select("provider_id, status, error_code, submitted_at, completed_at").
		Where("workspace_id = ? AND submitted_at IS NOT NULL AND submitted_at >= ?", workspaceID, now.Add(-7*24*time.Hour).UnixMilli()).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	byProvider := map[string][]provider.Observation{}
	for _, row := range rows {
		code := ""
		if row.ErrorCode != nil {
			code = *row.ErrorCode
		}
		byProvider[row.ProviderID] = append(byProvider[row.ProviderID], provider.Observation{
			SubmittedAt: row.SubmittedAt, CompletedAt: row.CompletedAt, Status: row.Status, ErrorCode: code,
		})
	}
	out := make([]ProviderHealthView, 0, len(views))
	for _, view := range views {
		observations := byProvider[view.ID]
		item := ProviderHealthView{ID: view.ID, Code: view.Code, Configured: view.Configured, DataSource: "observed", Windows: []ProviderWindowView{}}
		if view.Code == "mock" {
			item.DataSource = "simulated"
		}
		var day provider.WindowHealth
		for _, window := range healthWindows {
			summary := provider.Summarize(window.name, window.length, now, observations)
			if window.name == "24h" {
				day = summary
			}
			if window.name == "7d" {
				item.ObservationCount = summary.Observations
			}
			item.Windows = append(item.Windows, windowView(summary))
		}
		item.Status = provider.Verdict(day)
		if item.Status == provider.HealthNoObservations && item.ObservationCount > 0 {
			item.Status = provider.HealthInsufficient
		}
		out = append(out, item)
	}
	return out, nil
}

func windowView(w provider.WindowHealth) ProviderWindowView {
	view := ProviderWindowView{
		Window: w.Window, Observations: w.Observations, Succeeded: w.Succeeded, Failed: w.Failed, Unknown: w.Unknown,
		P95Millis: w.P95Millis, LatencySamples: w.LatencyCount, ErrorCounts: w.ErrorCounts,
	}
	if w.Observations > 0 {
		view.SuccessRatePercent = new(string)
		*view.SuccessRatePercent = percentOneDecimal(w.Succeeded, w.Observations)
	}
	return view
}
