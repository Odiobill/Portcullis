package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// deployBoundary reads a file from the repository root (four directories
// above this package directory) and fails the test when it is missing.
func deployBoundary(t *testing.T, rel string) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// TestComposeUsesGoControlPlaneAndCompletedMigrations pins the Slice 5
// runtime replacement: the Compose manager services are exactly the accepted
// Go stack — caddy, control_plane, the one-shot migrate gate, the registry
// database, and the optional backup sidecar — built from the control-plane
// sources, with schema setup dependency-gated ahead of the control plane.
func TestComposeUsesGoControlPlaneAndCompletedMigrations(t *testing.T) {
	compose := deployBoundary(t, "docker-compose.yml")

	servicesBlock := regexp.MustCompile(`(?s)services:\n(.*?)\nnetworks:`).FindStringSubmatch(compose)
	if servicesBlock == nil {
		t.Fatal("docker-compose.yml must define a services block followed by networks")
	}
	var services []string
	for _, m := range regexp.MustCompile(`(?m)^  ([a-z0-9_]+):\s*$`).FindAllStringSubmatch(servicesBlock[1], -1) {
		services = append(services, m[1])
	}
	want := []string{"backup", "caddy", "control_plane", "migrate", "portcullis_db"}
	sort.Strings(services)
	if strings.Join(services, ",") != strings.Join(want, ",") {
		t.Errorf("Compose services = %v, want exactly %v (the accepted Go stack)", services, want)
	}
	if !strings.Contains(compose, "context: ./apps/control-plane") {
		t.Error("docker-compose.yml must build the Go control plane from apps/control-plane")
	}
	if !regexp.MustCompile(`command: \["migrate"\][^
]*control-plane migrate`).MatchString(compose) {
		t.Error("a one-shot migrate service must run `control-plane migrate` against the fresh database")
	}
	if !strings.Contains(compose, "condition: service_completed_successfully") {
		t.Error("the control plane must depend on the migrate service with condition: service_completed_successfully")
	}
	// The migrate service must be gated on a healthy database.
	if !strings.Contains(compose, "condition: service_healthy") {
		t.Error("migration must be gated on a healthy database")
	}
}

// TestComposePreservesSecurityAndHostBoundaries pins the accepted security
// boundaries in the Compose file: no Docker socket, no public Caddy admin
// port, generated directory writable but manual Caddyfiles/Caddyfile/
// backups read-only to the control plane, and credentials only via
// environment (never in commands or volume paths).
func TestComposePreservesSecurityAndHostBoundaries(t *testing.T) {
	compose := deployBoundary(t, "docker-compose.yml")

	if strings.Contains(compose, "/var/run/docker.sock") {
		t.Error("no service may mount the Docker socket")
	}
	if regexp.MustCompile(`2019:2019`).MatchString(compose) {
		t.Error("the Caddy admin endpoint must never be published on a host port")
	}
	if !strings.Contains(compose, "./sites/manual:/etc/caddy/sites/manual:ro") {
		t.Error("operator-manual Caddyfiles must be mounted read-only into the control plane")
	}
	if !strings.Contains(compose, "./Caddyfile:/etc/caddy/Caddyfile:ro") {
		t.Error("the root Caddyfile must be mounted read-only into the control plane (validate only)")
	}
	if !strings.Contains(compose, "backup_data:/backups:ro") {
		t.Error("backups must be mounted read-only into the control plane")
	}
	if !strings.Contains(compose, "./sites/generated:/etc/caddy/sites/generated") {
		t.Error("the control plane needs the generated Caddyfile directory mounted read-write")
	}
	if regexp.MustCompile(`(?m)command:.*DB_PASSWORD`).MatchString(compose) {
		t.Error("credentials must flow via environment, never command arguments")
	}
	// The external networks accepted by the prior slices must be preserved.
	for _, net := range []string{"caddy_gateway", "db_network"} {
		pattern := regexp.MustCompile(`(?s)` + net + `:\n\s*(?:driver:.*\n\s*)?external:\s*true`)
		if !pattern.MatchString(compose) {
			t.Errorf("external network boundary %q must be preserved", net)
		}
	}
}

// TestCaddyBootstrapTargetsGoControlPlane pins that the bootstrap dashboard
// route proxies to the Go control plane on its documented port, and that the
// generated and manual Caddyfile boundaries stay distinct and read-only to
// Caddy.
func TestCaddyBootstrapTargetsGoControlPlane(t *testing.T) {
	caddyfile := deployBoundary(t, "Caddyfile")

	if !strings.Contains(caddyfile, "reverse_proxy control_plane:8080") {
		t.Error("the bootstrap route must reverse_proxy control_plane:8080")
	}
	if regexp.MustCompile(`reverse_proxy [^\n]*:3000`).MatchString(caddyfile) {
		t.Error("no bootstrap route may target a retired :3000 application port")
	}
	// Generated and manual boundaries stay distinct and read-only to Caddy.
	if !strings.Contains(caddyfile, "import /etc/caddy/sites/generated/*.caddy") ||
		!strings.Contains(caddyfile, "import /etc/caddy/sites/manual/*.caddy") {
		t.Error("generated and manual Caddyfile import boundaries must be preserved")
	}
}

