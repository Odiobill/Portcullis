package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const migrationUpPath = "../../migrations/000001_create_services.up.sql"

// TestMigrationDomainsConstraintRejectsEmptyArray is a source-level guard
// (no live Postgres): the domains CHECK must use a predicate that cannot
// pass for an empty TEXT[] array. PostgreSQL `array_length(x, 1)` returns
// NULL for an empty array dimension, and a CHECK passes on NULL, so the
// bare form `array_length(domains, 1) >= 1` silently allows ARRAY[]::TEXT[].
// The migration must instead use `cardinality(domains) >= 1` (or an
// equivalent non-NULL predicate).
func TestMigrationDomainsConstraintRejectsEmptyArray(t *testing.T) {
	sql, err := os.ReadFile(filepath.Clean(migrationUpPath))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	content := string(sql)

	if !strings.Contains(content, "domains") {
		t.Fatal("migration does not define domains")
	}
	if strings.Contains(content, "array_length(domains, 1) >= 1") {
		t.Error("migration uses bare array_length(domains, 1) >= 1: NULL CHECK semantics allow an empty domains array")
	}
	if !strings.Contains(content, "cardinality(domains) >= 1") {
		t.Error("migration must enforce cardinality(domains) >= 1 (or an equivalent non-NULL predicate)")
	}
}

// manualDirFixture creates a manual directory containing a sentinel file
// owned by the operator, plus (optionally) a nested path beneath it.
func manualDirFixture(t *testing.T, nested bool) (manualPath, sentinel string) {
	t.Helper()
	root := t.TempDir()
	manualPath = filepath.Join(root, "manual")
	if nested {
		manualPath = filepath.Join(root, "manual", "sites")
	}
	if err := os.MkdirAll(manualPath, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel = filepath.Join(filepath.Dir(manualPath), "operator.caddy")
	if nested {
		sentinel = filepath.Join(filepath.Dir(filepath.Dir(manualPath)), "operator.caddy")
	}
	if err := os.WriteFile(sentinel, []byte("# operator-owned\nimport acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return manualPath, sentinel
}

func assertUntouched(t *testing.T, op *fakeOperator, sentinel string) {
	t.Helper()
	if len(op.calls) != 0 {
		t.Errorf("operator must not run for a manual-directory store, calls = %v", op.calls)
	}
	if got := readFile(t, sentinel); !strings.HasPrefix(got, "# operator-owned") {
		t.Errorf("sentinel manual file changed:\n%s", got)
	}
}

func TestDeployFailsClosedOnManualDir(t *testing.T) {
	op := &fakeOperator{}
	manualPath, sentinel := manualDirFixture(t, false)
	st := NewStore(manualPath, op)

	if err := st.Deploy(proxyService()); err == nil {
		t.Fatal("Deploy into the manual directory must fail closed")
	}
	assertUntouched(t, op, sentinel)

	// No generated artifact may appear anywhere in the manual tree.
	entries, err := os.ReadDir(manualPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("manual directory gained entries: %v", entries)
	}
}

func TestDeployFailsClosedOnNestedManualDir(t *testing.T) {
	op := &fakeOperator{}
	manualPath, sentinel := manualDirFixture(t, true)
	st := NewStore(manualPath, op)

	if err := st.Deploy(proxyService()); err == nil {
		t.Fatal("Deploy into a directory nested beneath the manual directory must fail closed")
	}
	assertUntouched(t, op, sentinel)
}

func TestRemoveFailsClosedOnManualDir(t *testing.T) {
	op := &fakeOperator{}
	manualPath, sentinel := manualDirFixture(t, false)
	st := NewStore(manualPath, op)

	if err := st.Remove("operator"); err == nil {
		t.Fatal("Remove against the manual directory must fail closed")
	}
	assertUntouched(t, op, sentinel)

	// The sentinel must still exist.
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel manual file missing after Remove: %v", err)
	}
}

func TestRemoveFailsClosedOnNestedManualDir(t *testing.T) {
	op := &fakeOperator{}
	manualPath, sentinel := manualDirFixture(t, true)
	st := NewStore(manualPath, op)

	if err := st.Remove("operator"); err == nil {
		t.Fatal("Remove against a directory nested beneath the manual directory must fail closed")
	}
	assertUntouched(t, op, sentinel)
}

// TestStoreRejectsManualDirConfiguration pins the nesting-aware exclusion
// rule directly: any path segment named "manual" is rejected, arbitrary
// generated directories remain allowed.
func TestStoreRejectsManualDirConfiguration(t *testing.T) {
	for _, dir := range []string{
		"/etc/caddy/sites/manual",
		"/etc/caddy/manual",
		"/etc/caddy/manual/sites",
		"/srv/MANUAL",
	} {
		if err := EnsureGeneratedDirIsNotManual(dir); err == nil {
			t.Errorf("generated dir %q must be rejected as manual", dir)
		}
	}
	for _, dir := range []string{
		"/etc/caddy/sites/generated",
		"/etc/caddy/sites/generated-manual",    // different segment, allowed
		"/etc/caddy/sites/manual/../generated", // cleans to .../sites/generated: not manual
		"/tmp/test-generated-123",
	} {
		if err := EnsureGeneratedDirIsNotManual(dir); err != nil {
			t.Errorf("arbitrary generated dir %q must be allowed, got %v", dir, err)
		}
	}
}
