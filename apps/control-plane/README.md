# Portcullis control plane (Slice 1)

The Go replacement control plane (ADR-0001). Slice 1 provides:

- An English, server-rendered login page.
- Owner passcode login (`POST /login`).
- An authenticated placeholder dashboard (`GET /dashboard`).
- Logout (`POST /logout`) that clears and invalidates the session.

Sessions are expiring HMAC-SHA256-signed tokens (ADR-0002); a bare or
predictable cookie value never authorizes a request.

## Configuration (fails closed)

| Variable | Meaning |
| --- | --- |
| `PORTCULLIS_PASSCODE` | Owner passcode (required, non-empty). |
| `PORTCULLIS_SESSION_SECRET` | Session signing secret, at least 32 characters (required). Supply it outside source control. |
| `PORTCULLIS_ADDR` | Listen address, default `:8080`. |

The process refuses to start when a required variable is missing, empty, or
the secret is too short.

## Run

```sh
export PORTCULLIS_PASSCODE=... PORTCULLIS_SESSION_SECRET=...
go run .
```

## Test

```sh
go test ./...
go build ./...
```

## Registry schema, Caddyfile core, and service lifecycle (Slices 2–3)

- Versioned SQL migrations live in `migrations/` (`000001_create_services.up.sql` / `.down.sql`),
  defining the fresh Postgres `services` registry (ADR-0001 fresh-database
  direction; no legacy-schema compatibility). They are **not** applied by these slices.
- `internal/registry` validates proxy/static services (unsafe inputs and Caddy-injection
  attempts fail closed), deterministically generates Caddy site blocks (`import <tls-mode>`,
  `reverse_proxy`, `root *` + `file_server`), and deploys/removes generated files atomically
  inside one configured generated directory with an injected validate/reload `Operator`.
  Any validate/reload failure rolls back the prior file state and re-applies the prior active
  config; the original failure is returned. `sites/manual` is never touched; a generated
  directory at or beneath `manual` is rejected fail-closed.
- `internal/registry/repo.go` is the pgx persistence boundary (parameterized queries only,
  injectable `Executor`, no live connection in these slices). `internal/registry/lifecycle.go`
  orchestrates create/edit/delete across the repository and the Store with explicit
  compensation; a compensation failure surfaces as `*CompensationError`, never success.
- `internal/session/csrf.go` provides session-bound HMAC CSRF tokens; every lifecycle
  mutation route verifies them before any repository or Caddy effect.
- **Dependency rationale:** `github.com/jackc/pgx/v5` is the deliberate persistence driver
  required by ADR-0001 (see go.mod comment). The repository is fully tested against an
  in-memory pgx double; no database is connected to, migrated, or reset in these slices.

Out of scope for Slices 2–3: applying migrations, Caddy execution, backups, Compose wiring,
and deployment (see `Projects/Portcullis/go-replacement-work-slices.md`).

## Backup browser (Slice 4b)

`internal/backups` is a read-only browser over one configured absolute
backup directory (default `/backups`). It lists regular files directly
inside that directory — newest first, name tie-break — and serves a
selected listed file via `GET /backups/{name}` after owner-session
verification. Download names are validated as safe basenames and
resolved inside the store; symlinks, directories, and non-regular files
are never listed or downloadable; all failures return bounded English
errors without host paths. Downloads stream with
`application/octet-stream`, safe attachment disposition, and correct
length. No backup creation, deletion, retention, or upload exists.

## Opt-in project database provisioning (Slice 4c1)

`internal/provision` derives server-owned database/role names from the
opaque service ID, generates a cryptographically random alphanumeric
password, and executes the PostgreSQL administration statements
(`CREATE ROLE`/`CREATE DATABASE`/`GRANT`) through an injected executor.
Identifiers are strictly validated and quoted; the generated password
never appears in errors or logs and is shown exactly once on an
authenticated no-store credential page. `registry.Lifecycle.
CreateProvisioned` orchestrates registry/Caddy/provisioner: provisioning
failure removes the service again via the accepted compensation path,
and failed compensation surfaces as `*CompensationError`. Deleting a
service that carries provisioned DB identifiers fails closed (409):
automatic database decommissioning is intentionally not implemented.

