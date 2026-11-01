package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

func newStore(t *testing.T) (*storage.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "railvanta.db")
	s, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	exec(t, s.DB, `INSERT INTO workspaces VALUES ('w1','One','sandbox',0);
 INSERT INTO providers VALUES ('prov1','w1','mock',1,0);
 INSERT INTO payment_methods(id,workspace_id,rail,brand,last_four,created_at) VALUES ('m1','w1','mock','mock','0000',0);`)
	return s, path
}

func exec(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("SQL failed: %v\n%s", err, sql)
	}
}

// succeedPayment stores one succeeded payment with its attempt and balanced journal.
func succeedPayment(s *storage.Store, id string) error {
	return s.WithTx(context.Background(), func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,status,version,created_at,updated_at) VALUES(?, 'w1','m1',1000,'USD','processing',2,0,0)`, id).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO payment_attempts(id,workspace_id,payment_id,provider_id,routing_reason,attempt_number,provider_idempotency_key,status,created_at,updated_at) VALUES(?, 'w1', ?, 'prov1','test',1,?, 'succeeded',0,0)`, "a-"+id, id, "k-"+id).Error; err != nil {
			return err
		}
		if _, err := ledger.PostPaymentSuccess(tx, "w1", id, "prov1", "USD", 1000); err != nil {
			return err
		}
		return tx.Exec(`UPDATE payments SET status='succeeded' WHERE id=?`, id).Error
	})
}

func TestBackupUnderWriteLoadRestoresAndPassesChecks(t *testing.T) {
	s, path := newStore(t)
	for i := 0; i < 5; i++ {
		if err := succeedPayment(s, fmt.Sprintf("seed%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	var stop atomic.Bool
	var written atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			if err := succeedPayment(s, fmt.Sprintf("load%d", i)); err != nil {
				t.Error(err)
				return
			}
			written.Add(1)
		}
	}()
	for written.Load() < 20 {
	}
	outDir := filepath.Join(t.TempDir(), "backups")
	manifest, snapshot, err := Create(context.Background(), path, outDir, Release{Version: "test", GitSHA: "abc"})
	stop.Store(true)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion < 1 || len(manifest.SHA256) != 64 || manifest.SnapshotAt == "" || manifest.Release.Version != "test" {
		t.Fatalf("incomplete manifest %+v", manifest)
	}
	if _, err := os.Stat(snapshot + "-wal"); err == nil {
		t.Fatal("snapshot must be a standalone file")
	}
	latest, err := LatestSchema()
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	report, err := Restore(context.Background(), snapshot, dest, latest)
	if err != nil {
		t.Fatal(err)
	}
	if report.Check.Payments < 25 || report.Check.Payments != report.Check.Journals || !report.Check.OK() {
		t.Fatalf("unexpected restore report %+v", report.Check)
	}
	// The restored database opens and migrates cleanly with the normal store.
	restored, err := storage.Open(context.Background(), filepath.Join(dest, DatabaseFile))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := ledger.Check(restored.DB); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRefusesTamperedSnapshotAndDirtyDestination(t *testing.T) {
	s, path := newStore(t)
	if err := succeedPayment(s, "p1"); err != nil {
		t.Fatal(err)
	}
	_, snapshot, err := Create(context.Background(), path, t.TempDir(), Release{Version: "t"})
	if err != nil {
		t.Fatal(err)
	}
	latest, _ := LatestSchema()
	dirty := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirty, "stale"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), snapshot, dirty, latest); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("expected dirty destination refusal, got %v", err)
	}
	if _, err := Restore(context.Background(), snapshot, t.TempDir(), 0); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected newer-schema refusal, got %v", err)
	}
	f, err := os.OpenFile(snapshot, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0xff}, 3000); err != nil {
		t.Fatal(err)
	}
	f.Close()
	dest := t.TempDir()
	if _, err := Restore(context.Background(), snapshot, dest, latest); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum failure, got %v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatal("failed restore must leave the destination empty")
	}
}

func TestCheckDetectsDomainViolations(t *testing.T) {
	s, path := newStore(t)
	if err := succeedPayment(s, "ok"); err != nil {
		t.Fatal(err)
	}
	if result, err := Check(context.Background(), path); err != nil || !result.OK() {
		t.Fatalf("clean database must pass: %v %+v", err, result)
	}
	// Unbalance a posted journal and leave an unresolved attempt on a terminal payment.
	var triggers []string
	if err := s.DB.Raw(`SELECT name FROM sqlite_schema WHERE type='trigger' AND tbl_name='ledger_entries'`).Scan(&triggers).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range triggers {
		exec(t, s.DB, `DROP TRIGGER `+name) // simulate corruption that bypasses the immutability guard
	}
	exec(t, s.DB, `UPDATE ledger_entries SET amount_minor = amount_minor + 1 WHERE id = (SELECT id FROM ledger_entries LIMIT 1)`)
	exec(t, s.DB, `INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,status,version,created_at,updated_at) VALUES('pf','w1','m1',5,'USD','failed',3,0,0)`)
	exec(t, s.DB, `INSERT INTO payment_attempts(id,workspace_id,payment_id,provider_id,routing_reason,attempt_number,provider_idempotency_key,status,created_at,updated_at) VALUES('af','w1','pf','prov1','t',1,'kf','unknown',0,0)`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(result.Problems, "\n")
	for _, want := range []string{"unbalanced_journal", "unresolved attempt on terminal payment"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}
