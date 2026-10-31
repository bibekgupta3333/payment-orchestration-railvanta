package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/railvanta/railvanta/backend/migrations"
	"gorm.io/gorm"
)

const historyDDL = `CREATE TABLE schema_migrations (
 version INTEGER PRIMARY KEY NOT NULL,
 name TEXT NOT NULL,
 checksum TEXT NOT NULL CHECK (length(checksum) = 64),
 applied_at INTEGER NOT NULL
) STRICT;`

// Migration is a reviewed release unit. SQL must not change once applied.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// EmbeddedMigrations loads SQL in numbered order from the built binary.
func EmbeddedMigrations() ([]Migration, error) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var plan []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("invalid migration filename %s", entry.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, err
		}
		data, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			return nil, err
		}
		plan = append(plan, Migration{version, entry.Name(), string(data)})
	}
	sort.Slice(plan, func(i, j int) bool { return plan[i].Version < plan[j].Version })
	return plan, nil
}

// Migrate verifies the complete history and applies pending SQL atomically.
// Deployment must already guarantee exclusive ownership of this database file.
func Migrate(ctx context.Context, db *gorm.DB, plan []Migration) error {
	if len(plan) == 0 {
		return fmt.Errorf("migration plan is empty")
	}
	for i, m := range plan {
		if m.Version != i+1 || strings.TrimSpace(m.SQL) == "" {
			return fmt.Errorf("migration plan must be consecutive from version 1")
		}
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tables []string
		if err := tx.Raw("SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables).Error; err != nil {
			return err
		}
		hasHistory := false
		for _, table := range tables {
			if table == "schema_migrations" {
				hasHistory = true
			}
		}
		if !hasHistory && len(tables) > 0 {
			return fmt.Errorf("non-empty incompatible database: no migration history")
		}
		if !hasHistory {
			if err := tx.Exec(historyDDL).Error; err != nil {
				return err
			}
		}
		var history []SchemaMigration
		if err := tx.Order("version").Find(&history).Error; err != nil {
			return fmt.Errorf("read migration history: %w", err)
		}
		if len(history) > len(plan) {
			return fmt.Errorf("database schema is newer than this binary")
		}
		for i, row := range history {
			expected := plan[i]
			if row.Version != expected.Version || row.Name != expected.Name || row.Checksum != checksum(expected.SQL) {
				return fmt.Errorf("migration checksum/history drift at version %d", row.Version)
			}
		}
		if len(history) == 0 && len(tables) > 1 {
			return fmt.Errorf("non-empty incompatible database: empty migration history")
		}
		for _, m := range plan[len(history):] {
			if err := tx.Exec(m.SQL).Error; err != nil {
				return fmt.Errorf("apply migration %s: %w", m.Name, err)
			}
			record := SchemaMigration{Version: m.Version, Name: m.Name, Checksum: checksum(m.SQL), AppliedAt: time.Now().UnixMilli()}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
		}
		// Detect missing and unexpected tables; a hand-edited schema is not ready.
		expected := map[string]bool{"schema_migrations": true}
		tablePattern := regexp.MustCompile(`(?mi)^CREATE TABLE ([a-z_]+)`)
		for _, m := range plan {
			for _, match := range tablePattern.FindAllStringSubmatch(m.SQL, -1) {
				expected[match[1]] = true
			}
		}
		var actual []string
		if err := tx.Raw("SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&actual).Error; err != nil {
			return err
		}
		if len(actual) != len(expected) {
			return fmt.Errorf("database table set does not match migration plan")
		}
		for _, table := range actual {
			if !expected[table] {
				return fmt.Errorf("unexpected database table %s", table)
			}
		}
		return nil
	})
}
func checksum(sql string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(sql))) }
