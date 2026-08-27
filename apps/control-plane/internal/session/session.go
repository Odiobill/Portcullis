// Package session implements expiring, cryptographically verifiable owner
// sessions per ADR-0002. Tokens are HMAC-SHA256 signed with a mandatory
// secret; a bare or predictable cookie value never authorizes a request.
//
// Logout revocation is kept in an in-memory set: after a logout the presented
// token is rejected until process restart. This is sufficient for the
// single-owner control plane and is recorded as a known limitation.
package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// CookieName is the owner-session cookie name.
const CookieName = "portcullis_session"

// tokenVersion prefixes the signed payload; bump on format changes.
const tokenVersion = "v1"

var (
	// ErrInvalid is returned for any token that does not authorize a
	// request: missing, malformed, forged, tampered, expired, or revoked.
	ErrInvalid = errors.New("session: invalid or expired session token")
)

// Manager creates and verifies owner-session tokens.
type Manager struct {
	secret  []byte
	ttl     time.Duration
	mu      sync.Mutex
	revoked map[string]struct{} // keyed by session ID
}

// NewManager returns a Manager signing tokens with secret for ttl.
func NewManager(secret string, ttl time.Duration) (*Manager, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("session: secret must be at least 32 characters, got %d", len(secret))
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("session: ttl must be positive, got %v", ttl)
	}
	return &Manager{secret: []byte(secret), ttl: ttl, revoked: make(map[string]struct{})}, nil
}

// Create mints a new token expiring ttl after now.
func (m *Manager) Create(now time.Time) (token string, expires time.Time, err error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return "", time.Time{}, fmt.Errorf("session: entropy failure: %w", err)
	}
	expires = now.Add(m.ttl)
	payload := fmt.Sprintf("%s|%x|%d", tokenVersion, id, expires.Unix())
	sig := m.sign(payload)
	token = base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(sig)
	return token, expires, nil
}

// Verify reports whether token is a well-formed token signed by this
// manager's secret, not expired at now, and not revoked.
func (m *Manager) Verify(token string, now time.Time) error {
	if token == "" {
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
	if !hmac.Equal(sig, m.sign(string(payload))) {
		return ErrInvalid
	}

	parts := strings.Split(string(payload), "|")
	if len(parts) != 3 || parts[0] != tokenVersion {
		return ErrInvalid
	}
	id := parts[1]
	var expUnix int64
	if _, err := fmt.Sscanf(parts[2], "%d", &expUnix); err != nil {
		return ErrInvalid
	}
	if now.Unix() >= expUnix {
		return ErrInvalid
	}

	m.mu.Lock()
	_, revoked := m.revoked[id]
	m.mu.Unlock()
	if revoked {
		return ErrInvalid
	}
	return nil
}

// Revoke marks the session carried by token as invalid (logout).
// Unknown or malformed tokens are ignored.
func (m *Manager) Revoke(token string) {
	payloadEnc, _, ok := strings.Cut(token, ".")
	if !ok {
		return
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadEnc)
	if err != nil {
		return
	}
	parts := strings.Split(string(payload), "|")
	if len(parts) != 3 || parts[0] != tokenVersion {
		return
	}
	m.mu.Lock()
	m.revoked[parts[1]] = struct{}{}
	m.mu.Unlock()
}

// sign computes the HMAC-SHA256 of payload under the manager secret.
// hmac.Equal is used at verification to avoid timing leakage.
func (m *Manager) sign(payload string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}
