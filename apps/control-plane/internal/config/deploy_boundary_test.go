package config

import (
	"os"
	"path/filepath"
	"regexp"
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
// runtime replacement: the Compose manager service is the Go control plane
// (no Next.js/Prisma runtime), schema setup is a dependency-gated one-shot
// migration service, and the control plane only advertises health after the
// migrations completed successfully.
func TestComposeUsesGoControlPlaneAndCompletedMigrations(t *testing.T) {
	compose := deployBoundary(t, "docker-compose.yml")

	if regexp.MustCompile(`(?m)^\s*nextjs_app:`).MatchString(compose) {
		t.Error("docker-compose.yml must not define a nextjs_app runtime service anymore")
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
// route proxies to the Go control plane, not Next.js.
func TestCaddyBootstrapTargetsGoControlPlane(t *testing.T) {
	caddyfile := deployBoundary(t, "Caddyfile")

	if strings.Contains(caddyfile, "nextjs_app") {
		t.Error("Caddyfile must not reference the removed Next.js runtime")
	}
	if !strings.Contains(caddyfile, "reverse_proxy control_plane:8080") {
		t.Error("the bootstrap route must reverse_proxy control_plane:8080")
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
