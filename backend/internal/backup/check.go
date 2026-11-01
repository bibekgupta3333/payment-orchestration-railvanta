package backup

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/railvanta/railvanta/backend/internal/ledger"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// CheckResult is the outcome of the database and domain check.
type CheckResult struct {
	Problems []string
	Payments int64
	Journals int64
	Attempts int64
}

// OK reports whether no problem was found.
func (r CheckResult) OK() bool { return len(r.Problems) == 0 }

// Check opens the database read-only and verifies, inside one read
// transaction (so it is consistent under write load): SQLite integrity and
// foreign keys, every journal balanced and consistent with its payment
// (ledger.Verify), no attempt without a payment, no unresolved attempt on a
// terminal payment, and no ledger entry or journal without its parent.
func Check(ctx context.Context, path string) (CheckResult, error) {
	var result CheckResult
	absolute, err := filepath.Abs(path)
	if err != nil {
		return result, err
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute}).String() + "?mode=ro&_busy_timeout=10000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return result, fmt.Errorf("open database read-only: %w", err)
	}
	handle, err := db.DB()
	if err != nil {
		return result, err
	}
	defer handle.Close()
	handle.SetMaxOpenConns(1)
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		problems, err := integrityProblemsTx(tx)
		if err != nil {
			return err
		}
		result.Problems = append(result.Problems, problems...)
		violations, err := ledger.Verify(tx)
		if err != nil {
			return fmt.Errorf("ledger verify: %w", err)
		}
		for _, v := range violations {
			result.Problems = append(result.Problems, "ledger: "+v.String())
		}
		queries := []struct{ name, sql string }{
			{"attempt without payment", "SELECT a.id FROM payment_attempts a LEFT JOIN payments p ON p.id = a.payment_id WHERE p.id IS NULL"},
			{"unresolved attempt on terminal payment", "SELECT a.id FROM payment_attempts a JOIN payments p ON p.id = a.payment_id WHERE a.status IN ('created','submitted','unknown') AND p.status IN ('succeeded','failed','canceled')"},
			{"ledger entry without journal", "SELECT e.id FROM ledger_entries e LEFT JOIN ledger_journals j ON j.id = e.journal_id WHERE j.id IS NULL"},
			{"journal without payment", "SELECT j.id FROM ledger_journals j LEFT JOIN payments p ON p.id = j.payment_id WHERE j.payment_id IS NOT NULL AND p.id IS NULL"},
			{"succeeded payment without a succeeded attempt", "SELECT p.id FROM payments p WHERE p.status = 'succeeded' AND NOT EXISTS (SELECT 1 FROM payment_attempts a WHERE a.payment_id = p.id AND a.status = 'succeeded')"},
		}
		for _, q := range queries {
			var ids []string
			if err := tx.Raw(q.sql).Scan(&ids).Error; err != nil {
				return fmt.Errorf("%s: %w", q.name, err)
			}
			for _, id := range ids {
				result.Problems = append(result.Problems, q.name+": "+id)
			}
		}
		counts := []struct {
			table string
			dst   *int64
		}{{"payments", &result.Payments}, {"ledger_journals", &result.Journals}, {"payment_attempts", &result.Attempts}}
		for _, c := range counts {
			if err := tx.Raw("SELECT count(*) FROM " + c.table).Scan(c.dst).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

func integrityProblemsTx(tx *gorm.DB) ([]string, error) {
	var problems []string
	var lines []string
	if err := tx.Raw("PRAGMA integrity_check").Scan(&lines).Error; err != nil {
		return nil, err
	}
	for _, l := range lines {
		if l != "ok" {
			problems = append(problems, "integrity_check: "+l)
		}
	}
	rows, err := tx.Raw("PRAGMA foreign_key_check").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var table, parent string
		var rowid, idx *int64
		if err := rows.Scan(&table, &rowid, &parent, &idx); err != nil {
			return nil, err
		}
		problems = append(problems, fmt.Sprintf("foreign_key_check: %s references missing %s", table, parent))
	}
	return problems, rows.Err()
}
