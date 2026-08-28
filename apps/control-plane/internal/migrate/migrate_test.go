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
		// Carry the version state forward so consecutive Applies observe
		// previously recorded versions (rerun semantics).
		for k, v := range f.txs[len(f.txs)-1].applied {
			tx.applied[k] = v
		}
	}
	f.txs = append(f.txs, tx)
	return tx, nil
}

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

// TestApplyRunsPendingMigrationsInOrderAndRecordsVersions pins that a fresh
// database receives every committed migration exactly once, in filename
// order, each inside its own transaction that records the applied version.
func TestApplyRunsPendingMigrationsInOrderAndRecordsVersions(t *testing.T) {
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
	tx := pool.txs[0]
	// Statement order: idempotent tracking setup, then the two migrations.
	if len(tx.execs) != 3 {
		t.Fatalf("executed statements = %d, want 3 (tracking setup + 2 migrations)", len(tx.execs))
	}
	if !strings.Contains(tx.execs[0], "CREATE TABLE IF NOT EXISTS schema_migrations") {
		t.Errorf("version tracking table must be created before use, got %q", tx.execs[0])
	}
	if !strings.Contains(tx.execs[1], "CREATE TABLE services") {
		t.Errorf("first migration must run first, got %q", tx.execs[1])
	}
	if !strings.Contains(tx.execs[2], "CREATE INDEX idx_demo") {
		t.Errorf("second migration must run second, got %q", tx.execs[2])
	}
	if len(tx.applied) != 2 {
		t.Errorf("recorded versions = %d, want 2", len(tx.applied))
	}
	if !tx.applied["000001_create.up.sql"] || !tx.applied["000002_add_index.up.sql"] {
		t.Errorf("versions must be recorded under their filenames, got %v", tx.applied)
	}
	if !strings.Contains(tx.execs[0], "CREATE TABLE IF NOT EXISTS schema_migrations") {
		t.Errorf("version tracking table must be created before use, got %q", tx.execs[0])
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Errorf("successful apply must commit exactly once (commits=%d rollbacks=%d)", tx.commits, tx.rollbacks)
	}
}

// TestApplySkipsAlreadyAppliedMigrations pins rerun safety: Compose restarts
// and retries must never re-execute an already applied migration against
// the same fresh schema.
func TestApplySkipsAlreadyAppliedMigrations(t *testing.T) {
	pool := &fakePool{}
	fsys := migrationsFS(map[string]string{
		"000001_create.up.sql": "CREATE TABLE services (id TEXT PRIMARY KEY);",
		"000002_add.up.sql":    "ALTER TABLE services ADD COLUMN note TEXT;",
	})
	if _, err := Apply(context.Background(), pool, fsys); err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	// Second run against the same database state (fake carries versions).
	applied, err := Apply(context.Background(), pool, fsys)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if applied != 0 {
		t.Errorf("second run applied = %d, want 0 (rerun must be a safe no-op)", applied)
	}
	// Only the idempotent IF NOT EXISTS tracking-table statement may run
	// again; no migration SQL may re-execute.
	if len(pool.txs[1].execs) != 1 || !strings.Contains(pool.txs[1].execs[0], "CREATE TABLE IF NOT EXISTS schema_migrations") {
		t.Errorf("second run must only re-run the idempotent tracking setup, got %v", pool.txs[1].execs)
	}
}

// TestApplyFailsClosedAndRollsBackOnError pins that a failing migration
// aborts the apply, rolls back, records no version, and stops before any
// later migration.
func TestApplyFailsClosedAndRollsBackOnError(t *testing.T) {
	pool := &fakePool{}
	fsys := migrationsFS(map[string]string{
		"000001_bad.up.sql":   "CREATE TABLE broken ( TO BE OR NOT TO BE",
		"000002_never.up.sql": "SELECT 1;",
	})

	// Simulate the failure at the DB level for the broken statement.
	pool.failOnTx = "broken"
	applied, err := Apply(context.Background(), pool, fsys)
	if err == nil {
		t.Fatal("expected error for a failing migration, got nil")
	}
	if applied != 0 {
		t.Errorf("applied = %d, want 0 on failure", applied)
	}
	tx := pool.txs[0]
	if tx.rollbacks != 1 || tx.commits != 0 {
		t.Errorf("failure must roll back exactly once (commits=%d rollbacks=%d)", tx.commits, tx.rollbacks)
	}
	if _, ok := tx.applied["000001_bad.up.sql"]; ok {
		t.Error("failed migration must not be recorded as applied")
	}
	for _, exec := range tx.execs {
		if strings.Contains(exec, "SELECT 1;") {
			t.Error("execution must stop at the first failing migration")
		}
	}
}
