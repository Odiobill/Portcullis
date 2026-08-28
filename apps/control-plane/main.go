// Command control-plane is the Portcullis Go control plane (Slices 1–5).
// It serves the English login page, owner passcode login, the authenticated
// dashboard (service lifecycle, Caddy validate/reload, Caddy-log view,
// read-only backup browser, optional project-database provisioning, and
// session-authenticated migration dumps), and logout.
//
// Configuration fails closed (ADR-0002): the process exits unless
// PORTCULLIS_PASSCODE, PORTCULLIS_SESSION_SECRET, and PORTCULLIS_DATABASE_URL
// are set to acceptable values. The database URL is registry-only; secrets
// stay in the environment and never appear in logs or command arguments.
//
// The `migrate` subcommand applies the committed, versioned SQL migrations
// to the configured database and exits. It is the explicit Compose
// migration mechanism: the one-shot migrate service runs it before the
// control plane starts (dependency-gated), and version tracking makes
// reruns against the same fresh schema a safe no-op. There is no
// legacy-data compatibility path (ADR-0001).
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"portcullis/control-plane/internal/backups"
	"portcullis/control-plane/internal/caddyops"
	"portcullis/control-plane/internal/config"
	"portcullis/control-plane/internal/dump"
	"portcullis/control-plane/internal/migrate"
	"portcullis/control-plane/internal/provision"
	"portcullis/control-plane/internal/registry"
	"portcullis/control-plane/internal/server"
	"portcullis/control-plane/internal/session"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations
var migrationsFS embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := runMigrate(); err != nil {
			slog.Error("control-plane: migration failed", "err", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("control-plane: startup failed", "err", err)
		os.Exit(1)
	}
}

// migrationFS returns the embedded migrations directory as a flat fs.FS.
func migrationFS() (fs.FS, error) {
	return fs.Sub(migrationsFS, "migrations")
}

// runMigrate applies the committed migrations and exits. It needs only the
// database URL: it must never require owner secrets, so the one-shot
// migrate service can run with the narrowest environment.
func runMigrate() error {
	dsn := os.Getenv(config.EnvDatabaseURL)
	if dsn == "" {
		return fmt.Errorf("%s must be set and non-empty", config.EnvDatabaseURL)
	}
	sub, err := migrationFS()
	if err != nil {
		return fmt.Errorf("embedded migrations: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to registry database: %w", err)
	}
	defer pool.Close()
	applied, err := migrate.Apply(ctx, pool, sub)
	if err != nil {
		return err
	}
	slog.Info("control-plane: registry schema is up to date", "applied", applied)
	return nil
}

func run() error {
	cfg, err := config.LoadRuntime()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}

	sessions, err := session.NewManager(cfg.SessionSecret, cfg.SessionTTL)
	if err != nil {
		return fmt.Errorf("session manager: %w", err)
	}

	// Registry database: parsed once so the pg_dump environment hook can be
	// derived from the same DSN. Connection failures are fatal: the runtime
	// never serves a half-wired registry.
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database URL: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("connect to registry database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("registry database is not reachable: %w", err)
	}

	// Caddy boundaries: fixed validate/reload commands and a read-only log
	// reader; never the Docker socket or the admin API directly.
	operator, err := caddyops.NewOperator(caddyops.Config{
		ConfigPath:   cfg.CaddyConfigPath,
		AdminAddress: cfg.CaddyAdminAddress,
		LogPath:      cfg.CaddyLogPath,
	})
	if err != nil {
		return fmt.Errorf("caddy operator: %w", err)
	}
	logReader, err := caddyops.NewLogReader(cfg.CaddyLogPath)
	if err != nil {
		return fmt.Errorf("caddy log reader: %w", err)
	}

	// Generated/manual Caddyfile boundary: the store is scoped to the
	// generated directory only; sites/manual is never part of its config.
	store := registry.NewStore(cfg.GeneratedDir, operator)
	repo := registry.NewPostgresRepository(pool)
	lifecycle := registry.NewLifecycle(repo, store, nil)
	// Opt-in project database provisioning runs through the same pool; the
	// generated password is shown once and never persisted or logged.
	lifecycle.Provisioner = provision.NewPostgresAdmin(pool)

	browser, err := backups.NewBrowser(cfg.BackupDir)
	if err != nil {
		return fmt.Errorf("backup browser: %w", err)
	}

	// Migration dumps: fixed-argument pg_dump with the registry host/user;
	// credential material (if the DSN carries one) flows only through the
	// child environment via the hook, never through arguments.
	dumper, err := dump.New(dump.Config{
		DBHost: poolCfg.ConnConfig.Host,
		DBUser: poolCfg.ConnConfig.User,
		Env: func() []string {
			if poolCfg.ConnConfig.Password == "" {
				return os.Environ()
			}
			return append(os.Environ(), "PGPASSWORD="+poolCfg.ConnConfig.Password)
		},
	})
	if err != nil {
		return fmt.Errorf("dump boundary: %w", err)
	}

	addr := os.Getenv("PORTCULLIS_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := server.New(server.Config{
		Passcode:       cfg.Passcode,
		SessionManager: sessions,
		Lifecycle:      lifecycle,
		ReloadOperator: operator,
		LogReader:      logReader,
		Backups:        browser,
		Dumper:         dumper,
		Pinger:         pool,
	})
	slog.Info("control-plane listening", "addr", addr)
	return http.ListenAndServe(addr, srv.Handler())
}
