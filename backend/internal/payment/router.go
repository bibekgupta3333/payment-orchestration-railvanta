package payment

import (
	"context"

	"github.com/railvanta/railvanta/backend/internal/provider"
	"github.com/railvanta/railvanta/backend/internal/routing"
	"gorm.io/gorm"
)

// ProviderSet maps providers.code to the adapter that executes for it.
type ProviderSet map[string]provider.Provider

// Router is the single routing entry point shared by POST /v1/routing/evaluate
// and the dispatcher, so both report the same provider, rule and reason for the
// same input. It only reads.
type Router struct {
	// Providers, when non-nil, excludes providers whose code has no adapter:
	// the dispatcher could not execute there. Nil excludes nothing.
	Providers ProviderSet
}

// RouteInput is one routing question; amounts are integer minor units.
type RouteInput struct {
	WorkspaceID     string
	PaymentMethodID string
	Currency        string
	AmountMinor     int64
	// Excluded maps provider IDs the caller must not use to a human reason.
	Excluded map[string]string
}

// Decide loads the workspace-scoped routing snapshot through db (pass a
// transaction handle to read consistently with its writes) and returns the
// decision. Errors: routing.ErrMethodNotFound, routing.ErrNoEligibleProvider.
func (r Router) Decide(ctx context.Context, db *gorm.DB, in RouteInput) (routing.Decision, error) {
	snapshot, rail, err := routing.Load(ctx, db, in.WorkspaceID, in.PaymentMethodID)
	if err != nil {
		return routing.Decision{}, err
	}
	excluded := make(map[string]string, len(in.Excluded))
	for id, why := range in.Excluded {
		excluded[id] = why
	}
	if r.Providers != nil {
		for _, p := range snapshot.Providers {
			if _, ok := r.Providers[p.Code]; !ok {
				if _, already := excluded[p.ID]; !already {
					excluded[p.ID] = "no adapter is configured for provider code " + p.Code
				}
			}
		}
	}
	return routing.Decide(routing.Request{
		WorkspaceID: in.WorkspaceID, MethodID: in.PaymentMethodID, Rail: rail, Currency: in.Currency,
		AmountMinor: in.AmountMinor, Excluded: excluded,
	}, snapshot)
}
