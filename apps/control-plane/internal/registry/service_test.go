package registry

import (
	"strings"
	"testing"
	"time"
)

func validProxyService() Service {
	return Service{
		ID:             "svc-alpha",
		Type:           TypeProxy,
		Domains:        []string{"app.example.com"},
		TLSMode:        TLSACME,
		ProxyContainer: "app_container",
		ProxyPort:      3000,
		CreatedAt:      time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC),
	}
}

func validStaticService() Service {
	return Service{
		ID:         "site-beta",
		Type:       TypeStatic,
		Domains:    []string{"static.example.com", "www.static.example.com"},
		TLSMode:    TLSInternal,
		StaticRoot: "/srv/sites/static.example.com",
		CreatedAt:  time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC),
	}
}

func TestValidateAcceptsProxyService(t *testing.T) {
	if err := validProxyService().Validate(); err != nil {
		t.Fatalf("valid proxy service rejected: %v", err)
	}
}

func TestValidateAcceptsStaticService(t *testing.T) {
	if err := validStaticService().Validate(); err != nil {
		t.Fatalf("valid static service rejected: %v", err)
	}
}

func TestValidateAcceptsWildcardDomain(t *testing.T) {
	s := validProxyService()
	s.Domains = []string{"*.app.example.com"}
	if err := s.Validate(); err != nil {
		t.Fatalf("wildcard domain rejected: %v", err)
	}
}

func TestValidateRejectsBadServiceIDs(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"starts-hyphen":     "-abc",
		"starts-underscore": "_abc",
		"space":             "svc alpha",
		"braces":            "svc{id}",
		"semicolon":         "svc;id",
		"newline":           "svc\nid",
		"dot-traversal":     "../escape",
		"dots":              "a..b",
		"slash":             "a/b",
		"null-byte":         "a\x00b",
		"quote":             `svc"id`,
		"backslash":         `svc\id`,
		"tick":              "svc`id",
	}
	for name, id := range cases {
		s := validProxyService()
		s.ID = id
		if err := s.Validate(); err == nil {
			t.Errorf("bad service ID %q (%s) accepted", id, name)
		}
	}
}

func TestValidateRejectsBadDomains(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"no-tld":          "app",
		"space":           "app.example.com extra",
		"caddy-injection": "app.example.com {\nreverse_proxy evil:1\n}",
		"braces":          "foo{bar}.example.com",
		"semicolon":       "app.example.com;import evil",
		"double-dot":      "a..example.com",
		"label-hyphen":    "-app.example.com",
		"quote":           `app"example.com`,
		"null-byte":       "app.example.com\x00",
		"traversal":       "../../etc/passwd",
		"bare-star":       "*",
	}
	for name, domain := range cases {
		s := validProxyService()
		s.Domains = []string{domain}
		if err := s.Validate(); err == nil {
			t.Errorf("bad domain %q (%s) accepted", domain, name)
		}
	}
}

func TestValidateRejectsEmptyDomains(t *testing.T) {
	s := validProxyService()
	s.Domains = nil
	if err := s.Validate(); err == nil {
		t.Error("service with no domains accepted")
	}
}

func TestValidateRejectsBadTLSModes(t *testing.T) {
	for _, mode := range []string{"", "hacker", "acme extra", "ACME", "acme;import evil", "acme\n"} {
		s := validProxyService()
		s.TLSMode = TLSMode(mode)
		if err := s.Validate(); err == nil {
			t.Errorf("bad TLS mode %q accepted", mode)
		}
	}
}

func TestValidateRejectsBadServiceTypes(t *testing.T) {
	for _, typ := range []string{"", "gateway", "proxy extra", "PROXY"} {
		s := validProxyService()
		s.Type = ServiceType(typ)
		if err := s.Validate(); err == nil {
			t.Errorf("bad service type %q accepted", typ)
		}
	}
}

