package ledger

import (
	"errors"

	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Account codes are unique per (workspace, code, currency). Clearing accounts
// carry the provider ID so each executing provider has its own clearing asset.
const (
	MerchantPayableCode    = "merchant_payable"
	providerClearingPrefix = "provider_clearing:"
	kindAsset              = "asset"
	kindLiability          = "liability"
)

// ProviderClearingCode returns the account code for a provider's clearing account.
func ProviderClearingCode(providerID string) string { return providerClearingPrefix + providerID }

// EnsureProviderClearingAccount returns, creating if absent, the asset account
// provider_clearing:{providerID} for the workspace and currency.
func EnsureProviderClearingAccount(tx *gorm.DB, workspaceID, providerID, currency string) (*storage.LedgerAccount, error) {
	if workspaceID == "" || providerID == "" || !validCurrency(currency) {
		return nil, ErrInvalidJournal
	}
	var providers int64
	if err := tx.Table("providers").Where("id = ? AND workspace_id = ?", providerID, workspaceID).Count(&providers).Error; err != nil {
		return nil, err
	}
	if providers != 1 {
		return nil, ErrUnknownProvider
	}
	return ensureAccount(tx, storage.LedgerAccount{
		WorkspaceID: workspaceID, Code: ProviderClearingCode(providerID), Currency: currency, ProviderID: &providerID, Kind: kindAsset,
	})
}

// EnsureMerchantPayableAccount returns, creating if absent, the liability
// account merchant_payable for the workspace and currency.
func EnsureMerchantPayableAccount(tx *gorm.DB, workspaceID, currency string) (*storage.LedgerAccount, error) {
	if workspaceID == "" || !validCurrency(currency) {
		return nil, ErrInvalidJournal
	}
	return ensureAccount(tx, storage.LedgerAccount{
		WorkspaceID: workspaceID, Code: MerchantPayableCode, Currency: currency, Kind: kindLiability,
	})
}

// ensureAccount inserts the account unless the unique (workspace, code,
// currency) key exists, then loads and validates the stored row.
func ensureAccount(tx *gorm.DB, want storage.LedgerAccount) (*storage.LedgerAccount, error) {
	id, err := storage.NewID()
	if err != nil {
		return nil, err
	}
	want.ID = id
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "code"}, {Name: "currency"}},
		DoNothing: true,
	}).Create(&want).Error; err != nil {
		return nil, err
	}
	var account storage.LedgerAccount
	err = tx.Where("workspace_id = ? AND code = ? AND currency = ?", want.WorkspaceID, want.Code, want.Currency).Take(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	if account.Kind != want.Kind || !sameOptional(account.ProviderID, want.ProviderID) {
		return nil, ErrAccountMismatch
	}
	return &account, nil
}
