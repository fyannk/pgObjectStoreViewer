---
sidebar_position: 4
---

# The pgConsole sidecar

`RUNTIME_MODE=pgconsole-sidecar` selects a distinct producer surface: the same
scanner and the same evidence semantics, published to pgConsole over a
pod-private authenticated channel instead of to a browser.

:::warning Not a supported pair yet
The producer runtime is executable and proven at the producer boundary, and the
composed Pod and channel mechanics were proven live on OpenShift 4.20 under
`restricted-v2` on 2026-07-29. A live cross-check demonstration, pgtoolbox's
published per-profile resource sizing, and the first complete qualification
profile remain release gates — no pgConsole/ObjectStoreViewer runtime pair is
advertised as supported.
:::

## What the mode changes

| | standalone | pgconsole-sidecar |
|---|---|---|
| TCP listener | `LISTEN_ADDR`, default `:3000` | **none** |
| Browser routes | `/`, `/wals` | none |
| Evidence API | not served | `/var/run/objectstoreviewer/evidence.sock` |
| Repository format | `barman-cloud` or `pgbackrest` | `barman-cloud` only |
| Provider | `s3`, `azure`, `gcs` | `s3` only |
| Scopes | discovery or a list | exactly one `BARMAN_SERVER_NAMES` entry |
| Credentials | file pair or ambient chain | explicit `STORE_CREDENTIAL_MODE`, no ambient fallback |
| `LISTEN_ADDR` / `TRUSTED_USER_HEADER` | accepted | rejected, even when explicitly empty |
| `ALLOW_DOWNLOAD=true` | frozen no-op | rejected |

Each of those restrictions fails **before** the socket is created.

## Minimal invocation

```bash
RUNTIME_MODE=pgconsole-sidecar \
REPOSITORY_FORMAT=barman-cloud \
PROVIDER=s3 \
DESTINATION_PATH=s3://example-backups/repository \
BARMAN_SERVER_NAMES=orders \
EVIDENCE_TOKEN_FILE=/var/run/secrets/objectstoreviewer/token \
CNPG_CLUSTER_NAMESPACE=database-team \
CNPG_CLUSTER_UID=2f12b7d1-7e8d-4c37-a68f-233efc5f3191 \
CNPG_CLUSTER_NAME=orders \
STORE_CREDENTIAL_MODE=static-files \
AWS_ACCESS_KEY_ID_FILE=/var/run/secrets/objectstoreviewer/aws-access-key-id \
AWS_SECRET_ACCESS_KEY_FILE=/var/run/secrets/objectstoreviewer/aws-secret-access-key \
AWS_REGION=eu-west-1 \
./bin/objectstoreviewer
```

The scanner and its lightweight reachability check are forcibly confined to
`orders/` beneath the destination even if the S3 credential can read a broader
prefix. API requests read only the immutable publication and perform no store
operation.

## Credential modes

`STORE_CREDENTIAL_MODE` is required and exact:

- **`static-files`** — exactly the existing access-key and secret-key file
  pair, with an optional session-token file. `AWS_REGION` is an optional S3
  coordinate.
- **`aws-web-identity`** — requires `AWS_WEB_IDENTITY_TOKEN_FILE` (absolute,
  clean path), `AWS_ROLE_ARN`, and `AWS_REGION`; it rejects the static files and
  constructs only the explicit token-file/role provider.

There is no default chain and no ambient fallback in this mode.

## Required filesystem profile

The socket directory and token file *are* the security boundary, and startup
refuses to proceed unless they match. The gates live in `internal/evidenceapi`
and are asserted by `make test-container`.

| Object | Requirement |
|---|---|
| socket directory | a real directory (never a symlink), **setgid**, at least `02770`; the process's effective or supplementary GID must equal the directory's group |
| socket | created by the viewer as `0660`, inheriting the directory's group; a symlink or non-socket at the path is refused; a stale socket is removed only when a dial confirms nothing is listening |
| token file | non-symlink regular file, mode exactly `0440` (never `0400`), no setuid/setgid/sticky bits, at most 128 bytes, one canonical 43-character unpadded-base64url token |

A conformant kubelet applying `fsGroup` to the socket `emptyDir` ORs those bits
onto the volume's initial `0777` and delivers an effective `02777`, which the
startup gate accepts. The surplus world bits are inert: **confinement comes
from the mount set, never from the file mode**.

Therefore:

- mount the socket `emptyDir` into **only** the console and viewer containers;
- mount the token volume read-only into the same two containers, using a
  `subPath` so the path is a real file rather than a projected symlink; and
- keep both volumes out of the auth-proxy container.

## Token handling

pgtoolbox generates the token once and stores it in an operator-owned Secret.
Both containers must start from the same token revision. Tokens are **not
rotated in place** — updating contents and relying on projected-volume refresh
is not supported; a new revision means a new Pod.

The token authenticates the caller (`Authorization: Bearer <token>`); the fixed
socket path authenticates the server boundary. Loopback TCP, cluster Services,
and non-local sockets are not accepted.

## Liveness probe

```bash
./bin/objectstoreviewer probe
```

It reads only `EVIDENCE_TOKEN_FILE`, calls authenticated `/healthz` over the
fixed socket with a two-second ceiling, and emits no token, response body, or
raw transport error.

:::caution
Configure this as the kubelet **liveness** command. It must not be used as a
`/readyz`-based Pod readiness gate.
:::

## HTTP surface

Over the socket, authenticated:

