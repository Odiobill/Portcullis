package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeOperator is an injected validate/reload double: it records calls and
// can be told to fail specific operations. It never executes Caddy.
type fakeOperator struct {
	validateErr error
	reloadErr   error
	// validateErrFrom, when > 0, applies validateErr only from that 1-based
	// call onward (0 = immediately). Used to fail only restore attempts.
	validateErrFrom int
	calls           []string // "validate" / "reload" in call order
}

func (f *fakeOperator) Validate() error {
	f.calls = append(f.calls, "validate")
	if f.validateErr != nil && (f.validateErrFrom == 0 || len(f.calls) >= f.validateErrFrom) {
		return f.validateErr
	}
	return nil
}

func (f *fakeOperator) Reload() error {
	f.calls = append(f.calls, "reload")
	return f.reloadErr
}

var (
	errValidate = errors.New("caddy validation failed (test)")
	errReload   = errors.New("caddy reload failed (test)")
)

func newTestStore(t *testing.T, op *fakeOperator) *Store {
	t.Helper()
	root := t.TempDir()
	return NewStore(filepath.Join(root, "generated"), op)
}

func mustDeploy(t *testing.T, st *Store, s Service) error {
	t.Helper()
	return st.Deploy(s)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestDeployWritesServiceIDFileInGeneratedDir(t *testing.T) {
	op := &fakeOperator{}
	st := newTestStore(t, op)

	if err := mustDeploy(t, st, proxyService()); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	path := filepath.Join(st.dir, "svc-alpha.caddy")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("generated file missing at %s: %v", path, err)
	}
	want, _ := GenerateSiteBlock(proxyService())
	if got := readFile(t, path); got != want {
		t.Errorf("deployed content mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	// Validate then reload, exactly once each.
	if strings.Join(op.calls, ",") != "validate,reload" {
		t.Errorf("operator calls = %v, want [validate reload]", op.calls)
	}
	// No temp/backup artifacts may remain.
	entries, _ := os.ReadDir(st.dir)
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".bak") {
			t.Errorf("artifact left behind: %s", name)
		}
	}
}

func TestGeneratedFilePathRejectsEscape(t *testing.T) {
	st := newTestStore(t, &fakeOperator{})
	for _, id := range []string{"..", "../evil", "a/../../evil", "/abs/evil", `a\..\evil`} {
		if _, err := st.generatedFilePath(id); err == nil {
			t.Errorf("generatedFilePath accepted escaping id %q", id)
		}
	}
	path, err := st.generatedFilePath("svc-alpha")
	if err != nil {
		t.Fatalf("generatedFilePath(svc-alpha): %v", err)
	}
	if !strings.HasPrefix(path, st.dir+string(filepath.Separator)) || !strings.HasSuffix(path, "svc-alpha.caddy") {
		t.Errorf("contained path = %q, want under %s ending svc-alpha.caddy", path, st.dir)
	}
}

func TestDeployRejectsEscapingServiceID(t *testing.T) {
	op := &fakeOperator{}
	st := newTestStore(t, op)

	s := proxyService()
	s.ID = "../escape"
	if err := mustDeploy(t, st, s); err == nil {
		t.Fatal("Deploy accepted a path-escaping service ID")
	}
	if len(op.calls) != 0 {
		t.Errorf("operator must not run for rejected input, calls = %v", op.calls)
	}
	// Nothing may have been written outside the generated dir either.
	entries, err := os.ReadDir(filepath.Dir(st.dir))
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "generated" {
			t.Errorf("unexpected sibling created: %s", e.Name())
		}
	}
}

