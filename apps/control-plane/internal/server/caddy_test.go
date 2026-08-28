package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"portcullis/control-plane/internal/caddyops"
	"portcullis/control-plane/internal/registry"
	"portcullis/control-plane/internal/session"
)

// opsFixture wires a server with a reload operator, a log reader, and no
// lifecycle (service management stays covered by the other fixtures).
type opsFixture struct {
	srv    *Server
	op     *fakeCaddyOp
	logDir string
}

func newOpsFixture(t *testing.T, logContent string) *opsFixture {
	t.Helper()
	mgr, err := session.NewManager(testSecret, time.Hour)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	op := &fakeCaddyOp{}
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "portcullis.log")
	if logContent != "" {
		if err := os.WriteFile(logPath, []byte(logContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := caddyops.NewLogReader(logPath)
	if err != nil {
		t.Fatalf("NewLogReader: %v", err)
	}
	srv := New(Config{
		Passcode:       testPasscode,
		SessionManager: mgr,
		ReloadOperator: op,
		LogReader:      reader,
	})
	return &opsFixture{srv: srv, op: op, logDir: logDir}
}

func (f *opsFixture) authedSession(t *testing.T) (token, csrf string) {
	t.Helper()
	rec := login(t, f.srv, testPasscode)
	token = sessionTokenFrom(t, rec)
	csrf, err := f.srv.sessions.CSRFToken(token, time.Now())
	if err != nil {
		t.Fatalf("CSRFToken: %v", err)
	}
	return token, csrf
}

func caddyJSONLine(i int) string {
	return fmt.Sprintf(`{"level":"info","ts":%f,"msg":"handled request","request":{"remote_ip":"192.0.2.%d","host":"host%d.example.com","method":"GET","uri":"/"},"status":200}`,
		1785200000.0+float64(i), i, i)
}

func TestReloadSuccessFlow(t *testing.T) {
	f := newOpsFixture(t, caddyJSONLine(0))
	token, csrf := f.authedSession(t)

	rec := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/caddy/reload", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reload status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "reload=ok") {
		t.Errorf("reload redirect = %q, want reload=ok", loc)
	}
	// The dashboard action must validate first, then reload.
	if strings.Join(f.op.calls, ",") != "validate,reload" {
		t.Errorf("operator calls = %v, want exactly [validate reload]", f.op.calls)
	}

	// Dashboard must show the success message.
	dash := authedGet(t, &serviceFixture{srv: f.srv}, token, "/dashboard?reload=ok")
	if !strings.Contains(dash.Body.String(), "reload succeeded") {
		t.Errorf("dashboard lacks success message:\n%s", dash.Body.String())
	}
	if strings.Contains(dash.Body.String(), "reload failed") {
		t.Error("success flow must not claim failure")
	}
}

func TestReloadValidateFailurePreventsReload(t *testing.T) {
	f := newOpsFixture(t, caddyJSONLine(0))
	f.op.validateErr = errors.New("caddy validation failed (test)")
	token, csrf := f.authedSession(t)

	rec := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/caddy/reload", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reload status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "reload=failed") {
		t.Errorf("reload redirect = %q, want reload=failed", loc)
	}
	// A validation failure must never reach the reload command.
	if strings.Join(f.op.calls, ",") != "validate" {
		t.Errorf("operator calls = %v, want exactly [validate] with no reload", f.op.calls)
	}
	dash := authedGet(t, &serviceFixture{srv: f.srv}, token, "/dashboard?reload=failed")
	if !strings.Contains(dash.Body.String(), "reload failed") {
		t.Errorf("dashboard lacks failure message:\n%s", dash.Body.String())
	}
	if strings.Contains(dash.Body.String(), "succeeded") {
		t.Error("validation failure must never claim success")
	}
}

func TestReloadFailureFlowNeverClaimsSuccess(t *testing.T) {
	f := newOpsFixture(t, caddyJSONLine(0))
	f.op.reloadErr = errors.New("exit status 1")
	token, csrf := f.authedSession(t)

	rec := authedPost(t, &serviceFixture{srv: f.srv}, token, csrf, "/caddy/reload", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reload status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "reload=failed") {
		t.Errorf("reload redirect = %q, want reload=failed", loc)
	}
	// Validate runs first, then the reload; both are attempted and the
	// reload failure is the surfaced outcome.
	if strings.Join(f.op.calls, ",") != "validate,reload" {
		t.Errorf("operator calls = %v, want [validate reload]", f.op.calls)
	}

	dash := authedGet(t, &serviceFixture{srv: f.srv}, token, "/dashboard?reload=failed")
	if !strings.Contains(dash.Body.String(), "reload failed") {
		t.Errorf("dashboard lacks failure message:\n%s", dash.Body.String())
	}
	if strings.Contains(dash.Body.String(), "succeeded") {
		t.Error("failure flow must never claim success")
	}
}

