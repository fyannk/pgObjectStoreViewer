---
sidebar_position: 1
---

# Installation

ObjectStoreViewer is a single static Go binary shipped as a distroless
non-root image. There is no database, no persistent volume, no leader election,
and no cluster state. Installing it means giving one process a read-only
credential, one repository root, and a network boundary.

## Prerequisites

- A backup repository in S3 (or an S3-compatible endpoint such as MinIO),
  Azure Blob Storage, or GCS, written by Barman Cloud.
- A credential that can **list and get** under exactly the destination prefix,
  and that cannot write. See [credentials and
  policies](configuration.md#provider-credentials).
- For Kubernetes: an authentication proxy you control, and a cluster that
  supports `NetworkPolicy`.
- To build from source: Go 1.26+ and `make`. To build the image: Docker.

## Run it locally

```bash
make build
REPOSITORY_FORMAT=barman-cloud \
PROVIDER=s3 \
DESTINATION_PATH=s3://example-backups/repository \
AWS_REGION=eu-west-1 \
./bin/objectstoreviewer
```

Open `http://localhost:3000/`. The first render may show an incomplete or
unknown catalog until the first background scan finishes — the process serves
only a published snapshot and never scans inside a request.

For MinIO or another S3-compatible endpoint add
`ENDPOINT_URL=http://minio.storage.svc:9000`, and `ENDPOINT_CA_FILE` if the
endpoint presents a private CA.

## Run the image

```bash
docker pull ghcr.io/fyannk/pgobjectstoreviewer:v0.1.1
docker run --rm -p 3000:3000 \
  -e REPOSITORY_FORMAT=barman-cloud \
  -e PROVIDER=s3 \
  -e DESTINATION_PATH=s3://example-backups/repository \
  -e AWS_REGION=eu-west-1 \
  ghcr.io/fyannk/pgobjectstoreviewer:v0.1.1
```

Use a version tag or digest for deployments. The
[GitHub Release](https://github.com/fyannk/pgObjectStoreViewer/releases/latest)
provides the immutable digest, Linux binaries, checksums, SPDX SBOM, license
inventory, and vulnerability report. `make docker-build` remains available for
local source builds as `objectstoreviewer:dev`.

The image is a statically linked Go 1.27.0 binary on
`gcr.io/distroless/static-debian13:nonroot`, running as UID/GID 65532, with no
shell. It runs with a read-only root filesystem provided `/tmp` is writable.
`make test-container` checks exactly that profile — plus an arbitrary UID/GID
override, dropped capabilities, no-new-privileges, health and readiness
semantics, and SIGTERM handling.

## Deploy on Kubernetes

[`deploy/kubernetes-example.yaml`](https://github.com/fyannk/pgObjectStoreViewer/blob/main/deploy/kubernetes-example.yaml)
is the reference manifest: a Deployment, an internal `ClusterIP` Service on port
3000, and a default-deny ingress `NetworkPolicy`.

```bash
kubectl apply -f deploy/kubernetes-example.yaml
```

:::warning The deployment is only safe with its boundary
The viewer has no authentication and no TLS. Expose **only** the proxy; let the
proxy reach the internal Service on port 3000; have it strip any
client-supplied identity header before setting `X-Forwarded-User`; label the
proxy pod `app.kubernetes.io/component: auth-proxy` so the example
`NetworkPolicy` admits it; and never grant users or other namespaces direct
access to the viewer Service. See [security and the trust
boundary](security.md).
:::

The pod template already sets `automountServiceAccountToken: false`,
`runAsNonRoot`, the `RuntimeDefault` seccomp profile, all capabilities dropped,
no privilege escalation, a read-only root filesystem, and a 16 MiB `emptyDir`
at `/tmp`.

### Probes

| Probe | Path | Meaning |
|---|---|---|
| liveness | `/healthz` | process liveness only |
| readiness | `/readyz` | valid configuration **plus** a recent lightweight, prefix-scoped list that succeeded |

Neither endpoint runs a scan or exposes topology. An empty prefix is reachable
and therefore ready — it yields a complete inventory with zero totals.

:::caution Health is not catalog health
Do not build a readiness gate out of catalog health. A stale, incomplete, or
unknown catalog is a correct state to serve, not a reason to remove the pod
from a Service.
:::

### Resource budget

The initial budget is a **25m CPU / 64 MiB request** and a
**500m CPU / 256 MiB limit**. `make test-scale` validates that
memory limit against a million-object scan. Raise `SCAN_CONCURRENCY` only
together with the CPU limit.

## What to read next

- [Configuration](configuration.md) — the frozen variable contract and
  provider credentials.
- [Security](security.md) — the trust boundary and the read-only guarantees.
- [The pgConsole sidecar](sidecar.md) — the producer profile and its required
  filesystem layout.
- [Upgrade and uninstall](upgrade.md).
