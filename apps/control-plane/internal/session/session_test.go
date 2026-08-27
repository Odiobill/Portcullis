package session

import (
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret-that-is-long-enough-32b!"

func newManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(testSecret, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestCreateAndVerifyRoundTrip(t *testing.T) {
	m := newManager(t)
	token, exp, err := m.Create(time.Now())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if !exp.After(time.Now()) {
		t.Errorf("expiry %v should be in the future", exp)
	}
	if err := m.Verify(token, time.Now()); err != nil {
		t.Fatalf("Verify(valid token): %v", err)
	}
}

func TestVerifyRejectsMissingToken(t *testing.T) {
	m := newManager(t)
	if err := m.Verify("", time.Now()); err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestVerifyRejectsMalformedToken(t *testing.T) {
	m := newManager(t)
	cases := []string{
		"authenticated",
		"not-a-token",
		"aaa.bbb",
		"!!!.???",
		"a.b.c.d",
	}
	for _, tok := range cases {
		if err := m.Verify(tok, time.Now()); err == nil {
			t.Errorf("expected error for malformed token %q", tok)
		}
	}
}

func TestVerifyRejectsForgedSignature(t *testing.T) {
	m := newManager(t)
	token, _, err := m.Create(time.Now())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Tamper with the signature half.
	dot := strings.LastIndex(token, ".")
	if dot < 0 {
		t.Fatalf("token %q has no separator", token)
	}
	forged := token[:dot+1] + "QUJDREVGR0hJSktMTU5PUA" // valid base64, wrong signature
	if err := m.Verify(forged, time.Now()); err == nil {
		t.Error("expected error for forged signature")
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	m := newManager(t)
	// Sign a token with a different secret, then verify with the real manager:
	// same shape, different signature => must fail.
	other, err := NewManager("a-completely-different-secret-value!!", 5*time.Minute)
	if err != nil {
		t.Fatalf("NewManager(other): %v", err)
	}
	token, _, err := other.Create(time.Now())
	if err != nil {
		t.Fatalf("Create(other): %v", err)
	}
	if err := m.Verify(token, time.Now()); err == nil {
		t.Error("expected error for token signed under a different secret")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	m := newManager(t)
	token, _, err := m.Create(time.Now().Add(-10 * time.Minute))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Verify(token, time.Now()); err == nil {
		t.Error("expected error for expired token")
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	m := newManager(t)
	token, _, err := m.Create(time.Now())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m.Revoke(token)
	if err := m.Verify(token, time.Now()); err == nil {
		t.Error("expected error for revoked (logged-out) token")
	}
}

func TestPredictableCookieValueNeverAuthorizes(t *testing.T) {
	m := newManager(t)
	// The legacy bare value and any unsigned guess must be rejected.
	for _, bare := range []string{"authenticated", "1", "owner", "true"} {
		if err := m.Verify(bare, time.Now()); err == nil {
			t.Errorf("bare predictable value %q must never authorize", bare)
		}
	}
}

func TestCreateTokensAreUnique(t *testing.T) {
	m := newManager(t)
	t1, _, _ := m.Create(time.Now())
	t2, _, _ := m.Create(time.Now())
	if t1 == t2 {
		t.Error("two sessions created at the same instant must not share a token")
	}
}
