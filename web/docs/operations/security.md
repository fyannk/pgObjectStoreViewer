---
sidebar_position: 3
---

# Security

## The application is not a trust boundary

ObjectStoreViewer performs no authentication and no authorization, and it
terminates no TLS. Its standalone port must be reachable only through an
authenticated proxy and an operator-managed network boundary.

`TRUSTED_USER_HEADER` is display-only and never authorizes a request. Never add
a deployment that assumes the port, or a forwarded identity header, is safe
when directly reachable.

### Required deployment shape

1. Expose only the proxy through an Ingress or external Service.
2. Let the proxy reach the internal `objectstoreviewer` Service on port 3000.
3. The proxy strips any client-supplied identity header and sets
   `X-Forwarded-User` only after authentication.
4. Label the proxy pod `app.kubernetes.io/component: auth-proxy` so the shipped
   `NetworkPolicy` admits it.
5. Do not grant users or other namespaces direct access to the viewer Service.

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: objectstoreviewer-auth-proxy-only
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: objectstoreviewer
  policyTypes: [Ingress]
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app.kubernetes.io/component: auth-proxy
      ports:
        - protocol: TCP
          port: 3000
```

## Read-only, three times over

| Layer | Guarantee |
|---|---|
| Interface | domain code sees only list, bounded get/open, and stat/head — there is no write method to call |
| Build | `hack/check-readonly.sh` fails on mutation-shaped identifiers in `cmd/`/`internal/` and on forbidden actions in the shipped policy templates |
| Credential | the deployment credential must independently deny writes — the layer that survives a bug in the other two |

Use the templates under
[`deploy/policies`](https://github.com/fyannk/pgObjectStoreViewer/tree/main/deploy/policies)
and scope them to the exact destination prefix. See the [policy
reference](../reference/policies.md).

## Secrets never leave the process

Credentials, mounted secret values, provider authorization headers, signed
URLs, Azure SAS parameters, and raw credential-bearing SDK errors must not
appear in logs, traces, metrics, HTTP responses, HTML, links, tests, fixtures,
or snapshots. Redaction is mandatory at every adapter boundary, and mounted
secret contents are held in an opaque type whose `String()` is `[REDACTED]`.

Consequences you will notice in practice:

- startup errors name a variable and a reason, never a value or a path;
- provider failures are normalized to a small category vocabulary
  (`authentication`, `authorization`, `throttled`, `unavailable`, `timeout`,
  `not_found`, `safety_limit`, …) with no upstream message; and
- request logs use stable route names, never raw URLs, queries, identity
  headers, or authorization headers.

The unit suite asserts this across configuration, all three provider adapters,
the application, and the evidence API—including canary credentials that must
never appear anywhere in output. Run it with `make test`.

## Object keys are opaque

Keys are never mapped onto local filesystem paths, path-cleaning is never used
as an authorization boundary, and no key or cursor may escape its configured
store, destination prefix, and scope. In sidecar mode the scanner is forcibly
confined to the single configured Barman server prefix even when the credential
could read more.

## No raw backup content

Bounded format metadata (Barman `backup.info`, history files, pgBackRest
info/manifests) is parsed internally, but only allowlisted fields are rendered.
Arbitrary custom metadata and tags are hidden by default. The Barman catalog
retains only allowlisted facts — status/type, WAL anchors, timestamps, sizes,
compression, encryption, and structural-artifact evidence — and discards
unknown metadata fields and their values.

Raw object download is an opt-in Slice 10 feature that has not shipped; with
`ALLOW_DOWNLOAD=false` no usable download route or link exists.

## Runtime hardening

The HTTP server uses a 5-second read-header timeout, 15-second read timeout,
30-second write timeout, 60-second idle timeout, 16 KiB maximum headers, and a
15-second graceful-shutdown deadline. Sensitive responses are non-cacheable and
carry restrictive CSP, referrer, framing, and content-type headers.

The image is a statically linked binary in a distroless non-root runtime. The
container test runs it under an overridden UID/GID 12345 with a read-only
root, only `/tmp` mounted, all capabilities dropped, and no-new-privileges set.

## Sidecar channel

In `pgconsole-sidecar` mode there is no TCP listener at all. The channel is
HTTP/1.1 over a pod-private Unix socket authenticated by a pod-local bearer
token: the token authenticates the caller, and the socket path — in a volume
mounted only into the two intended containers — authenticates the server
boundary. The auth proxy must not carry either volume. See
[the sidecar profile](sidecar.md).

## Reporting a vulnerability

Do not open a public issue with reproduction details for a disclosure problem.
Report it privately to the maintainers with the redacted evidence described in
[troubleshooting](troubleshooting.md#collecting-evidence-for-a-bug-report).
