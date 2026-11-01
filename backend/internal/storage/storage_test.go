package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func execSQL(t *testing.T, db *gorm.DB, sql string) {
	t.Helper()
	if err := db.Exec(sql).Error; err != nil {
		t.Fatalf("SQL failed: %v\n%s", err, sql)
	}
}
func seed(t *testing.T, db *gorm.DB) {
	t.Helper()
	execSQL(t, db, `INSERT INTO workspaces VALUES ('w1','One','sandbox',0),('w2','Two','sandbox',0);
 INSERT INTO providers VALUES ('provider1','w1','mock',1,0),('provider2','w2','mock',1,0);
 INSERT INTO payment_methods(id,workspace_id,rail,brand,last_four,created_at) VALUES ('method1','w1','mock','mock','0000',0),('method2','w2','mock','mock','0000',0);
 INSERT INTO payment_method_tokens VALUES ('token1','w1','method1','provider1','opaque',0);
 INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,status,version,created_at,updated_at) VALUES ('p1','w1','method1',25000,'USD','created',1,0,0);
 INSERT INTO ledger_accounts(id,workspace_id,provider_id,code,currency,kind,created_at) VALUES ('asset','w1','provider1','provider1_clearing','USD','asset',0),('liability','w1',NULL,'merchant_payable','USD','liability',0),('eur','w1',NULL,'clearing','EUR','asset',0);
 INSERT INTO users VALUES('u1','operator@example.test',0);
 INSERT INTO workspace_members VALUES('u1','w1','operator',0);`)
}
func TestOpenSafetyAndIDs(t *testing.T) {
	s := testStore(t)
	if err := VerifySettings(context.Background(), s.DB); err != nil {
		t.Fatal(err)
	}
	handle, err := s.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if handle.Stats().MaxOpenConnections != 1 {
		t.Fatal("database must have one pooled connection")
	}
	var result string
	if err := s.DB.Raw("PRAGMA integrity_check").Scan(&result).Error; err != nil || result != "ok" {
		t.Fatalf("integrity %q %v", result, err)
	}
	var violations []map[string]interface{}
	if err := s.DB.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil || len(violations) != 0 {
		t.Fatalf("foreign keys %v %v", violations, err)
	}
	var tables int64
	s.DB.Raw("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables)
	if tables != 24 {
		t.Fatalf("got %d tables", tables)
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 {
		t.Fatalf("expected UUIDv7: %s %v", id, err)
	}
	for _, path := range []string{"", ":memory:", "file:memory?mode=memory"} {
		if _, err := Open(context.Background(), path); err == nil {
			t.Fatalf("unsafe path accepted %q", path)
		}
	}
	execSQL(t, s.DB, "PRAGMA foreign_keys=OFF")
	if err := VerifySettings(context.Background(), s.DB); err == nil {
		t.Fatal("misconfigured connection accepted")
	}
	execSQL(t, s.DB, "PRAGMA foreign_keys=ON")
}
func TestTransactionRollbackAndModel(t *testing.T) {
	s := testStore(t)
	wanted := errors.New("rollback")
	if err := s.WithTx(context.Background(), func(tx *gorm.DB) error {
		if err := tx.Create(&Workspace{ID: "rollback", Name: "rollback", Environment: "sandbox"}).Error; err != nil {
			return err
		}
		return wanted
	}); !errors.Is(err, wanted) {
		t.Fatal(err)
	}
	var count int64
	s.DB.Model(&Workspace{}).Where("id = ?", "rollback").Count(&count)
	if count != 0 {
		t.Fatal("transaction partially committed")
	}
	err := s.WithTx(context.Background(), func(tx *gorm.DB) error {
		return tx.Create(&Workspace{ID: "valid", Name: "valid", Environment: "sandbox"}).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	var workspace Workspace
	if err := s.DB.First(&workspace, "id = ?", "valid").Error; err != nil || workspace.CreatedAt < 1000000000000 {
		t.Fatalf("model or millisecond timestamp: %+v %v", workspace, err)
	}
}
func TestConstraintRejections(t *testing.T) {
	cases := []struct{ name, prepare, rejected string }{
		{"cross workspace method", "", `INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,created_at,updated_at) VALUES('bad','w1','method2',100,'USD',0,0)`},
		{"cross workspace token", "", `INSERT INTO payment_method_tokens VALUES('bad','w1','method2','provider1','opaque',0)`},
		{"fractional money", "", `INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,created_at,updated_at) VALUES('bad','w1','method1',1.5,'USD',0,0)`},
		{"out of range money", "", `INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,created_at,updated_at) VALUES('bad','w1','method1',9223372036854775808,'USD',0,0)`},
		{"zero money", "", `INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,created_at,updated_at) VALUES('bad','w1','method1',0,'USD',0,0)`},
		{"immutable amount", "", `UPDATE payments SET amount_minor=1 WHERE id='p1'`},
		{"immutable currency", "", `UPDATE payments SET currency='EUR' WHERE id='p1'`},
		{"immutable workspace", "", `UPDATE payments SET workspace_id='w2' WHERE id='p1'`},
		{"immutable method", "", `UPDATE payments SET payment_method_id='method2' WHERE id='p1'`},
		{"terminal status", `UPDATE payments SET status='succeeded' WHERE id='p1'`, `UPDATE payments SET status='processing' WHERE id='p1'`},
		{"invalid initial transition", "", `INSERT INTO payment_state_transitions VALUES('bad','w1','p1',NULL,'processing','bad','u1',0)`},
		{"audit deletion", `INSERT INTO payment_state_transitions VALUES('transition','w1','p1',NULL,'created','initial','u1',0)`, `DELETE FROM payment_state_transitions WHERE id='transition'`},
		{"audit update", `INSERT INTO payment_state_transitions VALUES('transition','w1','p1',NULL,'created','initial','u1',0)`, `UPDATE payment_state_transitions SET reason='changed' WHERE id='transition'`},
		{"routing amount range", "", `INSERT INTO routing_rules(id,workspace_id,provider_id,rail,currency,priority,min_amount_minor,max_amount_minor,created_at,updated_at) VALUES('r','w1','provider1','mock','USD',1,100,99,0,0)`},
		{"invalid outbox lease", "", `INSERT INTO outbox_events(id,workspace_id,event_type,deduplication_key,payload,status,available_at,created_at) VALUES('o','w1','test','key','{}','processing',0,0)`},
		{"inactive outbox lease", "", `INSERT INTO outbox_events(id,workspace_id,event_type,deduplication_key,payload,status,available_at,created_at,lease_owner,lease_until) VALUES('o','w1','test','key','{}','pending',0,0,'owner',5000)`},
		{"negative duplicate count", "", `INSERT INTO webhook_inbox(id,workspace_id,provider_id,provider_event_id,payload_hash,payload,duplicate_count,last_received_at,received_at) VALUES('e','w1','provider1','event','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','{}',-1,0,0)`},
		{"invalid member role", "", `INSERT INTO workspace_members VALUES('u1','w2','admin',0)`},
		{"session without membership", "", `INSERT INTO sessions VALUES('s','u1','w2','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',100,0)`},
		{"duplicate idempotency key", `INSERT INTO idempotency_records(id,workspace_id,operation,key,request_hash,status,created_at) VALUES('i1','w1','create','key','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','pending',0)`, `INSERT INTO idempotency_records(id,workspace_id,operation,key,request_hash,status,created_at) VALUES('i2','w1','create','key','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','pending',0)`},
		{"second unresolved attempt", `INSERT INTO payment_attempts(id,workspace_id,payment_id,provider_id,routing_reason,attempt_number,provider_idempotency_key,status,created_at,updated_at) VALUES('a1','w1','p1','provider1','mock',1,'key1','unknown',0,0)`, `INSERT INTO payment_attempts(id,workspace_id,payment_id,provider_id,routing_reason,attempt_number,provider_idempotency_key,created_at,updated_at) VALUES('a2','w1','p1','provider1','mock',2,'key2',0,0)`},

		{"invalid discrepancy kind", `INSERT INTO reconciliation_runs(id,workspace_id,run_key,status,window_start,window_end,created_at) VALUES('run','w1','key','pending',0,1,0)`, `INSERT INTO reconciliation_discrepancies(id,workspace_id,run_id,fingerprint,kind,details,created_at) VALUES('d','w1','run','key','invented','{}',0)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testStore(t)
			seed(t, s.DB)
			if c.prepare != "" {
				execSQL(t, s.DB, c.prepare)
			}
			if err := s.DB.Exec(c.rejected).Error; err == nil {
				t.Fatal("invalid write was accepted")
			}
		})
	}
}
func TestValidSchemaPaths(t *testing.T) {
	s := testStore(t)
	seed(t, s.DB)
	execSQL(t, s.DB, `INSERT INTO payment_state_transitions VALUES('initial','w1','p1',NULL,'created','initial','u1',0);
 INSERT INTO payment_state_transitions VALUES('later','w1','p1','created','processing','dispatch','u1',1);
 INSERT INTO routing_rules(id,workspace_id,provider_id,rail,currency,priority,min_amount_minor,max_amount_minor,created_at,updated_at) VALUES('r','w1','provider1','mock','USD',1,100,100,0,0);
 INSERT INTO outbox_events(id,workspace_id,event_type,deduplication_key,payload,status,available_at,created_at,lease_owner,lease_until) VALUES('o','w1','test','key','{}','processing',0,0,'claim-token',5000);
 INSERT INTO webhook_inbox(id,workspace_id,provider_id,provider_event_id,payload_hash,payload,last_received_at,received_at) VALUES('e','w1','provider1','event','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','{}',0,0);
 INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,created_at,updated_at) VALUES('max','w1','method1',9223372036854775807,'USD',0,0);
 INSERT INTO mock_provider_charges VALUES('charge','w1','provider1','provider-key','reference',100,'USD','succeeded','success',0,0);
 INSERT INTO simulation_runs(id,workspace_id,scenario,seed,input_json,created_at) VALUES('simulation','w1','success','seed','{}',0);
 INSERT INTO sessions VALUES('s','u1','w1','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',100,0);
 INSERT INTO api_keys VALUES('k','w1','test','prefix','cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc','read',NULL,NULL,0);`)
	var p Payment
	if err := s.DB.First(&p, "id = ?", "max").Error; err != nil || p.AmountMinor != 9223372036854775807 {
		t.Fatalf("int64 roundtrip %+v %v", p, err)
	}
	if err := s.DB.Exec(`INSERT INTO mock_provider_charges VALUES('duplicate','w1','provider1','provider-key','reference2',100,'USD','succeeded','success',0,0)`).Error; err == nil {
		t.Fatal("duplicate provider charge key accepted")
	}
}
func TestLedgerPosting(t *testing.T) {
	s := testStore(t)
	seed(t, s.DB)
	execSQL(t, s.DB, `INSERT INTO ledger_journals(id,workspace_id,payment_id,currency,posting_key,created_at) VALUES('j','w1','p1','USD','p1:success',0);
 INSERT INTO ledger_entries VALUES('debit','w1','j','asset','USD','debit',25000,0);`)
	reject := func(sql string) {
		t.Helper()
		if err := s.DB.Exec(sql).Error; err == nil {
			t.Fatal("invalid ledger write accepted: " + sql)
		}
	}
	reject(`UPDATE ledger_journals SET status='posted',posted_at=1 WHERE id='j'`)
	reject(`INSERT INTO ledger_entries VALUES('eur','w1','j','eur','USD','credit',25000,0)`)
	execSQL(t, s.DB, `INSERT INTO ledger_entries VALUES('credit','w1','j','liability','USD','credit',25000,0); UPDATE ledger_journals SET status='posted',posted_at=1 WHERE id='j';`)
	for _, sql := range []string{
		`INSERT INTO ledger_journals(id,workspace_id,currency,posting_key,created_at) VALUES('dup','w1','USD','p1:success',0)`,
		`UPDATE ledger_journals SET status='draft',posted_at=NULL WHERE id='j'`,
		`DELETE FROM ledger_journals WHERE id='j'`,
		`UPDATE ledger_entries SET amount_minor=1 WHERE id='debit'`,
		`DELETE FROM ledger_entries WHERE id='debit'`,
		`INSERT INTO ledger_entries VALUES('extra','w1','j','asset','USD','debit',1,0)`,
	} {
		reject(sql)
	}
	execSQL(t, s.DB, `INSERT INTO ledger_journals(id,workspace_id,currency,posting_key,reversal_of,created_at) VALUES('reverse','w1','USD','reverse','j',0);
 INSERT INTO ledger_entries VALUES('r1','w1','reverse','asset','USD','credit',25000,0),('r2','w1','reverse','liability','USD','debit',25000,0);
 UPDATE ledger_journals SET status='posted',posted_at=1 WHERE id='reverse';
 INSERT INTO ledger_journals(id,workspace_id,currency,posting_key,created_at) VALUES('overflow','w1','USD','overflow',0);
 INSERT INTO ledger_entries VALUES('o1','w1','overflow','asset','USD','debit',9223372036854775807,0),('o2','w1','overflow','asset','USD','debit',1,0),('o3','w1','overflow','liability','USD','credit',9223372036854775807,0),('o4','w1','overflow','liability','USD','credit',1,0);`)
	reject(`UPDATE ledger_journals SET status='posted',posted_at=1 WHERE id='overflow'`)
}
func TestMigrationUpgradeDriftAndCompatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := gorm.Open(sqlite.Open(path+"?_foreign_keys=on"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	plan, err := EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := Migrate(ctx, db, plan[:1]); err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, `INSERT INTO workspaces VALUES('preserved','Upgrade fixture','sandbox',0)`)
	if err := Migrate(ctx, db, plan); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db, plan); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	var count int64
	db.Model(&Workspace{}).Where("id = ?", "preserved").Count(&count)
	if count != 1 {
		t.Fatal("upgrade lost existing data")
	}
	changed := append([]Migration(nil), plan...)
	changed[0].SQL += "\n-- changed"
	if err := Migrate(ctx, db, changed); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("checksum drift not rejected: %v", err)
	}
	if err := Migrate(ctx, db, plan[:1]); err == nil {
		t.Fatal("newer database accepted")
	}
	execSQL(t, db, `UPDATE schema_migrations SET checksum='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' WHERE version=1`)
	if _, err := Open(ctx, path); err == nil {
		t.Fatal("startup accepted tampered migration checksum")
	}
}
func TestMigrationRefusesUnmanagedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	execSQL(t, db, `CREATE TABLE unmanaged(id TEXT) STRICT`)
	plan, err := EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(context.Background(), db, plan); err == nil {
		t.Fatal("unmanaged database accepted")
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "failed.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	plan := []Migration{{Version: 1, Name: "0001_invalid.sql", SQL: "CREATE TABLE partial(id TEXT PRIMARY KEY) STRICT; NOT VALID SQL;"}}
	if err := Migrate(context.Background(), db, plan); err == nil {
		t.Fatal("invalid migration succeeded")
	}
	var count int64
	if err := db.Raw("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed migration left %d tables", count)
	}
}
