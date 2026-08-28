// Package migrate applies the committed, versioned SQL migrations to the
// fresh Portcullis registry database. It is the explicit migration
// mechanism used by the Compose one-shot migrate service: version tracking
// makes reruns against the same fresh schema a safe no-op, and every
// migration runs inside its own transaction so a partial failure can never
// leave an unrecorded half-applied schema. No legacy Prisma/data
// compatibility path exists (ADR-0001).
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
// filename order and records its version in schema_migrations. Already
// recorded versions are skipped, so Apply is safe to rerun. It returns the
// number of migrations applied.
func Apply(ctx context.Context, pool Pool, fsys fs.FS) (int, error) {
	names, err := upMigrations(fsys)
	if err != nil {
		return 0, fmt.Errorf("migrate: list migrations: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrate: begin: %w", err)
	}
	defer tx.Rollback(ctx) // no-op after commit

	if _, err := tx.Exec(ctx, createTrackingSQL); err != nil {
		return 0, fmt.Errorf("migrate: create version tracking: %w", err)
	}

	applied := 0
	for _, name := range names {
		var exists bool
		if err := tx.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", name,
		).Scan(&exists); err != nil {
			return applied, fmt.Errorf("migrate: check version %s: %w", name, err)
		}
		if exists {
			continue
		}
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return applied, fmt.Errorf("migrate: read %s: %w", name, err)
		}
		// pgx executes a no-argument Exec over the simple query protocol,
		// which permits the multi-statement migration files as committed.
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return applied, fmt.Errorf("migrate: apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)", name,
		); err != nil {
			return applied, fmt.Errorf("migrate: record %s: %w", name, err)
		}
		applied++
	}

	if err := tx.Commit(ctx); err != nil {
		return applied, fmt.Errorf("migrate: commit: %w", err)
	}
	return applied, nil
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
