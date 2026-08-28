// Package dump implements the session-authenticated, per-service
// rate-limited migration dump boundary. The process invocation is injected
// (no test ever executes pg_dump), the argument array is fixed and
// shell-free, the database name always comes from the injected registry —
// never from request input — and any credential stays out of arguments
// entirely: credential material, if a deployment supplies one, flows only
// through the environment hook. The in-memory rate limiter resets on
// process restart.
package dump

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ErrRateLimited is returned when the service already had a dump within the
// rate-limit window.
var ErrRateLimited = errors.New("a dump for this service was already started; at most one dump per service is allowed every five minutes")

// executable is fixed: only the pg_dump binary may be started.
const executable = "pg_dump"

// rateWindow is the per-service minimum interval between dumps.
const rateWindow = 5 * time.Minute

// Limiter allows at most one dump per service per window. It is
// concurrency-safe and in-memory: a process restart resets it. Rejected
// attempts never mutate state, so they never extend the window.
type Limiter struct {
	mu     sync.Mutex
	window time.Duration
	last   map[string]time.Time
}

// NewLimiter returns a limiter with the given per-service window.
func NewLimiter(window time.Duration) *Limiter {
	return &Limiter{window: window, last: make(map[string]time.Time)}
}

// Allow reports whether serviceID may start a dump at now, consuming the
// slot when allowed. Rejected attempts never consume or extend quota.
func (l *Limiter) Allow(serviceID string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if last, ok := l.last[serviceID]; ok && now.Sub(last) < l.window {
		return false
	}
	l.last[serviceID] = now
	return true
}

// Release returns the service's slot after a failed start: a start failure
// is a rejected request and must not consume the five-minute window.
func (l *Limiter) Release(serviceID string) {
	l.mu.Lock()
	delete(l.last, serviceID)
	l.mu.Unlock()
}

// Commander starts the dump process. Production uses os/exec with the
// fixed pg_dump binary; tests inject fakes and never execute anything.
type Commander interface {
	// Start starts the command (no shell) and returns its stdout stream, a
	// completion waiter, a termination hook for client disconnects, and an
	// error when the process could not be started.
	Start(ctx context.Context, name string, args []string, env []string) (stdout io.ReadCloser, wait func() error, cancel func(), err error)
}

// Config carries the non-secret dump configuration.
type Config struct {
	// DBHost and DBUser are the configured PostgreSQL host/user for dumps.
	DBHost string
	DBUser string
	// Commander overrides process execution; nil selects the os/exec
	// runner. Injected for tests.
	Commander Commander
	// Now overrides the wall clock for the limiter; nil means time.Now.
	Now func() time.Time
	// Env overrides the child environment source; nil means os.Environ.
	Env func() []string
}

// Dumper resolves rate-limited, session-authenticated dump requests into
// pg_dump process starts.
type Dumper struct {
	dbHost    string
	dbUser    string
	limiter   *Limiter
	commander Commander
	now       func() time.Time
	env       func() []string
}

// New validates the configuration and returns a Dumper.
func New(cfg Config) (*Dumper, error) {
	for name, v := range map[string]string{"database host": cfg.DBHost, "database user": cfg.DBUser} {
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("dump: %s must not be empty", name)
		}
		if strings.ContainsAny(v, "\n\r\x00") {
			return nil, fmt.Errorf("dump: %s contains forbidden characters", name)
		}
	}
	commander := cfg.Commander
	if commander == nil {
		commander = execCommander{}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	env := cfg.Env
	if env == nil {
		env = os.Environ
	}
	return &Dumper{
		dbHost:    cfg.DBHost,
		dbUser:    cfg.DBUser,
		limiter:   NewLimiter(rateWindow),
		commander: commander,
		now:       now,
		env:       env,
	}, nil
}

// Args builds the fixed, shell-free argument array for a database name:
// `pg_dump --host <host> --user <user> -Fc --no-password <dbname>`. The
// database name is repository-derived and identifiers are validated
// upstream; a password never appears in arguments.
func (d *Dumper) Args(dbName string) []string {
	return []string{
		"--host", d.dbHost,
		"--user", d.dbUser,
		"-Fc",
		"--no-password",
		dbName,
	}
}

// Start consumes the service's rate slot and starts the pg_dump process for
// the repository-derived database name. It returns the stdout stream, the
// process waiter, and the termination hook.
func (d *Dumper) Start(ctx context.Context, serviceID, dbName string) (io.ReadCloser, func() error, func(), error) {
	if !d.limiter.Allow(serviceID, d.now()) {
		return nil, nil, nil, ErrRateLimited
	}
	env := d.env()
	stdout, wait, cancel, err := d.commander.Start(ctx, executable, d.Args(dbName), env)
	if err != nil {
		// A failed start is a rejected request: release the slot so the
		// immediate retry reaches the process boundary.
		d.limiter.Release(serviceID)
		return nil, nil, nil, fmt.Errorf("dump: pg_dump could not be started: %w", err)
	}
	return stdout, wait, cancel, nil
}

// execCommander is the default Commander over os/exec. The command is
// executed directly (no shell); credential material, when a deployment
// supplies one, flows only through the environment — never arguments.
type execCommander struct{}

func (execCommander) Start(ctx context.Context, name string, args []string, env []string) (io.ReadCloser, func() error, func(), error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	wait := func() error { return cmd.Wait() }
	cancel := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	return stdout, wait, cancel, nil
}
