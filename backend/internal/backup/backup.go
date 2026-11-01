// Package backup implements API-owned SQLite snapshots, restore into a clean
// directory and the domain check run against both. A backup is always taken
// with VACUUM INTO on a read-only connection, which produces a transactionally
// consistent snapshot even while the API is writing; the live .db, -wal and
// -shm files are never copied.
package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3" // registers the "sqlite3" driver also used by GORM
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// ManifestFormat is the sidecar JSON format version.
const ManifestFormat = 1

// DatabaseFile is the file name a restore writes inside the data directory.
const DatabaseFile = "railvanta.db"

// Release identifies the build that took the backup.
type Release struct {
	Version string `json:"version"`
	GitSHA  string `json:"git_sha"`
}

// Manifest is the JSON sidecar written next to every snapshot.
type Manifest struct {
	Format        int     `json:"format"`
	SnapshotAt    string  `json:"snapshot_at"`
	SchemaVersion int     `json:"schema_version"`
	SHA256        string  `json:"sha256"`
	SizeBytes     int64   `json:"size_bytes"`
	File          string  `json:"file"`
	Release       Release `json:"release"`
}

// ManifestPath returns the sidecar path of a snapshot file.
func ManifestPath(snapshot string) string { return snapshot + ".json" }

func readOnlyDSN(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: absolute}).String() + "?mode=ro&_busy_timeout=10000", nil
}

func openReadOnly(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("database %s: %w", path, err)
	}
	dsn, err := readOnlyDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// Create snapshots dbPath into outDir and returns the written manifest and the
// snapshot path. The snapshot is written to a temporary name, verified, fsynced
// and renamed, so a crash never leaves a half-written file under the final name.
func Create(ctx context.Context, dbPath, outDir string, release Release) (Manifest, string, error) {
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return Manifest{}, "", fmt.Errorf("create backup directory: %w", err)
	}
	src, err := openReadOnly(dbPath)
	if err != nil {
		return Manifest{}, "", err
	}
	defer src.Close()
	started := time.Now().UTC()
	name := "railvanta-" + started.Format("20060102T150405.000Z") + ".db"
	final := filepath.Join(outDir, name)
	temp := final + ".partial"
	for _, p := range []string{final, temp} {
		if _, err := os.Stat(p); err == nil {
			return Manifest{}, "", fmt.Errorf("refusing to overwrite %s", p)
		}
	}
	// VACUUM INTO runs inside one read transaction: a consistent snapshot.
	if _, err := src.ExecContext(ctx, "VACUUM INTO "+quote(temp)); err != nil {
		_ = os.Remove(temp)
		return Manifest{}, "", fmt.Errorf("snapshot: %w", err)
	}
	cleanup := func(err error) (Manifest, string, error) {
		_ = os.Remove(temp)
		return Manifest{}, "", err
	}
	if err := os.Chmod(temp, 0600); err != nil {
		return cleanup(err)
	}
	if err := fsyncFile(temp); err != nil {
		return cleanup(err)
	}
	snap, err := openReadOnly(temp)
	if err != nil {
		return cleanup(err)
	}
	version, err := schemaVersion(ctx, snap)
	if err == nil {
		err = integrity(ctx, snap)
	}
	_ = snap.Close()
	if err != nil {
		return cleanup(fmt.Errorf("verify snapshot: %w", err))
	}
	sum, size, err := hashFile(temp)
	if err != nil {
		return cleanup(err)
	}
	if err := os.Rename(temp, final); err != nil {
		return cleanup(err)
	}
	manifest := Manifest{Format: ManifestFormat, SnapshotAt: started.Format(time.RFC3339Nano), SchemaVersion: version, SHA256: sum, SizeBytes: size, File: name, Release: release}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, "", err
	}
	if err := writeFileSync(ManifestPath(final), append(body, '\n')); err != nil {
		return Manifest{}, "", err
	}
	_ = syncDir(outDir)
	return manifest, final, nil
}

// ReadManifest loads and validates the sidecar of snapshot.
func ReadManifest(snapshot string) (Manifest, error) {
	var m Manifest
	body, err := os.ReadFile(ManifestPath(snapshot))
	if err != nil {
		return m, fmt.Errorf("read manifest: %w", err)
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return m, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Format != ManifestFormat || len(m.SHA256) != 64 || m.SchemaVersion < 1 || m.SnapshotAt == "" {
		return m, errors.New("manifest is malformed or has an unsupported format")
	}
	return m, nil
}

// VerifySnapshot checks the sidecar against the snapshot bytes.
func VerifySnapshot(snapshot string) (Manifest, error) {
	m, err := ReadManifest(snapshot)
	if err != nil {
		return m, err
	}
	sum, size, err := hashFile(snapshot)
	if err != nil {
		return m, err
	}
	if sum != m.SHA256 || size != m.SizeBytes {
		return m, fmt.Errorf("checksum mismatch: manifest %s, file %s", m.SHA256, sum)
	}
	return m, nil
}

// RestoreReport describes a completed restore.
type RestoreReport struct {
	Manifest Manifest
	Database string
	Check    CheckResult
}

// Restore verifies snapshot against its manifest, copies it into the empty (or
// absent) destDir as railvanta.db and runs the full database and domain check.
// On any failure the restored file is removed so it cannot be started by
// accident. currentSchema is the newest schema this binary understands; a
// snapshot from a newer schema is refused (start the matching release).
func Restore(ctx context.Context, snapshot, destDir string, currentSchema int) (RestoreReport, error) {
	var report RestoreReport
	m, err := VerifySnapshot(snapshot)
	if err != nil {
		return report, err
	}
	report.Manifest = m
	if m.SchemaVersion > currentSchema {
		return report, fmt.Errorf("snapshot schema %d is newer than this binary (%d): start the matching release", m.SchemaVersion, currentSchema)
	}
	if err := os.MkdirAll(destDir, 0700); err != nil {
		return report, err
	}
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return report, err
	}
	if len(entries) > 0 {
		return report, fmt.Errorf("destination %s is not empty; restore only into a clean directory", destDir)
	}
	target := filepath.Join(destDir, DatabaseFile)
	if err := copyFile(snapshot, target); err != nil {
		_ = os.Remove(target)
		return report, err
	}
	report.Database = target
	result, err := Check(ctx, target)
	report.Check = result
	if err == nil && !result.OK() {
		err = fmt.Errorf("restored database failed checks: %s", strings.Join(result.Problems, "; "))
	}
	if err != nil {
		_ = os.Remove(target)
		return report, err
	}
	return report, nil
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT max(version) FROM schema_migrations").Scan(&v); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return int(v.Int64), nil
}

func integrity(ctx context.Context, db *sql.DB) error {
	var line string
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return err
	}
	var problems []string
	for rows.Next() {
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			return err
		}
		if line != "ok" {
			problems = append(problems, "integrity_check: "+line)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	fk, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer fk.Close()
	for fk.Next() {
		problems = append(problems, "foreign_key_check: violation")
	}
	if err := fk.Err(); err != nil {
		return err
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func quote(path string) string { return "'" + strings.ReplaceAll(path, "'", "''") + "'" }

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func fsyncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func writeFileSync(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// LatestSchema reports the newest schema version embedded in this binary.
func LatestSchema() (int, error) {
	plan, err := storage.EmbeddedMigrations()
	if err != nil {
		return 0, err
	}
	if len(plan) == 0 {
		return 0, errors.New("no embedded migrations")
	}
	return plan[len(plan)-1].Version, nil
}
