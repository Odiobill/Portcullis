// Package caddyops provides the production Caddy command boundary and the
// bounded read-only Caddy-log reader. The operator shells out to the caddy
// binary (never a Docker socket or the admin API directly) with fixed,
// fully specified arguments; tests inject a CommandRunner so no command
// ever executes. Configuration holds only non-secret paths/addresses and
// fails closed on unsafe values.
package caddyops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Safe defaults preserved from the current deployment.
const (
	DefaultConfigPath   = "/etc/caddy/Caddyfile"
	DefaultAdminAddress = "caddy:2019"
	DefaultLogPath      = "/var/log/caddy/portcullis.log"

	// Executable is fixed: the operator may only ever run the caddy binary.
	Executable = "caddy"

	// maxErrorBytes bounds command stderr/stdout echoed in errors.
	maxErrorBytes = 1200
)

// CommandRunner executes a command and returns bounded output.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
}

// Config carries the non-secret Caddy configuration. Empty values select
// the safe defaults; explicit unsafe values are rejected.
type Config struct {
	// ConfigPath is the Caddyfile path (default /etc/caddy/Caddyfile).
	ConfigPath string
	// AdminAddress is the private Caddy admin endpoint (default caddy:2019).
	AdminAddress string
	// LogPath is the single Caddy log file (default /var/log/caddy/portcullis.log).
	LogPath string
	// Runner overrides command execution; nil selects the real os/exec
	// runner for the fixed caddy binary. Injected for tests.
	Runner CommandRunner
}

// Operator runs the exact Caddy validate/reload operations and satisfies
// registry.Operator.
type Operator struct {
	configPath   string
	adminAddress string
	logPath      string
	runner       CommandRunner
}

// NewOperator validates the configuration and returns an Operator.
func NewOperator(cfg Config) (*Operator, error) {
	op := &Operator{
		configPath:   DefaultConfigPath,
		adminAddress: DefaultAdminAddress,
		logPath:      DefaultLogPath,
		runner:       cfg.Runner,
	}
	// Explicit non-empty overrides win; whitespace-only values are unsafe.
	if cfg.ConfigPath != "" {
		if err := validateValue("config path", cfg.ConfigPath); err != nil {
			return nil, err
		}
		op.configPath = cfg.ConfigPath
	}
	if cfg.AdminAddress != "" {
		if err := validateValue("admin address", cfg.AdminAddress); err != nil {
			return nil, err
		}
		op.adminAddress = cfg.AdminAddress
	}
	if cfg.LogPath != "" {
		if err := validateValue("log path", cfg.LogPath); err != nil {
			return nil, err
		}
		op.logPath = cfg.LogPath
	}
	if op.runner == nil {
		op.runner = execRunner{}
	}
	return op, nil
}

// validateValue rejects empty/whitespace values and argument-injection
// characters (newlines, NUL) in configured values.
func validateValue(kind, v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("caddyops: %s must not be empty", kind)
	}
	if strings.ContainsAny(v, "\n\r\x00") {
		return fmt.Errorf("caddyops: %s contains forbidden characters", kind)
	}
	return nil
}

// Validate runs `caddy validate --config <path> --adapter caddyfile`.
func (o *Operator) Validate() error {
	_, stderr, err := o.runner.Run(context.Background(), Executable,
		"validate", "--config", o.configPath, "--adapter", "caddyfile")
	if err != nil {
		return fmt.Errorf("caddyops: caddy validate failed: %w: %s", err, boundOutput(stderr))
	}
	return nil
}

// Reload runs `caddy reload --config <path> --adapter caddyfile --address
// <admin>`. Callers (the registry Store) never invoke it after a failed
// validation.
func (o *Operator) Reload() error {
	_, stderr, err := o.runner.Run(context.Background(), Executable,
		"reload", "--config", o.configPath, "--adapter", "caddyfile", "--address", o.adminAddress)
	if err != nil {
		return fmt.Errorf("caddyops: caddy reload failed: %w: %s", err, boundOutput(stderr))
	}
	return nil
}

// LogPath returns the configured Caddy log path (for wiring the LogReader).
func (o *Operator) LogPath() string { return o.logPath }

// boundOutput trims command output to its tail so errors stay useful but
// bounded.
func boundOutput(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return "(no output)"
	}
	if len(out) > maxErrorBytes {
		out = out[len(out)-maxErrorBytes:]
	}
	return out
}

// execRunner is the default CommandRunner over os/exec.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

var errCaddyUnavailable = errors.New("caddyops: caddy command failed")
