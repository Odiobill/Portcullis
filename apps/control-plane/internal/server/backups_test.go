package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"portcullis/control-plane/internal/backups"
	"portcullis/control-plane/internal/session"
)

type backupsFixture struct {
	srv    *Server
	b      *backups.Browser
	dir    string
	exists bool
}

func newBackupsFixture(t *testing.T) *backupsFixture {
	t.Helper()
	mgr, err := session.NewManager(testSecret, time.Hour)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	dir := t.TempDir()
	b, err := backups.NewBrowser(dir)
	if err != nil {
		t.Fatalf("NewBrowser: %v", err)
	}
	srv := New(Config{Passcode: testPasscode, SessionManager: mgr, Backups: b})
	return &backupsFixture{srv: srv, b: b, dir: dir, exists: true}
}

func (f *backupsFixture) removeDir() {
	if f.exists {
		if err := os.RemoveAll(f.dir); err != nil {
			panic(err)
		}
		f.exists = false
	}
}

func writeBackup(t *testing.T, f *backupsFixture, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardRendersBackupList(t *testing.T) {
	f := newBackupsFixture(t)
	writeBackup(t, f, "svc.dump.gz", strings.Repeat("x", 1234))
	writeBackup(t, f, "other.dump.gz", "hello")

	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	dash := authedGet(t, &serviceFixture{srv: f.srv}, token, "/dashboard")

	if dash.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", dash.Code)
	}
	body := dash.Body.String()
	if !strings.Contains(body, "Backups") {
		t.Error("dashboard lacks the Backups section")
	}
	if !strings.Contains(body, "svc.dump.gz") || !strings.Contains(body, "other.dump.gz") {
		t.Errorf("dashboard does not list backups:\n%s", body)
	}
	if !strings.Contains(body, "1234") {
		t.Error("dashboard does not render backup size")
	}
	if !strings.Contains(body, `href="/backups/svc.dump.gz"`) {
		t.Error("dashboard lacks download link")
	}
}

func TestDashboardBackupsEmptyAndErrorStates(t *testing.T) {
	f := newBackupsFixture(t)
	token, _ := func() (string, string) {
		rec := login(t, f.srv, testPasscode)
		return sessionTokenFrom(t, rec), ""
	}()

	// Empty state.
	dash := authedGet(t, &serviceFixture{srv: f.srv}, token, "/dashboard")
	if !strings.Contains(dash.Body.String(), "No backups found") {
		t.Errorf("empty state missing:\n%s", dash.Body.String())
	}

	// Missing directory → graceful English error state, still 200.
	f.removeDir()
	dash2 := authedGet(t, &serviceFixture{srv: f.srv}, token, "/dashboard")
	if dash2.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d on store error", dash2.Code)
	}
	if !strings.Contains(dash2.Body.String(), "not available") {
		t.Errorf("error state missing:\n%s", dash2.Body.String())
	}
	if strings.Contains(dash2.Body.String(), f.dir) {
		t.Error("dashboard leaked the host backup path")
	}
}

func TestDownloadSuccessHeadersAndBody(t *testing.T) {
	f := newBackupsFixture(t)
	content := "PGDMP-backup-payload"
	writeBackup(t, f, "svc.dump.gz", content)

	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)

	req := httptest.NewRequest(http.MethodGet, "/backups/svc.dump.gz", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	dl := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(dl, req)

	if dl.Code != http.StatusOK {
		t.Fatalf("download status = %d", dl.Code)
	}
	h := dl.Header()
	if ct := h.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	cd := h.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, `filename="svc.dump.gz"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if cl := h.Get("Content-Length"); cl != "20" {
		t.Errorf("Content-Length = %q, want 20", cl)
	}
	if dl.Body.String() != content {
		t.Errorf("body = %q, want %q", dl.Body.String(), content)
	}
}

func TestDownloadRejectsUnsafeSelections(t *testing.T) {
	f := newBackupsFixture(t)
	// A real backup and an outside secret file the attacker will aim at.
	writeBackup(t, f, "svc.dump.gz", "payload")
	outside := filepath.Join(f.dir, "..", "secret.txt")
	if err := os.WriteFile(outside, []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Symlink, directory, and (if supported) a fifo inside the store.
	if err := os.Symlink(outside, filepath.Join(f.dir, "link.dump")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.dir, "dir.dump"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := login(t, f.srv, testPasscode)
	token := sessionTokenFrom(t, rec)

	for _, name := range []string{
		"..",
		"../secret.txt",
		"secret.txt",     // parent-level file, not a listed child of the store
		"a%2Fb",          // encoded separator
		"svc.dump.gz%00", // encoded NUL
		"/etc/passwd",    // absolute path selection
		"link.dump",      // symlink
		"dir.dump",       // directory
		"missing.dump",   // absent
		`quote"name`,     // disposition-unsafe charset
		"semi;colon",     // disposition-unsafe charset
	} {
		req := httptest.NewRequest(http.MethodGet, "/backups/"+name, nil)
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
		dl := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(dl, req)
		// Traversal-shaped paths are redirected by the mux's path cleaning
		// (fail-closed, no content); everything else must 404 in the handler.
		if dl.Code != http.StatusNotFound && dl.Code != http.StatusTemporaryRedirect {
			t.Errorf("download %q status = %d, want 404 or 307", name, dl.Code)
		}
		if strings.Contains(dl.Body.String(), "TOP-SECRET") {
			t.Errorf("download %q leaked outside content", name)
		}
	}
}

func TestBackupListAndDownloadRejectUnauthenticated(t *testing.T) {
	f := newBackupsFixture(t)
	writeBackup(t, f, "svc.dump.gz", "payload")

	for _, path := range []string{"/dashboard", "/backups/svc.dump.gz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
			t.Errorf("GET %s unauthenticated status = %d, want redirect", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "payload") {
			t.Errorf("GET %s leaked backup content without a session", path)
		}
	}
}

func TestBackupBrowserUnavailableIsGraceful(t *testing.T) {
	mgr, err := session.NewManager(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// No Backups wired: the section shows the unavailable note.
	srv := New(Config{Passcode: testPasscode, SessionManager: mgr})
	rec := login(t, srv, testPasscode)
	token := sessionTokenFrom(t, rec)
	dash := authedGet(t, &serviceFixture{srv: srv}, token, "/dashboard")
	if !strings.Contains(dash.Body.String(), "not available") {
		t.Errorf("backup-unavailable note missing:\n%s", dash.Body.String())
	}
	dl := authedGet(t, &serviceFixture{srv: srv}, token, "/backups/x.dump")
	if dl.Code != http.StatusServiceUnavailable {
		t.Errorf("download without browser status = %d, want 503", dl.Code)
	}
}
