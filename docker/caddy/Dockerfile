# Caddy Dockerfile — conditional DNS-01 plugin build
#
# When CADDY_DNS_PROVIDER is set (e.g. 'namecheap', 'cloudflare', 'route53'),
# this build recompiles Caddy with the corresponding caddy-dns plugin via xcaddy.
# When CADDY_DNS_PROVIDER is empty, the build is a no-op (cp + COPY, <1s).
#
# Usage:
#   docker compose build --build-arg CADDY_DNS_PROVIDER=namecheap
#   # Or set CADDY_DNS_PROVIDER in .env and run:
#   docker compose build caddy

ARG CADDY_DNS_PROVIDER

FROM caddy:builder AS builder

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

FROM caddy:alpine

COPY --from=builder /tmp/caddy /usr/bin/caddy

# Verify the binary works
RUN caddy version
