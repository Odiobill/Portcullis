package migrate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakePool implements the migration Pool boundary over in-memory
// transactions. It never connects to PostgreSQL.
type fakePool struct {
	txs      []*fakeTx
	failOnTx string
}

func (f *fakePool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx := &fakeTx{applied: map[string]bool{}, failOn: f.failOnTx}
	if len(f.txs) > 0 {
		// Carry the version state forward so later transactions observe
		// versions recorded (committed) by earlier ones.
		for k, v := range f.txs[len(f.txs)-1].applied {
			tx.applied[k] = v
		}
	}
	f.txs = append(f.txs, tx)
	return tx, nil
}

// beginCount returns the number of transactions begun so far.
func (f *fakePool) beginCount() int { return len(f.txs) }

// fakeTx records every executed statement and simulates the
// schema_migrations table in memory.
type fakeTx struct {
	pgx.Tx    // panic on any unexpected interface method
	applied   map[string]bool
	execs     []string
	commits   int
	rollbacks int
	committed bool
	failOn    string
}

func (t *fakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if t.failOn != "" && strings.Contains(sql, t.failOn) {
		return pgconn.CommandTag{}, fmt.Errorf("simulated failure for %q", t.failOn)
	}
	if strings.HasPrefix(sql, "INSERT INTO schema_migrations") {
		if len(args) != 1 {
			return pgconn.CommandTag{}, errors.New("version insert must be parameterized")
		}
		version, ok := args[0].(string)
		if !ok {
			return pgconn.CommandTag{}, errors.New("version must be a string")
		}
		t.applied[version] = true
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	t.execs = append(t.execs, sql)
	return pgconn.NewCommandTag("CREATE TABLE"), nil
}

// fakeRow simulates the EXISTS version check.
type fakeRow struct {
	applied bool
}

func (r fakeRow) Scan(dest ...any) error {
	out, ok := dest[0].(*bool)
	if !ok {
		return errors.New("scan target must be *bool")
	}
	*out = r.applied
	return nil
}

func (t *fakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if !strings.Contains(sql, "schema_migrations") {
		return errorRow{err: errors.New("unexpected query outside schema_migrations tracking")}
	}
	if len(args) != 1 {
		return errorRow{err: errors.New("version lookup must be parameterized")}
	}
	version, ok := args[0].(string)
	if !ok {
		return errorRow{err: errors.New("version must be a string")}
	}
	return fakeRow{applied: t.applied[version]}
}

type errorRow struct{ err error }

func (r errorRow) Scan(dest ...any) error { return r.err }

func (t *fakeTx) Commit(ctx context.Context) error { t.commits++; t.committed = true; return nil }

// Rollback mirrors pgx semantics: after Commit it is a documented no-op
// (ErrTxClosed), so only a real rollback is counted.
func (t *fakeTx) Rollback(ctx context.Context) error {
	if t.committed {
		return pgx.ErrTxClosed
	}
	t.rollbacks++
	return nil
}

func migrationsFS(files map[string]string) fstest.MapFS {
	fs := fstest.MapFS{}
	for name, body := range files {
		fs[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fs
}

// trackingSQL is the one statement the tracking transaction may run.
const trackingSQLFragment = "CREATE TABLE IF NOT EXISTS schema_migrations"

// TestApplyRunsPendingMigrationsInOwnTransactions pins the Slice 5
// correction: a fresh database receives every migration exactly once, in
// filename order, and each pending migration runs inside its OWN
// transaction together with its version record — not one big
// all-pending-migrations transaction.
func TestApplyRunsPendingMigrationsInOwnTransactions(t *testing.T) {
	pool := &fakePool{}
	fsys := migrationsFS(map[string]string{
		"000002_add_index.up.sql": "CREATE INDEX idx_demo ON services(id);",
		"000001_create.up.sql":    "CREATE TABLE services (id TEXT PRIMARY KEY);",
	})

	applied, err := Apply(context.Background(), pool, fsys)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if applied != 2 {
		t.Errorf("applied = %d, want 2", applied)
	}

	// Transaction shape: one for tracking setup, then ONE PER MIGRATION.
	if got := pool.beginCount(); got != 3 {
		t.Fatalf("Begin calls = %d, want 3 (tracking + one per migration); single-transaction apply would begin 1", got)
	}

	tracking := pool.txs[0]
	if len(tracking.execs) != 1 || !strings.Contains(tracking.execs[0], trackingSQLFragment) {
		t.Errorf("tracking transaction must only create the tracking table, got %v", tracking.execs)
	}
	if tracking.commits != 1 || tracking.rollbacks != 0 {
		t.Errorf("tracking transaction must commit exactly once (commits=%d rollbacks=%d)", tracking.commits, tracking.rollbacks)
	}

	first := pool.txs[1]
	if len(first.execs) != 1 || !strings.Contains(first.execs[0], "CREATE TABLE services") {
		t.Errorf("first migration transaction must run only its own SQL (the version insert is recorded in-tx), got %v", first.execs)
	}
	if !first.applied["000001_create.up.sql"] {
		t.Error("first migration version must be recorded inside its own transaction")
	}
	if first.commits != 1 || first.rollbacks != 0 {
		t.Errorf("first migration transaction must commit exactly once (commits=%d rollbacks=%d)", first.commits, first.rollbacks)
	}

	second := pool.txs[2]
	if len(second.execs) != 1 || !strings.Contains(second.execs[0], "CREATE INDEX idx_demo") {
		t.Errorf("second migration transaction must run only its own SQL (the version insert is recorded in-tx), got %v", second.execs)
	}
	if !second.applied["000002_add_index.up.sql"] {
		t.Error("second migration version must be recorded inside its own transaction")
	}
	if second.commits != 1 || second.rollbacks != 0 {
		t.Errorf("second migration transaction must commit exactly once (commits=%d rollbacks=%d)", second.commits, second.rollbacks)
	}
}

// TestApplyPreservesEarlierCommittedMigrationWhenLaterFails is the
// distinguishing per-migration-semantics test: when the second migration
// fails, the first migration's transaction must remain COMMITTED and its
// version recorded — an all-migrations transaction would roll it back.
func TestApplyPreservesEarlierCommittedMigrationWhenLaterFails(t *testing.T) {
	pool := &fakePool{failOnTx: "ADD COLUMN note"}
	fsys := migrationsFS(map[string]string{
		"000001_create.up.sql": "CREATE TABLE services (id TEXT PRIMARY KEY);",
		"000002_add.up.sql":    "ALTER TABLE services ADD COLUMN note TEXT;",
		"000003_never.up.sql":  "SELECT 1;",
	})

	applied, err := Apply(context.Background(), pool, fsys)
	if err == nil {
		t.Fatal("expected error for the failing second migration, got nil")
	}
	if applied != 1 {
		t.Errorf("applied = %d, want 1 (the earlier migration must stay committed)", applied)
	}

	// Transactions: tracking + first migration + second migration; the
	// third migration must never begin.
	if got := pool.beginCount(); got != 3 {
		t.Errorf("Begin calls = %d, want 3 (tracking + first + failing second); later migrations must not run", got)
	}

	first := pool.txs[1]
	if first.commits != 1 || first.rollbacks != 0 {
		t.Errorf("first migration must remain committed (commits=%d rollbacks=%d)", first.commits, first.rollbacks)
	}
	if !first.applied["000001_create.up.sql"] {
		t.Error("first migration version must stay recorded after the later failure")
	}

	second := pool.txs[2]
	if second.rollbacks != 1 || second.commits != 0 {
		t.Errorf("failing second migration must roll back exactly once (commits=%d rollbacks=%d)", second.commits, second.rollbacks)
	}
	if _, ok := second.applied["000002_add.up.sql"]; ok {
		t.Error("failing migration must not be recorded as applied")
	}
	for _, tx := range pool.txs {
		for _, exec := range tx.execs {
			if strings.Contains(exec, "SELECT 1;") {
				t.Error("execution must stop at the first failing migration")
			}
		}
	}
}

// TestApplyFailsClosedAndRollsBackOnError pins that a failing first
// migration aborts the apply: nothing is applied, the failed transaction is
// rolled back, no version is recorded, and no later migration runs.
func TestApplyFailsClosedAndRollsBackOnError(t *testing.T) {
	pool := &fakePool{failOnTx: "broken"}
	fsys := migrationsFS(map[string]string{
		"000001_bad.up.sql":   "CREATE TABLE broken ( TO BE OR NOT TO BE",
		"000002_never.up.sql": "SELECT 1;",
	})

	applied, err := Apply(context.Background(), pool, fsys)
	if err == nil {
		t.Fatal("expected error for a failing migration, got nil")
	}
	if applied != 0 {
		t.Errorf("applied = %d, want 0 on failure", applied)
	}
	if got := pool.beginCount(); got != 2 {
		t.Errorf("Begin calls = %d, want 2 (tracking + failing migration); later migrations must not begin", got)
	}
	failed := pool.txs[1]
	if failed.rollbacks != 1 || failed.commits != 0 {
		t.Errorf("failure must roll back exactly once (commits=%d rollbacks=%d)", failed.commits, failed.rollbacks)
	}
	if _, ok := failed.applied["000001_bad.up.sql"]; ok {
		t.Error("failed migration must not be recorded as applied")
	}
}

// TestApplySkipsAlreadyAppliedMigrations pins rerun safety: Compose
// restarts and retries must never re-execute an already applied migration
// against the same fresh schema.
func TestApplySkipsAlreadyAppliedMigrations(t *testing.T) {
	pool := &fakePool{}
	fsys := migrationsFS(map[string]string{
		"000001_create.up.sql": "CREATE TABLE services (id TEXT PRIMARY KEY);",
		"000002_add.up.sql":    "ALTER TABLE services ADD COLUMN note TEXT;",
	})
	if _, err := Apply(context.Background(), pool, fsys); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	firstRunTxs := len(pool.txs)

	// Second run against the same database state (fake carries versions).
	applied, err := Apply(context.Background(), pool, fsys)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if applied != 0 {
		t.Errorf("second run applied = %d, want 0 (rerun must be a safe no-op)", applied)
	}
	// The second run may re-create the idempotent tracking table, but no
	// migration SQL may re-execute.
	for _, tx := range pool.txs[firstRunTxs:] {
		for _, exec := range tx.execs {
			if strings.Contains(exec, "CREATE TABLE services") ||
				strings.Contains(exec, "ALTER TABLE services") {
				t.Errorf("rerun re-executed migration SQL: %q", exec)
			}
		}
	}
}
