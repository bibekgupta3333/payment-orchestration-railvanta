package routing

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// ErrMethodNotFound is returned when the payment method is not in the workspace.
var ErrMethodNotFound = errors.New("payment method not found")

type providerRow struct {
	ID      string `gorm:"column:id"`
	Code    string `gorm:"column:code"`
	Enabled int64  `gorm:"column:enabled"`
}

type ruleRow struct {
	ID             string `gorm:"column:id"`
	ProviderID     string `gorm:"column:provider_id"`
	Rail           string `gorm:"column:rail"`
	Currency       string `gorm:"column:currency"`
	Priority       int64  `gorm:"column:priority"`
	MinAmountMinor int64  `gorm:"column:min_amount_minor"`
	MaxAmountMinor *int64 `gorm:"column:max_amount_minor"`
	Enabled        int64  `gorm:"column:enabled"`
}

// Load reads the routing snapshot for one method and returns the method's rail.
// Every query carries a workspace_id predicate; Load never writes. Pass the
// caller's transaction handle to read a snapshot consistent with its writes.
func Load(ctx context.Context, db *gorm.DB, workspaceID, methodID string) (Snapshot, string, error) {
	db = db.WithContext(ctx)
	var rails []string
	if err := db.Table("payment_methods").Where("workspace_id = ? AND id = ?", workspaceID, methodID).Pluck("rail", &rails).Error; err != nil {
		return Snapshot{}, "", fmt.Errorf("load payment method: %w", err)
	}
	if len(rails) == 0 {
		return Snapshot{}, "", ErrMethodNotFound
	}
	var providers []providerRow
	if err := db.Table("providers").Select("id, code, enabled").Where("workspace_id = ?", workspaceID).Order("id").Scan(&providers).Error; err != nil {
		return Snapshot{}, "", fmt.Errorf("load providers: %w", err)
	}
	var rules []ruleRow
	if err := db.Table("routing_rules").
		Select("id, provider_id, rail, currency, priority, min_amount_minor, max_amount_minor, enabled").
		Where("workspace_id = ?", workspaceID).Order("priority, id").Scan(&rules).Error; err != nil {
		return Snapshot{}, "", fmt.Errorf("load routing rules: %w", err)
	}
	var tokenProviders []string
	if err := db.Table("payment_method_tokens").Where("workspace_id = ? AND method_id = ?", workspaceID, methodID).Order("provider_id").Pluck("provider_id", &tokenProviders).Error; err != nil {
		return Snapshot{}, "", fmt.Errorf("load payment method tokens: %w", err)
	}
	snap := Snapshot{TokenProviderIDs: tokenProviders}
	for _, p := range providers {
		snap.Providers = append(snap.Providers, Provider{ID: p.ID, Code: p.Code, Enabled: p.Enabled == 1})
	}
	for _, r := range rules {
		snap.Rules = append(snap.Rules, Rule{
			ID: r.ID, ProviderID: r.ProviderID, Rail: r.Rail, Currency: r.Currency, Priority: r.Priority,
			MinAmountMinor: r.MinAmountMinor, MaxAmountMinor: r.MaxAmountMinor, Enabled: r.Enabled == 1,
		})
	}
	return snap, rails[0], nil
}
