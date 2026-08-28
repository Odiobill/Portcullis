// Package backups implements the read-only backup browser boundary: it
// lists regular backup files from one configured directory and opens a
// safely selected file for download. It never creates, modifies, deletes,
// or retains backups; the HTTP layer can never select a directory or an
// arbitrary host path.
package backups

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultDir is the safe default backup directory.
const DefaultDir = "/backups"

// ErrStoreUnavailable is returned when the configured backup directory is
// missing or unreadable. It never carries host paths.
var ErrStoreUnavailable = errors.New("the backup store is not available")

// ErrNotFound is returned when a selected backup cannot be served because
// it does not exist or is not a regular direct child of the store.
var ErrNotFound = errors.New("the selected backup was not found")

// File describes one listed backup file.
type File struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// safeNameRe bounds download names to a disposition-safe charset.
// (Declared in browser.go via regexp; kept local to avoid unused imports.)
//
// Browser is the read-only browser over one configured absolute directory.
type Browser struct {
	dir string
}

// NewBrowser returns a Browser over an absolute, structurally safe
// directory. Empty, relative, or unsafe values fail closed.
func NewBrowser(dir string) (*Browser, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("backups: directory must not be empty")
	}
	if strings.ContainsAny(dir, "\n\r\x00") {
		return nil, fmt.Errorf("backups: directory contains forbidden characters")
	}
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("backups: directory %q must be an absolute path", dir)
	}
	return &Browser{dir: filepath.Clean(dir)}, nil
}

// List returns the regular files directly inside the configured directory,
// newest first (modification time descending, name ascending as a stable
// tie-break). Directories, symlinks, device files, and other non-regular
// entries are never listed.
func (b *Browser) List() ([]File, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return nil, fmt.Errorf("%w", ErrStoreUnavailable)
	}

	var out []File
	for _, e := range entries {
		// Reject symlinks and non-regular entries without following them.
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, File{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// validName reports whether name is a safe single backup filename: one
// path segment, no traversal, no separators, and a disposition-safe
// charset (alphanumeric start, then letters, digits, dot, underscore,
// hyphen).
func validName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\\n\r\x00") {
		return false
	}
	if filepath.Base(name) != name {
		return false
	}
	c := name[0]
	isAlnum := func(b byte) bool {
		return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
	}
	if !isAlnum(c) {
		return false
	}
	for i := 1; i < len(name); i++ {
		b := name[i]
		if !isAlnum(b) && b != '.' && b != '_' && b != '-' {
			return false
		}
	}
	return true
}

// Open resolves name strictly inside the configured directory and returns
// an open read handle plus its metadata. The name is validated as a safe
// basename first, then the resolved path is verified to remain a direct
// child of the directory. Symlinks, directories, and other non-regular
// files fail closed with ErrNotFound.
func (b *Browser) Open(name string) (*os.File, File, error) {
	if !validName(name) {
		return nil, File{}, fmt.Errorf("%w", ErrNotFound)
	}
	resolved := filepath.Join(b.dir, name)
	if filepath.Dir(resolved) != b.dir {
		return nil, File{}, fmt.Errorf("%w", ErrNotFound)
	}

	// Lstat never follows symlinks: a symlinked entry is not downloadable.
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, File{}, fmt.Errorf("%w", ErrNotFound)
	}

	f, err := os.Open(resolved)
	if err != nil {
		return nil, File{}, fmt.Errorf("%w", ErrNotFound)
	}
	return f, File{Name: name, Size: info.Size(), ModTime: info.ModTime()}, nil
}

// unused guard for fs import removal if structure changes.
