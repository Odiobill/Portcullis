package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"portcullis/control-plane/internal/dump"
	"portcullis/control-plane/internal/registry"
	"portcullis/control-plane/internal/session"
)

// fakeDumpCommander records dump process starts without executing anything.
type fakeDumpCommander struct {
	mu          sync.Mutex
	starts      int
	lastArgs    []string
	lastEnv     []string
	startErr    error
	cancelCalls int
	streamErr   error
}

func (f *fakeDumpCommander) Start(_ context.Context, name string, args []string, env []string) (io.ReadCloser, func() error, func(), error) {
	f.mu.Lock()
	f.starts++
	f.lastArgs = args
	f.lastEnv = env
	f.mu.Unlock()
	if f.startErr != nil {
		return nil, nil, nil, f.startErr
	}
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.WriteString(pw, "PGDMP-fake-dump-payload")
		_ = pw.Close()
	}()
	return pr,
		func() error { return f.streamErr },
		func() {
			f.mu.Lock()
			f.cancelCalls++
			f.mu.Unlock()
		},
		nil
}

type dumpFixture struct {
	srv  *Server
	repo *fakeServiceRepo
	op   *fakeCaddyOp
	prov *fakeProvisioner
	cmd  *fakeDumpCommander
}

func newDumpFixture(t *testing.T, cfgErr func(*fakeDumpCommander)) *dumpFixture {
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
	cmd := &fakeDumpCommander{}
	if cfgErr != nil {
		cfgErr(cmd)
	}
	dumper, err := dump.New(dump.Config{DBHost: "postgres.internal", DBUser: "dump_user", Commander: cmd})
	if err != nil {
		t.Fatalf("dumper: %v", err)
	}
	srv := New(Config{Passcode: testPasscode, SessionManager: mgr, Lifecycle: lc, Dumper: dumper})
	return &dumpFixture{srv: srv, repo: repo, op: op, prov: prov, cmd: cmd}
}

func (f *dumpFixture) provisionedSessionAndCSRF(t *testing.T) (token, csrf, serviceID string) {
	t.Helper()
	rec := login(t, f.srv, testPasscode)
	token = sessionTokenFrom(t, rec)
	csrf, err := f.srv.sessions.CSRFToken(token, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services", provisionForm())
	if rec2.Code != http.StatusOK {
		t.Fatalf("seed provisioned create failed: %d", rec2.Code)
	}
	for id := range f.repo.services {
		serviceID = id
	}
	if serviceID == "" {
		t.Fatal("no service persisted")
	}
	return token, csrf, serviceID
}

func TestDumpSuccessStreamsAttachment(t *testing.T) {
	f := newDumpFixture(t, nil)
	token, csrf, id := f.provisionedSessionAndCSRF(t)

	dl := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services/"+id+"/dump", nil)

	if dl.Code != http.StatusOK {
		t.Fatalf("dump status = %d, body: %s", dl.Code, dl.Body.String())
	}
	if ct := dl.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	cd := dl.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".dump") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if !strings.Contains(dl.Body.String(), "PGDMP-fake-dump-payload") {
		t.Error("dump payload not streamed")
	}
	if f.cmd.starts != 1 {
		t.Errorf("process starts = %d, want 1", f.cmd.starts)
	}
}

func TestDumpRequiresSessionAndCSRFWithZeroEffects(t *testing.T) {
	t.Run("unauthenticated", func(t *testing.T) {
		f := newDumpFixture(t, nil)
		_, _, id := f.provisionedSessionAndCSRF(t)
		before := f.effects()
		req := httptest.NewRequest(http.MethodPost, "/services/"+id+"/dump", nil)
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
			t.Errorf("status = %d, want redirect", rec.Code)
		}
		assertNoDumpEffects(t, f, before)
	})
	t.Run("missing-csrf", func(t *testing.T) {
		f := newDumpFixture(t, nil)
		token, _, id := f.provisionedSessionAndCSRF(t)
		before := f.effects()
		rec := authedPost(t, &serviceFixture{srv: f.srv}, token, "", "/services/"+id+"/dump", nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
		assertNoDumpEffects(t, f, before)
	})
	t.Run("forged-csrf", func(t *testing.T) {
		f := newDumpFixture(t, nil)
		token, _, id := f.provisionedSessionAndCSRF(t)
		before := f.effects()
		rec := authedPost(t, &serviceFixture{srv: f.srv}, token, "forged.token", "/services/"+id+"/dump", nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
		assertNoDumpEffects(t, f, before)
	})
}

// assertNoDumpEffects verifies that the baseline snapshot taken before the
// dump request is unchanged: no process start, no registry mutation, no
// provisioner call. The seed's own legitimate effects are excluded.
func assertNoDumpEffects(t *testing.T, f *dumpFixture, before dumpEffects) {
	t.Helper()
	if f.cmd.starts != before.processStarts {
		t.Errorf("process started without auth/CSRF: %d -> %d", before.processStarts, f.cmd.starts)
	}
	if f.prov.calls != before.provisionerCalls {
		t.Errorf("provisioner effects without auth/CSRF: %d -> %d", before.provisionerCalls, f.prov.calls)
	}
	if len(f.repo.services) != before.recordCount {
		t.Errorf("registry mutation without auth/CSRF: %d -> %d", before.recordCount, len(f.repo.services))
	}
	if strings.Contains(strings.Join(f.repo.calls, ","), "delete:") {
		t.Error("repository mutation without auth/CSRF")
	}
}

type dumpEffects struct {
	processStarts    int
	provisionerCalls int
	recordCount      int
}

func (f *dumpFixture) effects() dumpEffects {
	return dumpEffects{processStarts: f.cmd.starts, provisionerCalls: f.prov.calls, recordCount: len(f.repo.services)}
}

func TestDumpBearerHeaderDoesNotAuthorize(t *testing.T) {
	f := newDumpFixture(t, nil)
	_, _, id := f.provisionedSessionAndCSRF(t)

	req := httptest.NewRequest(http.MethodPost, "/services/"+id+"/dump", nil)
	req.Header.Set("Authorization", "Bearer totally-valid-looking-bearer-token")
	before := f.effects()
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Errorf("bearer-header status = %d, want redirect to login", rec.Code)
	}
	assertNoDumpEffects(t, f, before)
}