| Route | Purpose |
|---|---|
| `GET /healthz` | process liveness only |
| `GET /readyz` | configuration validity plus recent reachability |
| `GET /api/v1alpha1/snapshot` | the publication identity, capabilities, inventory summary, and typed details |
| `GET /api/v1alpha1/backups` | paginated Barman backup rows |
| `GET /api/v1alpha1/wal-ranges` | paginated compact WAL ranges |
| `GET /api/v1alpha1/wal-gaps` | paginated candidate/confirmed gaps |
| `GET /api/v1alpha1/recovery-paths` | paginated per-timeline recovery paths |

Collections are cursor-paginated and consistent within one publication. See the
[evidence API reference](../reference/evidence-api.md) and its [resource
reference](../reference/evidence-api-resources.md).

## Consumer configuration

The consumer side is part of the same contract, so the composing Kubernetes
operator maps one agreed set:

| Input | Requirement |
|---|---|
| `REPOSITORY_EVIDENCE_URL` | fixed `unix:///var/run/objectstoreviewer/evidence.sock`; loopback and any TCP address are rejected |
| `REPOSITORY_EVIDENCE_TOKEN_FILE` | absolute path to the **same** mounted pod-local token file |
| `REPOSITORY_EXPECTED_FINGERPRINT` | full `sha256:` destination fingerprint, computed by the composing operator through the types module's canonicalization |
| `REPOSITORY_BARMAN_SERVER` | exact case-sensitive Barman server name |

Validation is all-or-nothing: with repository evidence enabled, all four are
present or the consumer fails before listening. Provider, format, and scope kind
are implied by the profile but validated against the literal wire constants
`s3`, `barman-cloud`, and `barman-server`.

## Who owns what

| Owner | Responsibility |
|---|---|
| ObjectStoreViewer | provider construction and read-only adapters; scanning, budgets, cancellation, cached publication; Barman parsing; evidence validation and rollups; wire projection and schema versions |
| the consumer | Kubernetes and CNPG collection; cross-source identity checks and correlation; polling into its own immutable snapshot; per-user gating and all browser rendering |
| the composing operator | resolving the Cluster, ObjectStore, effective server name, destination, and credentials; injecting identity and configuration; mounting credentials **only** into the viewer and the Kubernetes API token **only** into the console; creating the socket and token volumes; enforcing Pod security and resource invariants |

Collector, publisher, engine, provider, and analyzer interfaces are
ObjectStoreViewer implementation details. A consumer must not import them.

## Correlating a CNPG backup

The producer never reads CNPG resources and asserts no correlation. It exports
repository identity and exact Barman backup IDs; the consumer may correlate a
CNPG `Backup` only when **all** of these hold:

1. the response cluster namespace and UID exactly match the target Cluster;
2. the fingerprint, provider, format, and server mapping supplied by the
   composing operator
   exactly match the response;
3. the CNPG `Backup` is in the same namespace and belongs to the target Cluster
   by typed CNPG fields and ownership — never by name similarity;
4. the method is the supported in-tree Barman object-store method or an accepted
   Barman Cloud Plugin configuration; and
5. `Backup.status.backupId` is non-empty and exactly equals one repository
   `backup_id` in that mapped server.

Zero matches means "repository counterpart not observed". More than one match
means "ambiguous". Neither is guessed or collapsed into agreement, and CNPG phase
stays separately attributed from repository state even when correlation succeeds.

:::danger Not substitute correlation keys
Cluster name, `Backup` object name, scheduled-backup name, Barman server name,
timestamps, and object prefixes. None of them identifies a backup.
:::

## Polling and failure behavior

Poll `/snapshot` in the background with bounded jitter, and skip collection
requests while `(revision, evidence_generation)` is unchanged. Conditional HTTP
responses are not part of `v1alpha1`. Browser requests should read only the
consumer's own cached projection and never reach the socket.

Sidecar absence, restart, stale evidence, throttling, invalid schema, or failed
correlation must produce an explicit unavailable/stale/unknown panel while every
other source keeps rendering. A failed viewer refresh never replaces complete
evidence; a failed consumer poll likewise never replaces its last completely
assembled snapshot. Both layers expose their own staleness and failure category.

:::caution
None of those failures may make the consumer's own `/readyz` fail. Repository
evidence is a source, not a dependency.
:::

## Security invariants

- S3 credentials and Kubernetes credentials never coexist in one container.
- The sidecar holds no credential valid against the Kubernetes API; the consumer
  holds no object-store credential.
- The API is read-only, with no mutation or restore operation.
- The API carries no raw provider errors, authorization headers, token values,
  signed URLs, destination URLs, object metadata, raw catalog or history content,
  or backup bytes.
- Every string is bounded and validated before publication, and every response is
  deterministic for one immutable publication.
- Logs record request ID, route, status, duration, revision, and a stable error
  code — never headers, cursors, identities, backup IDs, scope names, or bodies.
- The auth proxy cannot access the socket or token volumes, and an injected
  container without those mounts cannot call the API just because it shares Pod
  loopback.

## Verification

```bash
make test-stress      # repeated channel, publication, runtime, and probe cases
make test-container   # standalone and sidecar restricted-runtime profiles
```

`make test-container` exercises the real image with ordinary
and OpenShift-style distinct arbitrary UIDs sharing only a supplementary
`fsGroup`; the `02770` directory floor with `0660`/`0440` socket and token
modes; positive authorized access; unmounted, socket-only, and token-only
denials; the absence of a TCP listener; and graceful socket cleanup. It is a
producer-side Docker check — the admitted-Pod, `subPath` Secret mount,
volume-isolation, and arbitrary-UID socket checks live in pgtoolbox's readiness
record from the 2026-07-29 live run.
