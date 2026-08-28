package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"portcullis/control-plane/internal/provision"
	"portcullis/control-plane/internal/registry"
	"portcullis/control-plane/internal/session"
)

// fakeProvisioner records provisioning specs and can fail.
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

type provisioningFixture struct {
	srv  *Server
	repo *fakeServiceRepo
	op   *fakeCaddyOp
	prov *fakeProvisioner
}

func newProvisioningFixture(t *testing.T) *provisioningFixture {
	t.Helper()
	mgr, err := session.NewManager(testSecret, time.Hour)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	repo := newFakeServiceRepo()
	op := &fakeCaddyOp{}
	prov := &fakeProvisioner{}
	lc := registry.NewLifecycle(repo, registry.NewStore(filepath.Join(t.TempDir(), "generated"), op), nextTestID)
	lc.Provisioner = prov
	srv := New(Config{Passcode: testPasscode, SessionManager: mgr, Lifecycle: lc})
	return &provisioningFixture{srv: srv, repo: repo, op: op, prov: prov}
}

func provisionForm() url.Values {
	return url.Values{"service_type": {"proxy"}, "domains": {"app.example.com"}, "tls_mode": {"acme"},
		"proxy_container": {"app_container"}, "proxy_port": {"3000"}, "provision_db": {"on"}}
}

func TestCreateWithProvisioningRendersOneTimeCredentialPage(t *testing.T) {
	f := newProvisioningFixture(t)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, err := f.srv.sessions.CSRFToken(token, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services", provisionForm())
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 credential page, body: %s", rec2.Code, rec2.Body.String())
	}
	if loc := rec2.Header().Get("Location"); loc != "" {
		t.Errorf("credential page must not be a redirect, got %q", loc)
	}

	// One-time presentation: DB name/user and the generated password appear.
	stored := f.repo.services["svc-test0000a"]
	body := rec2.Body.String()
	if !strings.Contains(body, stored.DBName) || !strings.Contains(body, stored.DBUser) {
		t.Errorf("credential page lacks identifiers:\n%s", body)
	}
	password := f.prov.specs[0].Password
	if !strings.Contains(body, password) {
		t.Errorf("credential page lacks the generated password:\n%s", body)
	}
	// Explicit one-time-save guidance.
	if !strings.Contains(strings.ToLower(body), "only once") && !strings.Contains(strings.ToLower(body), "save") {
		t.Error("credential page lacks one-time-save guidance")
	}
	// no-store protection.
	cc := rec2.Header().Get("Cache-Control")
	if !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestPasswordNeverInLaterResponses(t *testing.T) {
	f := newProvisioningFixture(t)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.srv.sessions.CSRFToken(token, time.Now())

	rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services", provisionForm())
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d", rec2.Code)
	}
	password := f.prov.specs[0].Password

	// Dashboard, edit form, and a second creation never contain the password.
	for _, path := range []string{"/dashboard", "/services/svc-test0000a/edit", "/services/new"} {
		dash := authedGet(t, &serviceFixture{srv: f.srv}, token, path)
		if strings.Contains(dash.Body.String(), password) {
			t.Errorf("password exposed at %s", path)
		}
	}
	// There is no credentials route to revisit.
	cred := authedGet(t, &serviceFixture{srv: f.srv}, token, "/services/svc-test0000a/credentials")
	if cred.Code != http.StatusNotFound {
		t.Errorf("credentials revisit status = %d, want 404", cred.Code)
	}
	// The cookie/session never carries the password.
	for _, c := range rec2.Result().Cookies() {
		if strings.Contains(c.Value, password) {
			t.Error("password leaked into a cookie")
		}
	}
}

func TestCreateWithoutProvisioningUnchangedAtHTTPLevel(t *testing.T) {
	f := newProvisioningFixture(t)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.srv.sessions.CSRFToken(token, time.Now())

	rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services", proxyForm())
	if rec2.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec2.Code)
	}
	if f.prov.calls != 0 {
		t.Errorf("provisioner called without opt-in: %d", f.prov.calls)
	}
	if f.repo.services["svc-test0000a"].DBName != "" {
		t.Error("unopt-in record gained DB identifiers")
	}
}