func TestReloadRequiresSessionAndCSRF(t *testing.T) {
	t.Run("unauthenticated", func(t *testing.T) {
		f := newOpsFixture(t, caddyJSONLine(0))
		req := httptest.NewRequest(http.MethodPost, "/caddy/reload", nil)
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
			t.Errorf("unauthenticated status = %d, want redirect", rec.Code)
		}
		if len(f.op.calls) != 0 {
			t.Errorf("command effects without auth: %v", f.op.calls)
		}
	})

	t.Run("missing-csrf", func(t *testing.T) {
		f := newOpsFixture(t, caddyJSONLine(0))
		rec := login(t, f.srv, testPasscode)
		token := sessionTokenFrom(t, rec)
		rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, "", "/caddy/reload", nil)
		if rec2.Code != http.StatusForbidden {
			t.Errorf("missing-CSRF status = %d, want 403", rec2.Code)
		}
		if len(f.op.calls) != 0 {
			t.Errorf("command effects without CSRF: %v", f.op.calls)
		}
	})

	t.Run("forged-csrf", func(t *testing.T) {
		// Use a real session so the request reaches the CSRF check; the
		// forged token must then fail with 403 and zero effects.
		f := newOpsFixture(t, caddyJSONLine(0))
		rec := login(t, f.srv, testPasscode)
		token := sessionTokenFrom(t, rec)
		rec2 := authedPost(t, &serviceFixture{srv: f.srv}, token, "forged.token", "/caddy/reload", nil)
		if rec2.Code != http.StatusForbidden {
			t.Errorf("forged-CSRF status = %d, want 403", rec2.Code)
		}
		if len(f.op.calls) != 0 {
			t.Errorf("command effects with forged CSRF: %v", f.op.calls)
		}
	})
}

func TestDashboardRendersRecentLogs(t *testing.T) {
	log := strings.Join([]string{
		caddyJSONLine(0),
		caddyJSONLine(1),
		"", "",
	}, "\n")
	f := newOpsFixture(t, log)
	token, _ := f.authedSession(t)

	dash := authedGet(t, &serviceFixture{srv: f.srv}, token, "/dashboard")
	if dash.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", dash.Code)
	}
	if !strings.Contains(dash.Body.String(), "host0.example.com") || !strings.Contains(dash.Body.String(), "host1.example.com") {
		t.Errorf("dashboard does not render recent log hosts:\n%s", dash.Body.String())
	}
	if !strings.Contains(dash.Body.String(), "Recent Caddy logs") {
		t.Error("dashboard lacks the log section heading")
	}
}

func TestDashboardRendersMalformedLogFallback(t *testing.T) {
	log := strings.Join([]string{
		caddyJSONLine(0),
		"\x00\x01 not json at all {{{",
	}, "\n")
	f := newOpsFixture(t, log)
	token, _ := f.authedSession(t)

	dash := authedGet(t, &serviceFixture{srv: f.srv}, token, "/dashboard")
	if !strings.Contains(dash.Body.String(), "not json at all") {
		t.Errorf("malformed line not rendered via raw fallback:\n%s", dash.Body.String())
	}
}

func TestDashboardLogUnavailableIsGraceful(t *testing.T) {
	// Log file not written → reader fails closed; dashboard must still work.
	f := newOpsFixture(t, "")
	token, _ := f.authedSession(t)

	dash := authedGet(t, &serviceFixture{srv: f.srv}, token, "/dashboard")
	if dash.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", dash.Code)
	}
	if !strings.Contains(dash.Body.String(), "not available") {
		t.Errorf("dashboard lacks graceful log-unavailable note:\n%s", dash.Body.String())
	}
}

func TestLogPathCannotBeSelectedByRequestInput(t *testing.T) {
	log := caddyJSONLine(0)
	f := newOpsFixture(t, log)
	token, _ := f.authedSession(t)

	// Any query/input attempting to point the reader elsewhere must be inert.
	for _, qs := range []string{
		"?file=/etc/passwd",
		"?log=../../etc/passwd",
		"?path=/etc/shadow",
		"?file=/etc/passwd&log=/etc/shadow",
	} {
		dash := authedGet(t, &serviceFixture{srv: f.srv}, token, "/dashboard"+qs)
		if dash.Code != http.StatusOK {
			t.Fatalf("dashboard status = %d for %s", dash.Code, qs)
		}
		if !strings.Contains(dash.Body.String(), "host0.example.com") {
			t.Errorf("request input %s changed the rendered log source", qs)
		}
		if strings.Contains(dash.Body.String(), "root:") {
			t.Errorf("request input %s caused reading a non-configured file", qs)
		}
	}
}

func TestReloadAndLogsDoNotWeakenLifecycleBehavior(t *testing.T) {
	// The lifecycle surface must keep its session+CSRF gates even with the
	// new operator wiring present.
	mgr, err := session.NewManager(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeServiceRepo()
	op := &fakeCaddyOp{}
	lc := registry.NewLifecycle(repo, registry.NewStore(filepath.Join(t.TempDir(), "generated"), op), nextTestID)
	reader, err := caddyops.NewLogReader(filepath.Join(t.TempDir(), "portcullis.log"))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Config{Passcode: testPasscode, SessionManager: mgr, Lifecycle: lc, ReloadOperator: op, LogReader: reader})

	// No session → redirect, no effects.
	req := httptest.NewRequest(http.MethodPost, "/services", strings.NewReader(url.Values{}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Errorf("unauthenticated lifecycle status = %d", rec.Code)
	}

	// Session without CSRF → 403, no effects.
	rec2 := login(t, srv, testPasscode)
	token := sessionTokenFrom(t, rec2)
	rec3 := authedPost(t, &serviceFixture{srv: srv}, token, "", "/services", proxyForm())
	if rec3.Code != http.StatusForbidden {
		t.Errorf("missing-CSRF lifecycle status = %d, want 403", rec3.Code)
	}
	if len(repo.calls) != 0 || len(op.calls) != 0 {
		t.Errorf("lifecycle effects leaked: repo=%v op=%v", repo.calls, op.calls)
	}
}
