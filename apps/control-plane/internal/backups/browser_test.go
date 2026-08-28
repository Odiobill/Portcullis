package backups

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// syscallMkfifo creates a named pipe to represent a non-regular file.
func syscallMkfifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}

func touchFile(t *testing.T, dir, name string, size int, mod time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mod.IsZero() {
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewBrowserRejectsUnsafeConfiguration(t *testing.T) {
	for _, dir := range []string{
		"",
		"relative/path",
		".",
		"bad\ndir",
		"bad\x00dir",
		"  ",
	} {
		if _, err := NewBrowser(dir); err == nil {
			t.Errorf("unsafe backup directory %q accepted", dir)
		}
	}
}

func TestNewBrowserAcceptsAbsoluteDirectory(t *testing.T) {
	if _, err := NewBrowser(t.TempDir()); err != nil {
		t.Fatalf("absolute directory rejected: %v", err)
	}
}

// TestNewBrowserRejectsTraversalSegments pins fail-closed configuration:
// any raw `..` path segment in the configured backup directory is rejected
// before cleaning, so traversal-bearing absolute paths can never be
// silently cleaned to a different host directory.
func TestNewBrowserRejectsTraversalSegments(t *testing.T) {
	for _, dir := range []string{
		"/backups/../etc",
		"/backups/..",
		"/..",
		"/backups/sub/../../etc",
		"/backups/..\x00hidden",
	} {
		b, err := NewBrowser(dir)
		if err == nil {
			t.Errorf("traversal-bearing directory %q accepted (resolved dir %q)", dir, b.dir)
		}
	}
}

func TestListOnlyRegularFilesNewestFirst(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	touchFile(t, dir, "old.dump.gz", 100, base)
	touchFile(t, dir, "new.dump.gz", 300, base.Add(2*time.Hour))
	touchFile(t, dir, "mid.dump.gz", 200, base.Add(1*time.Hour))

	// Non-listable entries: subdirectory, symlink to a file, symlink to a dir.
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "..", "target-outside.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link-to-file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc", filepath.Join(dir, "link-to-dir")); err != nil {
		t.Fatal(err)
	}

	b, err := NewBrowser(dir)
	if err != nil {
		t.Fatalf("NewBrowser: %v", err)
	}
	files, err := b.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("files = %d (%v), want 3 regular files", len(files), files)
	}
	wantOrder := []string{"new.dump.gz", "mid.dump.gz", "old.dump.gz"}
	for i, want := range wantOrder {
		if files[i].Name != want {
			t.Errorf("files[%d].Name = %q, want %q (newest-first)", i, files[i].Name, want)
		}
	}
	if files[0].Size != 300 {
		t.Errorf("files[0].Size = %d, want 300", files[0].Size)
	}
	if !files[0].ModTime.Equal(base.Add(2 * time.Hour)) {
		t.Errorf("files[0].ModTime = %v, want %v", files[0].ModTime, base.Add(2*time.Hour))
	}
}

func TestListStableOrderOnEqualModTimes(t *testing.T) {
	dir := t.TempDir()
	same := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	touchFile(t, dir, "b.dump", 1, same)
	touchFile(t, dir, "a.dump", 1, same)
	touchFile(t, dir, "c.dump", 1, same)

	b, _ := NewBrowser(dir)
	files, err := b.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := []string{}
	for _, f := range files {
		got = append(got, f.Name)
	}
	if strings.Join(got, ",") != "a.dump,b.dump,c.dump" {
		t.Errorf("tie-break order = %v, want alphabetical", got)
	}
}

func TestListMissingDirectoryFailsClosed(t *testing.T) {
	b, err := NewBrowser(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("NewBrowser: %v", err)
	}
	_, lerr := b.List()
	if !errors.Is(lerr, ErrStoreUnavailable) {
		t.Fatalf("want ErrStoreUnavailable, got %v", lerr)
	}
	if strings.Contains(lerr.Error(), "absent") {
		t.Errorf("error must not expose host paths, got: %s", lerr.Error())
	}
}

func TestListEmptyDirectoryIsEmptyState(t *testing.T) {
	b, _ := NewBrowser(t.TempDir())
	files, err := b.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("files = %v, want empty", files)
	}
}

