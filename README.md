# Portcullis - Secure Public Frontend Manager

Portcullis is a secure control plane for public servers hosting multiple services. It leverages Caddy, Next.js, and Postgres to provide a professional registration and management interface for multi-tenant environments, optionally sharing a single database instance.

![Portcullis Logo](./apps/web/public/logo.png)

## Features

- **Service Management**: Register and decommission public services via a secure, premium dashboard. Supports both reverse proxy (Docker containers) and **static file serving** (HTML/CSS/JS sites served directly by Caddy).
- **Multi-Domain Support**: Map multiple hostnames/domains to a single upstream service with automatic SSL.
- **Generated Caddyfiles**: Services are written to `sites/generated/<service-id>.caddy`, validated, then Caddy is reloaded with rollback safety.
- **Manual Operator Config**: Operator-owned Caddy blocks live in `sites/manual/*.caddy` and are never modified by Portcullis.
- **Automated Provisioning**: Creates a dedicated Postgres database and user for every registered project (supports both auto-generated and custom credentials).
- **DNS-01 TLS**: Modular DNS challenge support for staging/internal networks without public ports. Per-provider Caddyfile snippets for NameCheap, Cloudflare, and Route53.
- **Static File Serving**: Register static sites served directly by Caddy from `/srv/sites/<domain>`, without an app container.
- **Automatic Backups**: Nightly `pg_dump -Fc` per service with daily/weekly/monthly retention tiers (7/4/3). Sidecar container, enabled via `--profile backup`; dashboard lists and downloads backups read-only.
- **On-Demand Dump API**: `POST /api/services/[id]/dump`, passcode-protected, rate-limited, streaming dump for service migration.
- **Container Healthchecks**: All three core containers monitored with Docker healthchecks (`caddy version`, `/api/health`, `pg_isready`). `nextjs_app` waits for healthy DB before starting.
- **Secured Access**: Passcode-protected control plane designed for public-facing deployments.
- **Modern UI**: Next.js 16.2 App Router with Rspack, Tailwind CSS, premium dark branding, truncation-safe service cards, and refreshable Caddy log viewing.
- **PWA Ready**: Installable on mobile for total control on the go.
- **Secure Architecture**: Multi-network Docker setup isolating projects from the control plane and from each other.
- **Resource Limits**: Configured `mem_limit` and `cpus` on all containers to prevent runaway processes.
- **Log Rotation**: Docker `json-file` driver with `max-size: 10m, max-file: 3` on all containers.

## Tech Stack

| Component | Technology |
|---|---|
| Gateway | Caddy (Alpine, custom build for DNS-01) |
| Frontend/Backend | Next.js 16.2 (TypeScript, Rspack) |
| Database | PostgreSQL 18 |
| ORM | Prisma 7 |
| Infrastructure | Docker + Docker Compose |

## Quick Start

### 1. Prerequisite Networks
```bash
docker network create caddy_gateway
docker network create db_network
```

### 2. Environment Setup
```bash
cp .env.example .env
# Edit .env with your secrets
```

### 3. Deploy Stack
```bash
make build    # build all images
make up       # start stack (without backup sidecar)
# or: make up-all  # start with backup sidecar
```

### 4. Verify Startup

Database migrations run automatically from `apps/web/entrypoint.sh` when `portcullis_nextjs_app` starts. Do not run Prisma migrations manually after a normal deploy.

```bash
docker compose ps
docker logs --tail 100 portcullis_nextjs_app

docker exec portcullis_nextjs_app node -e "require('http').get('http://localhost:3000/api/health', r => { console.log(r.statusCode); process.exit(r.statusCode === 200 ? 0 : 1); }).on('error', e => { console.error(e); process.exit(1); })"
```

Expected health probe output: `200`.

## Staging rsync deployment

For Heimdall-style staging deploys, sync from the repository root with `.rsyncignore` so runtime state is preserved:

```bash
rsync -az --delete --exclude-from=.rsyncignore \
  ./ \
  dietpi@Heimdall:/srv/portcullis/
```

The ignore file protects local dependencies, build outputs, secrets, Postgres data, and operator/runtime Caddy state:

```text
.env
data/
sites/generated/
sites/manual/
node_modules/
.next/
```

Then on the server:

```bash
cd /srv/portcullis
docker compose up --build -d
```

If staging data can be discarded and the Postgres credentials or schema need a full reset, remove the bind-mounted database directory:

```bash
cd /srv/portcullis
docker compose down --remove-orphans
sudo rm -rf data/pg_data
docker compose up --build -d
```

This is necessary because Postgres uses a bind mount at `./data/pg_data`; `docker compose down -v` does not remove that directory.

## Makefile Reference

```bash
make help          # show all targets
make build         # build all images (Caddy + Next.js + Backup)
make up            # start stack (no backup)
make up-all        # start with backup sidecar (--profile backup)
make down          # stop all containers
make restart       # down + up
make logs          # tail all container logs
make logs-caddy    # tail Caddy logs
make logs-app      # tail Next.js logs
make logs-db       # tail Postgres logs
make logs-backup   # tail backup logs
make ps            # show container status + health
make db-reset      # reset database (⚠️ destroys all data)
make dump SERVICE=<id>  # on-demand pg_dump via API
make clean         # remove everything (⚠️ destroys all data)
```

## DNS-01 Configuration

Portcullis supports these TLS modes:

```text
acme
internal
namecheap_tls
cloudflare_tls
route53_tls
```

For staging/internal networks without public port 80, use DNS-01:

```env
# .env
CADDY_TLS_MODE=namecheap_tls    # or cloudflare_tls, route53_tls
CADDY_DNS_PROVIDER=namecheap    # for Docker image build
NAMECHEAP_API_KEY=your_api_key
NAMECHEAP_API_USER=your_username
```

Then rebuild Caddy with the DNS plugin: `make build`

## Wildcard certificate spike

Wildcard support is intentionally a staging spike before it becomes a dashboard feature. Use the committed safe template and runbook:

```text
sites/manual/wildcard-spike.caddy.example
docs/p6-wildcard-certificate-spike.md
```

The template is not imported by default. Copy it to `sites/manual/wildcard-spike.caddy` only on Heimdall when ready to prove DNS-01 wildcard issuance.

## Service Migration (graduation)

When a service outgrows Portcullis and moves to its own VPS:

```bash
# On the new VPS, pipe the dump directly into pg_restore:
curl -sS -H "Authorization: Bearer $PORTCULLIS_PASSCODE \
  "https://portcullis.domain/api/services/SERVICE_ID/dump" \
  | pg_restore -h localhost -U postgres -d new_db

# Or locally:
make dump SERVICE=proj_abc123
```

## Development

See [AGENTS.md](./AGENTS.md) for detailed architecture documentation, coding conventions, and migration workflows.