func TestProvisioningRequiresSessionAndCSRF(t *testing.T) {
	t.Run("unauthenticated", func(t *testing.T) {
		f := newProvisioningFixture(t)
		req := httptest.NewRequest(http.MethodPost, "/services", strings.NewReader(provisionForm().Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
			t.Errorf("status = %d, want redirect", rec.Code)
		}
		if f.prov.calls != 0 || len(f.repo.calls) != 0 || len(f.op.calls) != 0 {
			t.Errorf("effects without session: prov=%d repo=%v op=%v", f.prov.calls, f.repo.calls, f.op.calls)
		}
	})
	t.Run("missing-csrf", func(t *testing.T) {
		f := newProvisioningFixture(t)
		rec := login(t, f.srv, testPasscode)
		token := sessionTokenFrom(t, rec)
		rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, "", "/services", provisionForm())
		if rec2.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec2.Code)
		}
		if f.prov.calls != 0 || len(f.repo.calls) != 0 || len(f.op.calls) != 0 {
			t.Errorf("effects without CSRF: prov=%d repo=%v op=%v", f.prov.calls, f.repo.calls, f.op.calls)
		}
	})
	t.Run("forged-csrf", func(t *testing.T) {
		f := newProvisioningFixture(t)
		rec := login(t, f.srv, testPasscode)
		token := sessionTokenFrom(t, rec)
		rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, "forged.token", "/services", provisionForm())
		if rec2.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec2.Code)
		}
		if f.prov.calls != 0 || len(f.repo.calls) != 0 || len(f.op.calls) != 0 {
			t.Errorf("effects with forged CSRF: prov=%d repo=%v op=%v", f.prov.calls, f.repo.calls, f.op.calls)
		}
	})
}

func TestProvisioningFailureCompensatesWithDistinctMessage(t *testing.T) {
	f := newProvisioningFixture(t)
	f.prov.err = errors.New("database create failed")
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.srv.sessions.CSRFToken(token, time.Now())

	rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services", provisionForm())
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec2.Code)
	}
	body := rec2.Body.String()
	if !strings.Contains(body, "provisioning failed") || !strings.Contains(body, "removed") {
		t.Errorf("distinct compensation message missing:\n%s", body)
	}
	if strings.Contains(body, "succeeded") {
		t.Error("failed provisioning must not claim success")
	}
	// Registry/Caddy compensation happened.
	if len(f.repo.services) != 0 {
		t.Errorf("record not compensated: %v", f.repo.services)
	}
}

func TestProvisioningCompensationFailureExplicit(t *testing.T) {
	f := newProvisioningFixture(t)
	f.prov.err = errors.New("database create failed")
	f.repo.deleteErr = errors.New("delete failed")
	// Seed-free flow: create consumes op calls 1-2; compensating delete:
	// Remove calls 3-4 succeed, repo.Delete fails, restore validate (5) fails.
	f.op.validateErrFrom = 5
	f.op.validateErr = errors.New("caddy validation failed (test)")

	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.srv.sessions.CSRFToken(token, time.Now())

	rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services", provisionForm())
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec2.Code)
	}
	if !strings.Contains(strings.ToLower(rec2.Body.String()), "manual inspection") {
		t.Errorf("explicit compensation-failure message missing:\n%s", rec2.Body.String())
	}
	if strings.Contains(rec2.Body.String(), "succeeded") {
		t.Error("compensation failure must not claim success")
	}
}

func TestDeleteProvisionedFailsClosedAtHTTPLevel(t *testing.T) {
	f := newProvisioningFixture(t)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.srv.sessions.CSRFToken(token, time.Now())

	if rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services", provisionForm()); rec2.Code != http.StatusOK {
		t.Fatalf("seed create failed: %d", rec2.Code)
	}

	// Resolve the created ID from the repository (the counter is global).
	var createdID string
	for id := range f.repo.services {
		createdID = id
	}
	if createdID == "" {
		t.Fatal("seed create persisted nothing")
	}
	rec3 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services/"+createdID+"/delete", nil)
	if rec3.Code != http.StatusConflict {
		t.Fatalf("delete status = %d, want 409", rec3.Code)
	}
	if !strings.Contains(rec3.Body.String(), "database") {
		t.Errorf("fail-closed explanation missing:\n%s", rec3.Body.String())
	}
	if _, ok := f.repo.services[createdID]; !ok {
		t.Error("record deleted despite fail-closed guard")
	}
}
