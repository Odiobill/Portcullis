package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"portcullis/control-plane/internal/registry"
	"portcullis/control-plane/internal/session"
)

// fakeCaddyOp is an injected validate/reload double for the server tests.
type fakeCaddyOp struct {
	validateErr, reloadErr error
	// validateErrFrom, when > 0, applies validateErr only from that 1-based
	// call onward (0 = immediately).
	validateErrFrom int
	calls           []string
}

func (f *fakeCaddyOp) Validate() error {
	f.calls = append(f.calls, "validate")
	if f.validateErr != nil && (f.validateErrFrom == 0 || len(f.calls) >= f.validateErrFrom) {
		return f.validateErr
	}
	return nil
}

func (f *fakeCaddyOp) Reload() error {
	f.calls = append(f.calls, "reload")
	return f.reloadErr
}

// fakeServiceRepo is an in-memory repository double with injectable failures.
type fakeServiceRepo struct {
	services map[string]registry.Service
	order    []string
	calls    []string

	createErr, updateErr, deleteErr, getErr, listErr error
}

func newFakeServiceRepo() *fakeServiceRepo {
	return &fakeServiceRepo{services: map[string]registry.Service{}}
}

func (f *fakeServiceRepo) Create(_ context.Context, s registry.Service) error {
	f.calls = append(f.calls, "create:"+s.ID)
	if f.createErr != nil {
		return f.createErr
	}
	if _, ok := f.services[s.ID]; ok {
		return errors.New("duplicate")
	}
	f.services[s.ID] = s
	f.order = append(f.order, s.ID)
	return nil
}

func (f *fakeServiceRepo) Update(_ context.Context, s registry.Service) error {
	f.calls = append(f.calls, "update:"+s.ID)
	if f.updateErr != nil {
		return f.updateErr
	}
	if _, ok := f.services[s.ID]; !ok {
		return registry.ErrNotFound
	}
	f.services[s.ID] = s
	return nil
}

func (f *fakeServiceRepo) Delete(_ context.Context, id string) error {
	f.calls = append(f.calls, "delete:"+id)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.services[id]; !ok {
		return registry.ErrNotFound
	}
	delete(f.services, id)
	return nil
}

func (f *fakeServiceRepo) Get(_ context.Context, id string) (registry.Service, error) {
	f.calls = append(f.calls, "get:"+id)
	if f.getErr != nil {
		return registry.Service{}, f.getErr
	}
	s, ok := f.services[id]
	if !ok {
		return registry.Service{}, registry.ErrNotFound
	}
	return s, nil
}

func (f *fakeServiceRepo) List(_ context.Context) ([]registry.Service, error) {
	f.calls = append(f.calls, "list")
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := []registry.Service{}
	for _, id := range f.order {
		if s, ok := f.services[id]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

var idCounter int

func nextTestID() string {
	idCounter++
	return "svc-test0000" + string(rune('a'+idCounter-1))
}

type serviceFixture struct {
	srv    *Server
	repo   *fakeServiceRepo
	op     *fakeCaddyOp
	mgr    *session.Manager
	genDir string
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	mgr, err := session.NewManager(testSecret, time.Hour)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	repo := newFakeServiceRepo()
	op := &fakeCaddyOp{}
	genDir := filepath.Join(t.TempDir(), "generated")
	store := registry.NewStore(genDir, op)
	lc := registry.NewLifecycle(repo, store, nextTestID)
	idCounter = 0
	srv := New(Config{Passcode: testPasscode, SessionManager: mgr, Lifecycle: lc})
	return &serviceFixture{srv: srv, repo: repo, op: op, mgr: mgr, genDir: genDir}
}

func sessionTokenFrom(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName && c.Value != "" {
			return c.Value
		}
	}
	t.Fatal("no session cookie in response")
	return ""
}

func authedGet(t *testing.T, f *serviceFixture, token, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	return rec
}

// authedPost posts form values with session and CSRF token.
func authedPost(t *testing.T, f *serviceFixture, token, csrf, path string, vals url.Values) *httptest.ResponseRecorder {
	t.Helper()
	if vals == nil {
		vals = url.Values{}
	}
	if csrf != "" {
		vals.Set("csrf_token", csrf)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	return rec
}

func proxyForm() url.Values {
	return url.Values{
		"service_type":    {"proxy"},
		"domains":         {"App.Example.com"},
		"tls_mode":        {"acme"},
		"proxy_container": {"app_container"},
		"proxy_port":      {"3000"},
	}
}

func staticForm() url.Values {
	return url.Values{
		"service_type": {"static"},
		"domains":      {"static.example.com"},
		"tls_mode":     {"internal"},
		"static_root":  {"/srv/sites/static.example.com"},
	}
}

func TestDashboardListsServices(t *testing.T) {
	f := newServiceFixture(t)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, err := f.mgr.CSRFToken(token, time.Now())
	if err != nil {
		t.Fatalf("CSRFToken: %v", err)
	}

	// Empty dashboard renders in English.
	rec2 := authedGet(t, f, token, "/dashboard")
	if rec2.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "Add service") {
		t.Error("dashboard lacks Add service affordance")
	}

	// Create a service through the HTTP flow, then it must be listed.
	rec3 := authedPost(t, f, token, csrf, "/services", proxyForm())
	if rec3.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d, body: %s", rec3.Code, rec3.Body.String())
	}
	rec4 := authedGet(t, f, token, "/dashboard")
	if !strings.Contains(rec4.Body.String(), "app.example.com") {
		t.Errorf("dashboard does not list the service:\n%s", rec4.Body.String())
	}
	if !strings.Contains(rec4.Body.String(), "svc-test0000a") {
		t.Error("dashboard does not show the service ID")
	}
}

