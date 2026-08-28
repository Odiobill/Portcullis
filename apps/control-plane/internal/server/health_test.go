package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakePinger implements the server health Pinger boundary in memory.
type fakePinger struct {
	err error
}

func (f fakePinger) Ping(ctx context.Context) error { return f.err }

func newHealthServer(pinger Pinger) *Server {
	return New(Config{Passcode: testPasscode, Pinger: pinger})
}

// TestHealthzReportsReadyWithReachableDatabase pins the readiness contract:
// /healthz returns 200 only when the database answers a ping, so the
// container must never advertise a ready registry before the schema (and
// the database behind it) is actually usable.
func TestHealthzReportsReadyWithReachableDatabase(t *testing.T) {
	srv := newHealthServer(fakePinger{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz with reachable DB: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "ok\n" {
		t.Errorf("GET /healthz body = %q, want \"ok\\n\"", body)
	}
}

// TestHealthzFailsClosedWithoutDatabaseOrPing pins the failure modes: no
// database wired, or a failing ping, must report 503 and never 200.
func TestHealthzFailsClosedWithoutDatabaseOrPing(t *testing.T) {
	t.Run("no pinger wired", func(t *testing.T) {
		srv := newHealthServer(nil)
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 when no database is wired", rec.Code)
		}
	})
	t.Run("ping fails", func(t *testing.T) {
		srv := newHealthServer(fakePinger{err: errors.New("connection refused")})
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 when the ping fails", rec.Code)
		}
	})
}

// TestHealthzIsUnauthenticated pins that /healthz is intentionally
// reachable without a session: container healthchecks and orchestrators
// must be able to probe readiness without owner credentials. It must not
// leak anything beyond a fixed readiness token.
func TestHealthzIsUnauthenticated(t *testing.T) {
	srv := newHealthServer(fakePinger{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusSeeOther {
		t.Fatal("/healthz must not redirect to the login page; healthchecks carry no session")
	}
}
