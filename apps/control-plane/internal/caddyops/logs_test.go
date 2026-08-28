package caddyops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLogFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "portcullis.log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func jsonLine(ts float64, host, remote string, status int) string {
	return fmt.Sprintf(`{"level":"info","ts":%f,"logger":"http.log.access.log0","msg":"handled request","request":{"remote_ip":"%s","remote_port":"51000","proto":"HTTP/2.0","method":"GET","host":"%s","uri":"/"},"user_id":"","duration":0.001,"size":1024,"status":%d,"resp_headers":{"Server":["Caddy"]}}`,
		ts, remote, host, status)
}

func TestRecentParsesCaddyJSON(t *testing.T) {
	path := writeLogFile(t, strings.Join([]string{
		jsonLine(1785200000.123, "app.example.com", "203.0.113.5", 200),
		jsonLine(1785200100.456, "alt.example.com", "198.51.100.7", 404),
		"",
	}, "\n"))
	r, err := NewLogReader(path)
	if err != nil {
		t.Fatalf("NewLogReader: %v", err)
	}

	entries, err := r.Recent()
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Host != "app.example.com" || entries[0].Status != "200" || entries[0].RemoteIP != "203.0.113.5" {
		t.Errorf("entry 0 = %+v", entries[0])
	}
	if entries[1].Host != "alt.example.com" || entries[1].Status != "404" {
		t.Errorf("entry 1 = %+v", entries[1])
	}
	if _, err := time.Parse(time.RFC3339, entries[0].Time); err != nil {
		t.Errorf("timestamp %q is not RFC3339: %v", entries[0].Time, err)
	}
	if entries[0].Raw != "" {
		t.Errorf("parsed entries must not carry a raw fallback, got %q", entries[0].Raw)
	}
}

func TestRecentKeepsChronologicalOrderAndCapsAtFifty(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, jsonLine(float64(1785200000+i), fmt.Sprintf("host%d.example.com", i), "192.0.2.1", 200))
	}
	r, err := NewLogReader(writeLogFile(t, strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("NewLogReader: %v", err)
	}

	entries, err := r.Recent()
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 50 {
		t.Fatalf("entries = %d, want 50", len(entries))
	}
	// Oldest of the retained window first, newest last.
	if !strings.Contains(entries[0].Host, "host10.example.com") {
		t.Errorf("first retained entry = %+v, want host10", entries[0])
	}
	if !strings.Contains(entries[49].Host, "host59.example.com") {
		t.Errorf("last retained entry = %+v, want host59", entries[49])
	}
}

func TestRecentSkipsEmptyLines(t *testing.T) {
	path := writeLogFile(t, "\n\n"+jsonLine(1785200000, "app.example.com", "192.0.2.1", 200)+"\n\n")
	r, err := NewLogReader(path)
	if err != nil {
		t.Fatalf("NewLogReader: %v", err)
	}
	entries, err := r.Recent()
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 (empty lines skipped)", len(entries))
	}
}

func TestRecentMalformedLineFallsBackToRaw(t *testing.T) {
	garbage := "\x00\x01 not json at all {{{"
	path := writeLogFile(t, strings.Join([]string{
		jsonLine(1785200000, "app.example.com", "192.0.2.1", 200),
		garbage,
		"partially {broken json",
		"",
	}, "\n"))
	r, err := NewLogReader(path)
	if err != nil {
		t.Fatalf("NewLogReader: %v", err)
	}

	entries, err := r.Recent()
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	if entries[0].Raw != "" {
		t.Errorf("first entry should parse cleanly, raw = %q", entries[0].Raw)
	}
	if entries[1].Raw != garbage {
		t.Errorf("malformed line not preserved: %q", entries[1].Raw)
	}
	if entries[2].Raw == "" {
		t.Error("partially broken JSON must fall back to raw")
	}
}

func TestRecentMissingFileFailsClosedWithoutPathLeak(t *testing.T) {
	dir := t.TempDir()
	r, err := NewLogReader(filepath.Join(dir, "does-not-exist.log"))
	if err != nil {
		t.Fatalf("NewLogReader: %v", err)
	}
	recent, rerr := r.Recent()
	_ = recent
	if rerr == nil {
		t.Fatal("missing log file must fail closed")
	}
	if !strings.Contains(rerr.Error(), "not available") {
		t.Errorf("error must be a bounded English message, got: %s", rerr.Error())
	}
	if strings.Contains(rerr.Error(), dir) {
		t.Errorf("error must not expose host paths, got: %s", rerr.Error())
	}
	if !errors.Is(rerr, ErrLogUnavailable) {
		t.Errorf("error must match ErrLogUnavailable, got %v", rerr)
	}
}

func TestRecentReadErrorFailsClosed(t *testing.T) {
	// A directory always fails to read.
	r, err := NewLogReader(t.TempDir())
	if err != nil {
		t.Fatalf("NewLogReader: %v", err)
	}
	recent, rerr := r.Recent()
	_ = recent
	if rerr == nil {
		t.Fatal("unreadable log must fail closed")
	}
	if !errors.Is(rerr, ErrLogUnavailable) {
		t.Errorf("error must match ErrLogUnavailable, got %v", rerr)
	}
}

func TestNewLogReaderRejectsUnsafePath(t *testing.T) {
	for _, p := range []string{"", "bad\npath", "bad\x00path"} {
		if _, err := NewLogReader(p); err == nil {
			t.Errorf("unsafe log path %q accepted", p)
		}
	}
}