func TestServiceCRUDFlowsProxyAndStatic(t *testing.T) {
	f := newServiceFixture(t)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.mgr.CSRFToken(token, time.Now())

	// Create proxy service via HTTP (post/redirect/get).
	rec1 := authedPost(t, f, token, csrf, "/services", proxyForm())
	if rec1.Code != http.StatusSeeOther || rec1.Header().Get("Location") != "/dashboard" {
		t.Fatalf("proxy create: status = %d, location = %q", rec1.Code, rec1.Header().Get("Location"))
	}
	if _, ok := f.repo.services["svc-test0000a"]; !ok {
		t.Fatal("proxy service not persisted")
	}
	if _, err := os.Stat(filepath.Join(f.genDir, "svc-test0000a.caddy")); err != nil {
		t.Fatalf("proxy generated file missing: %v", err)
	}

	// Create static service via HTTP.
	rec2 := authedPost(t, f, token, csrf, "/services", staticForm())
	if rec2.Code != http.StatusSeeOther {
		t.Fatalf("static create: status = %d, body: %s", rec2.Code, rec2.Body.String())
	}
	if _, ok := f.repo.services["svc-test0000b"]; !ok {
		t.Fatal("static service not persisted")
	}
	if _, err := os.Stat(filepath.Join(f.genDir, "svc-test0000b.caddy")); err != nil {
		t.Fatalf("static generated file missing: %v", err)
	}

	// Edit form prefills.
	rec3 := authedGet(t, f, token, "/services/svc-test0000a/edit")
	if rec3.Code != http.StatusOK || !strings.Contains(rec3.Body.String(), "app.example.com") {
		t.Fatalf("edit form: status = %d", rec3.Code)
	}

	// Edit persists the change and redeploys.
	edit := proxyForm()
	edit.Set("domains", "moved.example.com")
	rec4 := authedPost(t, f, token, csrf, "/services/svc-test0000a", edit)
	if rec4.Code != http.StatusSeeOther {
		t.Fatalf("edit: status = %d, body: %s", rec4.Code, rec4.Body.String())
	}
	if got := f.repo.services["svc-test0000a"].Domains[0]; got != "moved.example.com" {
		t.Errorf("edit not persisted: %v", got)
	}
	content, _ := os.ReadFile(filepath.Join(f.genDir, "svc-test0000a.caddy"))
	if !strings.Contains(string(content), "moved.example.com") {
		t.Errorf("generated file not updated: %s", content)
	}

	// Delete removes the file, then the record.
	rec5 := authedPost(t, f, token, csrf, "/services/svc-test0000a/delete", nil)
	if rec5.Code != http.StatusSeeOther {
		t.Fatalf("delete: status = %d", rec5.Code)
	}
	if _, ok := f.repo.services["svc-test0000a"]; ok {
		t.Error("record not deleted")
	}
	if _, err := os.Stat(filepath.Join(f.genDir, "svc-test0000a.caddy")); !os.IsNotExist(err) {
		t.Error("generated file not removed")
	}

	// Static service still intact.
	if _, ok := f.repo.services["svc-test0000b"]; !ok {
		t.Error("static service must be unaffected")
	}
}