## Session-authenticated migration dumps (Slice 4c2)

`internal/dump` provides a session+CSRF-protected, rate-limited
`pg_dump -Fc` streaming boundary. The target service is resolved only
through the injected registry; the argument array is fixed and
shell-free (`--host`, `--user`, `-Fc`, `--no-password`, database name)
with credentials — when a deployment supplies one — flowing only via
the environment hook, never arguments. At most one dump per service per
five minutes (in-memory limiter; resets on process restart; rejected
attempts never consume quota). Successful starts stream
`application/octet-stream` with safe attachment disposition; the child
process is terminated on client disconnect, and post-header process
failures are logged without secrets and never claimed as completed
dumps. No bearer-token authentication exists.

## Compose runtime wiring (Slice 5)

`main.go` is the production runtime for the Compose `control_plane` service.
Startup wiring is explicit and fails closed:

1. `config.LoadRuntime()` — passcode, session secret, and the registry
   database URL are mandatory; every path defaults to the accepted
   deployment boundary (generated Caddyfile directory, Caddyfile, private
   admin endpoint `caddy:2019`, Caddy log, backup directory). Relative
   directories are rejected.
2. `pgxpool` connects to the fresh registry database and must answer a ping,
   or the process exits.
3. Dependencies are constructed exactly as accepted in Slices 2–4: the
   Caddy validate/reload operator, the read-only log reader, the
   generated-only Caddyfile store (`sites/manual` is never part of its
   configuration), the pgx repository and lifecycle (with the optional
   provisioner attached), the read-only backup browser, and the
   fixed-argument `pg_dump` boundary. The dump host/user derive from the
   registry DSN; credential material flows only through the child
   environment hook, never through arguments.

### Migrations: `control-plane migrate`

The `migrate` subcommand applies the committed versioned SQL migrations from
the embedded `migrations/` directory. Each migration runs in its own
transaction and is recorded in `schema_migrations`; already-recorded
versions are skipped, so reruns (Compose retries/restarts) are safe no-ops.
This is the only migration mechanism; no legacy-data compatibility path exists.

### Health

`GET /healthz` is an unauthenticated readiness probe returning `200 ok` only
when the registry database answers a ping (2-second timeout), so the
container never advertises a ready registry before the schema is usable.

### Runtime configuration reference

| Variable | Meaning | Default |
| --- | --- | --- |
| `PORTCULLIS_PASSCODE` | Owner passcode (required). | — |
| `PORTCULLIS_SESSION_SECRET` | Session signing secret, ≥32 chars (required). | — |
| `PORTCULLIS_DATABASE_URL` | PostgreSQL URL of the fresh registry database (required). | — |
| `PORTCULLIS_GENERATED_DIR` | Writable generated-Caddyfile directory (absolute). | `/etc/caddy/sites/generated` |
| `PORTCULLIS_CADDY_CONFIG` | Root Caddyfile for validate/reload. | `/etc/caddy/Caddyfile` |
| `PORTCULLIS_CADDY_ADMIN` | Private Caddy admin endpoint. | `caddy:2019` |
| `PORTCULLIS_CADDY_LOG` | Caddy log file for the bounded reader. | `/var/log/caddy/portcullis.log` |
| `PORTCULLIS_BACKUP_DIR` | Read-only backup directory. | `/backups` |
| `PORTCULLIS_ADDR` | HTTP listen address. | `:8080` |

Secrets are supplied via the environment only; they never appear in logs,
arguments, or source fixtures. The Docker image (`Dockerfile`) is a static
CGO-disabled Go build on Alpine with the `caddy` binary (validate/reload)
and a version-matched `postgresql18-client` (`pg_dump`) installed.