func TestDeployAndRemoveNeverTouchManualDir(t *testing.T) {
	op := &fakeOperator{}
	root := t.TempDir()
	genDir := filepath.Join(root, "generated")
	manualDir := filepath.Join(root, "manual")
	if err := os.MkdirAll(manualDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manualFile := filepath.Join(manualDir, "operator.caddy")
	manualContent := "# operator-owned\nimport acme\n"
	if err := os.WriteFile(manualFile, []byte(manualContent), 0o644); err != nil {
		t.Fatal(err)
	}

	st := NewStore(genDir, op)
	if err := st.Deploy(proxyService()); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if err := st.Remove("svc-alpha"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if got := readFile(t, manualFile); got != manualContent {
		t.Errorf("manual file changed:\n%s", got)
	}
	entries, err := os.ReadDir(manualDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "operator.caddy" {
		t.Errorf("manual dir gained entries: %v", entries)
	}
}

func TestDeployRollbackValidateFailNoPriorFile(t *testing.T) {
	op := &fakeOperator{validateErr: errValidate}
	st := newTestStore(t, op)

	err := mustDeploy(t, st, proxyService())
	if !errors.Is(err, errValidate) {
		t.Fatalf("want original validation error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(st.dir, "svc-alpha.caddy")); !os.IsNotExist(statErr) {
		t.Error("failed deploy without prior file must leave no generated file")
	}
	// Rollback attempted to restore active config: validate+reload again.
	if strings.Join(op.calls, ",") != "validate,validate,reload" {
		t.Errorf("operator calls = %v, want [validate validate reload]", op.calls)
	}
}

func TestDeployRollbackValidateFailWithPriorFile(t *testing.T) {
	op := &fakeOperator{}
	st := newTestStore(t, op)

	prior := proxyService()
	prior.Domains = []string{"old.example.com"}
	if err := st.Deploy(prior); err != nil {
		t.Fatalf("prior deploy: %v", err)
	}

	// Now every validation fails.
	op.validateErr = errValidate
	s := proxyService()
	err := mustDeploy(t, st, s)
	if !errors.Is(err, errValidate) {
		t.Fatalf("want original validation error, got %v", err)
	}
	path := filepath.Join(st.dir, "svc-alpha.caddy")
	want, _ := GenerateSiteBlock(prior)
	if got := readFile(t, path); got != want {
		t.Errorf("prior file not restored\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestDeployRollbackReloadFailNoPriorFile(t *testing.T) {
	op := &fakeOperator{reloadErr: errReload}
	st := newTestStore(t, op)

	err := mustDeploy(t, st, proxyService())
	if !errors.Is(err, errReload) {
		t.Fatalf("want original reload error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(st.dir, "svc-alpha.caddy")); !os.IsNotExist(statErr) {
		t.Error("failed deploy without prior file must leave no generated file")
	}
	if strings.Join(op.calls, ",") != "validate,reload,validate,reload" {
		t.Errorf("operator calls = %v, want [validate reload validate reload]", op.calls)
	}
}

func TestDeployRollbackReloadFailWithPriorFile(t *testing.T) {
	op := &fakeOperator{}
	st := newTestStore(t, op)

	prior := proxyService()
	prior.Domains = []string{"old.example.com"}
	if err := st.Deploy(prior); err != nil {
		t.Fatalf("prior deploy: %v", err)
	}

	// From here on reload always fails.
	op.reloadErr = errReload
	s := proxyService()
	err := mustDeploy(t, st, s)
	if !errors.Is(err, errReload) {
		t.Fatalf("want original reload error, got %v", err)
	}
	path := filepath.Join(st.dir, "svc-alpha.caddy")
	want, _ := GenerateSiteBlock(prior)
	if got := readFile(t, path); got != want {
		t.Errorf("prior file not restored\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRemoveSuccessRemovesFile(t *testing.T) {
	op := &fakeOperator{}
	st := newTestStore(t, op)
	if err := st.Deploy(proxyService()); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	op.calls = nil
	if err := st.Remove("svc-alpha"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(st.dir, "svc-alpha.caddy")); !os.IsNotExist(statErr) {
		t.Error("Remove left the generated file behind")
	}
	if strings.Join(op.calls, ",") != "validate,reload" {
		t.Errorf("operator calls = %v, want [validate reload]", op.calls)
	}
}

func TestRemoveRollbackValidateFail(t *testing.T) {
	op := &fakeOperator{}
	st := newTestStore(t, op)
	if err := st.Deploy(proxyService()); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	path := filepath.Join(st.dir, "svc-alpha.caddy")
	before := readFile(t, path)

	op.calls = nil
	op.validateErr = errValidate
	err := st.Remove("svc-alpha")
	if !errors.Is(err, errValidate) {
		t.Fatalf("want original validation error, got %v", err)
	}
	if got := readFile(t, path); got != before {
		t.Errorf("file not restored after failed remove\n--- got ---\n%s\n--- want ---\n%s", got, before)
	}
	if strings.Join(op.calls, ",") != "validate,validate,reload" {
		t.Errorf("operator calls = %v, want [validate validate reload]", op.calls)
	}
}

func TestRemoveRollbackReloadFail(t *testing.T) {
	op := &fakeOperator{}
	st := newTestStore(t, op)
	if err := st.Deploy(proxyService()); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	path := filepath.Join(st.dir, "svc-alpha.caddy")
	before := readFile(t, path)

	op.calls = nil
	op.reloadErr = errReload
	err := st.Remove("svc-alpha")
	if !errors.Is(err, errReload) {
		t.Fatalf("want original reload error, got %v", err)
	}
	if got := readFile(t, path); got != before {
		t.Errorf("file not restored after failed remove\n--- got ---\n%s\n--- want ---\n%s", got, before)
	}
	if strings.Join(op.calls, ",") != "validate,reload,validate,reload" {
		t.Errorf("operator calls = %v, want [validate reload validate reload]", op.calls)
	}
}

func TestRemoveNoopWhenFileAbsent(t *testing.T) {
	op := &fakeOperator{}
	st := newTestStore(t, op)
	if err := st.Remove("never-deployed"); err != nil {
		t.Fatalf("Remove on absent file should be a no-op, got %v", err)
	}
	if len(op.calls) != 0 {
		t.Errorf("operator must not run when there is nothing to remove, calls = %v", op.calls)
	}
}
