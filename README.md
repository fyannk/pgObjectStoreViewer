# ObjectStoreViewer

ObjectStoreViewer is a read-only web application for inspecting PostgreSQL
backup repositories in object storage. The architecture supports Barman Cloud
and pgBackRest as separate repository formats over provider-neutral S3, Azure,
and GCS read adapters.

The current production runtime provides Barman inventory, provider-neutral
rendering over S3, Azure Blob Storage, and GCS, Barman timeline ancestry,
backup consistency ranges, and conservative observed recovery coverage. It also
provides an explicit
`pgconsole-sidecar` producer mode that publishes the same immutable evidence
over the private authenticated Unix socket and exercises its producer-owned
container filesystem profiles. It is executable for integration work, but no
pgConsole/ObjectStoreViewer runtime pair is qualified or advertised as
supported yet.
It scans one configured S3, Azure Blob Storage, or GCS repository root in the background and
atomically publishes a bounded inventory to the browser. The page shows exact
object and stored-byte totals only after a complete scan, validated Barman
server or pgBackRest stanza scopes, a bounded recent-object list, and refresh
freshness. It does not yet interpret pgBackRest metadata or dependency chains.
For Barman Cloud it reads
bounded `backup.info` metadata and reports completed, in-progress, failed,
malformed, unsupported, and structurally incomplete backups separately. A
structurally usable row means supported terminal metadata plus stat-able backup
data artifacts; it is not proof that a restore will succeed. Barman WAL objects
are classified into compact segment ranges, candidate/confirmed gaps, partial,
history, backup-history, duplicate, and unknown evidence using the PostgreSQL
version and segment size from backup metadata. Bounded timeline history then
connects structurally usable backup anchors to per-timeline recovery paths.
Each path stops conservatively at missing ancestry, unknown evidence, a
candidate gap, a confirmed gap, or the current observed archive frontier.

ObjectStoreViewer reports structural evidence. It does not prove that a restore
will succeed.

## Run an inventory

```bash
make build
REPOSITORY_FORMAT=barman-cloud \
PROVIDER=s3 \
DESTINATION_PATH=s3://example-backups/repository \
AWS_REGION=eu-west-1 \
./bin/objectstoreviewer
```

For MinIO, also set `ENDPOINT_URL`, for example
`http://minio.storage.svc:9000`. Static S3 credentials must be mounted as files
and selected with `AWS_ACCESS_KEY_ID_FILE` and `AWS_SECRET_ACCESS_KEY_FILE`;
workload identity remains the preferred ambient mode.

Open `http://localhost:3000/`. `/healthz` returns process liveness. `/readyz`
returns ready after a recent lightweight, prefix-scoped list succeeds. An empty
prefix is reachable and therefore ready; it produces a complete inventory with
zero totals. The same cached inventory and Barman catalog semantics apply to
S3, Azure, and GCS; provider errors are normalized to the same redacted states.
The cached `/wals` page filters compact Barman WAL evidence by server, class,
timeline, and WAL-name range. Neither page performs provider I/O.

## Run the pgConsole sidecar producer

`RUNTIME_MODE=pgconsole-sidecar` selects the distinct producer surface. This
initial profile accepts exactly one Barman Cloud server over S3, creates no TCP
listener or browser routes, and serves only the authenticated evidence API at
`/var/run/objectstoreviewer/evidence.sock`. The socket directory and immutable
token file must follow the filesystem profile enforced by
`internal/evidenceapi` and documented in the
[sidecar guide](web/docs/operations/sidecar.md).

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
`orders/` beneath the configured destination, even if the S3 credential can
read a broader prefix. API requests read only the immutable publication and
perform no store operation. The kubelet liveness command is:

```bash
./bin/objectstoreviewer probe
```

It reads only `EVIDENCE_TOKEN_FILE`, calls authenticated `/healthz` over the
fixed socket with a two-second ceiling, and emits no token, response body, or
raw transport error. It must not be configured as a `/readyz`-based Pod
readiness gate. The composed Pod and channel mechanics were proven live on
OpenShift 4.20 under `restricted-v2` on 2026-07-29; the live cross-check
demonstration, pgtoolbox's published per-profile resource sizing, and the
first complete qualification profile remain release gates, so this producer
mode alone is not yet a supported cross-project deployment.