func TestValidateRejectsBadProxyContainers(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"space":         "app container",
		"port-override": "app_container:9999",
		"braces":        "app{container}",
		"semicolon":     "app;import evil",
		"newline":       "app\ncontainer",
		"quote":         `app"container`,
		"null-byte":     "app\x00",
		"starts-hyphen": "-app",
		"starts-dot":    ".app",
	}
	for name, container := range cases {
		s := validProxyService()
		s.ProxyContainer = container
		if err := s.Validate(); err == nil {
			t.Errorf("bad container %q (%s) accepted", container, name)
		}
	}
}

func TestValidateRejectsBadProxyPorts(t *testing.T) {
	for _, port := range []int{0, -1, 65536, 99999} {
		s := validProxyService()
		s.ProxyPort = port
		if err := s.Validate(); err == nil {
			t.Errorf("bad port %d accepted", port)
		}
	}
}

func TestValidateRequiresContainerAndPortForProxy(t *testing.T) {
	s := validProxyService()
	s.ProxyContainer = ""
	if err := s.Validate(); err == nil {
		t.Error("proxy service without container accepted")
	}
	s = validProxyService()
	s.ProxyPort = 0
	if err := s.Validate(); err == nil {
		t.Error("proxy service without port accepted")
	}
}

func TestValidateRejectsBadStaticRoots(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"relative":         "sites/app",
		"outside-boundary": "/etc/passwd",
		"wrong-prefix":     "/srv/site/app",
		"traversal":        "/srv/sites/../../etc",
		"inner-traversal":  "/srv/sites/app/../../../etc",
		"dot-suffix":       "/srv/sites/app/..",
		"newline":          "/srv/sites/app\n}",
		"braces":           "/srv/sites/app{x}",
		"null-byte":        "/srv/sites/app\x00",
		"quote":            `/srv/sites/app"root`,
	}
	for name, root := range cases {
		s := validStaticService()
		s.StaticRoot = root
		if err := s.Validate(); err == nil {
			t.Errorf("bad static root %q (%s) accepted", root, name)
		}
	}
}

func TestValidateRejectsCrossFieldTypeFields(t *testing.T) {
	s := validProxyService()
	s.StaticRoot = "/srv/sites/app"
	if err := s.Validate(); err == nil {
		t.Error("proxy service with static root accepted")
	}

	s = validStaticService()
	s.ProxyContainer = "app_container"
	if err := s.Validate(); err == nil {
		t.Error("static service with proxy container accepted")
	}
	s = validStaticService()
	s.ProxyPort = 3000
	if err := s.Validate(); err == nil {
		t.Error("static service with proxy port accepted")
	}
}

func TestValidateRejectsUnsafeDBNames(t *testing.T) {
	for _, db := range []string{"db;import evil", "db\x00", `db"x`, "db name"} {
		s := validProxyService()
		s.DBName = db
		if err := s.Validate(); err == nil {
			t.Errorf("bad DB name %q accepted", db)
		}
		s = validProxyService()
		s.DBUser = db
		if err := s.Validate(); err == nil {
			t.Errorf("bad DB user %q accepted", db)
		}
	}
}

func TestValidateAcceptsSafeDBNameAndUser(t *testing.T) {
	s := validProxyService()
	s.DBName = "svc_alpha_db"
	s.DBUser = "svc_alpha_user"
	if err := s.Validate(); err != nil {
		t.Fatalf("safe DB name/user rejected: %v", err)
	}
}

// Generation tests live in caddyfile_test.go; this guard keeps validation
// wired into generation (Caddy-injection must fail closed).
func TestGenerateRejectsInvalidService(t *testing.T) {
	s := validProxyService()
	s.Domains = []string{"app.example.com {\nreverse_proxy evil:1\n}"}
	out, err := GenerateSiteBlock(s)
	if err == nil {
		t.Fatal("generation accepted a Caddy-injection domain")
	}
	if strings.Contains(out, "reverse_proxy evil") {
		t.Error("generated output leaked injected directive")
	}
}
