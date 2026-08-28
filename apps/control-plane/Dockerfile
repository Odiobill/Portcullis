# Control-plane Dockerfile — Go control plane with a DNS-01-capable Caddy
# and a version-matched pg_dump.
#
# Build stages:
#   1. Static Go binary (CGO_ENABLED=0) from the committed sources.
#   2. Conditional Caddy build mirroring docker/caddy: when
#      CADDY_DNS_PROVIDER is set (e.g. 'namecheap'), the builder compiles
#      Caddy with the matching caddy-dns plugin via xcaddy so that
#      validate/reload of the root Caddyfile can expand DNS-01 TLS snippets
#      (module must be registered in the same binary that runs validate).
#      When it is empty, the stock builder Caddy binary is used as fallback.
#
# Runtime stage: the postgres:18-alpine image provides a pg_dump client that
# is version-matched with the PostgreSQL 18 registry database (pg_dump
# refuses servers newer than its own major version). The Alpine caddy
# package is installed for its runtime dependencies and the selected builder
# output is copied to /usr/sbin/caddy — the PATH-resolved Alpine location
# (apk installs caddy there, so /usr/bin/caddy would be ineffective).
#
# No database client credentials are baked into the image: everything
# arrives through environment variables at runtime.

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/control-plane .

FROM caddy:builder AS caddy-builder
ARG CADDY_DNS_PROVIDER
WORKDIR /build
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    if [ -n "${CADDY_DNS_PROVIDER}" ]; then \
      echo "→ Building Caddy with caddy-dns/${CADDY_DNS_PROVIDER} plugin"; \
      xcaddy build \
        --with github.com/caddy-dns/${CADDY_DNS_PROVIDER} \
        --output /tmp/caddy; \
    else \
      echo "→ No DNS provider set — copying stock Caddy binary"; \
      cp /usr/bin/caddy /tmp/caddy; \
    fi

FROM postgres:18-alpine

# Alpine's caddy package (runtime dependencies) plus a version-matched
# pg_dump client; then the DNS-01-capable (or stock) builder binary replaces
# the apk binary at its PATH-resolved location.
RUN apk add --no-cache caddy
COPY --from=caddy-builder /tmp/caddy /usr/sbin/caddy
RUN caddy version \
    && pg_dump --version

COPY --from=build /out/control-plane /usr/local/bin/control-plane

# The root Caddyfile, generated/manual site directories, Caddy logs, and
# backups are bind-mounted/volume-mounted at run time; no secrets are
# copied into the image.
EXPOSE 8080
ENTRYPOINT ["control-plane"]