`make test-container` exercises the real image with ordinary
and OpenShift-style distinct arbitrary UIDs sharing only a supplementary
`fsGroup`; the `02770` socket-directory floor (pinned exactly by the harness;
conformant kubelets deliver an effective `02777`, which the startup gate
accepts) with `0660`/`0440` socket and token modes; positive authorized
access; unmounted, socket-only, and token-only denials; no TCP listener; and
graceful socket cleanup. It is a producer-side Docker test; the admitted-Pod,
`subPath` Secret mount, volume-isolation, and arbitrary-UID socket checks are
recorded in pgtoolbox's readiness record from the 2026-07-29 live run.

## Standalone evidence semantics and deployment

The dashboard labels recovery results as observed structural coverage. Its
lower time bound comes from backup completion metadata and its frontier time is
the provider receipt/modification time of the latest contiguous archived WAL,
not transaction time or an exact PITR endpoint. It never proves restore
success.

The Barman catalog retains only allowlisted metadata facts: status/type, WAL
anchors, timestamps, sizes, compression, encryption, and structural-artifact
evidence. Unknown metadata fields and their values are discarded. Snapshot and
incremental structures, malformed metadata, missing artifacts, and artifact
confirmation failures remain unknown or unhealthy as appropriate; none become
recovery anchors. Logical, deduplicated, and stored artifact bytes are separate
values and must not be compared as interchangeable totals.

The application has no authentication or TLS. Its port must be reachable only
through an authenticated proxy and an operator-managed network boundary.
`TRUSTED_USER_HEADER` is display-only and never authorizes a request.

For pgtoolbox, deploy the viewer behind the operator's authentication proxy as
follows:

1. expose only the proxy through an Ingress or external Service;
2. let the proxy reach the internal `objectstoreviewer` Service on port 3000;
3. strip any client-supplied identity header and set `X-Forwarded-User` only
   after authentication;
4. label the proxy pod `app.kubernetes.io/component: auth-proxy` so the example
   NetworkPolicy admits it; and
5. do not grant users or other namespaces direct access to the viewer Service.

[`deploy/kubernetes-example.yaml`](deploy/kubernetes-example.yaml) includes the
internal Service and ingress NetworkPolicy. Authentication and TLS remain the
proxy/operator's responsibility; the viewer never treats the displayed header
as authorization.

## Frozen single-repository configuration

Invalid or ambiguous combinations fail before the application listens. Values
and secret-file paths are not included in startup errors.

| Variable | Default | Validation and meaning |
|---|---:|---|
| `RUNTIME_MODE` | standalone | Empty selects the existing standalone HTTP/UI runtime; exact `pgconsole-sidecar` selects the private producer profile below |
| `REPOSITORY_FORMAT` | required | Exactly `barman-cloud` or `pgbackrest`; no detection or fallback |
| `PROVIDER` | required | Exactly `s3`, `azure`, or `gcs` |
| `DESTINATION_PATH` | required | Credential-free `s3://bucket/prefix`, `azure://container/prefix`, or `gs://bucket/prefix`, matching `PROVIDER` |
| `ENDPOINT_URL` | empty | Credential-free HTTP(S) S3 endpoint override; rejected for other providers |
| `ENDPOINT_CA_FILE` | empty | S3-only custom CA, at most 1 MiB and containing at least one PEM certificate; rejected for Azure/GCS |
| `BARMAN_SERVER_NAMES` | discovery | Barman-only comma-separated scope names |
| `PGBACKREST_STANZAS` | discovery | pgBackRest-only comma-separated scope names |
| `PGBACKREST_CIPHER_PASS_FILE` | empty | pgBackRest-only mounted passphrase, at most 64 KiB |
| `LISTEN_ADDR` | `:3000` | Plain HTTP `host:port` |
| `TRUSTED_USER_HEADER` | `X-Forwarded-User` | Display-only HTTP field name; explicit empty value disables display |
| `ALLOW_DOWNLOAD` | `false` | Frozen opt-in flag; no download route ships before Slice 10 |
| `CATALOG_REFRESH_INTERVAL` | `5m` | 30 seconds through 24 hours |
| `STORE_REQUEST_TIMEOUT` | `10s` | 1 second through 5 minutes |
| `SCAN_CONCURRENCY` | `4` | 1 through 64 provider operations |
| `MAX_OBJECTS_PER_SCAN` | `1000000` | 1,000 through 10,000,000; a continuation beyond it makes the scan incomplete/unknown |
| `WAL_PAGE_SIZE` | `200` | 1 through 1,000 rows |
| `EXPECTED_RETENTION_POLICY` | empty | Frozen bounded string; shown as configured but remains unknown until format-owned policy interpretation in Slice 8 |
| `EXPECTED_MINIMUM_REDUNDANCY` | empty | Optional integer from 0 through 100,000; Barman compares it with the visible structurally usable backup count as a sanity signal |

