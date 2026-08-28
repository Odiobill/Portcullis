# Control-plane Dockerfile — Go control plane with Caddy and pg_dump
#
# Build stage: static Go binary (CGO_ENABLED=0) from the committed sources.
# Runtime stage: the postgres:18-alpine image provides a pg_dump client that
# is version-matched with the PostgreSQL 18 registry database (pg_dump
# refuses servers newer than its own major version), and we layer the caddy
# binary on top for validate/reload of the root Caddyfile (fixed arguments,
# never the Docker socket or the admin API directly).
#
# No database client credentials are baked into the image: everything
# arrives through environment variables at runtime.

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/control-plane .

FROM postgres:18-alpine

# Caddy validate/reload command boundary.
RUN apk add --no-cache caddy \
    && caddy version \
    && pg_dump --version

COPY --from=build /out/control-plane /usr/local/bin/control-plane

# The root Caddyfile, generated/manual site directories, Caddy logs, and
# backups are bind-mounted/volume-mounted at run time; no secrets are
# copied into the image.
EXPOSE 8080
ENTRYPOINT ["control-plane"]
