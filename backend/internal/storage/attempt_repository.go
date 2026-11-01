package storage

import "gorm.io/gorm"

// Attempt repository functions take the caller's handle and carry a
// workspace_id predicate, like payment_repository.go.

// InsertAttempt inserts a new attempt row exactly as given.
func InsertAttempt(tx *gorm.DB, attempt *PaymentAttempt) error {
	return tx.Create(attempt).Error
}

// GetAttempt reads one attempt within the workspace.
func GetAttempt(tx *gorm.DB, workspaceID, attemptID string) (PaymentAttempt, error) {
	var attempt PaymentAttempt
	err := tx.Where("workspace_id = ? AND id = ?", workspaceID, attemptID).Take(&attempt).Error
	return attempt, notFound(err)
}

// AttemptUpdate is a status-predicate guarded change to one attempt.
type AttemptUpdate struct {
	WorkspaceID string
	AttemptID   string
	// From lists the statuses the row must currently have.
	From              []string
	To                string
	ProviderReference *string
	ErrorCode         *string
	SubmittedAt       *int64
	CompletedAt       *int64
	Now               int64
}

// UpdateAttemptStatus applies u only when the attempt's status is in u.From.
// It requires exactly one affected row; otherwise ErrStaleVersion.
func UpdateAttemptStatus(tx *gorm.DB, u AttemptUpdate) error {
	values := map[string]any{"status": u.To, "updated_at": u.Now}
	if u.ProviderReference != nil {
		values["provider_reference"] = *u.ProviderReference
	}
	if u.ErrorCode != nil {
		values["error_code"] = *u.ErrorCode
	}
	if u.SubmittedAt != nil {
		values["submitted_at"] = *u.SubmittedAt
	}
	if u.CompletedAt != nil {
		values["completed_at"] = *u.CompletedAt
	}
	result := tx.Model(&PaymentAttempt{}).
		Where("workspace_id = ? AND id = ? AND status IN ?", u.WorkspaceID, u.AttemptID, u.From).
		Updates(values)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrStaleVersion
	}
	return nil
}

// FindMethodToken reads the token a provider holds for a workspace-owned method.
func FindMethodToken(tx *gorm.DB, workspaceID, methodID, providerID string) (PaymentMethodToken, error) {
	var token PaymentMethodToken
	err := tx.Where("workspace_id = ? AND method_id = ? AND provider_id = ?", workspaceID, methodID, providerID).Take(&token).Error
	return token, notFound(err)
}

// FindProvider reads one provider within the workspace.
func FindProvider(tx *gorm.DB, workspaceID, providerID string) (Provider, error) {
	var provider Provider
	err := tx.Where("workspace_id = ? AND id = ?", workspaceID, providerID).Take(&provider).Error
	return provider, notFound(err)
}
