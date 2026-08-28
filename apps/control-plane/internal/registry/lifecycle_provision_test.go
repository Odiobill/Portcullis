package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"portcullis/control-plane/internal/provision"
)

// fakeProvisioner records specs and can fail.
type fakeProvisioner struct {
	specs []provision.Spec
	calls int
	err   error
}

func (f *fakeProvisioner) Provision(_ context.Context, spec provision.Spec) error {
	f.calls++
	f.specs = append(f.specs, spec)
	return f.err
}

type provisionFixture struct {
	repo   *fakeServiceRepo
	op     *fakeOperator
	prov   *fakeProvisioner
	lc     *Lifecycle
	genDir string
}

func newProvisionFixture(t *testing.T) *provisionFixture {
	t.Helper()
	f := &provisionFixture{
		repo:   newFakeServiceRepo(),
		op:     &fakeOperator{},
		prov:   &fakeProvisioner{},
		genDir: filepath.Join(t.TempDir(), "generated"),
	}
	f.lc = NewLifecycle(f.repo, NewStore(f.genDir, f.op), counterID())
	f.lc.Provisioner = f.prov
	return f
}

func TestCreateProvisionedGeneratesIdentifiersAndOneTimeCredential(t *testing.T) {
	f := newProvisionFixture(t)

	for _, typ := range []ServiceType{TypeProxy, TypeStatic} {
		s := lcProxyService("ignored")
		s.Type = typ
		if typ == TypeStatic {
			s = lcStaticLike()
		}
		created, cred, err := f.lc.CreateProvisioned(context.Background(), s)
		if err != nil {
			t.Fatalf("CreateProvisioned(%s): %v", typ, err)
		}

		// Identifiers are server-owned, derived deterministically from the
		// opaque service ID.
		wantDB, wantUser, err := provision.Identifiers(created.ID)
		if err != nil {
			t.Fatalf("Identifiers(%s): %v", created.ID, err)
		}
		if created.DBName != wantDB || created.DBUser != wantUser {
			t.Errorf("persisted identifiers = %q/%q, want %q/%q", created.DBName, created.DBUser, wantDB, wantUser)
		}
		stored := f.repo.services[created.ID]
		if stored.DBName != wantDB || stored.DBUser != wantUser {
			t.Errorf("stored identifiers = %q/%q", stored.DBName, stored.DBUser)
		}

		// The credential is returned once: DB name/user plus a
		// cryptographically generated password.
		if cred == nil || cred.Password == "" {
			t.Fatal("no one-time credential returned")
		}
		if cred.DBName != wantDB || cred.DBUser != wantUser {
			t.Errorf("credential identifiers = %q/%q", cred.DBName, cred.DBUser)
		}
		if len(cred.Password) < 24 {
			t.Errorf("password too short: %d", len(cred.Password))
		}

		// Provisioner received exactly the generated spec.
		if f.prov.calls != 1 || f.prov.specs[0].DBName != wantDB ||
			f.prov.specs[0].UserName != wantUser || f.prov.specs[0].Password != cred.Password {
			t.Errorf("provisioner specs = %+v", f.prov.specs)
		}

		// Reset for the second type run.
		f.prov.calls = 0
		f.prov.specs = nil
		delete(f.repo.services, created.ID)
		f.repo.createOrder = nil
		if err := os.Remove(filepath.Join(f.genDir, created.ID+".caddy")); err != nil {
			t.Fatal(err)
		}
		f.op.calls = nil
	}
}

// lcStaticLike is a valid static service for provisioning tests.
func lcStaticLike() Service {
	return Service{
		ID:         "ignored",
		Type:       TypeStatic,
		Domains:    []string{"static.example.com"},
		TLSMode:    TLSInternal,
		StaticRoot: "/srv/sites/static.example.com",
	}
}

