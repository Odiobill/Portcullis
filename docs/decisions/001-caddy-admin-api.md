# ADR 001: Caddy Admin API for Runtime Configuration

## Status
Accepted

## Context
Portcullis needs to register and decommission staging services at runtime without restarting the gateway or manual Caddyfile edits.

## Decision
Use Caddy's REST Admin API for all project-related routing.

## Consequences
- Routing state is ephemeral in Caddy (must be synced from Postgres).
- No reload-induced downtime.
