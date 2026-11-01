package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/railvanta/railvanta/backend/internal/storage"
)

func TestSubcommandsBackupRestoreCheck(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "railvanta.db")
	store, err := storage.Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		handled, code := runSubcommand(args, &out, &errOut)
		if !handled {
			t.Fatalf("%v not handled", args)
		}
		return code, out.String(), errOut.String()
	}
	if code, out, errOut := run("check", "--db", db); code != 0 || !strings.Contains(out, "checks=ok") {
		t.Fatalf("check: %d %s %s", code, out, errOut)
	}
	backups := filepath.Join(dir, "backups")
	code, out, errOut := run("backup", "--db", db, "--out-dir", backups)
	if code != 0 || !strings.Contains(out, "sha256=") {
		t.Fatalf("backup: %d %s %s", code, out, errOut)
	}
	files, _ := filepath.Glob(filepath.Join(backups, "*.db"))
	if len(files) != 1 {
		t.Fatalf("expected one snapshot, got %v", files)
	}
	dest := filepath.Join(dir, "restored")
	if code, out, errOut := run("restore", "--backup", files[0], "--dest-dir", dest); code != 0 || !strings.Contains(out, "checks=ok") {
		t.Fatalf("restore: %d %s %s", code, out, errOut)
	}
	// A second restore into the now non-empty directory is refused.
	if code, _, errOut := run("restore", "--backup", files[0], "--dest-dir", dest); code == 0 || !strings.Contains(errOut, "not empty") {
		t.Fatalf("expected refusal: %d %s", code, errOut)
	}
	if code, _, _ := run("check", "--db", filepath.Join(dir, "missing.db")); code == 0 {
		t.Fatal("check of a missing database must fail")
	}
	if handled, _ := runSubcommand([]string{"serve"}, os.Stdout, os.Stderr); handled {
		t.Fatal("unknown arguments must fall through to the server")
	}
}
