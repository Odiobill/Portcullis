package dump

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCommander records process starts without executing anything.
type fakeCommander struct {
	mu          sync.Mutex
	starts      []startRecord
	startErr    error
	stdout      io.ReadCloser
	waitErr     error
	cancelCalls int
}

type startRecord struct {
	name string
	args []string
	env  []string
}

func (f *fakeCommander) Start(_ context.Context, name string, args []string, env []string) (io.ReadCloser, func() error, func(), error) {
	f.mu.Lock()
	f.starts = append(f.starts, startRecord{name: name, args: args, env: env})
	f.mu.Unlock()
	if f.startErr != nil {
		return nil, nil, nil, f.startErr
	}
	if f.stdout == nil {
		f.stdout = io.NopCloser(strings.NewReader("dump-bytes"))
	}
	return f.stdout,
		func() error { return f.waitErr },
		func() {
			f.mu.Lock()
			f.cancelCalls++
			f.mu.Unlock()
		},
		nil
}

func testDumper(t *testing.T, cmd Commander, now func() time.Time) *Dumper {
	t.Helper()
	d, err := New(Config{
		DBHost:    "postgres.internal",
		DBUser:    "dump_user",
		Commander: cmd,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

func TestArgsExactConstruction(t *testing.T) {
	d := testDumper(t, &fakeCommander{}, nil)
	want := []string{"--host", "postgres.internal", "--user", "dump_user", "-Fc", "--no-password", "portcullis_abc123"}
	got := d.Args("portcullis_abc123")
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args = %q, want %q", got, want)
	}
	if strings.Contains(strings.Join(got, " "), "password=") {
		t.Error("arguments must not carry a password")
	}
}

func TestNewDumperRejectsUnsafeConfiguration(t *testing.T) {
	for _, cfg := range []Config{
		{DBHost: "", DBUser: "u"},
		{DBHost: "h", DBUser: ""},
		{DBHost: "h\ninject", DBUser: "u"},
		{DBHost: "h", DBUser: "u\x00"},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("unsafe configuration %+v accepted", cfg)
		}
	}
}

func TestLimiterOneDumpPerServicePerWindow(t *testing.T) {
	l := NewLimiter(5 * time.Minute)
	now := time.Now()
	if _, ok := l.Allow("svc-a", now); !ok {
		t.Fatal("first dump must be allowed")
	}
	if _, ok := l.Allow("svc-a", now.Add(4*time.Minute)); ok {
		t.Error("second dump within the window must be rejected")
	}
	if _, ok := l.Allow("svc-b", now); !ok {
		t.Error("per-service isolation: another service must be allowed")
	}
	if _, ok := l.Allow("svc-a", now.Add(5*time.Minute).Add(time.Second)); !ok {
		t.Error("after the window a new dump must be allowed")
	}
}

func TestLimiterRejectionDoesNotConsumeQuota(t *testing.T) {
	l := NewLimiter(5 * time.Minute)
	t0 := time.Now()
	res, ok := l.Allow("svc-a", t0)
	if !ok {
		t.Fatal("first dump must be allowed")
	}
	// Rejected attempts (within the window) must not extend the window.
	if _, ok := l.Allow("svc-a", t0.Add(time.Minute)); ok {
		t.Fatal("within-window attempt must be rejected")
	}
	if _, ok := l.Allow("svc-a", t0.Add(time.Minute)); ok {
		t.Fatal("second within-window attempt must be rejected")
	}
	// A newer slot is acquired at t0+5m+1s.
	newer, ok := l.Allow("svc-a", t0.Add(5*time.Minute).Add(time.Second))
	if !ok {
		t.Fatal("newer slot must be acquirable after the window")
	}
	// Releasing the stale t0 reservation must be a no-op: the newer slot
	// stays intact and still rate-limits within its own window.
	l.Release(res)
	if _, ok := l.Allow("svc-a", newer.acquired.Add(2*time.Second)); ok {
		t.Fatal("stale release must not clear the newer slot; within-window request must be rejected")
	}
}

var errStartFailure = errors.New("pg_dump: binary not found")

func TestStartRateLimitsBeforeProcess(t *testing.T) {
	cmd := &fakeCommander{}
	now := time.Unix(1785200000, 0)
	d := testDumper(t, cmd, func() time.Time { return now })

	if _, _, _, err := d.Start(context.Background(), "svc-a", "portcullis_a"); err != nil {
		t.Fatalf("first start: %v", err)
	}
	err := func() error {
		_, _, _, err := d.Start(context.Background(), "svc-a", "portcullis_a")
		return err
	}()
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
	if len(cmd.starts) != 1 {
		t.Errorf("process starts = %d, want 1 (rate limit must precede any process action)", len(cmd.starts))
	}
	if !strings.Contains(err.Error(), "five minutes") {
		t.Errorf("error must be bounded English guidance, got: %s", err.Error())
	}
}

func TestStartPassesExactCommandAndEnv(t *testing.T) {
	cmd := &fakeCommander{}
	now := time.Unix(1785200000, 0)
	env := []string{"PATH=/usr/bin", "PGPASSWORD=test-secret"}
	d, err := New(Config{
		DBHost:    "postgres.internal",
		DBUser:    "dump_user",
		Commander: cmd,
		Now:       func() time.Time { return now },
		Env:       func() []string { return env },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, _, _, err := d.Start(context.Background(), "svc-a", "portcullis_a"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rec := cmd.starts[0]
	if rec.name != "pg_dump" {
		t.Errorf("executable = %q, want pg_dump (no shell)", rec.name)
	}
	want := []string{"--host", "postgres.internal", "--user", "dump_user", "-Fc", "--no-password", "portcullis_a"}
	if strings.Join(rec.args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args = %q, want %q", rec.args, want)
	}
	if strings.Join(rec.env, "\x00") != strings.Join(env, "\x00") {
		t.Errorf("env = %q, want %q", rec.env, env)
	}
}

func TestStartFailureSurfacedBeforeStreaming(t *testing.T) {
	cmd := &fakeCommander{startErr: errors.New("pg_dump: binary not found")}
	d := testDumper(t, cmd, nil)
	_, _, _, err := d.Start(context.Background(), "svc-a", "portcullis_a")
	if err == nil {
		t.Fatal("start failure must be surfaced")
	}
	if !strings.Contains(err.Error(), "pg_dump: binary not found") {
		t.Errorf("bounded start error must retain the cause, got: %s", err.Error())
	}
}

// TestStartFailureReleasesQuota pins that a failed process start is a
// rejected request: the five-minute slot is released so an immediate retry
// reaches the commander again instead of ErrRateLimited.
func TestStartFailureReleasesQuota(t *testing.T) {
	cmd := &fakeCommander{startErr: errStartFailure}
	d := testDumper(t, cmd, nil)

	_, _, _, err1 := d.Start(context.Background(), "svc-a", "portcullis_a")
	if err1 == nil {
		t.Fatal("first start must fail")
	}
	if !errors.Is(err1, errStartFailure) {
		t.Fatalf("primary start failure not preserved: %v", err1)
	}

	_, _, _, err2 := d.Start(context.Background(), "svc-a", "portcullis_a")
	if errors.Is(err2, ErrRateLimited) {
		t.Fatal("failed start consumed the rate quota; immediate retry must reach the commander")
	}
	if err2 == nil {
		t.Fatal("second start must fail with the injected start error")
	}
	if len(cmd.starts) != 2 {
		t.Fatalf("commander starts = %d, want 2", len(cmd.starts))
	}
}
