package httpapi

import (
	"net/http"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// GET /v1/settings (WBS 4.4.8). Read-only workspace settings for the
// authenticated principal. auth.Authenticate has already bound the request to a
// live workspace membership (a session must still join workspace_members; an API
// key must be unrevoked and unexpired), and every query below is scoped to that
// workspace. The response never contains API keys, key hashes, session or CSRF
// material, tokens or provider credentials: provider configuration is reduced
// to a code, a label and a status.

type providerSetting struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Status string `json:"status"`
}

func (h *handler) serveSettings(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) bool {
	if r.Method != http.MethodGet || r.URL.Path != "/v1/settings" {
		return false
	}
	db := h.db.WithContext(r.Context())
	var workspace struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Environment string `json:"environment"`
	}
	if err := db.Table("workspaces").Where("id = ?", principal.WorkspaceID).Take(&workspace).Error; err != nil {
		fail(w, 404, "not_found", "Workspace not found", id)
		return true
	}
	var providers []storage.Provider
	if err := db.Where("workspace_id = ?", principal.WorkspaceID).Find(&providers).Error; err != nil {
		fail(w, 503, "unavailable", "Settings unavailable", id)
		return true
	}
	known := []providerSetting{{Code: "mock", Label: "Mock provider", Status: "not_configured"}, {Code: "stripe", Label: "Stripe", Status: "not_configured"}, {Code: "plaid", Label: "Plaid", Status: "not_configured"}}
	for _, configured := range providers {
		for i := range known {
			if known[i].Code == configured.Code {
				known[i].Status = "disabled"
				if configured.Enabled == 1 {
					known[i].Status = "enabled"
				}
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"workspace": workspace,
		"role":      principal.Role,
		"providers": known,
		// Fixed capabilities of this build. They are facts, not settings. API key
		// management is the sandbox generator in apikeys.go; roles and live
		// activation remain unavailable.
		"capabilities": map[string]string{"api_key_management": "available", "role_management": "unavailable", "live_activation": "unavailable"},
	})
	return true
}
