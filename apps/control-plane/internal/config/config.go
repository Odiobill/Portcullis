// Package config loads and validates control-plane configuration.
// Configuration fails closed: the application refuses to start without a
// passcode and a session secret, both supplied outside source control.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	// EnvPasscode is the environment variable holding the owner passcode.
	EnvPasscode = "PORTCULLIS_PASSCODE"
	// EnvSessionSecret is the environment variable holding the session
	// signing secret. It must be high-entropy and outside version control.
	EnvSessionSecret = "PORTCULLIS_SESSION_SECRET"

	// minSecretLen keeps casual low-entropy secrets out.
	minSecretLen = 32
	// DefaultSessionTTL is the owner-session lifetime.
	DefaultSessionTTL = 12 * time.Hour
)

// Config is the validated runtime configuration of the control plane.
type Config struct {
	Passcode      string
	SessionSecret string
	SessionTTL    time.Duration
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
