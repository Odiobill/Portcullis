# ADR 005: Prisma Migrations inside Container

## Status
Accepted

## Context
Prisma needs DB access to generate/apply migrations, but the DB is only reachable inside Docker.

## Decision
Run migrations inside the app container, then copy files back to host source.

## Consequences
- Predictable environment for migrations.
- Avoids exposing DB ports to the host.