Scope names are trimmed, deduplicated, and byte-sorted. They may not be empty,
`.`/`..`, contain path separators or control characters, or exceed 128 bytes.
An empty applicable list delegates discovery to the selected format. Setting a
scope or cipher variable for the other format is an error.

### Credential precedence

Static secret values are accepted only through mounted files. Files are read
with a size ceiling and trailing CR/LF removed; contents are held in an opaque
redacted type.

- S3 uses `AWS_ACCESS_KEY_ID_FILE` and `AWS_SECRET_ACCESS_KEY_FILE` together,
  with optional `AWS_SESSION_TOKEN_FILE` and non-secret `AWS_REGION`. Without
  the pair, the adapter uses the AWS SDK workload-identity chain. Direct
  `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` values
  are rejected.
- Azure uses `AZURE_STORAGE_CONNECTION_STRING_FILE`; or
  `AZURE_STORAGE_ACCOUNT` with exactly one of
  `AZURE_STORAGE_ACCOUNT_KEY_FILE` and `AZURE_STORAGE_SAS_TOKEN_FILE`; or
  `AZURE_STORAGE_ACCOUNT` alone with workload identity through
  `DefaultAzureCredential`. The account identifies the Blob endpoint.
- GCS uses `GOOGLE_APPLICATION_CREDENTIALS` as a mounted, valid JSON file, or
  Application Default Credentials/workload identity when it is absent.

Credential variables for a provider other than `PROVIDER` are rejected.

### Sidecar-only configuration

The following inputs are accepted only with
`RUNTIME_MODE=pgconsole-sidecar`. In that mode `LISTEN_ADDR` and
`TRUSTED_USER_HEADER` are rejected even when explicitly empty, and
`ALLOW_DOWNLOAD=true` is rejected.

| Variable | Requirement |
|---|---|
| `EVIDENCE_TOKEN_FILE` | Required absolute, clean path; startup accepts only a non-symlink regular `0440` file containing one canonical 32-byte unpadded-base64url token |
| `CNPG_CLUSTER_NAMESPACE` | Required bounded namespace identity |
| `CNPG_CLUSTER_UID` | Required bounded immutable Cluster UID |
| `CNPG_CLUSTER_NAME` | Optional bounded display name |
| `STORE_CREDENTIAL_MODE` | Required exact `static-files` or `aws-web-identity`; no ambient/default-chain fallback |
| `AWS_WEB_IDENTITY_TOKEN_FILE` | Required absolute, clean path in `aws-web-identity` mode |
| `AWS_ROLE_ARN` | Required bounded role ARN in `aws-web-identity` mode |
| `AWS_REGION` | Required in `aws-web-identity` mode; optional existing S3 coordinate in `static-files` mode |

`static-files` requires exactly the existing access-key and secret-key file
pair, with an optional session-token file. `aws-web-identity` rejects those
static files and constructs only the explicit token-file/role provider. Other
sidecar profile restrictions—Barman Cloud, S3, and exactly one configured
Barman server—fail before the socket is created.

For Kubernetes workload identity, set only the non-secret provider coordinates:

```yaml
# Azure workload identity
- name: PROVIDER
  value: azure
- name: DESTINATION_PATH
  value: azure://backup-container/repository
- name: AZURE_STORAGE_ACCOUNT
  value: exampleaccount

# GKE workload identity / GCS ADC
- name: PROVIDER
  value: gcs
- name: DESTINATION_PATH
  value: gs://example-backups/repository
```

For mounted static credentials, point the applicable file variable at the
read-only Secret volume, for example
`AZURE_STORAGE_SAS_TOKEN_FILE=/credentials/azure-sas` or
`GOOGLE_APPLICATION_CREDENTIALS=/credentials/gcs.json`. Do not place the
credential value itself in an environment variable. Bind the identities using
the read-only templates under [`deploy/policies`](deploy/policies/).

## Runtime security defaults

