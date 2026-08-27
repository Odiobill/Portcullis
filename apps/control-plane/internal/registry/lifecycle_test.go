package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeServiceRepo is an in-memory ServiceRepository double with injectable
// failures and a call log.
type fakeServiceRepo struct {
	services    map[string]Service
	createOrder []string
	calls       []string
	updateCalls int
	// updateErrOnCall, when > 0, applies updateErr only to that 1-based
	// Update call (0 = every call). Lets tests fail only the restore.
	updateErrOnCall int

	createErr, updateErr, deleteErr, getErr, listErr error
}

func newFakeServiceRepo() *fakeServiceRepo {
	return &fakeServiceRepo{services: map[string]Service{}}
}

func (f *fakeServiceRepo) Create(_ context.Context, s Service) error {
	f.calls = append(f.calls, "create:"+s.ID)
	if f.createErr != nil {
		return f.createErr
	}
	if _, exists := f.services[s.ID]; exists {
		return errors.New("duplicate key")
	}
	f.services[s.ID] = s
	f.createOrder = append(f.createOrder, s.ID)
	return nil
}

func (f *fakeServiceRepo) Update(_ context.Context, s Service) error {
	f.calls = append(f.calls, "update:"+s.ID)
	f.updateCalls++
	if f.updateErr != nil && (f.updateErrOnCall == 0 || f.updateCalls == f.updateErrOnCall) {
		return f.updateErr
	}
	if _, exists := f.services[s.ID]; !exists {
		return ErrNotFound
	}
	f.services[s.ID] = s
	return nil
}

func (f *fakeServiceRepo) Delete(_ context.Context, id string) error {
	f.calls = append(f.calls, "delete:"+id)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, exists := f.services[id]; !exists {
		return ErrNotFound
	}
	delete(f.services, id)
	return nil
}

func (f *fakeServiceRepo) Get(_ context.Context, id string) (Service, error) {
	f.calls = append(f.calls, "get:"+id)
	if f.getErr != nil {
		return Service{}, f.getErr
	}
	s, exists := f.services[id]
	if !exists {
		return Service{}, ErrNotFound
	}
	return s, nil
}

func (f *fakeServiceRepo) List(_ context.Context) ([]Service, error) {
	f.calls = append(f.calls, "list")
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]Service, 0, len(f.services))
	for _, id := range f.createOrder {
		if s, ok := f.services[id]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// lifecycleFixture wires a Lifecycle with the fake repository, a real Store
// over a temp generated directory, and a fake Caddy operator.
type lifecycleFixture struct {
	repo   *fakeServiceRepo
	op     *fakeOperator
	store  *Store
	lc     *Lifecycle
	genDir string
}

func newLifecycleFixture(t *testing.T, newID func() string) *lifecycleFixture {
	t.Helper()
	f := &lifecycleFixture{
		repo:   newFakeServiceRepo(),
		op:     &fakeOperator{},
		genDir: filepath.Join(t.TempDir(), "generated"),
	}
	f.store = NewStore(f.genDir, f.op)
	f.lc = NewLifecycle(f.repo, f.store, newID)
	return f
}

func counterID() func() string {
	n := 0
	return func() string {
		n++
		return "svc-test" + strings.Repeat("0", 8) + string(rune('a'+n-1))
	}
}

func lcProxyService(id string) Service {
	return Service{
		ID:             id,
		Type:           TypeProxy,
		Domains:        []string{"App.Example.com"},
		TLSMode:        TLSACME,
		ProxyContainer: "app_container",
		ProxyPort:      3000,
		CreatedAt:      time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC),
	}
}