func TestDumpRejectsUnknownAndUnprovisionedServices(t *testing.T) {
	f := newDumpFixture(t, nil)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.srv.sessions.CSRFToken(token, time.Now())

	// Unknown service (valid session+CSRF; repository rejects it).
	rec1 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services/svc-missing/dump", nil)
	if rec1.Code != http.StatusNotFound {
		t.Errorf("unknown service status = %d, want 404", rec1.Code)
	}

	// Unprovisioned service.
	if rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services", proxyForm()); rec2.Code != http.StatusSeeOther {
		t.Fatalf("seed unprovisioned create failed: %d", rec2.Code)
	}
	var unprovisioned string
	for id := range f.repo.services {
		if f.repo.services[id].DBName == "" {
			unprovisioned = id
		}
	}
	rec3 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services/"+unprovisioned+"/dump", nil)
	if rec3.Code != http.StatusConflict {
		t.Errorf("unprovisioned service status = %d, want 409", rec3.Code)
	}
	if !strings.Contains(rec3.Body.String(), "no provisioned database") {
		t.Errorf("explanation missing:\n%s", rec3.Body.String())
	}
	if f.cmd.starts != 0 {
		t.Errorf("process started for rejected targets: %d", f.cmd.starts)
	}
}

func TestDumpRateLimited(t *testing.T) {
	f := newDumpFixture(t, nil)
	token, csrf, id := f.provisionedSessionAndCSRF(t)

	for i := 0; i < 2; i++ {
		dl := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services/"+id+"/dump", nil)
		if i == 0 {
			if dl.Code != http.StatusOK {
				t.Fatalf("first dump status = %d", dl.Code)
			}
			continue
		}
		if dl.Code != http.StatusTooManyRequests {
			t.Fatalf("second dump status = %d, want 429", dl.Code)
		}
		if !strings.Contains(dl.Body.String(), "five minutes") {
			t.Errorf("rate-limit guidance missing:\n%s", dl.Body.String())
		}
	}
	if f.cmd.starts != 1 {
		t.Errorf("process starts = %d, want 1", f.cmd.starts)
	}
}

func TestDumpStartFailureBeforeHeaders(t *testing.T) {
	f := newDumpFixture(t, func(c *fakeDumpCommander) { c.startErr = errors.New("pg_dump: binary not found") })
	token, csrf, id := f.provisionedSessionAndCSRF(t)

	dl := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services/"+id+"/dump", nil)

	if dl.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", dl.Code)
	}
	if dl.Header().Get("Content-Type") == "application/octet-stream" {
		t.Error("start failure must fail before streaming headers")
	}
	if !strings.Contains(dl.Body.String(), "could not be started") {
		t.Errorf("bounded English start-failure message missing:\n%s", dl.Body.String())
	}
	if strings.Contains(dl.Body.String(), "PGPASSWORD") {
		t.Error("start failure must not leak credentials")
	}
}

func TestDumpDashboardActionOnlyForProvisionedServices(t *testing.T) {
	f := newDumpFixture(t, nil)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.srv.sessions.CSRFToken(token, time.Now())

	// One provisioned and one unprovisioned service.
	if rec1 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services", provisionForm()); rec1.Code != http.StatusOK {
		t.Fatalf("provisioned seed failed: %d", rec1.Code)
	}
	if rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/services", proxyForm()); rec2.Code != http.StatusSeeOther {
		t.Fatalf("unprovisioned seed failed: %d", rec2.Code)
	}

	dash := authedGet(t, &serviceFixture{srv: f.srv}, token, "/dashboard")
	body := dash.Body.String()

	// Resolve both service IDs.
	var provisionedID, unprovisionedID string
	for id, svc := range f.repo.services {
		if svc.DBName != "" {
			provisionedID = id
		} else {
			unprovisionedID = id
		}
	}
	if provisionedID == "" || unprovisionedID == "" {
		t.Fatalf("fixture services incomplete: %v", f.repo.services)
	}
	if !strings.Contains(body, "Download dump") {
		t.Error("dashboard lacks the dump action for provisioned services")
	}
	if !strings.Contains(body, "/services/"+provisionedID+"/dump") {
		t.Errorf("dump action missing for provisioned service %s", provisionedID)
	}
	if strings.Contains(body, "/services/"+unprovisionedID+"/dump") {
		t.Error("dump action offered for an unprovisioned service")
	}
	// Credentials never appear on the dashboard.
	if strings.Contains(body, f.prov.specs[0].Password) {
		t.Error("dashboard leaked the provisioned password")
	}
}