func TestCreateWithoutProvisioningUnchanged(t *testing.T) {
	f := newProvisionFixture(t)
	created, err := f.lc.Create(context.Background(), lcProxyService("ignored"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.DBName != "" || created.DBUser != "" {
		t.Errorf("unopt-in record gained DB identifiers: %q/%q", created.DBName, created.DBUser)
	}
	if f.prov.calls != 0 {
		t.Errorf("provisioner called without opt-in: %d", f.prov.calls)
	}
}

func TestCreateProvisionedRequiresProvisioner(t *testing.T) {
	f := newProvisionFixture(t)
	f.lc.Provisioner = nil
	_, _, err := f.lc.CreateProvisioned(context.Background(), lcProxyService("ignored"))
	if err == nil {
		t.Fatal("provisioning without an injected provisioner must fail closed")
	}
	if len(f.repo.calls) != 0 || len(f.op.calls) != 0 {
		t.Errorf("effects before availability check: repo=%v op=%v", f.repo.calls, f.op.calls)
	}
}

func TestProvisioningFailureCompensatesLifecycle(t *testing.T) {
	f := newProvisionFixture(t)
	f.prov.err = errors.New("database create failed")

	_, _, err := f.lc.CreateProvisioned(context.Background(), lcProxyService("ignored"))
	if err == nil {
		t.Fatal("provisioning failure must be surfaced")
	}
	if isCompensationError(err) {
		t.Fatalf("successful compensation must not be a compensation error: %v", err)
	}
	// The accepted lifecycle compensation path removed the record and the
	// generated file.
	if len(f.repo.services) != 0 {
		t.Errorf("record not compensated: %v", f.repo.services)
	}
	entries, _ := os.ReadDir(f.genDir)
	if len(entries) != 0 {
		t.Errorf("generated files not compensated: %v", entries)
	}
}

func TestProvisioningFailureSurfacesCompensationFailure(t *testing.T) {
	f := newProvisionFixture(t)
	f.prov.err = errors.New("database create failed")
	// Seed create consumed operator calls 1-2. The compensating delete runs
	// store.Remove (calls 3-4, must succeed) then repo.Delete (fails), then
	// the restore deploy (call 5) fails.
	f.repo.deleteErr = errors.New("delete failed")
	f.op.validateErr = errValidate
	f.op.validateErrFrom = 5

	_, _, err := f.lc.CreateProvisioned(context.Background(), lcProxyService("ignored"))
	var comp *CompensationError
	if !errors.As(err, &comp) {
		t.Fatalf("want CompensationError, got %v", err)
	}
	if comp.Operation != "create-provision" {
		t.Errorf("operation = %q", comp.Operation)
	}
	if comp.Compensate == nil {
		t.Error("compensation failure not preserved")
	}
}

// TestProvisioningCleanupFailureSurfacesCompensationEvenWhenRegistryCleanupSucceeds
// pins the compensation-safety rule: when the provisioner could not clean
// up its own partial database/role, CreateProvisioned must return
// *CompensationError / ErrCompensationFailed even though the registry and
// Caddy cleanup succeeded — silent complete-rollback claims are forbidden.
func TestProvisioningCleanupFailureSurfacesCompensationEvenWhenRegistryCleanupSucceeds(t *testing.T) {
	f := newProvisionFixture(t)
	f.prov.err = &provision.CleanupError{
		Primary:  errors.New("database create failed"),
		Failures: []error{errors.New("drop role failed")},
	}

	_, _, err := f.lc.CreateProvisioned(context.Background(), lcProxyService("ignored"))
	var comp *CompensationError
	if !errors.As(err, &comp) {
		t.Fatalf("cleanup failure must surface as *CompensationError, got %v", err)
	}
	if !errors.Is(err, ErrCompensationFailed) {
		t.Errorf("err must match ErrCompensationFailed, got %v", err)
	}
	if comp.Operation != "create-provision" {
		t.Errorf("operation = %q", comp.Operation)
	}
	// The registry/Caddy compensation itself succeeded.
	if len(f.repo.services) != 0 {
		t.Errorf("record not compensated: %v", f.repo.services)
	}
	entries, _ := os.ReadDir(f.genDir)
	if len(entries) != 0 {
		t.Errorf("generated files not compensated: %v", entries)
	}
}

// TestProvisioningCleanupAndRegistryCleanupBothFailPreservesBoth pins that
// when both the provisioner cleanup and the registry/Caddy compensation
// fail, the material errors are preserved as a *CompensationError.
func TestProvisioningCleanupAndRegistryCleanupBothFailPreservesBoth(t *testing.T) {
	f := newProvisionFixture(t)
	f.prov.err = &provision.CleanupError{
		Primary:  errors.New("database create failed"),
		Failures: []error{errors.New("drop role failed")},
	}
	// Create consumed op calls 1-2; compensating delete: Remove calls 3-4
	// succeed, repo.Delete fails, restore validate (5) fails.
	f.repo.deleteErr = errors.New("delete failed")
	f.op.validateErr = errValidate
	f.op.validateErrFrom = 5

	_, _, err := f.lc.CreateProvisioned(context.Background(), lcProxyService("ignored"))
	var comp *CompensationError
	if !errors.As(err, &comp) {
		t.Fatalf("want *CompensationError, got %v", err)
	}
	if !errors.Is(err, ErrCompensationFailed) {
		t.Errorf("err must match ErrCompensationFailed, got %v", err)
	}
	var cleanup *provision.CleanupError
	if !errors.As(err, &cleanup) {
		t.Errorf("provisioner cleanup failure not preserved: %v", err)
	}
	if !errors.Is(comp.Compensate, f.repo.deleteErr) {
		t.Errorf("registry/Caddy cleanup failure not preserved: %v", comp.Compensate)
	}
}

func TestPasswordNeverPersistedOrLogged(t *testing.T) {
	f := newProvisionFixture(t)
	created, cred, err := f.lc.CreateProvisioned(context.Background(), lcProxyService("ignored"))
	if err != nil {
		t.Fatalf("CreateProvisioned: %v", err)
	}
	stored := f.repo.services[created.ID]
	// The Service struct carries only DB name/user; assert the password is
	// nowhere in the persisted representation.
	if strings.Contains(stored.DBName, cred.Password) || strings.Contains(stored.DBUser, cred.Password) {
		t.Error("password leaked into persisted identifiers")
	}
}

func TestDeleteProvisionedServiceFailsClosed(t *testing.T) {
	f := newProvisionFixture(t)
	created, _, err := f.lc.CreateProvisioned(context.Background(), lcProxyService("ignored"))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	f.op.calls = nil
	f.repo.calls = nil

	_, err = f.lc.Delete(context.Background(), created.ID)
	if !errors.Is(err, ErrProvisionedService) {
		t.Fatalf("want ErrProvisionedService, got %v", err)
	}
	// Fail closed: no Caddy or registry mutation, no admin call.
	if len(f.op.calls) != 0 {
		t.Errorf("operator touched during fail-closed delete: %v", f.op.calls)
	}
	if _, ok := f.repo.services[created.ID]; !ok {
		t.Error("record deleted despite fail-closed guard")
	}
	if _, err := os.Stat(filepath.Join(f.genDir, created.ID+".caddy")); err != nil {
		t.Error("generated file removed despite fail-closed guard")
	}
}

func TestDeleteUnprovisionedStillWorks(t *testing.T) {
	f := newProvisionFixture(t)
	created, err := f.lc.Create(context.Background(), lcProxyService("ignored"))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := f.lc.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("Delete unprovisioned: %v", err)
	}
	if _, ok := f.repo.services[created.ID]; ok {
		t.Error("record not deleted")
	}
}

// TestEditPreservesProvisioningMetadata pins the immutability invariant:
// DBName/DBUser are server-owned metadata that survive any edit regardless
// of caller-supplied values, so the fail-closed deletion guard keeps
// protecting the real project database afterwards.
func TestEditPreservesProvisioningMetadata(t *testing.T) {
	f := newProvisionFixture(t)
	created, cred, err := f.lc.CreateProvisioned(context.Background(), lcProxyService("ignored"))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Simulate an owner edit form: DB fields are absent (empty).
	edited := created
	edited.Domains = []string{"moved.example.com"}
	edited.DBName = ""
	edited.DBUser = ""
	updated, err := f.lc.Edit(context.Background(), edited)
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if updated.DBName != cred.DBName || updated.DBUser != cred.DBUser {
		t.Fatalf("edit cleared provisioning metadata: %q/%q, want %q/%q",
			updated.DBName, updated.DBUser, cred.DBName, cred.DBUser)
	}
	stored := f.repo.services[created.ID]
	if stored.DBName != cred.DBName || stored.DBUser != cred.DBUser {
		t.Fatalf("stored metadata cleared: %q/%q", stored.DBName, stored.DBUser)
	}

	// The fail-closed deletion guard must still protect the database.
	f.op.calls = nil
	f.repo.calls = nil
	_, err = f.lc.Delete(context.Background(), created.ID)
	if !errors.Is(err, ErrProvisionedService) {
		t.Fatalf("want ErrProvisionedService after edit, got %v", err)
	}
	if len(f.op.calls) != 0 {
		t.Errorf("operator touched during fail-closed delete: %v", f.op.calls)
	}
	if _, ok := f.repo.services[created.ID]; !ok {
		t.Error("record deleted despite fail-closed guard")
	}
}
