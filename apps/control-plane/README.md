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

## Registry schema and Caddyfile core (Slice 2)

- Versioned SQL migrations live in `migrations/` (`000001_create_services.up.sql` / `.down.sql`),
  defining the fresh Postgres `services` registry (ADR-0001 fresh-database direction; no
  legacy Prisma compatibility). They are **not** applied by this slice.
- `internal/registry` validates proxy/static services (unsafe inputs and Caddy-injection
  attempts fail closed), deterministically generates Caddy site blocks (`import <tls-mode>`,
  `reverse_proxy`, `root *` + `file_server`), and deploys/removes generated files atomically
  inside one configured generated directory with an injected validate/reload `Operator`.
  Any validate/reload failure rolls back the prior file state and re-applies the prior active
  config; the original failure is returned. `sites/manual` is never touched.
- **Dependency rationale:** `github.com/jackc/pgx/v5` is declared in `go.mod` now as the
  deliberate persistence driver required by ADR-0001, but nothing imports it in Slice 2 —
  no database connection, CRUD, or migration execution exists yet (see go.mod comment).

Out of scope for Slice 1: database access, service CRUD, Caddy integration,
backups, Compose wiring, and deployment (see
`Projects/Portcullis/go-replacement-work-slices.md`).