func TestOpenRejectsUnsafeNames(t *testing.T) {
	dir := t.TempDir()
	touchFile(t, dir, "safe.dump", 10, time.Time{})
	b, _ := NewBrowser(dir)

	for _, name := range []string{
		"../secret",
		"..",
		".",
		"a/b",
		"/etc/passwd",
		"./safe.dump",
		"safe.dump/",
		"bad\x00name",
		"bad\nname",
		`quote"name`,
		"semi;colon",
	} {
		f, _, err := b.Open(name)
		if err == nil {
			f.Close()
			t.Errorf("unsafe name %q accepted", name)
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("unsafe name %q: want ErrNotFound, got %v", name, err)
		}
	}
}

func TestOpenRejectsSymlinkDirectoryAndNonRegular(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "..", "outside.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.dump")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir.dump"), 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(dir, "fifo.dump")
	if err := syscallMkfifo(fifoPath); err == nil {
		defer os.Remove(fifoPath)
	} else {
		t.Skip("mkfifo unavailable on this platform")
	}

	b, _ := NewBrowser(dir)
	for _, name := range []string{"link.dump", "subdir.dump", "fifo.dump"} {
		f, _, err := b.Open(name)
		if err == nil {
			f.Close()
			t.Errorf("non-regular entry %q downloadable", name)
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("non-regular entry %q: want ErrNotFound, got %v", name, err)
		}
	}
}

func TestOpenMissingFileFailsClosed(t *testing.T) {
	b, _ := NewBrowser(t.TempDir())
	f, _, err := b.Open("absent.dump")
	if err == nil {
		f.Close()
		t.Fatal("missing file must fail closed")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// TestOpenSymlinkSwapAfterValidationCannotEscape is the deterministic
// pre-open replacement seam for the Lstat→Open symlink-swap race: after
// containment validation but before the open, the validated regular entry
// is exchanged for a symlink to an outside secret file. The no-follow open
// must fail closed with ErrNotFound and never yield the secret content.
func TestOpenSymlinkSwapAfterValidationCannotEscape(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "..", "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	touchFile(t, dir, "svc.dump", 5, time.Time{})
	b, _ := NewBrowser(dir)

	swapHook = func(storeDir, name string) {
		if storeDir != dir || name != "svc.dump" {
			t.Errorf("swap hook called with unexpected dir/name: %q %q", storeDir, name)
			return
		}
		// The attacker swaps the validated regular file for a symlink
		// pointing outside the store just before the open.
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secret, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { swapHook = nil }()

	f, _, err := b.Open("svc.dump")
	if err == nil {
		f.Close()
		t.Fatal("a symlink swapped in after validation must not open")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if strings.Contains(err.Error(), "TOP-SECRET") {
		t.Error("error must not leak outside content")
	}
	if strings.Contains(err.Error(), dir) {
		t.Error("error must not expose host paths")
	}
}

func TestOpenSuccessReturnsRegularFileAndMetadata(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("backup-bytes-", 20)
	touchFile(t, dir, "svc.dump.gz", 0, time.Time{})
	if err := os.WriteFile(filepath.Join(dir, "svc.dump.gz"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := NewBrowser(dir)

	f, meta, err := b.Open("svc.dump.gz")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	if meta.Name != "svc.dump.gz" {
		t.Errorf("meta.Name = %q", meta.Name)
	}
	if meta.Size != int64(len(content)) {
		t.Errorf("meta.Size = %d, want %d", meta.Size, len(content))
	}
	buf := make([]byte, len(content))
	if _, err := f.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != content {
		t.Error("opened file does not read the expected content")
	}
}

func TestErrorsAreEnglishAndBounded(t *testing.T) {
	if !strings.Contains(ErrStoreUnavailable.Error(), "not available") {
		t.Errorf("store error not English/bounded: %s", ErrStoreUnavailable.Error())
	}
	if !strings.Contains(ErrNotFound.Error(), "not found") {
		t.Errorf("not-found error not English/bounded: %s", ErrNotFound.Error())
	}
}
