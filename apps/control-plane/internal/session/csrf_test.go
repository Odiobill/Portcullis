package session

import (
	"strings"
	"testing"
	"time"
)

func csrfManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(testSecret, time.Hour)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestCSRFTokenRoundTrip(t *testing.T) {
	m := csrfManager(t)
	now := time.Now()
	sessionToken, _, err := m.Create(now)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	csrf, err := m.CSRFToken(sessionToken, now)
	if err != nil {
		t.Fatalf("CSRFToken: %v", err)
	}
	if csrf == "" || csrf == sessionToken {
		t.Fatalf("CSRF token must be non-empty and distinct from the session token")
	}
	if err := m.VerifyCSRF(sessionToken, csrf, now); err != nil {
		t.Fatalf("VerifyCSRF(valid): %v", err)
	}
}

func TestCSRFTokenIsDistinctPerSession(t *testing.T) {
	m := csrfManager(t)
	now := time.Now()
	t1, _, _ := m.Create(now)
	t2, _, _ := m.Create(now)
	c1, _ := m.CSRFToken(t1, now)
	c2, _ := m.CSRFToken(t2, now)
	if c1 == c2 {
		t.Error("CSRF tokens must be session-bound and distinct")
	}
}

func TestVerifyCSRFRejectsForgedToken(t *testing.T) {
	m := csrfManager(t)
	now := time.Now()
	sessionToken, _, _ := m.Create(now)
	if err := m.VerifyCSRF(sessionToken, "not.a.real.csrf", now); err == nil {
		t.Error("forged CSRF token accepted")
	}
	if err := m.VerifyCSRF(sessionToken, "authenticated", now); err == nil {
		t.Error("bare predictable CSRF value accepted")
	}
	// A token signed for a different session must not verify here.
	otherSession, _, _ := m.Create(now)
	otherCSRF, _ := m.CSRFToken(otherSession, now)
	if err := m.VerifyCSRF(sessionToken, otherCSRF, now); err == nil {
		t.Error("CSRF token from another session accepted")
	}
}

func TestVerifyCSRFRejectsExpiredToken(t *testing.T) {
	m := csrfManager(t)
	now := time.Now()
	sessionToken, _, _ := m.Create(now)
	csrf, err := m.CSRFToken(sessionToken, now)
	if err != nil {
		t.Fatalf("CSRFToken: %v", err)
	}
	later := now.Add(2 * time.Hour)
	if err := m.VerifyCSRF(sessionToken, csrf, later); err == nil {
		t.Error("expired CSRF token accepted")
	}
}

func TestVerifyCSRFRejectsInvalidOrRevokedSession(t *testing.T) {
	m := csrfManager(t)
	now := time.Now()
	sessionToken, _, _ := m.Create(now)
	csrf, _ := m.CSRFToken(sessionToken, now)

	// Revoked session: its CSRF token must die with it.
	m.Revoke(sessionToken)
	if err := m.VerifyCSRF(sessionToken, csrf, now); err == nil {
		t.Error("CSRF accepted for revoked session")
	}
	if err := m.VerifyCSRF("", csrf, now); err == nil {
		t.Error("CSRF accepted without a session token")
	}
	if err := m.VerifyCSRF(sessionToken, "", now); err == nil {
		t.Error("empty CSRF accepted")
	}
}

func TestCSRFTokenFormatIsOpaque(t *testing.T) {
	m := csrfManager(t)
	now := time.Now()
	sessionToken, _, _ := m.Create(now)
	csrf, _ := m.CSRFToken(sessionToken, now)
	if strings.Contains(csrf, "csrf") || strings.Contains(csrf, "owner") {
		t.Error("CSRF token must not embed predictable semantics in cleartext")
	}
}
