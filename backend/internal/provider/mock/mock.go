// Package mock is a deterministic, durable sandbox provider. It owns the
// mock_provider_charges table: no other package reads or writes it. The adapter
// holds its own *gorm.DB and runs only short transactions of its own; it never
// joins a caller's transaction, and every pause happens with no transaction open.
//
// Scenario selection is deterministic and comes only from the persisted payment
// method token reference (payment_method_tokens.token_reference), never from
// randomness or the wall clock:
//
//	token reference prefix              scenario
//	mock_success[_...]                  success
//	mock_decline[_...]                  decline
//	mock_delayed_success[_...]          delayed_success
//	mock_timeout_before_execution[_...] timeout_before_execution
//	mock_timeout_after_success[_...]    timeout_after_success
//	mock_outage[_...]                   outage
//	anything else (e.g. mock_reference_<method>) success
//
// The scenario chosen for a key is persisted on its charge row, so replays and
// lookups use the recorded scenario even if token references change later.
package mock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/railvanta/railvanta/backend/internal/provider"
)

// Scenario is a deterministic mock behaviour.
type Scenario string

const (
	ScenarioSuccess                Scenario = "success"
	ScenarioDecline                Scenario = "decline"
	ScenarioDelayedSuccess         Scenario = "delayed_success"
	ScenarioTimeoutBeforeExecution Scenario = "timeout_before_execution"
	ScenarioTimeoutAfterSuccess    Scenario = "timeout_after_success"
	ScenarioOutage                 Scenario = "outage"
	// ScenarioLatency is only passed to SleepFunc for a profile's fixed latency.
	ScenarioLatency Scenario = "profile_latency"
)

// Scenarios lists every scenario, longest token prefix first.
var Scenarios = []Scenario{
	ScenarioTimeoutBeforeExecution, ScenarioTimeoutAfterSuccess, ScenarioDelayedSuccess,
	ScenarioSuccess, ScenarioDecline, ScenarioOutage,
}

// TokenPrefix is the token-reference prefix that selects s.
func TokenPrefix(s Scenario) string { return "mock_" + string(s) }

// ScenarioForToken maps a persisted token reference to its scenario.
func ScenarioForToken(tokenReference string) Scenario {
	for _, s := range Scenarios {
		p := TokenPrefix(s)
		if tokenReference == p || strings.HasPrefix(tokenReference, p+"_") {
			return s
		}
	}
	return ScenarioSuccess
}

// Charge statuses persisted in mock_provider_charges.status.
const (
	StatusProcessing = "processing"
	StatusSucceeded  = "succeeded"
	StatusDeclined   = "declined"
	StatusUnknown    = "unknown"
)

// Charge is the adapter-local GORM model for mock_provider_charges.
type Charge struct {
	ID                     string `gorm:"column:id;primaryKey"`
	WorkspaceID            string `gorm:"column:workspace_id"`
	ProviderID             string `gorm:"column:provider_id"`
	ProviderIdempotencyKey string `gorm:"column:provider_idempotency_key"`
	ProviderReference      string `gorm:"column:provider_reference"`
	AmountMinor            int64  `gorm:"column:amount_minor"`
	Currency               string `gorm:"column:currency"`
	Status                 string `gorm:"column:status"`
	Scenario               string `gorm:"column:scenario"`
	CreatedAt              int64  `gorm:"column:created_at"`
	UpdatedAt              int64  `gorm:"column:updated_at"`
}

// TableName pins the model to the migration-owned table.
func (Charge) TableName() string { return "mock_provider_charges" }

// SleepFunc pauses a scenario. It must return ctx.Err() if ctx ends first.
// Tests replace it to block a submit on a channel or to skip delays.
type SleepFunc func(ctx context.Context, scenario Scenario, d time.Duration) error

// Options configure an Adapter. Zero values select sensible defaults.
type Options struct {
	// Now returns Unix milliseconds for created_at/updated_at only; it never
	// influences scenario selection or outcomes.
	Now func() int64
	// Sleep pauses delayed_success and the timeout scenarios.
	Sleep SleepFunc
	// DelayedSuccessDelay is the processing time of delayed_success (default 2s).
	DelayedSuccessDelay time.Duration
	// TimeoutDelay is how long timeout scenarios hang before giving up (default 5s);
	// a caller deadline that ends sooner wins.
	TimeoutDelay time.Duration
	// Code is the provider code reported by Capabilities (default "mock").
	Code string
	// Rails overrides the rails reported by Capabilities (default card, ach, mock).
	Rails []string
	// Latency is a short FIXED simulated processing time applied before every
	// scenario except outage (default 0 = none). It is never random and never
	// derived from the wall clock; tests zero it through the Sleep hook.
	Latency time.Duration
}

