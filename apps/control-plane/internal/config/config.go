// Package config loads and validates control-plane configuration.
// Configuration fails closed: the application refuses to start without a
// passcode and a session secret, both supplied outside source control.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// EnvPasscode is the environment variable holding the owner passcode.
	EnvPasscode = "PORTCULLIS_PASSCODE"
	// EnvSessionSecret is the environment variable holding the session
	// signing secret. It must be high-entropy and outside version control.
	EnvSessionSecret = "PORTCULLIS_SESSION_SECRET"
	// EnvDatabaseURL is the PostgreSQL connection URL of the fresh registry
	// database. Required for the Compose runtime; secrets stay out of
	// arguments and logs.
	EnvDatabaseURL = "PORTCULLIS_DATABASE_URL"

	// Optional path/address overrides (all default to the accepted
	// deployment boundaries).
	EnvGeneratedDir    = "PORTCULLIS_GENERATED_DIR"
	EnvCaddyConfigPath = "PORTCULLIS_CADDY_CONFIG"
	EnvCaddyAdmin      = "PORTCULLIS_CADDY_ADMIN"
	EnvCaddyLogPath    = "PORTCULLIS_CADDY_LOG"
	EnvBackupDir       = "PORTCULLIS_BACKUP_DIR"

	// minSecretLen keeps casual low-entropy secrets out.
	minSecretLen = 32
	// DefaultSessionTTL is the owner-session lifetime.
	DefaultSessionTTL = 12 * time.Hour

	// Safe production defaults preserved from the accepted deployment.
	DefaultGeneratedDir    = "/etc/caddy/sites/generated"
	DefaultCaddyConfigPath = "/etc/caddy/Caddyfile"
	DefaultCaddyAdmin      = "caddy:2019"
	DefaultCaddyLogPath    = "/var/log/caddy/portcullis.log"
	DefaultBackupDir       = "/backups"
)

// Config is the validated runtime configuration of the control plane.
type Config struct {
	Passcode      string
	SessionSecret string
	SessionTTL    time.Duration

	// DatabaseURL is the PostgreSQL URL of the fresh registry database.
	DatabaseURL string
	// GeneratedDir is the writable generated-Caddyfile directory.
	GeneratedDir string
	// CaddyConfigPath is the root Caddyfile (validate/reload target).
	CaddyConfigPath string
	// CaddyAdminAddress is the private Caddy admin endpoint.
	CaddyAdminAddress string
	// CaddyLogPath is the single Caddy log file (read-only reader).
	CaddyLogPath string
	// BackupDir is the read-only backup browser directory.
	BackupDir string
}

// Load reads the environment and returns an error when required values are
// missing, empty, or invalid. It never falls back to defaults for secrets.
func Load() (Config, error) {
	cfg := Config{SessionTTL: DefaultSessionTTL}

	cfg.Passcode = os.Getenv(EnvPasscode)
	if cfg.Passcode == "" {
		return Config{}, fmt.Errorf("%s must be set and non-empty", EnvPasscode)
	}

	cfg.SessionSecret = os.Getenv(EnvSessionSecret)
	if len(cfg.SessionSecret) < minSecretLen {
		return Config{}, fmt.Errorf("%s must be set to at least %d characters", EnvSessionSecret, minSecretLen)
	}

	return cfg, nil
}

// LoadRuntime reads the full Compose runtime configuration on top of Load:
// the registry database URL is mandatory (the runtime refuses to start
// without an explicit database) and every path/address defaults to the safe
// accepted deployment boundary. Relative directories are rejected so the
// control plane can never write generated Caddyfiles to an unintended
// location.
func LoadRuntime() (Config, error) {
	cfg, err := Load()
	if err != nil {
		return Config{}, err
	}

	cfg.DatabaseURL = strings.TrimSpace(os.Getenv(EnvDatabaseURL))
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("%s must be set and non-empty", EnvDatabaseURL)
	}
	if !strings.HasPrefix(cfg.DatabaseURL, "postgres://") && !strings.HasPrefix(cfg.DatabaseURL, "postgresql://") {
		return Config{}, fmt.Errorf("%s must be a postgres:// or postgresql:// URL", EnvDatabaseURL)
	}
	if strings.ContainsAny(cfg.DatabaseURL, "\n\r\x00") {
		return Config{}, fmt.Errorf("%s contains forbidden characters", EnvDatabaseURL)
	}

	specs := []struct {
		env      string
		ptr      *string
		fallback string
		absolute bool
	}{
		{EnvGeneratedDir, &cfg.GeneratedDir, DefaultGeneratedDir, true},
		{EnvCaddyConfigPath, &cfg.CaddyConfigPath, DefaultCaddyConfigPath, true},
		{EnvCaddyAdmin, &cfg.CaddyAdminAddress, DefaultCaddyAdmin, false},
		{EnvCaddyLogPath, &cfg.CaddyLogPath, DefaultCaddyLogPath, true},
		{EnvBackupDir, &cfg.BackupDir, DefaultBackupDir, true},
	}
	for _, spec := range specs {
		v := strings.TrimSpace(os.Getenv(spec.env))
		if v == "" {
			v = spec.fallback
		}
		if strings.ContainsAny(v, "\n\r\x00") {
			return Config{}, fmt.Errorf("%s contains forbidden characters", spec.env)
		}
		if spec.absolute && !filepath.IsAbs(v) {
			return Config{}, fmt.Errorf("%s must be an absolute path, got %q", spec.env, v)
		}
		*spec.ptr = v
	}

	return cfg, nil
}

// Validate mirrors Load's rules for already-constructed configs (used in
// tests and by future embedding hosts). It is exported so wiring code can
// fail closed on the same terms.
func (c Config) Validate() error {
	if c.Passcode == "" {
		return errors.New("passcode must be non-empty")
	}
	if len(c.SessionSecret) < minSecretLen {
		return fmt.Errorf("session secret must be at least %d characters", minSecretLen)
	}
	if c.SessionTTL <= 0 {
		return errors.New("session TTL must be positive")
	}
	return nil
}
