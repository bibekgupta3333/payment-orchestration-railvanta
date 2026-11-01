package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/backup"
	"github.com/railvanta/railvanta/backend/internal/config"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// runSubcommand handles `railvanta-api backup|restore|check|seed-demo`. It reports
// whether args named a subcommand; the exit code is meaningful only then.
func runSubcommand(args []string, stdout, stderr io.Writer) (handled bool, code int) {
	if len(args) == 0 {
		return false, 0
	}
	switch args[0] {
	case "backup", "restore", "check", "seed-demo":
	default:
		return false, 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch args[0] {
	case "backup":
		err = backupCommand(ctx, args[1:], stdout)
	case "restore":
		err = restoreCommand(ctx, args[1:], stdout)
	case "check":
		err = checkCommand(ctx, args[1:], stdout)
	case "seed-demo":
		err = seedDemoCommand(ctx, stdout)
	}
	if err != nil {
		fmt.Fprintf(stderr, "railvanta-api %s: %v\n", args[0], err)
		return true, 1
	}
	return true, 0
}

func release() backup.Release {
	version := os.Getenv("RAILVANTA_RELEASE")
	if version == "" {
		version = "dev"
	}
	return backup.Release{Version: version, GitSHA: os.Getenv("RAILVANTA_GIT_SHA")}
}

func defaultDB() string {
	if v := os.Getenv("SQLITE_PATH"); v != "" {
		return v
	}
	return "data/railvanta.db"
}

func backupCommand(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	db := fs.String("db", defaultDB(), "SQLite database to snapshot (default SQLITE_PATH)")
	dir := fs.String("out-dir", "", "directory that receives the snapshot and its .json manifest")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("--out-dir is required")
	}
	manifest, path, err := backup.Create(ctx, *db, *dir, release())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "backup=%s\nsnapshot_at=%s\nschema_version=%d\nsha256=%s\nrelease=%s\n", path, manifest.SnapshotAt, manifest.SchemaVersion, manifest.SHA256, manifest.Release.Version)
	return nil
}

func restoreCommand(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	snapshot := fs.String("backup", "", "snapshot file (its .json manifest must sit next to it)")
	dest := fs.String("dest-dir", "", "clean directory that receives railvanta.db")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *snapshot == "" || *dest == "" {
		return fmt.Errorf("--backup and --dest-dir are required")
	}
	latest, err := backup.LatestSchema()
	if err != nil {
		return err
	}
	report, err := backup.Restore(ctx, *snapshot, *dest, latest)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "restored=%s\nsnapshot_at=%s\nbackup_release=%s\nrunning_release=%s\nschema_version=%d\npayments=%d\njournals=%d\nattempts=%d\nchecks=ok\n",
		report.Database, report.Manifest.SnapshotAt, report.Manifest.Release.Version, release().Version, report.Manifest.SchemaVersion, report.Check.Payments, report.Check.Journals, report.Check.Attempts)
	return nil
}

func checkCommand(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	db := fs.String("db", defaultDB(), "SQLite database to check (default SQLITE_PATH)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	result, err := backup.Check(ctx, *db)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "payments=%d\njournals=%d\nattempts=%d\n", result.Payments, result.Journals, result.Attempts)
	if !result.OK() {
		return fmt.Errorf("%d violation(s):\n  %s", len(result.Problems), strings.Join(result.Problems, "\n  "))
	}
	fmt.Fprintln(out, "checks=ok")
	return nil
}

// seedDemoCommand provisions the sandbox demo workspace, payment methods and
// the credentials given in DEMO_* variables into SQLITE_PATH. Containers keep
// DEMO_MODE off (its trust boundary is a loopback API bind), so this explicit
// operator command is how a container sandbox, and the container E2E suite,
// receive credentials. It never prints them and needs APP_ENV=sandbox.
func seedDemoCommand(ctx context.Context, out io.Writer) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	store, err := storage.Open(ctx, cfg.SQLitePath)
	if err != nil {
		return err
	}
	defer store.Close()
	credentials := auth.DemoCredentials{OperatorKey: os.Getenv("DEMO_OPERATOR_KEY"), ReaderKey: os.Getenv("DEMO_READER_KEY"), OtherWorkspaceKey: os.Getenv("DEMO_OTHER_WORKSPACE_KEY"), SessionToken: os.Getenv("DEMO_SESSION_TOKEN"), CSRFToken: os.Getenv("DEMO_CSRF_TOKEN")}
	if err := auth.SeedDemo(ctx, store.DB, credentials); err != nil {
		return err
	}
	fmt.Fprintln(out, "seeded=ok")
	return nil
}