// Adapter implements provider.Provider against SQLite.
type Adapter struct {
	db          *gorm.DB
	opts        Options
	unavailable atomic.Bool
}

var _ provider.Provider = (*Adapter)(nil)

// New builds an adapter that owns its own database handle.
func New(db *gorm.DB, opts Options) *Adapter {
	if opts.Now == nil {
		opts.Now = func() int64 { return time.Now().UnixMilli() }
	}
	if opts.Sleep == nil {
		opts.Sleep = sleep
	}
	if opts.DelayedSuccessDelay <= 0 {
		opts.DelayedSuccessDelay = 2 * time.Second
	}
	if opts.TimeoutDelay <= 0 {
		opts.TimeoutDelay = 5 * time.Second
	}
	return &Adapter{db: db, opts: opts}
}

func sleep(ctx context.Context, _ Scenario, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// SetAvailable is an explicit operator/test switch simulating a whole-provider
// outage: while unavailable, Submit returns Ambiguous without recording anything
// and Lookup returns provider.ErrUnavailable.
func (a *Adapter) SetAvailable(available bool) { a.unavailable.Store(!available) }

// Latency is the fixed simulated processing latency of this adapter (0 = none).
func (a *Adapter) Latency() time.Duration { return a.opts.Latency }

// Capabilities reports idempotent replay and lookup support.
func (a *Adapter) Capabilities() provider.Capabilities {
	code, rails := a.opts.Code, a.opts.Rails
	if code == "" {
		code = "mock"
	}
	if rails == nil {
		rails = []string{"card", "ach", "mock"}
	}
	return provider.Capabilities{Code: code, IdempotentReplay: true, LookupByIdempotencyKey: true, Rails: append([]string(nil), rails...)}
}

// ProviderReference derives the deterministic reference for a key.
func ProviderReference(providerID, key string) string {
	sum := sha256.Sum256([]byte(providerID + "\x00" + key))
	return "mock_ch_" + hex.EncodeToString(sum[:12])
}

// Submit executes at most once per (provider, idempotency key).
func (a *Adapter) Submit(ctx context.Context, req provider.SubmitRequest) provider.Result {
	if err := req.Validate(); err != nil {
		// Rejected locally before anything was recorded: proven no effect.
		return provider.Result{Outcome: provider.OutcomeDeclined, Code: provider.CodeInvalidRequest, Cause: err}
	}
	if err := ctx.Err(); err != nil {
		return provider.Ambiguous(provider.ContextCode(err), err)
	}
	if a.unavailable.Load() {
		return provider.Ambiguous(provider.CodeProviderUnavailable, provider.ErrUnavailable)
	}
	scenario := ScenarioForToken(req.TokenReference)
	if a.opts.Latency > 0 && scenario != ScenarioOutage {
		if err := a.opts.Sleep(ctx, ScenarioLatency, a.opts.Latency); err != nil {
			// Nothing was recorded yet; the caller resolves it by lookup.
			return provider.Ambiguous(provider.ContextCode(err), err)
		}
	}
	switch scenario {
	case ScenarioOutage:
		// The request never reaches execution; nothing is recorded.
		return provider.Ambiguous(provider.CodeProviderUnavailable, provider.ErrUnavailable)
	case ScenarioTimeoutBeforeExecution:
		// Hang, then give up without executing; a lookup finds no record.
		err := a.opts.Sleep(ctx, scenario, a.opts.TimeoutDelay)
		return provider.Ambiguous(provider.CodeTimeout, errors.Join(errors.New("mock timed out before execution"), err))
	}

	initial := StatusSucceeded
	switch scenario {
	case ScenarioDecline:
		initial = StatusDeclined
	case ScenarioDelayedSuccess:
		initial = StatusProcessing
	}
	charge, created, err := a.record(ctx, req, scenario, initial)
	if err != nil {
		return provider.Ambiguous(storageCode(err), err)
	}
	if !created {
		return a.replay(ctx, req, charge)
	}

	switch scenario {
	case ScenarioDelayedSuccess:
		if err := a.opts.Sleep(ctx, scenario, a.opts.DelayedSuccessDelay); err != nil {
			// The response is lost but the provider keeps processing;
			// a later lookup or replay with the same key settles it.
			return provider.Ambiguous(provider.ContextCode(err), err)
		}
		settled, err := a.settle(ctx, charge)
		if err != nil {
			return provider.Ambiguous(storageCode(err), err)
		}
		return resultFor(settled, false)
	case ScenarioTimeoutAfterSuccess:
		err := a.opts.Sleep(ctx, scenario, a.opts.TimeoutDelay)
		// The charge executed but the response is lost.
		return provider.Ambiguous(provider.CodeTimeout, errors.Join(errors.New("mock timed out after success"), err))
	}
	return resultFor(charge, false)
}

// Lookup returns the recorded outcome for a key; Found is false when the mock
// never recorded the key. A processing delayed_success charge settles here.
func (a *Adapter) Lookup(ctx context.Context, req provider.LookupRequest) (provider.LookupResult, error) {
	if req.WorkspaceID == "" || req.ProviderID == "" || req.IdempotencyKey == "" {
		return provider.LookupResult{}, errors.Join(provider.ErrInvalidRequest, errors.New("workspace, provider and idempotency key are required"))
	}
	if err := ctx.Err(); err != nil {
		return provider.LookupResult{}, err
	}
	if a.unavailable.Load() {
		return provider.LookupResult{}, provider.ErrUnavailable
	}
	var charge Charge
	err := a.db.WithContext(ctx).
		Where("workspace_id = ? AND provider_id = ? AND provider_idempotency_key = ?", req.WorkspaceID, req.ProviderID, req.IdempotencyKey).
		Take(&charge).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return provider.LookupResult{Found: false}, nil
	}
	if err != nil {
		return provider.LookupResult{}, fmt.Errorf("mock lookup: %w", err)
	}
	if charge.Status == StatusProcessing {
		if charge, err = a.settle(ctx, charge); err != nil {
			return provider.LookupResult{}, fmt.Errorf("mock settle: %w", err)
		}
	}
	return provider.LookupResult{Found: true, Result: resultFor(charge, false)}, nil
}

