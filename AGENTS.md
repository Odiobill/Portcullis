# Portcullis — Agent Briefing

> **Read this file in full before doing anything else.**
> This is not a README. It is a briefing written for the agent working on this project.
> Update the relevant sections at the end of every conversation (see [End-of-Session Checklist](#end-of-session-checklist)).

---

## What Is Portcullis?

Portcullis is a self-hosted staging infrastructure manager. It sits on a shared VPS and acts as the single control plane for all experimental projects running there.

It consists of:

- A **Caddy gateway** that handles all inbound traffic (ports 80/443), provisions SSL certificates automatically via ACME, and routes requests to the appropriate upstream containers — without ever restarting or reloading a config file.
- A **Next.js PWA** (the manager UI) that lets the operator register, inspect, and decommission staging services through a browser interface, including on mobile.
- A **Postgres instance** shared across all registered projects (one database + one user per project, never shared credentials).
- A thin **backend API** (Next.js route handlers) that translates UI actions into Caddy Admin API calls and Postgres provisioning commands.

When a project hosted under Portcullis matures and is promoted to its own VPS, it is simply deregistered here and redeployed independently.

---

## Stack

| Layer | Technology | Version |
|---|---|---|
| Gateway | Caddy | alpine (latest stable) |
| Manager UI/API | Next.js (App Router, TypeScript, Tailwind) | 16.2 |
| Database | PostgreSQL | 18 alpine |
| Runtime | Node.js | LTS (as required by Next.js 16.2) |
| Containerisation | Docker + Docker Compose | latest stable - installed |
| ORM / Migrations | Prisma | latest compatible |
| i18n | next-intl | latest compatible |

**Do not suggest replacing or upgrading any of these without explicit instruction.**

---

## Architecture

### Docker Networks

Two external Docker networks must exist on the host before any stack is started. Actually, the "docker compose" should be able to create them automatically, but they can be created once manually:

```bash
docker network create caddy_gateway
docker network create db_network
```

- `caddy_gateway` — Caddy and any upstream app container that needs to receive proxied traffic.
- `db_network` — Caddy's Postgres container and any project app container that needs database access.

### Container Layout

```
caddy_gateway (external network)
  └── caddy              ← ports 80, 443, 443/udp exposed to host
  └── [projectX_app]     ← upstream containers from other stacks

db_network (external network)
  └── portcullis_db      ← shared Postgres instance
  └── [projectX_app]     ← project containers that need DB access

portcullis_internal (stack-internal network)
  └── caddy
  └── nextjs_app
  └── portcullis_db
```

Caddy's admin API is bound to `localhost:2019` **inside the container** — never exposed on a host port. The Next.js backend reaches it via the internal network at `http://caddy:2019`.

### Caddyfile (bootstrap only)

The Caddyfile is used **only for initial bootstrap** — to route traffic to the Portcullis manager UI itself. All subsequent routes (for registered projects) are managed exclusively via the Caddy Admin API at runtime. Never add project routes to the Caddyfile. If a specific .env variable indicates the "main address" of Portcullis as "staging.yourdomain.com", then the equivalent Caddyfile would probably work as this one:

```
{
  admin localhost:2019
}

staging.yourdomain.com {
  reverse_proxy nextjs_app:3000
}
```

### Caddy Admin API

All runtime Caddy configuration is performed through its REST admin API. The canonical wrapper lives in `lib/caddy-api.ts`. **Never call the Caddy API directly from anywhere else in the codebase.** Always go through this module.

Key operations:
- `POST /config/apps/http/servers/https/routes` — add a route (with `@id` for addressability)
- `DELETE /id/{id}` — remove a route by ID
- `GET /config/apps/http/servers/https/routes` — list active routes
- Sync all routes from Postgres on startup (Caddy state is ephemeral; Postgres is the source of truth)

---

## Development Environment

### Local Machine (primary workspace)

All development, building, and testing happens locally. The local machine:

- Has a full Docker + Docker Compose setup identical to the VPS.
- Has an internet connection but is **not reachable from outside** (no NAT). This means Caddy cannot obtain real SSL certificates locally. Use Caddy's internal CA or self-signed certs for local dev, or disable HTTPS in the local Caddyfile override.
- Is where the agent runs, edits files, and executes build/lint/typecheck commands.

### Allowed Commands (agent may run these)

```bash
tsc --noEmit                  # type-check after changes
npx prisma generate           # regenerate Prisma client after schema changes
npx prisma migrate diff       # inspect pending migrations
docker compose build          # build images
docker compose up -d          # start stack
docker compose down           # stop stack (never use -v unless explicitly instructed)
docker compose logs -f        # inspect logs
docker exec -it <container>   # exec into a running container
```

### Forbidden Commands (agent must NOT run these)

```bash
npm run dev          # not useful; everything runs in Docker
npm run start        # same reason
npx prisma migrate deploy   # never run directly — see Prisma workflow below
```

---

## Prisma Migration Workflow

> **This is the single most common source of repeated mistakes. Read carefully.**

Prisma migrations must be generated and applied **inside the running container**, not on the host. The migration files must then be copied back into the source tree so they are included in the next image build.

### Step-by-step

1. Edit `prisma/schema.prisma` as needed.

2. Regenerate the Prisma client on the host (for type-checking):
   ```bash
   npx prisma generate
   ```

3. Start the current stack so the DB container is running:
   ```bash
   docker compose up -d portcullis_db nextjs_app
   ```

4. Generate the migration **inside the app container**:
   ```bash
   docker exec -it portcullis_nextjs_app \
     npx prisma migrate dev --name <descriptive_migration_name>
   ```
   This applies the migration to the running DB and writes the migration files inside the container at `prisma/migrations/`.

5. Copy the new migration files back to the host source tree:
   ```bash
   docker cp portcullis_nextjs_app:/app/prisma/migrations ./prisma/
   ```

6. Verify the files are present in `prisma/migrations/` on the host, then commit them.

7. On the VPS, `docker compose up --build -d` will build a new image that includes the migration files. On container startup, the entrypoint runs `prisma migrate deploy` (non-interactive, applies pending migrations).

### Why this workflow?

Prisma's migration engine needs a live database connection to generate migration SQL. The database is only reachable from within the Docker network, not from the host directly. Running migrations on the host would require exposing the DB port, which we deliberately avoid.

**The agent must propose this workflow whenever a schema change is required, without needing to be reminded.**

---

## Internationalisation (i18n)

The manager UI is a PWA and must support multiple languages from the start. The i18n library is **next-intl**.

- All user-facing strings go in `messages/{locale}.json`. Never hardcode UI strings.
- Supported locales are defined in `i18n/config.ts`.
- Initial languages: `en` (default), `it`. Additional languages are added by creating the corresponding messages file.
- Use the `useTranslations` hook in client components and `getTranslations` in server components and route handlers.

---

## PWA Requirements

Portcullis is a PWA, not just a website.

- A Web App Manifest (`public/manifest.json`) must be present and complete (name, short_name, icons, display, start_url, theme_color).
- A service worker must be registered. Use `next-pwa` or equivalent compatible with Next.js 16.2.
- The UI must be responsive and usable on mobile (the operator may register or decommission a service from their phone).
- Offline behaviour: the shell should load offline; API-dependent views should show a clear offline state rather than breaking.

---

## Project Structure

```
portcullis/
├── AGENTS.md                  # this file — keep it updated
├── README.md                  # human-facing docs — updated each session
├── docker-compose.yml         # gateway + manager stack
├── docker-compose.local.yml   # local overrides (no HTTPS, ports exposed for debugging)
├── Caddyfile                  # bootstrap only — do not add project routes here
├── .env.example               # all required env vars, no secrets
├── apps/
│   └── web/                   # Next.js 16.2 app
│       ├── app/               # App Router
│       ├── components/
│       ├── lib/
│       │   ├── caddy-api.ts   # all Caddy Admin API calls — single entry point
│       │   └── db.ts          # Postgres connection (node-postgres or Prisma client)
│       ├── messages/
│       │   ├── en.json
│       │   └── it.json
│       ├── prisma/
│       │   ├── schema.prisma
│       │   └── migrations/
│       └── public/
│           └── manifest.json
└── docs/
    ├── decisions/             # Architecture Decision Records (ADRs)
    └── tasks/
        └── current-task.md   # scoped context for the current session
```

---

## Coding Conventions

- **TypeScript strict mode** is enabled. No `any` without a comment explaining why.
- All Caddy Admin API interactions go through `lib/caddy-api.ts`. No exceptions.
- All database access goes through `lib/db.ts` (or the Prisma client exported from it).
- Server Actions and Route Handlers live colocated with the feature they serve, not in a global `api/` folder.
- No hardcoded strings in UI components — always use next-intl.
- Tailwind only for styling. No CSS modules, no styled-components, no inline `style` props except for dynamic values that Tailwind cannot express.
- Prefer `async/await` over Promise chains.
- Every function that calls an external system (Caddy API, Postgres) must handle errors explicitly and return typed results, not throw naked exceptions into the UI.

---

## Deployment (usually performed by the user)

### VPS sync + deploy

```bash
# From local machine — sync source to VPS (adjust path and host)
rsync -avz --exclude node_modules --exclude .next \
  ./ user@vps-host:/opt/portcullis/

# On VPS
cd /opt/portcullis
docker compose up --build -d
```

### First-time VPS setup

```bash
docker network create caddy_gateway
docker network create db_network
docker compose up --build -d
```

### Environment variables

All secrets are passed via environment variables. See `.env.example`. On the VPS, a `.env` file (not committed) or Docker secrets are used. The agent must never hardcode secrets or commit `.env` files.

---

## Architecture Decision Records

Significant decisions are recorded in `docs/decisions/`. Before re-litigating any of the following, read the corresponding ADR.

| # | Decision |
|---|---|
| 001 | Caddy Admin API over Caddyfile for runtime config |
| 002 | Shared Postgres with per-project DB isolation |
| 003 | Next.js 16.2 App Router for manager UI |
| 004 | External Docker networks for cross-stack routing |
| 005 | Prisma migrations generated inside container, committed to source |

---

## Current State

> **This section is updated at the end of every session.**

### What exists

- [x] Project scaffolding
- [x] Docker Compose stack (Caddy + Next.js + Postgres)
- [x] Caddyfile (bootstrap)
- [x] `lib/caddy-api.ts` (fully implemented)
- [x] `lib/db.ts` (implemented with `pg` driver adapter for Prisma 7)
- [x] Prisma schema (Service model)
- [x] i18n setup (next-intl, en + it)
- [x] PWA manifest + service worker
- [x] Service registration UI (dashboard + server actions)
- [x] Service deregistration UI
- [x] Postgres provisioning on registration (lib/db-provisioning.ts)
- [x] Caddy route sync on startup (instrumentation.ts)
- [x] Consolidated repository structure (root .git only)

### Known gotchas

- **Next.js 16.2 Routing**: Use `proxy.ts` instead of `middleware.ts` for internationalized routing and request interception.
- **Rspack Support**: Initialized with `--rspack` for optimized build performance. Some typegen tools might require manual triggers in v16.2.
- **Prisma 7 Config**: Database connection URLs have been moved from `schema.prisma` to `prisma.config.ts`.
- **Prisma 7 Runtime**: In Next.js standalone builds, the `PrismaClient` may fail to pick up the configuration automatically. Use the `pg` driver adapter (`@prisma/adapter-pg`) to explicitly provide the connection string and adapter to the constructor.
- **Caddyfile Snippets**: Snippets must be defined at the TOP of the `Caddyfile`. Use `import {env.VAR}` syntax carefully; ensure the `caddy` service in `docker-compose.yml` explicitly passes these variables.
- **Prisma Migrations in Docker**: Follow the workflow in [Prisma Migration Workflow](#prisma-migration-workflow). If selective copying of node_modules fails, copy the entire `node_modules` into the runner stage.
- **Next-intl Plugin**: Ensure `createNextIntlPlugin()` is used in `next.config.ts` to avoid "Couldn't find next-intl config file" errors in standalone mode.
- **Caddy Admin API Bind**: The Caddy Admin API must be bound to `:2019` in the `Caddyfile` to be accessible from the Next.js container; `localhost:2019` is only reachable from within Caddy itself.
- **Instrumentation Delay**: Added a 5-second delay to `instrumentation.ts` to ensure Caddy is reachable and DNS has stabilized before the initial route sync.

### Last session summary

Resolved 500 errors by correctly configuring the `next-intl` plugin in `next.config.ts`. Fixed Caddy Admin API connectivity issues by updating the bind address and adding a startup delay in `instrumentation.ts`. Consolidated the project into a single git repository by removing nested `.git` directories and implementing a recursive root `.gitignore`.

---

## End-of-Session Checklist

> Run through this before ending every conversation.

The agent must perform the following steps at the end of each session **without being asked**:

1. **Update "Current State"** above — tick completed items, add new ones if scope expanded.

2. **Update "Known gotchas"** — add any issue encountered that was non-obvious or required more than one attempt to resolve. Be specific enough that the next agent session can avoid the same mistake.

3. **Update "Last session summary"** — two to five sentences describing what was built or changed.

4. **Update `README.md`** — add or update the feature list to reflect what was implemented. The README is human-facing; write it accordingly.

5. **Remind the operator** to:
   - Review and commit all changes (`git diff` before committing).
   - Sync and deploy to the VPS if the session produced a deployable change.

6. **If a `docs/tasks/current-task.md` was used**, archive it to `docs/tasks/YYYY-MM-DD-<slug>.md` and clear the current file.

