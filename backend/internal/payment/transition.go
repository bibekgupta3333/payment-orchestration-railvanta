package payment

import (
	"errors"

	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// Transition describes one guarded status change of an existing payment.
type Transition struct {
	WorkspaceID    string
	PaymentID      string
	From           Status
	To             Status
	Version        int64 // the version the caller read; becomes Version+1
	Reason         string
	ActorReference string
	Now            int64 // UTC Unix milliseconds
}

// ApplyTransition validates the graph, performs a compare-and-swap on
// (status, version) and appends the audit row, all on the caller's tx. It
// returns storage.ErrStaleVersion when another writer changed the payment
// first, and ErrIllegalTransition for a change outside the graph. Use it for
// every post-acceptance change (dispatch claim, outcomes, cancellation).
func ApplyTransition(tx *gorm.DB, t Transition) error {
	if t.From == "" {
		return errors.Join(ErrIllegalTransition, errors.New("existing payments require a from status"))
	}
	if err := ValidateTransition(t.From, t.To); err != nil {
		return err
	}
	if t.Reason == "" || t.ActorReference == "" {
		return errors.New("transition reason and actor are required")
	}
	if err := storage.UpdatePaymentStatus(tx, t.WorkspaceID, t.PaymentID, string(t.From), string(t.To), t.Version, t.Now); err != nil {
		return err
	}
	id, err := storage.NewID()
	if err != nil {
		return err
	}
	from := string(t.From)
	return storage.InsertStateTransition(tx, &storage.PaymentStateTransition{
		ID: id, WorkspaceID: t.WorkspaceID, PaymentID: t.PaymentID, FromStatus: &from, ToStatus: string(t.To),
		Reason: t.Reason, ActorReference: t.ActorReference, CreatedAt: t.Now,
	})
}