// TestControlPlaneDockerfilePinsRuntimeDependencies pins the runtime image
// contract: multi-stage Go build, the caddy binary for validate/reload, and
// a version-matched pg_dump client (postgres base image) so on-demand dumps
// can run.
func TestControlPlaneDockerfilePinsRuntimeDependencies(t *testing.T) {
	dockerfile := deployBoundary(t, filepath.Join("apps", "control-plane", "Dockerfile"))

	if !strings.Contains(dockerfile, "golang:") {
		t.Error("Dockerfile must build from a golang image")
	}
	if !strings.Contains(dockerfile, "caddy") {
		t.Error("the runtime image must contain the caddy binary for validate/reload")
	}
	if !strings.Contains(dockerfile, "pg_dump") {
		t.Error("the runtime image must provide a pg_dump client for on-demand dumps")
	}
	if !strings.Contains(dockerfile, "postgres:18-alpine") {
		t.Error("the runtime image must be version-matched with the PostgreSQL 18 registry database")
	}
	if !strings.Contains(dockerfile, "CGO_ENABLED=0") {
		t.Error("the Go build should be static (CGO_ENABLED=0) for a minimal runtime image")
	}
}

// serviceBlock returns the Compose YAML block of one top-level service:
// from its two-space-indented key line to the next service key (or EOF).
func serviceBlock(t *testing.T, compose, name string) string {
	t.Helper()
	start := regexp.MustCompile(`(?m)^  ` + name + `:\n`).FindStringIndex(compose)
	if start == nil {
		t.Fatalf("service %q not found in docker-compose.yml", name)
	}
	rest := compose[start[1]:]
	end := regexp.MustCompile(`(?m)^  [a-z0-9_]+:`).FindStringIndex(rest)
	if end != nil {
		rest = rest[:end[0]]
	}
	return rest
}

// TestComposeDNSProviderBuildArgsAndGatewayEnvFile pins the Heimdall-verified
// DNS-01 deployment fixes: both build consumers of the control-plane image
// receive the DNS provider build argument, and the control_plane container
// receives the gateway environment file so caddy validate/reload expands the
// root Caddyfile with the same domain/TLS/DNS values as the Caddy container
// instead of silently overwriting live configuration with defaults.
func TestComposeDNSProviderBuildArgsAndGatewayEnvFile(t *testing.T) {
	compose := deployBoundary(t, "docker-compose.yml")

	providerArg := "CADDY_DNS_PROVIDER: ${CADDY_DNS_PROVIDER:-}"
	for _, svc := range []string{"migrate", "control_plane"} {
		block := serviceBlock(t, compose, svc)
		if !strings.Contains(block, "build:") || !strings.Contains(block, "context: ./apps/control-plane") {
			t.Errorf("%s must build from apps/control-plane", svc)
		}
		if !strings.Contains(block, providerArg) {
			t.Errorf("%s build must receive CADDY_DNS_PROVIDER: ${CADDY_DNS_PROVIDER:-}", svc)
		}
	}

	cp := serviceBlock(t, compose, "control_plane")
	if !regexp.MustCompile(`(?m)^    env_file:\n      - \.env\s*$`).MatchString(cp) {
		t.Error("control_plane must include env_file: .env so Caddy CLI expansion matches the gateway environment")
	}
	if !strings.Contains(cp, "PORTCULLIS_DATABASE_URL") {
		t.Error("control_plane explicit environment entries must be retained (Compose precedence over env_file)")
	}
}

// TestControlPlaneDockerfileDNS01ProviderBuild pins the image-level DNS-01
// fix: the control-plane image builds its Caddy binary through the same
// conditional caddy:builder/xcaddy provider pattern as the gateway image,
// and the selected binary is copied to /usr/sbin/caddy — the PATH-resolved
// Alpine location — so a plugin build cannot be shadowed by the apk binary.
func TestControlPlaneDockerfileDNS01ProviderBuild(t *testing.T) {
	dockerfile := deployBoundary(t, filepath.Join("apps", "control-plane", "Dockerfile"))

	if !regexp.MustCompile(`(?m)^FROM caddy:builder AS caddy-builder\s*$`).MatchString(dockerfile) {
		t.Error("Dockerfile must have a caddy:builder stage named caddy-builder")
	}
	if !regexp.MustCompile(`(?m)^ARG CADDY_DNS_PROVIDER\s*$`).MatchString(dockerfile) {
		t.Error("the builder stage must accept ARG CADDY_DNS_PROVIDER")
	}
	if !strings.Contains(dockerfile, "xcaddy build \\\n        --with github.com/caddy-dns/${CADDY_DNS_PROVIDER}") &&
		!strings.Contains(dockerfile, "xcaddy build --with github.com/caddy-dns/${CADDY_DNS_PROVIDER}") {
		t.Error("the builder stage must conditionally run xcaddy build --with github.com/caddy-dns/${CADDY_DNS_PROVIDER}")
	}
	if !strings.Contains(dockerfile, "cp /usr/bin/caddy /tmp/caddy") {
		t.Error("an empty provider must fall back to the stock builder Caddy binary")
	}
	if !regexp.MustCompile(`COPY --from=caddy-builder /tmp/caddy /usr/sbin/caddy`).MatchString(dockerfile) {
		t.Error("the selected Caddy binary must be copied to /usr/sbin/caddy (PATH-resolved Alpine location), never /usr/bin/caddy")
	}
	if regexp.MustCompile(`COPY --from=\S+ \S+ /usr/bin/caddy\s`).MatchString(dockerfile + "\n") {
		t.Error("no builder output may be copied to the ineffective /usr/bin/caddy location")
	}
	// Runtime requirements stay intact.
	if !strings.Contains(dockerfile, "apk add --no-cache caddy") {
		t.Error("the runtime must still install the Alpine caddy package (provides runtime deps)")
	}
	if !strings.Contains(dockerfile, "caddy version") || !strings.Contains(dockerfile, "pg_dump --version") {
		t.Error("image-build checks for caddy version and pg_dump --version must remain")
	}
}
