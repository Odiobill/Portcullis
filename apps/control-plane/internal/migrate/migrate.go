// Package migrate applies the committed, versioned SQL migrations to the
// fresh Portcullis registry database. It is the explicit migration
// mechanism used by the Compose one-shot migrate service: version tracking
// makes reruns against the same fresh schema a safe no-op, and every
// migration runs inside its own transaction — together with its version
// record — so a partial failure can never roll back an already committed
// earlier migration, and a failed migration leaves no half-applied schema.
// No legacy Prisma/data compatibility path exists (ADR-0001).
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Pool is the minimal connection surface the migrator needs. *pgxpool.Pool
// satisfies it; tests inject fakes and never connect to PostgreSQL.
type Pool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Apply executes every not-yet-recorded *.up.sql migration in fsys in
// filename order. Version tracking is initialized in its own short
// transaction; then EACH pending migration runs in its own transaction that
// also records its version, so the migration SQL and its version marker
// commit atomically together. An already-committed migration stays recorded
// and untouched if a later migration fails, a failure rolls back only the
// failing migration and stops the run, and already-recorded versions are
// skipped — making Apply safe to rerun. It returns the number of migrations
// applied.
func Apply(ctx context.Context, pool Pool, fsys fs.FS) (int, error) {
	names, err := upMigrations(fsys)
	if err != nil {
		return 0, fmt.Errorf("migrate: list migrations: %w", err)
	}

	// Tracking initialization in its own transaction: IF NOT EXISTS keeps
	// it safe on a fresh database and a rerun alike.
	initTx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrate: begin tracking setup: %w", err)
	}
	if _, err := initTx.Exec(ctx, createTrackingSQL); err != nil {
		_ = initTx.Rollback(ctx)
		return 0, fmt.Errorf("migrate: create version tracking: %w", err)
	}
	if err := initTx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("migrate: commit tracking setup: %w", err)
	}

	applied := 0
	for _, name := range names {
		wasApplied, err := applyOne(ctx, pool, fsys, name)
		if err != nil {
			return applied, err
		}
		if wasApplied {
			applied++
		}
	}
	return applied, nil
}

// applyOne runs exactly one migration inside exactly one transaction: the
// version check, the migration SQL, and the version record all share that
// transaction. An already-recorded version commits an empty read-only
// transaction (a skip); a failure rolls back only this migration. The
// boolean reports whether the migration was applied.
func applyOne(ctx context.Context, pool Pool, fsys fs.FS, name string) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("migrate: begin %s: %w", name, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx) // no-op after commit
		}
	}()

	var exists bool
	if err := tx.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", name,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("migrate: check version %s: %w", name, err)
	}
	if exists {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("migrate: commit skip %s: %w", name, err)
		}
		committed = true
		return false, nil
	}

	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		return false, fmt.Errorf("migrate: read %s: %w", name, err)
	}
	// pgx executes a no-argument Exec over the simple query protocol,
	// which permits the multi-statement migration files as committed.
	if _, err := tx.Exec(ctx, string(body)); err != nil {
		return false, fmt.Errorf("migrate: apply %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO schema_migrations (version) VALUES ($1)", name,
	); err != nil {
		return false, fmt.Errorf("migrate: record %s: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("migrate: commit %s: %w", name, err)
	}
	committed = true
	return true, nil
}

// upMigrations returns the sorted *.up.sql filenames in fsys.
func upMigrations(fsys fs.FS) ([]string, error) {
	matches, err := fs.Glob(fsys, "*.up.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// createTrackingSQL is the version-tracking table. IF NOT EXISTS keeps the
// first statement rerun-safe on its own.
const createTrackingSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// isVersionInsert reports whether the statement records a version.
func isVersionInsert(sql string) bool {
	return strings.HasPrefix(sql, "INSERT INTO schema_migrations")
}
