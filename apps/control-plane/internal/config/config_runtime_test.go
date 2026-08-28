package config

import (
	"strings"
	"testing"
)

func runtimeEnv() map[string]string {
	return map[string]string{
		EnvPasscode:                "correct-passcode",
		EnvSessionSecret:           "some-secret-at-least-32-bytes-long!!",
		EnvDatabaseURL:             "postgresql://owner:secret@portcullis_db:5432/portcullis",
		"PORTCULLIS_GENERATED_DIR": "/etc/caddy/sites/generated",
	}
}

func setRuntimeEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, key := range []string{
		EnvPasscode, EnvSessionSecret, EnvDatabaseURL,
		"PORTCULLIS_GENERATED_DIR", "PORTCULLIS_CADDY_CONFIG", "PORTCULLIS_CADDY_ADMIN",
		"PORTCULLIS_CADDY_LOG", "PORTCULLIS_BACKUP_DIR",
	} {
		t.Setenv(key, "")
	}
	for key, value := range env {
		t.Setenv(key, value)
	}
}

// TestLoadRuntimeFailsClosedOnMissingDatabaseURL pins the Slice 5 runtime
// boundary: the Compose control plane must refuse to start without an
// explicit registry database URL. Secrets and paths never get defaults that
// would silently point the runtime at the wrong database.
func TestLoadRuntimeFailsClosedOnMissingDatabaseURL(t *testing.T) {
	env := runtimeEnv()
	delete(env, EnvDatabaseURL)
	setRuntimeEnv(t, env)

	_, err := LoadRuntime()
	if err == nil {
		t.Fatal("expected error when PORTCULLIS_DATABASE_URL is missing, got nil")
	}
	if !strings.Contains(err.Error(), EnvDatabaseURL) {
		t.Errorf("error must name the missing variable, got: %s", err.Error())
	}
}

// TestLoadRuntimeFailsClosedOnNonPostgresDatabaseURL rejects values that
// are not PostgreSQL URLs so a stray legacy DATABASE_URL can never be
// silently interpreted as the registry DSN.
func TestLoadRuntimeFailsClosedOnNonPostgresDatabaseURL(t *testing.T) {
	for _, dsn := range []string{
		"mysql://user:pass@db:3306/app",
		"postgresql://host/db\ninjection",
		"not-a-url",
	} {
		env := runtimeEnv()
		env[EnvDatabaseURL] = dsn
		setRuntimeEnv(t, env)
		if _, err := LoadRuntime(); err == nil {
			t.Errorf("expected error for database URL %q, got nil", dsn)
		}
	}
}

// TestLoadRuntimeSafeDefaults pins the safe production defaults: the
// generated Caddyfile directory, Caddy boundaries, log path, and backup
// directory all default to the accepted deployment paths; the operator only
// has to supply the database URL and owner secrets.
func TestLoadRuntimeSafeDefaults(t *testing.T) {
	setRuntimeEnv(t, runtimeEnv())

	cfg, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime: %v", err)
	}
	if cfg.GeneratedDir != "/etc/caddy/sites/generated" {
		t.Errorf("GeneratedDir = %q, want the generated-sites default", cfg.GeneratedDir)
	}
	if cfg.CaddyConfigPath != "/etc/caddy/Caddyfile" {
		t.Errorf("CaddyConfigPath = %q, want the Caddyfile default", cfg.CaddyConfigPath)
	}
	if cfg.CaddyAdminAddress != "caddy:2019" {
		t.Errorf("CaddyAdminAddress = %q, want the private admin default", cfg.CaddyAdminAddress)
	}
	if cfg.CaddyLogPath != "/var/log/caddy/portcullis.log" {
		t.Errorf("CaddyLogPath = %q, want the Caddy log default", cfg.CaddyLogPath)
	}
	if cfg.BackupDir != "/backups" {
		t.Errorf("BackupDir = %q, want the read-only backup default", cfg.BackupDir)
	}
	if cfg.DatabaseURL != "postgresql://owner:secret@portcullis_db:5432/portcullis" {
		t.Errorf("DatabaseURL not carried through: %q", cfg.DatabaseURL)
	}
}

// TestLoadRuntimeOverridesAndRejectsUnsafePaths checks that explicit
// overrides win and that relative or control-character-bearing paths fail
// closed: the control plane must never write generated Caddyfiles outside
// an explicit absolute directory.
func TestLoadRuntimeOverridesAndRejectsUnsafePaths(t *testing.T) {
	env := runtimeEnv()
	env["PORTCULLIS_GENERATED_DIR"] = "/data/generated"
	env["PORTCULLIS_CADDY_ADMIN"] = "caddy:2019"
	env["PORTCULLIS_BACKUP_DIR"] = "/srv/backups"
	setRuntimeEnv(t, env)
	cfg, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime with overrides: %v", err)
	}
	if cfg.GeneratedDir != "/data/generated" || cfg.BackupDir != "/srv/backups" {
		t.Errorf("overrides must win, got GeneratedDir=%q BackupDir=%q", cfg.GeneratedDir, cfg.BackupDir)
	}

	env["PORTCULLIS_GENERATED_DIR"] = "sites/generated" // relative
	setRuntimeEnv(t, env)
	if _, err := LoadRuntime(); err == nil {
		t.Error("relative generated directory must be rejected")
	}

	env["PORTCULLIS_GENERATED_DIR"] = "/etc/caddy/sites/gen\ninjected"
	setRuntimeEnv(t, env)
	if _, err := LoadRuntime(); err == nil {
		t.Error("control characters in the generated directory must be rejected")
	}
}
