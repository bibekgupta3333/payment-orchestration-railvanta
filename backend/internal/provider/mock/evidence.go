package mock

import "gorm.io/gorm"

// ChargesByKeys is a read-only evidence query for reconciliation: it returns the
// mock charges recorded for the given idempotency keys at one provider, keyed by
// idempotency key. It never writes and never calls the provider.
func ChargesByKeys(db *gorm.DB, workspaceID, providerID string, keys []string) (map[string]Charge, error) {
	out := make(map[string]Charge, len(keys))
	const chunk = 500
	for start := 0; start < len(keys); start += chunk {
		end := min(start+chunk, len(keys))
		var rows []Charge
		err := db.Where("workspace_id = ? AND provider_id = ? AND provider_idempotency_key IN ?", workspaceID, providerID, keys[start:end]).Find(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			out[row.ProviderIdempotencyKey] = row
		}
	}
	return out, nil
}
