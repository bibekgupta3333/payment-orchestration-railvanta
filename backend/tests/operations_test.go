package tests

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/provider"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Matrix "Disk full": SQLite's page limit makes growth fail with the same
// SQLITE_FULL a full disk produces. Acceptance must fail with a bounded 503
// (never a 500, panic or partial rows), the database must stay intact, and
// service resumes once space returns.
func TestDiskFullFailsWritesBoundedWithoutCorruption(t *testing.T) {
	e := newEnv(t, payment.Options{})
	e.accept(auth.DemoMethodID, "1", "USD")
	var pages int64
	if err := e.store.DB.Raw("PRAGMA page_count").Scan(&pages).Error; err != nil {
		t.Fatal(err)
	}
	var limit int64
	if err := e.store.DB.Raw("PRAGMA max_page_count").Scan(&limit).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.store.DB.Exec("PRAGMA max_page_count = " + itoa(pages)).Error; err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if !restored {
			restored = true
			if err := e.store.DB.Exec("PRAGMA max_page_count = " + itoa(limit)).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	defer restore()
	// Large merchant references force page allocation; stop at the first failure.
	filler := strings.Repeat("x", 230)
	var failed *http.Response
	accepted := int64(1)
	for i := 0; i < 400 && failed == nil; i++ {
		body := `{"amount_minor":"5","currency":"USD","payment_method_id":"` + auth.DemoMethodID + `","merchant_reference":"` + filler + itoa(int64(i)) + `"}`
		response := e.create("disk-"+itoa(int64(i)), body)
		switch response.Code {
		case 202:
			accepted++
		case 503:
			failed = response.Result()
		default:
			t.Fatalf("disk full produced %d, want a bounded 503: %s", response.Code, response.Body)
		}
	}
	if failed == nil {
		t.Skip("page limit never reached; fixture too small")
	}
	if failed.Header.Get("Retry-After") == "" {
		t.Error("503 must carry Retry-After")
	}
	// Every acceptance table agrees: no partial payment, no orphan outbox row.
	e.requireCounts(accepted)
	// The writable probe used by readiness classifies a full database as
	// degraded for the errors SQLite really returned.
	var err error
	for i := 0; i < 400 && err == nil; i++ {
		err = e.store.DB.Exec("INSERT INTO workspaces(id,name,environment,created_at) VALUES(?,?,'sandbox',1)", "w-full-"+itoa(int64(i)), filler+filler+filler).Error
	}
	if err == nil || !storage.IsWriteFailure(err) || storage.IsTransient(err) {
		t.Fatalf("SQLITE_FULL must classify as a write failure: %v", err)
	}
	restore()
	var integrity string
	if err := e.store.DB.Raw("PRAGMA integrity_check").Scan(&integrity).Error; err != nil || integrity != "ok" {
		t.Fatalf("integrity %q %v", integrity, err)
	}
	if err := storage.CheckWritable(context.Background(), e.store.DB); err != nil {
		t.Fatalf("recovered database must be writable: %v", err)
	}
	if response := e.create("after-space", intent("9", "USD", auth.DemoMethodID)); response.Code != 202 {
		t.Fatalf("recovery %d %s", response.Code, response.Body)
	}
	e.requireCounts(accepted + 1)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// Matrix "Disk full" (readiness): the writable probe behind /health/ready
// reports a degraded database (read-only file system, I/O failure) but not
// mere lock contention. Wiring into the HTTP handler is a 503 in cmd/api.
func TestWritableProbeReportsDegradedButNotBusy(t *testing.T) {
	e := newEnv(t, payment.Options{})
	ctx := context.Background()
	if err := storage.CheckWritable(ctx, e.store.DB); err != nil {
		t.Fatalf("healthy database: %v", err)
	}
	if err := e.store.DB.Exec("PRAGMA query_only = ON").Error; err != nil {
		t.Fatal(err)
	}
	err := storage.CheckWritable(ctx, e.store.DB)
	if !errors.Is(err, storage.ErrDegraded) {
		t.Fatalf("read-only database must be degraded: %v", err)
	}
	if err := e.store.DB.Exec("PRAGMA query_only = OFF").Error; err != nil {
		t.Fatal(err)
	}
	if err := storage.CheckWritable(ctx, e.store.DB); err != nil {
		t.Fatalf("recovered: %v", err)
	}
	// A held write lock is contention, not degradation.
	release := holdWriteLock(t, e.path)
	defer release()
	probeCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := storage.CheckWritable(probeCtx, e.store.DB); err != nil {
		t.Fatalf("busy database must not be reported degraded: %v", err)
	}
}

// Matrix "SQLite busy": a busy database during the post-provider outcome
// transaction is retried as a DB-only transaction; the provider is called once
// and the payment settles with one charge and one attempt.
func TestBusyDuringOutcomeRecordingDoesNotRepeatProviderCall(t *testing.T) {
	var release func()
	var calls int
	d := newDispatchEnv(t, dispatchConfig{})
	d.build(dispatchConfig{hooks: payment.ExecutorHooks{BeforeRecord: func(context.Context, payment.PreparedAttempt, provider.Result) error {
		calls++
		release = holdWriteLock(t, d.path)
		time.AfterFunc(5500*time.Millisecond, release) // busy_timeout is 5s: first try fails, retry succeeds
		return nil
	}}})
	id := d.accept(scenarioMethod["success"], "4200", "USD")
	if _, err := d.dispatcher.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if release != nil {
		release()
	}
	d.drain()
	requireStatus(t, d.payment(id), payment.StatusSucceeded)
	if calls != 1 {
		t.Fatalf("record hook ran %d times", calls)
	}
	if n := len(d.charges("1=1")); n != 1 {
		t.Fatalf("provider charged %d times", n)
	}
	if n := len(d.attempts(id)); n != 1 {
		t.Fatalf("%d attempts", n)
	}
	d.requireLedger()
}

// Matrix "Migration failure": a failing or inconsistent migration aborts
// startup (storage.Open errors, so cmd/api never binds a listener and /health/*
// is never served) and leaves the database byte-for-byte equivalent.
func TestMigrationFailureKeepsDatabaseUnchangedAndStartupRefused(t *testing.T) {
	e := newEnv(t, payment.Options{})
	e.accept(auth.DemoMethodID, "100", "USD")
	before := e.dump()
	plan, err := storage.EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	bad := func(sql string) []storage.Migration {
		return append(append([]storage.Migration{}, plan...), storage.Migration{Version: len(plan) + 1, Name: "9999_bad.sql", SQL: sql})
	}
	fixtures := map[string][]storage.Migration{
		"invalid SQL midway": bad("CREATE TABLE half_applied(id TEXT) STRICT; ALTER TABLE payments ADD COLUMN extra TEXT; THIS IS NOT SQL;"),
		"duplicate version":  append(append([]storage.Migration{}, plan...), plan[len(plan)-1]),
		"empty migration":    bad("   "),
	}
	for name, fixture := range fixtures {
		if err := storage.Migrate(ctx, e.store.DB, fixture); err == nil {
			t.Fatalf("%s: migration succeeded", name)
		}
		if after := e.dump(); !reflect.DeepEqual(before, after) {
			t.Fatalf("%s changed the database", name)
		}
		var extra int64
		if err := e.store.DB.Raw("SELECT count(*) FROM pragma_table_info('payments') WHERE name = 'extra'").Scan(&extra).Error; err != nil || extra != 0 {
			t.Fatalf("%s left a column: %d %v", name, extra, err)
		}
	}
	// Tampered history: the next startup refuses to open the database.
	var original string
	if err := e.store.DB.Raw("SELECT checksum FROM schema_migrations WHERE version = 1").Scan(&original).Error; err != nil {
		t.Fatal(err)
	}
	e.exec("UPDATE schema_migrations SET checksum = ? WHERE version = 1", strings.Repeat("a", 64))
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := storage.Open(ctx, e.path); err == nil {
		_ = reopened.Close()
		t.Fatal("startup accepted a tampered migration history")
	} else if !strings.Contains(err.Error(), "drift") && !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("unexpected startup error: %v", err)
	}
	// The refused startup did not modify anything; restoring the checksum
	// (the "restore from backup" stand-in) makes it start with the original data.
	raw, err := gorm.Open(sqlite.Open("file:"+e.path+"?_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	var tamperedRows int64
	if err := raw.Raw("SELECT count(*) FROM schema_migrations WHERE checksum = ?", strings.Repeat("a", 64)).Scan(&tamperedRows).Error; err != nil || tamperedRows != 1 {
		t.Fatalf("refused startup altered migration history: %d %v", tamperedRows, err)
	}
	if err := raw.Exec("UPDATE schema_migrations SET checksum = ? WHERE version = 1", original).Error; err != nil {
		t.Fatal(err)
	}
	handle, _ := raw.DB()
	_ = handle.Close()
	e.open(payment.Options{})
	if after := e.dump(); !reflect.DeepEqual(before, after) {
		t.Fatal("data differs after restoring the migration history")
	}
}
