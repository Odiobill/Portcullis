# Portcullis — Secure Public Frontend Manager

Portcullis is a secure control plane for public servers hosting multiple services. It leverages Caddy, Next.js, and Postgres to provide a professional registration and management interface for multi-tenant environments, optionally sharing a single database instance.

![Portcullis Logo](./apps/web/public/logo.png)

## Features

- **Service Management**: Register and decommission public services via a secure, premium dashboard.
- **Multi-Domain Support**: Map multiple hostnames/domains to a single upstream service with automatic SSL.
- **Dynamic Gateway**: Zero-restart routing via Caddy's Admin API; automatic route sync on startup.
- **Automated Provisioning**: Creates a dedicated Postgres database and user for every registered project (supports both auto-generated and custom credentials).
- **DNS-01 TLS**: Modular DNS challenge support for staging/internal networks without public ports. Per-provider Caddyfile snippets for NameCheap, Cloudflare, and Route53.
- **Automatic Backups**: Nightly `pg_dump -Fc` per service with daily/weekly/monthly retention tiers (7/4/3). Sidecar container, enabled via `--profile backup`.
- **On-Demand Dump API**: `POST /api/services/[id]/dump` — passcode-protected, rate-limited, streaming dump for service migration.
- **Container Healthchecks**: All three core containers monitored with Docker healthchecks (`caddy version`, HTTP probe, `pg_isready`). `nextjs_app` waits for healthy DB before starting.
- **Secured Access**: Passcode-protected control plane designed for public-facing deployments.
- **Modern UI**: Next.js 16.2 App Router with Rspack, Tailwind CSS, and premium dark branding.
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

### 4. Initialize Database
```bash
docker exec -it portcullis_nextjs_app ./node_modules/.bin/prisma migrate deploy
```

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

Portcullis supports three TLS modes. For staging/internal networks without public port 80, use DNS-01:

```env
# .env
CADDY_TLS_MODE=namecheap_tls    # or cloudflare_tls, route53_tls
CADDY_DNS_PROVIDER=namecheap    # for Docker image build
NAMECHEAP_API_KEY=your_api_key NAMECHEAP_API_USER=your_username
```

Then rebuild Caddy with the DNS plugin: `make build`

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
