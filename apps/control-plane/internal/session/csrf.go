// CSRF protection: server-verifiable tokens bound to the owner session.
// A CSRF token is an HMAC-SHA256 signature (under the session secret) over
// the session token and an expiry, so it is unpredictable without the
// secret, bound to the exact session it was issued for, and expires with
// that session. Verification always re-validates the session itself.
package session

import (
	"crypto/hmac"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

const csrfVersion = "csrf1"

// csrfTokenTTL caps the CSRF token lifetime independently of the session
// TTL: a still-valid session whose CSRF token has aged out must re-fetch a
// form rather than submit with a stale token.
const csrfTokenTTL = time.Hour

// CSRFToken returns a CSRF token bound to sessionToken, valid for at most
// csrfTokenTTL and never beyond the session itself. It fails closed when
// the session itself is not currently valid, so a CSRF token can never
// outlive or out-scope its session.
func (m *Manager) CSRFToken(sessionToken string, now time.Time) (string, error) {
	if sessionToken == "" {
		return "", ErrInvalid
	}
	if err := m.Verify(sessionToken, now); err != nil {
		return "", ErrInvalid
	}
	ttl := m.ttl
	if csrfTokenTTL < ttl {
		ttl = csrfTokenTTL
	}
	expiry := now.Add(ttl)
	payload := fmt.Sprintf("%s|%d", csrfVersion, expiry.Unix())
	sig := m.sign(sessionToken + "|" + payload)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(sig), nil
}

// VerifyCSRF reports whether token is a well-formed CSRF token issued for
// sessionToken, unexpired at now, while the session itself is still valid.
// Any mismatch (missing, forged, foreign-session, expired, revoked session)
// returns ErrInvalid.
func (m *Manager) VerifyCSRF(sessionToken, token string, now time.Time) error {
	if sessionToken == "" || token == "" {
		return ErrInvalid
	}
	if err := m.Verify(sessionToken, now); err != nil {
		return ErrInvalid
	}
	payloadEnc, sigEnc, ok := strings.Cut(token, ".")
	if !ok || strings.Contains(sigEnc, ".") {
		return ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadEnc)
	if err != nil {
		return ErrInvalid
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigEnc)
	if err != nil {
		return ErrInvalid
	}
	if !hmac.Equal(sig, m.sign(sessionToken+"|"+string(payload))) {
		return ErrInvalid
	}
	parts := strings.Split(string(payload), "|")
	if len(parts) != 2 || parts[0] != csrfVersion {
		return ErrInvalid
	}
	var expUnix int64
	if _, err := fmt.Sscanf(parts[1], "%d", &expUnix); err != nil {
		return ErrInvalid
	}
	if now.Unix() >= expUnix {
		return ErrInvalid
	}
	return nil
}
