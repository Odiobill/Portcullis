package session

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
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

// TestRevokeIgnoresForgedTokens proves logout revocation cannot populate
// revocation state from attacker-supplied unsigned tokens: since POST
// /logout is public, forged cookies must never grow the map or enable
// any revocation without a valid session signature.
func TestRevokeIgnoresForgedTokens(t *testing.T) {
	m := newManager(t)

	future := time.Now().Add(time.Hour).Unix()
	forgedPayload := fmt.Sprintf("v1|%x|%d", []byte("attacker-chosen-id"), future)
	forgedSig := sha256.Sum256([]byte("not-the-session-secret"))
	forged := base64.RawURLEncoding.EncodeToString([]byte(forgedPayload)) + "." +
		base64.RawURLEncoding.EncodeToString(forgedSig[:])

	m.Revoke(forged)
	if len(m.revoked) != 0 {
		t.Errorf("forged token must not populate revocation state, map = %v", m.revoked)
	}
	// A token signed under a different secret is equally untrusted.
	other, err := NewManager("a-completely-different-secret-value!!", time.Hour)
	if err != nil {
		t.Fatalf("NewManager(other): %v", err)
	}
	otherToken, _, err := other.Create(time.Now())
	if err != nil {
		t.Fatalf("Create(other): %v", err)
	}
	m.Revoke(otherToken)
	if len(m.revoked) != 0 {
		t.Errorf("token signed under a foreign secret must not populate revocation state, map = %v", m.revoked)
	}
}

// TestRevokeIgnoresTamperedAndExpiredTokens covers the remaining
// unauthenticated revocation paths: a tampered signature on a genuine
// token and a correctly signed but expired token must both be ignored.
func TestRevokeIgnoresTamperedAndExpiredTokens(t *testing.T) {
	m := newManager(t)

	token, _, err := m.Create(time.Now())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	dot := strings.LastIndex(token, ".")
	tampered := token[:dot+1] + "QUJDREVGR0hJSktMTU5PUA"
	m.Revoke(tampered)
	if len(m.revoked) != 0 {
		t.Errorf("tampered token must not populate revocation state, map = %v", m.revoked)
	}

	expiredMgr, err := NewManager(testSecret, time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	expiredToken, _, err := expiredMgr.Create(time.Now().Add(-2 * time.Second))
	if err != nil {
		t.Fatalf("Create(expired): %v", err)
	}
	expiredMgr.Revoke(expiredToken)
	if len(expiredMgr.revoked) != 0 {
		t.Errorf("expired token must not populate revocation state, map = %v", expiredMgr.revoked)
	}
}

// TestForgedLogoutCannotRevokeGenuineSession proves the end-to-end harm is
// closed: logging out with a forged cookie must not invalidate any session
// the attacker did not legitimately hold.
func TestForgedLogoutCannotRevokeGenuineSession(t *testing.T) {
	m := newManager(t)
	genuine, _, err := m.Create(time.Now())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Attacker forges a token claiming the genuine session's ID but with a
	// bogus signature, and presents it at logout.
	genuinePayloadEnc := genuine[:strings.Index(genuine, ".")]
	forgedSig := sha256.Sum256([]byte("attacker-guess"))
	forged := genuinePayloadEnc + "." + base64.RawURLEncoding.EncodeToString(forgedSig[:])
	m.Revoke(forged)

	if err := m.Verify(genuine, time.Now()); err != nil {
		t.Fatalf("genuine session must survive forged logout, got %v", err)
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