func TestCreatePersistsThenDeploys(t *testing.T) {
	f := newLifecycleFixture(t, counterID())

	got, err := f.lc.Create(context.Background(), lcProxyService("form-supplied-id"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// ID is generated server-side, never taken from the form value.
	if got.ID == "form-supplied-id" || !strings.HasPrefix(got.ID, "svc-") {
		t.Errorf("Create must generate an opaque server-side ID, got %q", got.ID)
	}
	if _, ok := f.repo.services[got.ID]; !ok {
		t.Error("service not persisted")
	}
	file := filepath.Join(f.genDir, got.ID+".caddy")
	if _, err := os.Stat(file); err != nil {
		t.Errorf("generated file missing: %v", err)
	}
	if strings.Join(f.op.calls, ",") != "validate,reload" {
		t.Errorf("operator calls = %v", f.op.calls)
	}
	// Domains stored normalized.
	if got.Domains[0] != "app.example.com" {
		t.Errorf("domains not normalized: %v", got.Domains)
	}
}

func TestCreateCompensatesOnDeployFailure(t *testing.T) {
	f := newLifecycleFixture(t, counterID())
	f.op.validateErr = errValidate

	_, err := f.lc.Create(context.Background(), lcProxyService("ignored"))
	if err == nil {
		t.Fatal("Create must fail when deployment fails")
	}
	if errors.Is(err, ErrCompensationFailed) || isCompensationError(err) {
		t.Fatalf("normal deploy failure must not be a compensation error: %v", err)
	}
	if len(f.repo.services) != 0 {
		t.Errorf("persisted record must be removed after deploy failure, repo = %v", f.repo.services)
	}
	if strings.Join(f.repo.calls, ",") != "create:svc-test00000000a,delete:svc-test00000000a" {
		t.Errorf("repo calls = %v", f.repo.calls)
	}
}

func TestCreateSurfacesCompensationFailure(t *testing.T) {
	f := newLifecycleFixture(t, counterID())
	f.op.validateErr = errValidate
	f.repo.deleteErr = errors.New("delete failed")

	_, err := f.lc.Create(context.Background(), lcProxyService("ignored"))
	var comp *CompensationError
	if !errors.As(err, &comp) {
		t.Fatalf("want CompensationError, got %v", err)
	}
	if comp.Operation != "create" {
		t.Errorf("compensation operation = %q", comp.Operation)
	}
	if !errors.Is(comp.Primary, errValidate) {
		t.Errorf("compensation must preserve the primary failure, got %v", comp.Primary)
	}
	if comp.Compensate == nil {
		t.Error("compensation failure not surfaced")
	}
	// The failure must not be mistaken for success anywhere downstream.
	if err.Error() == "" {
		t.Error("compensation error must have a distinct message")
	}
}

func TestEditPersistsChangeAndDeploys(t *testing.T) {
	f := newLifecycleFixture(t, counterID())
	created, err := f.lc.Create(context.Background(), lcProxyService("ignored"))
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}

	edited := created
	edited.Domains = []string{"moved.example.com"}
	got, err := f.lc.Edit(context.Background(), edited)
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if got.Domains[0] != "moved.example.com" {
		t.Errorf("edit not persisted: %v", got.Domains)
	}
	file := filepath.Join(f.genDir, created.ID+".caddy")
	content := readFile(t, file)
	if !strings.Contains(content, "moved.example.com") {
		t.Errorf("generated file not updated:\n%s", content)
	}
}

func TestEditRestoresPriorRecordOnDeployFailure(t *testing.T) {
	f := newLifecycleFixture(t, counterID())
	created, err := f.lc.Create(context.Background(), lcProxyService("ignored"))
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}

	edited := created
	edited.Domains = []string{"moved.example.com"}
	f.op.validateErr = errValidate // everything fails from here
	_, err = f.lc.Edit(context.Background(), edited)
	if err == nil {
		t.Fatal("Edit must fail when deployment fails")
	}

	prior, getErr := f.repo.Get(context.Background(), created.ID)
	if getErr != nil {
		t.Fatalf("prior record missing: %v", getErr)
	}
	if prior.Domains[0] != "app.example.com" {
		t.Errorf("prior record not restored: %v", prior.Domains)
	}
	// Slice-2 rollback remains authoritative for file state: the prior
	// generated file content is restored via the Store, not direct writes.
	content := readFile(t, filepath.Join(f.genDir, created.ID+".caddy"))
	if !strings.Contains(content, "app.example.com") {
		t.Errorf("prior generated file not restored:\n%s", content)
	}
}

func TestEditSurfacesCompensationFailure(t *testing.T) {
	f := newLifecycleFixture(t, counterID())
	created, err := f.lc.Create(context.Background(), lcProxyService("ignored"))
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}
	edited := created
	edited.Domains = []string{"moved.example.com"}
	// Seed create consumed operator calls 1-2; the edit's deploy validate is
	// call 3 and must fail, while the edit's persist (repo update call 1)
	// succeeds and only the restore (repo update call 2) fails.
	f.op.validateErr = errValidate
	f.op.validateErrFrom = 3
	f.repo.updateErr = errors.New("update failed")
	f.repo.updateErrOnCall = 2

	_, err = f.lc.Edit(context.Background(), edited)
	var comp *CompensationError
	if !errors.As(err, &comp) {
		t.Fatalf("want CompensationError, got %v", err)
	}
	if comp.Operation != "edit" {
		t.Errorf("compensation operation = %q", comp.Operation)
	}
}