func TestServiceRoutesRejectUnauthenticated(t *testing.T) {
	f := newServiceFixture(t)
	cases := []struct {
		method, path string
		vals         url.Values
	}{
		{http.MethodGet, "/services/new", nil},
		{http.MethodPost, "/services", proxyForm()},
		{http.MethodGet, "/services/svc-x/edit", nil},
		{http.MethodPost, "/services/svc-x", proxyForm()},
		{http.MethodPost, "/services/svc-x/delete", nil},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		if c.vals != nil {
			req = httptest.NewRequest(c.method, c.path, strings.NewReader(c.vals.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
			t.Errorf("%s %s unauthenticated status = %d, want redirect", c.method, c.path, rec.Code)
		}
	}
	if len(f.repo.calls) != 0 {
		t.Errorf("repository touched without a session: %v", f.repo.calls)
	}
	if len(f.op.calls) != 0 {
		t.Errorf("operator touched without a session: %v", f.op.calls)
	}
}

func TestServiceMutationsRequireCSRF(t *testing.T) {
	cases := []struct {
		name string
		csrf string
	}{
		{"missing", ""},
		{"forged", "totally-forged.value"},
		{"predictable", "authenticated"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newServiceFixture(t)
			rec := login(t, f.srv, testPasscode)
			token := sessionTokenFrom(t, rec)

			for _, path := range []string{"/services", "/services/svc-test0000a", "/services/svc-test0000a/delete"} {
				rec := authedPost(t, f, token, c.csrf, path, proxyForm())
				if rec.Code != http.StatusForbidden {
					t.Errorf("%s: status = %d, want 403", path, rec.Code)
				}
			}
			// Zero registry or Caddy effects.
			if len(f.repo.calls) != 0 {
				t.Errorf("repository effects without CSRF: %v", f.repo.calls)
			}
			if len(f.op.calls) != 0 {
				t.Errorf("Caddy effects without CSRF: %v", f.op.calls)
			}
			entries, _ := os.ReadDir(f.genDir)
			if len(entries) != 0 {
				t.Errorf("files written without CSRF: %v", entries)
			}
		})
	}
}

func TestServiceMutationsRejectExpiredCSRF(t *testing.T) {
	// Server with an injectable clock so CSRF expiry can be simulated.
	// Long-lived session with the default 1-hour CSRF TTL: the session stays
	// valid while the CSRF token has expired.
	mgr, err := session.NewManager(testSecret, 10*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeServiceRepo()
	op := &fakeCaddyOp{}
	genDir := filepath.Join(t.TempDir(), "generated")
	lc := registry.NewLifecycle(repo, registry.NewStore(genDir, op), nextTestID)
	clock := time.Now()
	srv := New(Config{Passcode: testPasscode, SessionManager: mgr, Lifecycle: lc, Now: func() time.Time { return clock }})

	rec := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"passcode": {testPasscode}}.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Handler().ServeHTTP(rec, loginReq)
	token := sessionTokenFrom(t, rec)
	csrf, err := mgr.CSRFToken(token, clock)
	if err != nil {
		t.Fatal(err)
	}

	// Advance the clock past the CSRF (and session) TTL.
	clock = clock.Add(2 * time.Hour)

	fx := &serviceFixture{srv: srv, repo: repo, op: op, mgr: mgr, genDir: genDir}
	rec2 := authedPost(t, fx, token, csrf, "/services", proxyForm())
	if rec2.Code != http.StatusForbidden {
		t.Errorf("expired CSRF status = %d, want 403", rec2.Code)
	}
	if len(repo.calls) != 0 || len(op.calls) != 0 {
		t.Errorf("effects after CSRF expiry: repo=%v op=%v", repo.calls, op.calls)
	}
}

func TestCreateRejectsInvalidInputBeforeAnyEffect(t *testing.T) {
	f := newServiceFixture(t)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.mgr.CSRFToken(token, time.Now())

	bad := proxyForm()
	bad.Set("domains", "not a domain{")
	rec2 := authedPost(t, f, token, csrf, "/services", bad)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("invalid input status = %d, want 400", rec2.Code)
	}
	if len(f.repo.calls) != 0 {
		t.Errorf("repository touched for invalid input: %v", f.repo.calls)
	}
	if len(f.op.calls) != 0 {
		t.Errorf("operator touched for invalid input: %v", f.op.calls)
	}
	entries, _ := os.ReadDir(f.genDir)
	if len(entries) != 0 {
		t.Errorf("files written for invalid input: %v", entries)
	}
	// The form re-renders with an English validation message.
	if !strings.Contains(rec2.Body.String(), "valid domain") && !strings.Contains(rec2.Body.String(), "domain") {
		t.Errorf("validation feedback missing:\n%s", rec2.Body.String())
	}
}

