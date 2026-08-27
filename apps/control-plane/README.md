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

Out of scope for Slice 1: database access, service CRUD, Caddy integration,
backups, Compose wiring, and deployment (see
`Projects/Portcullis/go-replacement-work-slices.md`).
