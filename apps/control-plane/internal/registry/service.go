// Package registry implements the Portcullis service-registry domain core:
// service validation, deterministic generated-Caddyfile text, and
// rollback-safe deploy/remove of generated files. Per ADR-0001 the service
// set is proxy and static only, and the interface is English-only.
//
// The persistence boundary (pgx) is deliberately out of this package in
// Slice 2: no database connection, CRUD, or migration execution exists yet.
package registry

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// TLSMode selects the Caddy TLS snippet imported by the generated block.
type TLSMode string

// Supported TLS modes, mirroring the Caddy snippets in the gateway Caddyfile.
const (
	TLSACME       TLSMode = "acme"
	TLSInternal   TLSMode = "internal"
	TLSNamecheap  TLSMode = "namecheap_tls"
	TLSCloudflare TLSMode = "cloudflare_tls"
	TLSRoute53    TLSMode = "route53_tls"
)

// ServiceType discriminates proxy and static services.
type ServiceType string

// Supported service types.
const (
	TypeProxy  ServiceType = "proxy"
	TypeStatic ServiceType = "static"
)

// StaticRootPrefix is the only permitted static-root boundary.
const StaticRootPrefix = "/srv/sites/"

// Service is a registry entry for one reverse-proxy or static site.
type Service struct {
	// ID is an opaque, immutable identifier; it is never derived from
	// user input and doubles as the generated Caddyfile filename.
	ID string
	// Type is the service discriminant (proxy or static).
	Type ServiceType
	// Domains are the site labels of the generated block.
	Domains []string
	// TLSMode names the Caddy TLS snippet to import.
	TLSMode TLSMode
	// ProxyContainer is the upstream container name (proxy services only).
	ProxyContainer string
	// ProxyPort is the upstream container port (proxy services only).
	ProxyPort int
	// StaticRoot is the served filesystem root (static services only);
	// it must remain within StaticRootPrefix.
	StaticRoot string
	// DBName and DBUser are optional per-service database identifiers.
	// No credentials are stored here.
	DBName string
	DBUser string
	// CreatedAt is the registry creation timestamp.
	CreatedAt time.Time
}

// Validation regexes mirror the legacy engine's character classes so
// existing service inputs keep working, tightened with an explicit
// disallowed-character check that blocks Caddy token injection.
var (
	serviceIDRe  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
	hostnameRe   = regexp.MustCompile(`^(\*\.)?([a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)
	containerRe  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	simpleNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
	disallowedRe = regexp.MustCompile("[\\s{}'\"`;\\\\\\n\\r]")
)

// allowedTLSModes and allowedServiceTypes are the exhaustive enumerations.
var allowedTLSModes = map[TLSMode]bool{
	TLSACME: true, TLSInternal: true, TLSNamecheap: true,
	TLSCloudflare: true, TLSRoute53: true,
}

var allowedServiceTypes = map[ServiceType]bool{TypeProxy: true, TypeStatic: true}

// Validate reports whether the service is safe to persist and generate.
// Every rejection is fail-closed: the caller must not persist or generate
// from an invalid Service.
func (s Service) Validate() error {
	if !allowedServiceTypes[s.Type] {
		return fmt.Errorf("registry: invalid service type %q", string(s.Type))
	}
	if !allowedTLSModes[s.TLSMode] {
		return fmt.Errorf("registry: invalid TLS mode %q", string(s.TLSMode))
	}
	if !serviceIDRe.MatchString(s.ID) || disallowedRe.MatchString(s.ID) || strings.ContainsRune(s.ID, 0) {
		return fmt.Errorf("registry: invalid service ID %q", s.ID)
	}
	if len(s.Domains) == 0 {
		return errors.New("registry: at least one domain is required")
	}
	for _, d := range s.Domains {
		if !hostnameRe.MatchString(d) || disallowedRe.MatchString(d) || strings.ContainsRune(d, 0) {
			return fmt.Errorf("registry: invalid domain %q", d)
		}
	}

	switch s.Type {
	case TypeProxy:
		if s.ProxyContainer == "" ||
			!containerRe.MatchString(s.ProxyContainer) ||
			disallowedRe.MatchString(s.ProxyContainer) ||
			strings.ContainsRune(s.ProxyContainer, 0) {
			return fmt.Errorf("registry: invalid proxy container name %q", s.ProxyContainer)
		}
		if s.ProxyPort < 1 || s.ProxyPort > 65535 {
			return fmt.Errorf("registry: proxy port %d out of range 1-65535", s.ProxyPort)
		}
		if s.StaticRoot != "" {
			return errors.New("registry: static root must be empty for proxy services")
		}
	case TypeStatic:
		if err := validateStaticRoot(s.StaticRoot); err != nil {
			return err
		}
		if s.ProxyContainer != "" || s.ProxyPort != 0 {
			return errors.New("registry: proxy target must be empty for static services")
		}
	}

	// Optional DB identifiers must not be able to inject SQL or Caddy text.
	for name, v := range map[string]string{"db_name": s.DBName, "db_user": s.DBUser} {
		if v == "" {
			continue
		}
		if !simpleNameRe.MatchString(v) || strings.ContainsRune(v, 0) {
			return fmt.Errorf("registry: invalid %s %q", name, v)
		}
	}
	return nil
}

// validateStaticRoot enforces the /srv/sites/ boundary: the cleaned path
// must remain inside the prefix, so traversal like /srv/sites/../.. cannot
// escape even though it starts with the prefix.
func validateStaticRoot(root string) error {
	if root == "" {
		return errors.New("registry: static root is required for static services")
	}
	if disallowedRe.MatchString(root) || strings.ContainsRune(root, 0) {
		return fmt.Errorf("registry: static root contains disallowed characters %q", root)
	}
	cleaned := filepath.Clean(root)
	if !strings.HasPrefix(cleaned, StaticRootPrefix) || cleaned == strings.TrimSuffix(StaticRootPrefix, "/") {
		return fmt.Errorf("registry: static root %q must stay inside %s", root, StaticRootPrefix)
	}
	// filepath.Clean collapses ".."; after cleaning, any remaining ".."
	// segment means the input was malformed rather than traversing, but
	// reject it regardless.
	for _, part := range strings.Split(strings.TrimPrefix(cleaned, StaticRootPrefix), "/") {
		if part == ".." || part == "." {
			return fmt.Errorf("registry: static root %q must be a path under %s without traversal", root, StaticRootPrefix)
		}
	}
	return nil
}

// NormalizeDomain returns the lowercased canonical form of a domain.
// Validation happens in Service.Validate; this is for storage formatting.
func NormalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSuffix(strings.ToLower(domain), "."))
}
