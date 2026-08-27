package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"portcullis/control-plane/internal/session"
)

const (
	testPasscode = "correct-passcode"
	testSecret   = "test-secret-that-is-long-enough-32b!"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	mgr, err := session.NewManager(testSecret, 5*time.Minute)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	return New(Config{Passcode: testPasscode, SessionManager: mgr})
}

func login(t *testing.T, s *Server, passcode string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"passcode": {passcode}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestLoginPageIsEnglishAndServerRendered(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /login status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Sign in", "Portcullis", "passcode"} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
			t.Errorf("login page does not contain %q", want)
		}
	}
}

func TestLoginRejectsWrongPasscode(t *testing.T) {
	s := newTestServer(t)
	rec := login(t, s, "wrong-passcode")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /login with wrong passcode status = %d, want 401", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName && c.Value != "" {
			t.Errorf("must not set a session cookie on failed login, got %q", c.Value)
		}
	}
}

func TestLoginSetsSecureSessionCookie(t *testing.T) {
	s := newTestServer(t)
	rec := login(t, s, testPasscode)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("successful login status = %d, want 303", rec.Code)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("successful login did not set the session cookie")
	}
	if !cookie.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
	if !cookie.Secure {
		t.Error("session cookie must be Secure")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("session cookie Path = %q, want %q", cookie.Path, "/")
	}
}

func TestDashboardRedirectsUnauthenticated(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Errorf("unauthenticated GET /dashboard status = %d, want redirect", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("redirect Location = %q, want %q", loc, "/login")
	}
}

func TestDashboardRejectsBareCookieValue(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	// The legacy forgeable value must never authorize.
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: "authenticated"})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Errorf("bare cookie value status = %d, want redirect to login", rec.Code)
	}
}

func TestDashboardAcceptsValidSession(t *testing.T) {
	s := newTestServer(t)
	rec := login(t, s, testPasscode)
	var token string
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("no session token captured from login")
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req)

	if rec2.Code != http.StatusOK {
		t.Fatalf("authenticated GET /dashboard status = %d, want 200", rec2.Code)
	}
	body := rec2.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Error("dashboard page does not contain expected English heading")
	}
}

func TestDashboardRejectsExpiredSession(t *testing.T) {
	mgr, err := session.NewManager(testSecret, time.Second)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	clock := time.Now()
	s := New(Config{Passcode: testPasscode, SessionManager: mgr, Now: func() time.Time { return clock }})

	token, _, err := mgr.Create(clock)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Advance the injected clock past the TTL; the same token must now fail.
	clock = clock.Add(2 * time.Second)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Errorf("expired session status = %d, want redirect to login", rec.Code)
	}
}

func TestLogoutClearsAndInvalidatesSession(t *testing.T) {
	s := newTestServer(t)
	rec := login(t, s, testPasscode)
	var token string
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			token = c.Value
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req)

	var clearCookie *http.Cookie
	for _, c := range rec2.Result().Cookies() {
		if c.Name == session.CookieName {
			clearCookie = c
		}
	}
	if clearCookie == nil {
		t.Fatal("logout did not touch the session cookie")
	}
	if clearCookie.Value != "" {
		t.Errorf("logout cookie Value = %q, want empty", clearCookie.Value)
	}
	if clearCookie.MaxAge != -1 {
		t.Errorf("logout cookie MaxAge = %d, want -1", clearCookie.MaxAge)
	}

	// The presented token must no longer authorize anything.
	req3 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req3.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	rec3 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusSeeOther && rec3.Code != http.StatusFound {
		t.Errorf("post-logout dashboard status = %d, want redirect to login", rec3.Code)
	}
}
