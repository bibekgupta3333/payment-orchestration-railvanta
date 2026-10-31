// Package storage owns the SQLite connection and explicit schema migrations.
package storage

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store is the API's single database owner. Never open this file from Next.js.
type Store struct{ DB *gorm.DB }

// Open configures every pooled connection, verifies settings and migrates before use.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil, fmt.Errorf("SQLITE_PATH must be a filesystem path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("create database file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	source := (&url.URL{Scheme: "file", Path: absolute}).String() + "?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000&_synchronous=FULL&_txlock=immediate"
	db, err := gorm.Open(sqlite.Open(source), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	handle, err := db.DB()
	if err != nil {
		return nil, err
	}
	handle.SetMaxOpenConns(1)
	handle.SetMaxIdleConns(1)
	fail := func(err error) (*Store, error) { _ = handle.Close(); return nil, err }
	if err := handle.PingContext(ctx); err != nil {
		return fail(err)
	}
	if err := VerifySettings(ctx, db); err != nil {
		return fail(err)
	}
	plan, err := EmbeddedMigrations()
	if err != nil {
		return fail(err)
	}
	if err := Migrate(ctx, db, plan); err != nil {
		return fail(err)
	}
	return &Store{DB: db}, nil
}

// VerifySettings rejects a connection whose required safety settings did not apply.
func VerifySettings(ctx context.Context, db *gorm.DB) error {
	settings := []struct{ name, want string }{
		{"journal_mode", "wal"}, {"foreign_keys", "1"}, {"busy_timeout", "5000"}, {"synchronous", "2"},
	}
	for _, setting := range settings {
		var got string
		if err := db.WithContext(ctx).Raw("PRAGMA " + setting.name).Scan(&got).Error; err != nil {
			return fmt.Errorf("read SQLite %s: %w", setting.name, err)
		}
		if strings.ToLower(got) != setting.want {
			return fmt.Errorf("SQLite %s is %q, require %q", setting.name, got, setting.want)
		}
	}
	return nil
}

// Close releases the database after HTTP and dispatch have drained.
func (s *Store) Close() error {
	db, err := s.DB.DB()
	if err != nil {
		return err
	}
	return db.Close()
}

// WithTx supplies the same transaction handle to all participating repositories.
func (s *Store) WithTx(ctx context.Context, fn func(*gorm.DB) error) error {
	return s.DB.WithContext(ctx).Transaction(fn)
}

// NewID creates UUIDv7 identifiers without substituting fixed business IDs.
func NewID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
