package caddyops

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner records commands and never executes anything.
type fakeRunner struct {
	calls       []cmdCall
	runErr      error
	stderr      string
	stdout      string
	failOn      string // command name that triggers runErr ("" = never)
	failFromArg string // fail only when args contain this value
}

type cmdCall struct {
	name string
	args []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	f.calls = append(f.calls, cmdCall{name: name, args: args})
	if f.runErr != nil && (f.failOn == "" || f.failOn == name) && (f.failFromArg == "" || containsArg(args, f.failFromArg)) {
		return f.stdout, f.stderr, f.runErr
	}
	return f.stdout, f.stderr, nil
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func testOperator(t *testing.T, runner *fakeRunner) *Operator {
	t.Helper()
	op, err := NewOperator(Config{Runner: runner})
	if err != nil {
		t.Fatalf("NewOperator: %v", err)
	}
	return op
}

func TestValidateRunsExactArguments(t *testing.T) {
	runner := &fakeRunner{}
	op := testOperator(t, runner)

	if err := op.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("validate calls = %d, want 1", len(runner.calls))
	}
	call := runner.calls[0]
	if call.name != "caddy" {
		t.Errorf("executable = %q, want caddy", call.name)
	}
	want := []string{"validate", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"}
	if strings.Join(call.args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("validate args = %q, want %q", call.args, want)
	}
}

func TestReloadRunsExactArguments(t *testing.T) {
	runner := &fakeRunner{}
	op := testOperator(t, runner)

	if err := op.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("reload calls = %d, want 1", len(runner.calls))
	}
	call := runner.calls[0]
	if call.name != "caddy" {
		t.Errorf("executable = %q, want caddy", call.name)
	}
	want := []string{"reload", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile", "--address", "caddy:2019"}
	if strings.Join(call.args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("reload args = %q, want %q", call.args, want)
	}
}

func TestCustomPathsChangeArguments(t *testing.T) {
	runner := &fakeRunner{}
	op, err := NewOperator(Config{
		ConfigPath:   "/srv/portcullis/Caddyfile",
		AdminAddress: "caddy.internal:2019",
		Runner:       runner,
	})
	if err != nil {
		t.Fatalf("NewOperator: %v", err)
	}
	if err := op.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	want := []string{"reload", "--config", "/srv/portcullis/Caddyfile", "--adapter", "caddyfile", "--address", "caddy.internal:2019"}
	if strings.Join(runner.calls[0].args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("reload args = %q, want %q", runner.calls[0].args, want)
	}
}

func TestValidateFailureErrorIsBoundedAndUseful(t *testing.T) {
	runner := &fakeRunner{
		runErr: errors.New("exit status 1"),
		stderr: strings.Repeat("x", 4000) + "Error: adapting config, last error: something specific",
	}
	op := testOperator(t, runner)
	err := op.Validate()
	if err == nil {
		t.Fatal("validate failure must be surfaced")
	}
	msg := err.Error()
	if len(msg) > 2000 {
		t.Errorf("error message too large (%d bytes): must be bounded", len(msg))
	}
	if !strings.Contains(msg, "something specific") {
		t.Errorf("bounded error must retain the useful tail, got: %s", msg)
	}
}

func TestReloadFailureSurfaced(t *testing.T) {
	runner := &fakeRunner{runErr: errors.New("exit status 1"), stderr: "reload rejected"}
	op := testOperator(t, runner)
	err := op.Reload()
	if err == nil {
		t.Fatal("reload failure must be surfaced")
	}
	if !strings.Contains(err.Error(), "reload rejected") {
		t.Errorf("reload error must include bounded stderr, got: %s", err.Error())
	}
}

func TestNewOperatorAppliesSafeDefaults(t *testing.T) {
	op, err := NewOperator(Config{Runner: &fakeRunner{}})
	if err != nil {
		t.Fatalf("NewOperator: %v", err)
	}
	if op.configPath != "/etc/caddy/Caddyfile" {
		t.Errorf("config path = %q", op.configPath)
	}
	if op.adminAddress != "caddy:2019" {
		t.Errorf("admin address = %q", op.adminAddress)
	}
}

func TestNewOperatorRejectsUnsafeConfiguration(t *testing.T) {
	cases := []Config{
		{ConfigPath: "bad\npath"},
		{ConfigPath: "bad\x00path"},
		{AdminAddress: "caddy:2019 extra\n"},
		{AdminAddress: "bad\x00admin"},
		{LogPath: "bad\nlog"},
		{LogPath: "bad\x00log"},
	}
	for i, cfg := range cases {
		if _, err := NewOperator(cfg); err == nil {
			t.Errorf("case %d: unsafe configuration %+v accepted", i, cfg)
		}
	}
}

func TestNewOperatorRejectsEmptyExplicitValues(t *testing.T) {
	// An explicitly empty value is unsafe; the default only applies when
	// unset. Model "unset" with the zero value vs. quoted-empty override by
	// checking the rejected cases for whitespace-only paths.
	for _, cfg := range []Config{{ConfigPath: "   "}, {AdminAddress: "  "}, {LogPath: "  "}} {
		if _, err := NewOperator(cfg); err == nil {
			t.Errorf("whitespace-only configuration %+v accepted", cfg)
		}
	}
}