// record inserts the charge in one short transaction or returns the existing one.
func (a *Adapter) record(ctx context.Context, req provider.SubmitRequest, scenario Scenario, status string) (Charge, bool, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Charge{}, false, err
	}
	now := a.opts.Now()
	charge := Charge{
		ID: id.String(), WorkspaceID: req.WorkspaceID, ProviderID: req.ProviderID,
		ProviderIdempotencyKey: req.IdempotencyKey, ProviderReference: ProviderReference(req.ProviderID, req.IdempotencyKey),
		AmountMinor: req.AmountMinor, Currency: req.Currency, Status: status, Scenario: string(scenario),
		CreatedAt: now, UpdatedAt: now,
	}
	var stored Charge
	var created bool
	err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "provider_id"}, {Name: "provider_idempotency_key"}},
			DoNothing: true,
		}).Create(&charge)
		if res.Error != nil {
			return res.Error
		}
		created = res.RowsAffected == 1
		return tx.Where("provider_id = ? AND provider_idempotency_key = ?", req.ProviderID, req.IdempotencyKey).Take(&stored).Error
	})
	if err != nil {
		return Charge{}, false, err
	}
	return stored, created, nil
}

// replay returns the original charge for a repeated key without re-executing.
func (a *Adapter) replay(ctx context.Context, req provider.SubmitRequest, charge Charge) provider.Result {
	if charge.WorkspaceID != req.WorkspaceID || charge.AmountMinor != req.AmountMinor || charge.Currency != req.Currency {
		// The key already names a different charge; its effect on THIS request is unknown.
		res := provider.Ambiguous(provider.CodeIdempotencyConflict, errors.New("idempotency key reused with different parameters"))
		res.ProviderReference = charge.ProviderReference
		return res
	}
	if charge.Status == StatusProcessing {
		settled, err := a.settle(ctx, charge)
		if err != nil {
			res := provider.Ambiguous(storageCode(err), err)
			res.ProviderReference = charge.ProviderReference
			return res
		}
		charge = settled
	}
	return resultFor(charge, true)
}

// settle completes a processing delayed_success charge exactly once.
func (a *Adapter) settle(ctx context.Context, charge Charge) (Charge, error) {
	var settled Charge
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Charge{}).
			Where("id = ? AND status = ?", charge.ID, StatusProcessing).
			Updates(map[string]any{"status": StatusSucceeded, "updated_at": a.opts.Now()}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", charge.ID).Take(&settled).Error
	})
	return settled, err
}

func resultFor(charge Charge, replayed bool) provider.Result {
	res := provider.Result{ProviderReference: charge.ProviderReference, Replayed: replayed}
	switch charge.Status {
	case StatusSucceeded:
		res.Outcome = provider.OutcomeSucceeded
	case StatusDeclined:
		res.Outcome, res.Code = provider.OutcomeDeclined, provider.CodeDeclined
	default:
		res.Outcome, res.Code = provider.OutcomeAmbiguous, provider.CodeTimeout
	}
	return res
}

func storageCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return provider.ContextCode(err)
	}
	return provider.CodeStorageError
}
