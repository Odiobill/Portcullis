package caddyops

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// ErrLogUnavailable is the bounded English error returned when the Caddy
// log cannot be read. It never carries host paths.
var ErrLogUnavailable = errors.New("the Caddy log is not available")

// maxLogTailBytes bounds how much of the log file is read per request.
const maxLogTailBytes = 256 * 1024

// recentLogLines is the maximum number of non-empty lines returned.
const recentLogLines = 50

// LogEntry is one rendered recent-log line. JSON lines expose parsed Caddy
// fields; malformed lines fall back to Raw.
type LogEntry struct {
	Time     string // RFC3339 UTC timestamp when parseable
	Host     string
	Status   string
	RemoteIP string
	Raw      string // original line, set only when JSON parsing failed
}

// LogReader reads the configured single Caddy log file read-only. The path
// is fixed at construction and never accepted from request input.
type LogReader struct {
	path string
}

// NewLogReader returns a reader for the configured log path. Unsafe paths
// fail closed.
func NewLogReader(path string) (*LogReader, error) {
	if err := validateValue("log path", path); err != nil {
		return nil, err
	}
	return &LogReader{path: path}, nil
}

// Recent returns up to the most recent 50 non-empty log lines, oldest
// first. Caddy JSON access-log fields are parsed where available;
// malformed lines fall back to a raw entry. Missing or unreadable files
// return ErrLogUnavailable.
func (r *LogReader) Recent() ([]LogEntry, error) {
	lines, err := r.tailLines()
	if err != nil {
		return nil, err
	}

	var out []LogEntry
	for _, line := range lines {
		line = string(trimSpaceBytes([]byte(line)))
		if line == "" {
			continue
		}
		out = append(out, parseLogLine(line))
	}
	// Keep only the most recent recentLogLines non-empty entries.
	if len(out) > recentLogLines {
		out = out[len(out)-recentLogLines:]
	}
	return out, nil
}

// tailLines reads at most the last maxLogTailBytes of the file and returns
// its lines. Any read failure maps to ErrLogUnavailable.
func (r *LogReader) tailLines() ([]string, error) {
	f, err := os.Open(r.path)
	if err != nil {
		return nil, fmt.Errorf("%w", ErrLogUnavailable)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w", ErrLogUnavailable)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w", ErrLogUnavailable)
	}

	size := info.Size()
	offset := int64(0)
	partialFirstLine := false
	if size > maxLogTailBytes {
		offset = size - maxLogTailBytes
		partialFirstLine = true
	}
	if _, err := f.Seek(offset, 0); err != nil {
		return nil, fmt.Errorf("%w", ErrLogUnavailable)
	}
	buf := make([]byte, size-offset)
	if _, err := f.Read(buf); err != nil {
		return nil, fmt.Errorf("%w", ErrLogUnavailable)
	}

	lines := splitLines(string(buf))
	// If we started mid-file, the first chunk is likely a partial line.
	if partialFirstLine && len(lines) > 0 && len(buf) > 0 && buf[0] != '\n' {
		lines = lines[1:]
	}
	return lines, nil
}

// parseLogLine converts one log line into an entry, falling back to Raw.
func parseLogLine(line string) LogEntry {
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return LogEntry{Raw: line}
	}

	entry := LogEntry{}
	if ts, ok := obj["ts"].(float64); ok {
		entry.Time = time.Unix(int64(ts), 0).UTC().Format(time.RFC3339)
	}
	entry.Status = jsonToString(obj["status"])
	if req, ok := obj["request"].(map[string]any); ok {
		entry.Host = jsonToString(req["host"])
		entry.RemoteIP = jsonToString(req["remote_ip"])
	}
	// If nothing useful parsed out, keep the raw line visible.
	if entry.Time == "" && entry.Host == "" && entry.Status == "" {
		return LogEntry{Raw: line}
	}
	return entry
}

// jsonToString renders a JSON scalar as a display string.
func jsonToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return ""
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func trimSpaceBytes(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && isSpaceByte(b[i]) {
		i++
	}
	for j > i && isSpaceByte(b[j-1]) {
		j--
	}
	return b[i:j]
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}
