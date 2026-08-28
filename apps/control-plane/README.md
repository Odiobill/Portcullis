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
  defining the fresh Postgres `services` registry (ADR-0001 fresh-database direction; no
  legacy Prisma compatibility). They are **not** applied by these slices.
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