func TestCreateSurfacesDeployFailureWithCompensation(t *testing.T) {
	f := newServiceFixture(t)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.mgr.CSRFToken(token, time.Now())

	f.op.validateErr = errors.New("caddy validation failed (test)")
	rec2 := authedPost(t, f, token, csrf, "/services", proxyForm())
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("deploy failure status = %d, want 500", rec2.Code)
	}
	// Compensation: the persisted record must be gone again.
	if len(f.repo.services) != 0 {
		t.Errorf("record not compensated after deploy failure: %v", f.repo.services)
	}
	if _, err := os.Stat(filepath.Join(f.genDir, "svc-test0000a.caddy")); !os.IsNotExist(err) {
		t.Error("no generated file may remain after a compensated create")
	}
}

func TestCreateSurfacesCompensationFailure(t *testing.T) {
	f := newServiceFixture(t)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.mgr.CSRFToken(token, time.Now())

	f.op.validateErr = errors.New("caddy validation failed (test)")
	f.repo.deleteErr = errors.New("compensation delete failed")
	rec2 := authedPost(t, f, token, csrf, "/services", proxyForm())
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "could not be completed") {
		t.Errorf("compensation failure must be surfaced distinctly in English, body:\n%s", rec2.Body.String())
	}
}

func TestDeleteSurfacesCompensationFailure(t *testing.T) {
	f := newServiceFixture(t)
	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.mgr.CSRFToken(token, time.Now())

	// Seed a service through the normal flow.
	if rec := authedPost(t, f, token, csrf, "/services", proxyForm()); rec.Code != http.StatusSeeOther {
		t.Fatalf("seed create failed: %d", rec.Code)
	}
	// DB delete fails and the generated-file restore also fails. The seed
	// create consumed operator calls 1-2; the removal (calls 3-4) must
	// succeed so the failure happens at the DB delete, and only the restore
	// (call 5 onward) fails validation.
	f.repo.deleteErr = errors.New("db delete failed")
	f.op.validateErr = errors.New("caddy validation failed (test)")
	f.op.validateErrFrom = 5
	rec2 := authedPost(t, f, token, csrf, "/services/svc-test0000a/delete", nil)
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "could not be completed") {
		t.Errorf("compensation failure must be surfaced distinctly, body:\n%s", rec2.Body.String())
	}
}

func TestServiceLifecycleNeverTouchesManualDir(t *testing.T) {
	f := newServiceFixture(t)
	root := filepath.Dir(f.genDir)
	manualDir := filepath.Join(root, "manual")
	if err := os.MkdirAll(manualDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manualFile := filepath.Join(manualDir, "operator.caddy")
	manualContent := "# operator-owned\nimport acme\n"
	if err := os.WriteFile(manualFile, []byte(manualContent), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	csrf, _ := f.mgr.CSRFToken(token, time.Now())

	if rec := authedPost(t, f, token, csrf, "/services", proxyForm()); rec.Code != http.StatusSeeOther {
		t.Fatalf("create failed: %d", rec.Code)
	}
	if rec := authedPost(t, f, token, csrf, "/services/svc-test0000a/delete", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete failed: %d", rec.Code)
	}

	if got, _ := os.ReadFile(manualFile); string(got) != manualContent {
		t.Errorf("manual file changed:\n%s", got)
	}
	entries, _ := os.ReadDir(manualDir)
	if len(entries) != 1 || entries[0].Name() != "operator.caddy" {
		t.Errorf("manual dir gained entries: %v", entries)
	}
}

func TestDashboardWithoutLifecycleStaysAvailable(t *testing.T) {
	mgr, err := session.NewManager(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Config{Passcode: testPasscode, SessionManager: mgr})
	rec := login(t, srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), "not available") {
		t.Errorf("foundation dashboard must remain usable without a lifecycle, status = %d", rec2.Code)
	}
}
