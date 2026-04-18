# ADR 002: Shared Postgres with per-project isolation

## Status
Accepted

## Context
Projects need databases. Maintaining a separate Postgres container per project is resource-intensive.

## Decision
Use a single "Portcullis" Postgres instance. Provision a new database and a dedicated user with scoped permissions for each service.

## Consequences
- Efficient resource usage.
- Strict isolation through credentials.
