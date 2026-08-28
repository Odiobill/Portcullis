# ADR 001: Caddy Runtime Configuration

## Status
Superseded. The accepted mechanism is the Go control plane's fixed-argument
Caddy CLI boundary (`internal/caddyops`): `caddy validate` before deploy and
`caddy reload --address http://caddy:2019` after it. The private admin
endpoint (`caddy:2019`, inside the Docker network) is reached only through
that CLI — never via direct REST calls from the control plane, and never
published on a host port.

## Context
Portcullis needs to register and decommission staging services at runtime
without restarting the gateway or manual Caddyfile edits.

## Decision (current, accepted)
The Go control plane validates and reloads Caddy through the caddy binary
with fixed, fully specified arguments; generated Caddyfiles are deployed
atomically under `sites/generated/` with rollback on validate/reload
failure. Operator-managed files under `sites/manual/` are read-only to the
control plane.

## Consequences
- No Docker socket and no direct admin-API dependency in application code.
- Reload failures roll back the generated file state and re-apply the prior
  active configuration.
