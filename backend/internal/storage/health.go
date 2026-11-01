package storage

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// ErrDegraded reports that the database cannot durably accept writes
// (disk full, I/O error, read-only file, corruption). Readiness maps it to
// 503; a merely busy database is not degraded.
var ErrDegraded = errors.New("database cannot accept writes")

// CheckWritable commits one tiny no-op write (rewriting an existing migration
// history row with its own value) so readiness reflects whether a durable write
// is possible, not only whether the connection answers. Lock contention is
// transient and does not count as degraded.
func CheckWritable(ctx context.Context, db *gorm.DB) error {
	err := db.WithContext(ctx).Exec("UPDATE schema_migrations SET applied_at = applied_at WHERE version = 1").Error
	if err == nil || IsTransient(err) {
		return nil
	}
	if IsWriteFailure(err) {
		return errors.Join(ErrDegraded, err)
	}
	return err
}

// IsTransient reports SQLITE_BUSY/LOCKED or a canceled probe.
func IsTransient(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") || strings.Contains(text, "database table is locked")
}

// IsWriteFailure reports SQLITE_FULL, SQLITE_IOERR, SQLITE_READONLY or SQLITE_CORRUPT.
func IsWriteFailure(err error) bool {
	text := strings.ToLower(err.Error())
	for _, marker := range []string{"database or disk is full", "disk i/o error", "readonly database", "disk image is malformed"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
