package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Operator is the injected Caddy operation boundary. Production callers
// implement it with `caddy validate` and the admin-API reload; tests
// inject fakes so no Caddy binary or Docker/Compose wiring is involved.
type Operator interface {
	// Validate checks the full Caddy configuration on disk.
	Validate() error
	// Reload applies the on-disk configuration to the running Caddy.
	Reload() error
}

// Store performs rollback-safe deploy/remove of generated Caddyfiles under
// one configured generated directory. It never reads, writes, deletes, or
// enumerates sites/manual: the manual directory is simply not part of its
// configuration.
type Store struct {
	dir string
	op  Operator
}

// NewStore returns a Store scoped to generatedDir with the given injected
// Caddy operator.
func NewStore(generatedDir string, op Operator) *Store {
	return &Store{dir: generatedDir, op: op}
}

// generatedFilePath maps a service ID to its generated file inside dir and
// rejects any ID that would escape the directory (defense in depth on top
// of Service.Validate's character rules).
func (st *Store) generatedFilePath(serviceID string) (string, error) {
	// Reject anything that could move the path before resolving: separators,
	// traversal segments, and Windows-style separators.
	if serviceID == "" || strings.ContainsAny(serviceID, `/\`) || strings.Contains(serviceID, "..") {
		return "", fmt.Errorf("registry: service ID %q is not a safe generated filename", serviceID)
	}
	resolvedDir, err := filepath.Abs(st.dir)
	if err != nil {
		return "", fmt.Errorf("registry: resolve generated dir: %w", err)
	}
	resolvedFile, err := filepath.Abs(filepath.Join(resolvedDir, serviceID+".caddy"))
	if err != nil {
		return "", fmt.Errorf("registry: resolve generated path for %q: %w", serviceID, err)
	}
	if filepath.Dir(resolvedFile) != resolvedDir {
		return "", fmt.Errorf("registry: generated path for %q escapes the generated directory", serviceID)
	}
	return resolvedFile, nil
}

// Deploy writes the service's generated Caddyfile atomically, then applies
// it by validating and reloading. If validation or reload fails, the prior
// generated state is restored and an attempt is made to restore the prior
// active Caddy configuration (validate + reload); the original failure is
// always returned.
func (st *Store) Deploy(s Service) error {
	content, err := GenerateSiteBlock(s)
	if err != nil {
		return err
	}
	path, err := st.generatedFilePath(s.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(st.dir, 0o755); err != nil {
		return fmt.Errorf("registry: ensure generated dir: %w", err)
	}

	prior, hadPrior, err := readIfExists(path)
	if err != nil {
		return err
	}

	if err := writeFileAtomic(path, content); err != nil {
		return err
	}

	if err := st.op.Validate(); err != nil {
		st.rollback(path, prior, hadPrior)
		return err
	}
	if err := st.op.Reload(); err != nil {
		st.rollback(path, prior, hadPrior)
		return err
	}
	return nil
}

// Remove deletes the service's generated Caddyfile and applies the change
// with validate+reload. If either fails, the file is restored and the prior
// active configuration is re-applied; the original failure is returned.
// Removing a service with no generated file is a no-op.
func (st *Store) Remove(serviceID string) error {
	path, err := st.generatedFilePath(serviceID)
	if err != nil {
		return err
	}
	prior, exists, err := readIfExists(path)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("registry: remove generated file: %w", err)
	}

	if err := st.op.Validate(); err != nil {
		st.rollback(path, prior, true)
		return err
	}
	if err := st.op.Reload(); err != nil {
		st.rollback(path, prior, true)
		return err
	}
	return nil
}

// rollback restores the prior generated state and attempts to restore the
// prior active Caddy configuration. Secondary failures are deliberately
// swallowed: the caller must observe the original failure.
func (st *Store) rollback(path string, prior string, hadPrior bool) {
	var err error
	if hadPrior {
		err = writeFileAtomic(path, prior)
	} else {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			err = nil
		}
	}
	if err != nil {
		return
	}
	// Best-effort restore of the active configuration.
	_ = st.op.Validate()
	_ = st.op.Reload()
}

// readIfExists returns the file content and whether it existed.
func readIfExists(path string) (string, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("registry: read %s: %w", path, err)
	}
	return string(b), true, nil
}

// writeFileAtomic writes content via a same-directory temp file and a
// rename, so readers never observe a partial file. Temp files are cleaned
// up on failure.
func writeFileAtomic(path string, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("registry: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("registry: rename %s: %w", tmp, err)
	}
	return nil
}

// ManualDirName documents the boundary: generated files live beside a
// manual directory that this package never touches.
const ManualDirName = "manual"

// EnsureGeneratedDirIsNotManual is a compile-time-readable guard used by
// callers wiring configuration; it rejects a generated directory whose base
// name is the manual directory.
func EnsureGeneratedDirIsNotManual(dir string) error {
	if strings.EqualFold(filepath.Base(dir), ManualDirName) {
		return fmt.Errorf("registry: %q must not be the manual directory", dir)
	}
	return nil
}
