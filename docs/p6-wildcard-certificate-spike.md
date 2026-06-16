# Portcullis P6 Wildcard Certificate Spike

This spike proves Caddy wildcard certificate behavior on Heimdall before wildcard support is promoted into the dashboard.

The template is committed as:

```text
sites/manual/wildcard-spike.caddy.example
```

It is not imported by Caddy because the main `Caddyfile` imports only `*.caddy`. This prevents an accidental wildcard route from intercepting generated service routes or triggering DNS-01 issuance before the operator is ready.

## Constraints

- Let's Encrypt wildcard certificates require DNS-01.
- `*.staging.lucchesi.io` covers `foo.staging.lucchesi.io`.
- It does not cover `api.foo.staging.lucchesi.io`.
- The `acme` HTTP-01 mode cannot issue wildcard certificates.
- On Heimdall, use `namecheap_tls` unless the staging DNS provider changes.

## Heimdall procedure

From the Heimdall checkout:

```bash
cd /srv/portcullis
cp sites/manual/wildcard-spike.caddy.example sites/manual/wildcard-spike.caddy
```

Validate and reload through the Next.js container, which has the same plugin-enabled Caddy binary used for runtime validation:

```bash
docker exec portcullis_nextjs_app caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
docker exec portcullis_nextjs_app caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile --address http://caddy:2019
```

Check Caddy logs for DNS-01 activity:

```bash
docker compose logs caddy --tail 200 | grep -Ei 'dns|challenge|certificate|namecheap|acme'
```

Verify the route response:

```bash
curl -k https://test.staging.lucchesi.io
```

Expected:

```text
wildcard ok
```

Verify certificate SANs:

```bash
openssl s_client \
  -connect staging.lucchesi.io:443 \
  -servername test.staging.lucchesi.io \
  </dev/null 2>/dev/null \
  | openssl x509 -noout -subject -issuer -ext subjectAltName
```

Expected evidence:

- issuer is Let's Encrypt or the expected ACME issuer
- `DNS:*.staging.lucchesi.io` appears in `subjectAltName`
- Caddy logs show DNS-01, not HTTP-01

## Cleanup

After the spike:

```bash
rm sites/manual/wildcard-spike.caddy
docker exec portcullis_nextjs_app caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
docker exec portcullis_nextjs_app caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile --address http://caddy:2019
```

Keep the `.example` template in Git for repeatable staging verification.

## Product decision after spike

Only after the Heimdall spike is proven should Portcullis add dashboard-level wildcard certificate support. Candidate schema fields are intentionally deferred:

```prisma
usesWildcardCert Boolean @default(false) @map("uses_wildcard_cert")
wildcardBaseDomain String? @map("wildcard_base_domain")
```

Do not add these fields until the DNS-01 behavior is confirmed on the target deployment.