The HTTP server uses a 5-second read-header timeout, 15-second read timeout,
30-second write timeout, 60-second idle timeout, 16 KiB maximum headers, and a
15-second graceful-shutdown deadline. Sensitive responses are non-cacheable
and use restrictive CSP, referrer, framing, and content-type headers. Request
logs use stable route names and never raw URLs, queries, identity headers, or
authorization headers.

The initial deployment budget is a 25m CPU/64 MiB request and 500m CPU/256 MiB
limit, with a 16 MiB `/tmp` volume. `make test-scale` validates the memory limit
and records current scan/render measurements. See
[`deploy/kubernetes-example.yaml`](deploy/kubernetes-example.yaml) and the
prefix-scoped S3 policy template in
[`deploy/policies/s3-read-only.json`](deploy/policies/s3-read-only.json).

The deployment credential must independently allow only provider list/get
operations. The application has no mutation operation in its domain or any
provider adapter. See the S3, Azure, and GCS policy templates under
[`deploy/policies`](deploy/policies/).

## Build and verification

```bash
make build
make test
make test-race
make test-stress
make check-api
make test-integration
make test-scale
make test-container
make lint
make vuln
make check
make package
make docs
make docker-build
make release-check
```

The [verification reference](web/docs/reference/checks.md) explains each layer
and the required pathological fixtures. `make check` is the complete local
non-Docker gate. `make test-integration` generates a genuine Barman 3.19.1
fixture and runs the shared Barman journey over MinIO, Azurite, and
fake-gcs-server, requiring byte-identical normalized output. `make
release-check` adds scale, restricted amd64/arm64 container profiles, an SPDX
SBOM, image digest, license report, and high/critical vulnerability gate.

Docker, privileged binfmt setup for local arm64 emulation, and network access to
the pinned test images and the vulnerability database are required.
`make lint` runs gofmt, vet, the license-boilerplate and read-only scans, the
read-store surface guard, and golangci-lint with gosec enforced across both
modules. `make vuln` runs the official
[Go vulnerability checker](https://pkg.go.dev/golang.org/x/vuln@v1.6.0/cmd/govulncheck)
pinned to v1.6.0.

The image is built with Go 1.26.5 as a statically linked Go binary in a
distroless non-root runtime.
The container test overrides it to UID/GID 12345, enables a read-only root,
mounts only `/tmp`, drops all capabilities, sets no-new-privileges, checks
health/readiness semantics, and sends SIGTERM.

## Documentation

The [documentation site](web/) is the reader-facing documentation: overview and
the evidence model, operations guides (install, configuration, security,
sidecar, troubleshooting, upgrade), architecture, the configuration/HTTP/evidence-API
reference, limits, compatibility, verification, tutorials, decisions, and the roadmap.

```bash
make docs                      # build it (fails on broken links)
cd web && npm ci && npm start  # develop it
```

Pushing to `main` publishes it to GitHub Pages.

**The code is the truth.** The site explains what the code does; it is not a
specification. Where the two disagree, the code is right and the page is a bug.
Contributor rules live in [`AGENTS.md`](AGENTS.md).

## Contributing

Build commands, verification layers, the repository invariants, and the layout are
in [`CONTRIBUTING.md`](CONTRIBUTING.md). The complete normative rules for human
and automated contributors are in [`AGENTS.md`](AGENTS.md).

## Architecture

The code is the authority on behavior; read it and the tests beside it.
`internal/config` defines what a valid deployment is, `internal/store` plus a
provider adapter defines the read boundary, `internal/formats/barmancloud` owns
the semantics, `internal/inventory` owns scanning and atomic publication, and
`internal/web` and `internal/evidenceapi` turn a snapshot into output.

The dependency-light [`api` module](api) carries the
`evidence.objectstoreviewer.io/v1alpha1` wire vocabulary, its generated schema,
and deterministic wire goldens; it has zero external module dependencies so
consumers do not inherit a cloud SDK. The producer/consumer boundary it serves
remains unqualified across projects: no compatible pgConsole/ObjectStoreViewer
pair is advertised until the first complete qualification profile passes.

The reasoning behind these structures — why one application for many formats, why
a sidecar rather than a library, why a Unix socket, why gap confirmation is not
persisted — is recorded in
[web/docs/architecture/decisions.md](web/docs/architecture/decisions.md).

Licensed under Apache-2.0. See [`LICENSE`](LICENSE).
