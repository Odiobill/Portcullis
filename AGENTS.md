# Portcullis — Agent Briefing

Briefing for code agents working in this repository. Authority for project
decisions lives in the Piren vault (`Projects/Portcullis/`); this file
covers only how to work with the code.

## What this repository is

Portcullis is a secure control plane for public servers hosting multiple
services behind Caddy. The single accepted architecture is:

- **Go control plane** (`apps/control-plane`): English-only, server-rendered
  HTML via `net/http` templates; `pgx` against PostgreSQL with explicit
  versioned SQL migrations in `apps/control-plane/migrations/`. Owner
  sessions are expiring HMAC-signed tokens (HttpOnly, Secure-in-TLS,
  SameSite, path-scoped) and every mutation verifies a session-bound CSRF
  token. No client-side framework, no i18n routing, no ORM.
- **Caddy gateway** (`docker/caddy`): TLS termination, DNS-01 plugin builds,
  optional wildcard spike; validates/reloads through the control plane.
- **PostgreSQL 18**: one registry database (fresh schema, disposable by
  design) plus optional per-service project databases.
- **Docker Compose** (`docker-compose.yml`): `caddy`, `control_plane`,
  one-shot `migrate`, `portcullis_db`, and an optional `backup` sidecar
  (`--profile backup`). `control_plane` starts only after `migrate`
  completed successfully against a healthy database.

## Security and boundary rules (do not break these)

- Caddy admin endpoint stays private inside the Docker network (`caddy:2019`);
  never publish it on a host port.
- The control plane never mounts the Docker socket and never talks to the
  Caddy admin API directly; it runs `caddy validate`/`caddy reload` with
  fixed arguments through `internal/caddyops`.
- Generated Caddyfiles (`sites/generated/`) are written only by the control
  plane via the rollback-safe store; operator files (`sites/manual/`) are
  mounted read-only into the control plane and are never touched by it.
- Backups and Caddy logs are consumed read-only; nothing in the repository
  creates, deletes, restores, or retains backups.
- On-demand dumps are session+CSRF-authenticated dashboard actions streaming
  fixed-argument `pg_dump -Fc`; there is no bearer-token endpoint and no
  credential ever appears in process arguments.
- Provisioner failures are surfaced password-free as compensation/manual-
  inspection conditions; a failed compensation is never reported as success.
- Secrets (passcode, session secret, database credentials) live only in the
  environment, never in source, logs, arguments, or fixtures. Required
  configuration fails closed.

## Working on the code

- Go module: `apps/control-plane` (module `portcullis/control-plane`).
  Packages live under `internal/`: `session` (auth/CSRF), `registry`
  (service CRUD + Caddyfile store), `caddyops`, `backups`, `provision`,
  `dump`, `migrate`, `config`, `server` (HTTP wiring).
- Every production behavior change follows RED → GREEN tests first. Tests
  inject fakes (commander, executor, pool, clock); no test ever executes
  pg_dump, connects to PostgreSQL, or runs Caddy.
- Verify before committing:

  ```sh
  cd apps/control-plane
  gofmt -l .            # must be empty
  go test ./... -count=1
  go test ./... -race -count=1
  go vet ./...
  go build ./...
  ```

- Parse-only Compose check: `docker compose config`.

## Disposable verification versus live authority

- `docker-compose.smoke.yml` runs the full stack in an isolated, disposable
  project (own network/volumes, no host ports, no binds into `data/` or
  `sites/`) — safe to run with a unique project name and tear down with
  `docker compose -f docker-compose.smoke.yml -p <unique> down -v`.
- Everything else that touches a running instance is explicitly out of
  bounds for agent work.

## Hard prohibitions

Without a separate, explicit steward decision, never: deploy or cut over,
run Compose lifecycle/build against the live project, migrate or reset any
existing database, restore a backup, delete volumes or `data/` content,
modify `sites/generated/` or `sites/manual` runtime content, edit `.env`,
or perform any destructive action against a running Portcullis instance.
Destructive cutover steps exist only as the reviewed, never-executed plan in
the Piren vault runbook.
