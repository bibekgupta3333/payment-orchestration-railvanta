package auth

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

const (
	DemoWorkspaceID  = "00000000-0000-7000-8000-000000000001"
	OtherWorkspaceID = "00000000-0000-7000-8000-000000000002"
	DemoMethodID     = "00000000-0000-7000-8000-000000000101"
	OtherMethodID    = "00000000-0000-7000-8000-000000000102"
)

// DemoCredentials must be supplied explicitly by a loopback-only bootstrap.
// Empty session fields skip session creation; API keys are required.
type DemoCredentials struct{ OperatorKey, ReaderKey, OtherWorkspaceKey, SessionToken, CSRFToken string }

// SeedDemo persists deterministic public fixture IDs and hashed credentials.
// Existing keys are never reset, so revocation/expiry survive an API restart.
func SeedDemo(ctx context.Context, db *gorm.DB, credentials DemoCredentials) error {
	seenPrefixes := make(map[string]bool)
	for _, key := range []string{credentials.OperatorKey, credentials.ReaderKey, credentials.OtherWorkspaceKey} {
		prefix, ok := APIKeyPrefix(key)
		if !ok {
			return errors.New("demo bootstrap requires three prefix.secret API keys with high-entropy secrets")
		}
		if seenPrefixes[prefix] {
			return errors.New("demo API key prefixes must be distinct across roles and workspaces")
		}
		seenPrefixes[prefix] = true
	}
	if (credentials.SessionToken == "") != (credentials.CSRFToken == "") || credentials.SessionToken != "" && (len(credentials.SessionToken) < 24 || len(credentials.CSRFToken) < 24) {
		return errors.New("demo session and CSRF tokens must both be high entropy")
	}
	now := time.Now().UnixMilli()
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, workspace := range []string{DemoWorkspaceID, OtherWorkspaceID} {
			name, method := "Demo workspace", DemoMethodID
			if i == 1 {
				name, method = "Other workspace", OtherMethodID
			}
			provider := "00000000-0000-7000-8000-00000000020" + string(rune('1'+i))
			statements := []struct {
				SQL  string
				Args []any
			}{
				{"INSERT INTO workspaces(id,name,environment,created_at) VALUES(?,?,'sandbox',?) ON CONFLICT(id) DO NOTHING", []any{workspace, name, now}},
				{"INSERT INTO providers(id,workspace_id,code,enabled,created_at) VALUES(?,?,'mock',1,?) ON CONFLICT(id) DO NOTHING", []any{provider, workspace, now}},
				{"INSERT INTO payment_methods(id,workspace_id,rail,brand,last_four,created_at) VALUES(?,?,'mock','Mock','4242',?) ON CONFLICT(id) DO NOTHING", []any{method, workspace, now}},
				{"INSERT INTO payment_method_tokens(id,workspace_id,method_id,provider_id,token_reference,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", []any{"00000000-0000-7000-8000-00000000030" + string(rune('1'+i)), workspace, method, provider, "mock_reference_" + method, now}},
			}
			for _, statement := range statements {
				if err := tx.Exec(statement.SQL, statement.Args...).Error; err != nil {
					return err
				}
			}
		}
		for i, key := range []string{credentials.OperatorKey, credentials.ReaderKey, credentials.OtherWorkspaceKey} {
			prefix, _ := APIKeyPrefix(key)
			var existing struct{ KeyHash string }
			lookup := tx.Table("api_keys").Where("prefix = ?", prefix).Take(&existing)
			if lookup.Error != nil && !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
				return lookup.Error
			}
			if lookup.Error == nil && !equalHash(key, existing.KeyHash) {
				return errors.New("demo credentials changed: reuse the original credentials or use a fresh sandbox database")
			}
			workspace, role := DemoWorkspaceID, "operator"
			if i == 1 {
				role = "read"
			}
			if i == 2 {
				workspace = OtherWorkspaceID
			}
			if err := tx.Exec("INSERT INTO api_keys(id,workspace_id,name,prefix,key_hash,role,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(prefix) DO NOTHING", "00000000-0000-7000-8000-00000000040"+string(rune('1'+i)), workspace, "Demo fixture", prefix, Hash(key), role, now).Error; err != nil {
				return err
			}
		}
		if credentials.SessionToken != "" {
			var existing struct{ TokenHash, CSRFHash string }
			lookup := tx.Table("sessions").Where("id = ?", "00000000-0000-7000-8000-000000000601").Take(&existing)
			if lookup.Error != nil && !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
				return lookup.Error
			}
			if lookup.Error == nil && (!equalHash(credentials.SessionToken, existing.TokenHash) || !equalHash(credentials.CSRFToken, existing.CSRFHash)) {
				return errors.New("demo session credentials changed: reuse the original credentials or use a fresh sandbox database")
			}
			user := "00000000-0000-7000-8000-000000000501"
			if err := tx.Exec("INSERT INTO users(id,email,created_at) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING", user, "demo@example.invalid", now).Error; err != nil {
				return err
			}
			if err := tx.Exec("INSERT INTO workspace_members(user_id,workspace_id,role,created_at) VALUES(?,?,'operator',?) ON CONFLICT(user_id,workspace_id) DO NOTHING", user, DemoWorkspaceID, now).Error; err != nil {
				return err
			}
			if err := tx.Exec("INSERT INTO sessions(id,user_id,workspace_id,token_hash,csrf_hash,expires_at,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", "00000000-0000-7000-8000-000000000601", user, DemoWorkspaceID, Hash(credentials.SessionToken), Hash(credentials.CSRFToken), time.Now().Add(24*time.Hour).UnixMilli(), now).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