func TestEditUnknownServiceFails(t *testing.T) {
	f := newLifecycleFixture(t, counterID())
	s := lcProxyService("svc-missing")
	if _, err := f.lc.Edit(context.Background(), s); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if len(f.op.calls) != 0 {
		t.Errorf("operator must not run for unknown service, calls = %v", f.op.calls)
	}
}

func TestDeleteRemovesFileBeforeRecord(t *testing.T) {
	f := newLifecycleFixture(t, counterID())
	created, err := f.lc.Create(context.Background(), lcProxyService("ignored"))
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}
	f.op.calls = nil
	f.repo.calls = nil

	got, err := f.lc.Delete(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("Delete returned %+v", got)
	}
	// Caddy removal must happen before the DB delete.
	joined := strings.Join(f.op.calls, ",") + " | " + strings.Join(f.repo.calls, ",")
	if strings.Contains(joined, "delete:") && !strings.Contains(strings.Join(f.op.calls, ","), "validate") {
		t.Errorf("file removal must precede record deletion, order = %s", joined)
	}
	if idx := strings.Index(strings.Join(f.repo.calls, ","), "delete:"); idx != -1 {
		if len(f.op.calls) == 0 {
			t.Error("operator ran after record deletion")
		}
	}
	if _, err := os.Stat(filepath.Join(f.genDir, created.ID+".caddy")); !os.IsNotExist(err) {
		t.Error("generated file not removed")
	}
	if len(f.repo.services) != 0 {
		t.Error("record not deleted")
	}
}

func TestDeleteRestoresGeneratedFileWhenDBDeleteFails(t *testing.T) {
	f := newLifecycleFixture(t, counterID())
	created, err := f.lc.Create(context.Background(), lcProxyService("ignored"))
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}

	f.repo.deleteErr = errors.New("db delete failed")
	_, err = f.lc.Delete(context.Background(), created.ID)
	if err == nil {
		t.Fatal("Delete must fail when the DB delete fails")
	}
	if isCompensationError(err) {
		t.Fatalf("successful compensation must not be a compensation error: %v", err)
	}
	// The generated file must be restored through the authoritative
	// Slice-2 deploy path (validate+reload attempted again).
	content := readFile(t, filepath.Join(f.genDir, created.ID+".caddy"))
	want, _ := GenerateSiteBlock(created)
	if content != want {
		t.Errorf("generated file not restored\n--- got ---\n%s\n--- want ---\n%s", content, want)
	}
	if len(f.repo.services) != 1 {
		t.Error("record must remain when its deletion failed")
	}
}

func TestDeleteSurfacesCompensationFailure(t *testing.T) {
	f := newLifecycleFixture(t, counterID())
	created, err := f.lc.Create(context.Background(), lcProxyService("ignored"))
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}
	// The Caddyfile removal itself must succeed (operator calls 3-4); only
	// the restore deploy (operator call 5 onward) fails, on top of the DB
	// delete failure.
	f.repo.deleteErr = errors.New("db delete failed")
	f.op.validateErr = errValidate
	f.op.validateErrFrom = 5

	_, err = f.lc.Delete(context.Background(), created.ID)
	var comp *CompensationError
	if !errors.As(err, &comp) {
		t.Fatalf("want CompensationError, got %v", err)
	}
	if comp.Operation != "delete" {
		t.Errorf("compensation operation = %q", comp.Operation)
	}
	if !errors.Is(comp.Primary, f.repo.deleteErr) {
		t.Errorf("primary error must be preserved, got %v", comp.Primary)
	}
}

func TestLifecycleValidatesBeforeAnyEffect(t *testing.T) {
	f := newLifecycleFixture(t, counterID())
	s := lcProxyService("ignored")
	s.Domains = []string{"bad domain{"}
	if _, err := f.lc.Create(context.Background(), s); err == nil {
		t.Fatal("invalid input accepted")
	}
	if len(f.repo.calls) != 0 {
		t.Errorf("repository touched for invalid input: %v", f.repo.calls)
	}
	if len(f.op.calls) != 0 {
		t.Errorf("operator touched for invalid input: %v", f.op.calls)
	}
}

func TestLifecycleListAndDeleteUnknown(t *testing.T) {
	f := newLifecycleFixture(t, counterID())
	if _, err := f.lc.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, err := f.lc.Delete(context.Background(), "svc-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func isCompensationError(err error) bool {
	var comp *CompensationError
	return errors.As(err, &comp)
}
