# ADR 004: External Docker Networks

## Status
Accepted

## Context
Caddy needs to reach containers in other stacks.

## Decision
Use pre-created external networks (`caddy_gateway`, `db_network`) to bridge independent Docker Compose projects.

## Consequences
- Simplified cross-container communication.
- Requires manual network creation on first setup.
