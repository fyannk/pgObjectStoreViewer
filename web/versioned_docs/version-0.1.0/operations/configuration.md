---
sidebar_position: 2
---

# Configuration

The single-repository configuration contract was **frozen** by the Slice 0
decision gate on 2026-07-27. Invalid or ambiguous combinations fail before the
process listens, and startup errors name a variable and a reason but never a
value or a secret-file path.

The complete variable table lives in the
[configuration reference](../reference/configuration-variables.md). This page
explains the parts that trip people up.

## One repository, explicitly named

```bash
REPOSITORY_FORMAT=barman-cloud    # or pgbackrest — no detection, no fallback
PROVIDER=s3                       # or azure, gcs
DESTINATION_PATH=s3://example-backups/repository
```

`DESTINATION_PATH` must be credential-free and must match `PROVIDER`:
`s3://bucket/prefix`, `azure://container/prefix`, or `gs://bucket/prefix`. A
scheme that disagrees with `PROVIDER` is a startup error, not a coercion.

Multiple stores and multiple formats in one process are a Slice 11 feature and
do not exist yet.

## Scopes

`BARMAN_SERVER_NAMES` (Barman only) and `PGBACKREST_STANZAS` (pgBackRest only)
restrict the scan to named scopes. Names are trimmed, deduplicated, and
byte-sorted. A name may not be empty, `.`/`..`, contain a path separator or
control character, or exceed 128 bytes.

An empty list delegates discovery to the format. Setting the *other* format's
scope or cipher variable is an error — the process will not quietly ignore it.

## Provider credentials

Static secret values are accepted **only through mounted files**. Files are
read with a size ceiling, trailing CR/LF is stripped, and contents are held in
an opaque redacted type that cannot be printed or logged.

### S3

```bash
AWS_ACCESS_KEY_ID_FILE=/credentials/access-key-id
AWS_SECRET_ACCESS_KEY_FILE=/credentials/secret-access-key
# optional
AWS_SESSION_TOKEN_FILE=/credentials/session-token
AWS_REGION=eu-west-1
```

Both files are required together; a session-token file without the pair is an
error. Without the pair, the adapter uses the AWS SDK workload-identity chain —
the preferred mode.

:::danger Direct credential variables are rejected
`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` as
*values* fail startup. Use the `*_FILE` forms or workload identity.
:::

### Azure

Exactly one of:

- `AZURE_STORAGE_CONNECTION_STRING_FILE`;
- `AZURE_STORAGE_ACCOUNT` **plus** exactly one of
  `AZURE_STORAGE_ACCOUNT_KEY_FILE` / `AZURE_STORAGE_SAS_TOKEN_FILE`; or
- `AZURE_STORAGE_ACCOUNT` alone, using workload identity through
  `DefaultAzureCredential`.

The account name identifies the Blob endpoint.

### GCS

`GOOGLE_APPLICATION_CREDENTIALS` pointing at a mounted, valid JSON file — or
nothing at all, which selects Application Default Credentials / workload
identity.

### Workload identity in Kubernetes

Set only the non-secret coordinates:

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

Credential variables belonging to a provider other than `PROVIDER` are
rejected.

## S3-compatible endpoints

```bash
ENDPOINT_URL=http://minio.storage.svc:9000    # credential-free origin, S3 only
ENDPOINT_CA_FILE=/etc/ssl/minio/ca.pem        # S3 only, ≤ 1 MiB, ≥ 1 PEM cert
```

Both are rejected for Azure and GCS. `ENDPOINT_URL` must be an origin: no path,
no userinfo, no query.

## Scan and rendering budgets

| Variable | Default | Range |
|---|---:|---|
| `CATALOG_REFRESH_INTERVAL` | `5m` | 30 s – 24 h |
| `STORE_REQUEST_TIMEOUT` | `10s` | 1 s – 5 min |
| `SCAN_CONCURRENCY` | `4` | 1 – 64 |
| `MAX_OBJECTS_PER_SCAN` | `1000000` | 1,000 – 10,000,000 |
| `WAL_PAGE_SIZE` | `200` | 1 – 1,000 |

`MAX_OBJECTS_PER_SCAN` is a safety ceiling, not a target: a continuation beyond
it makes the scan incomplete, and totals become unknown rather than wrong.
Raising it raises the memory the scan can hold — check it against the pod's
memory limit.

## Expectations

```bash
EXPECTED_RETENTION_POLICY="RECOVERY WINDOW OF 7 DAYS"
EXPECTED_MINIMUM_REDUNDANCY=3
```

`EXPECTED_RETENTION_POLICY` is a frozen bounded string, displayed as configured;
format-owned policy interpretation arrives in Slice 8, so the derived status
stays unknown until then. `EXPECTED_MINIMUM_REDUNDANCY` (0 – 100,000) is
compared with the visible structurally usable backup count as a sanity signal.

## Display identity

`TRUSTED_USER_HEADER` (default `X-Forwarded-User`) names the HTTP field whose
value is displayed. Setting it to an explicit empty value disables the display.
It never authorizes a request.

## Frozen flags

`ALLOW_DOWNLOAD` exists and defaults to `false`. No download route ships before
Slice 10, so setting it `true` currently changes nothing in standalone mode and
is rejected outright in sidecar mode.

## Sidecar-only variables

`RUNTIME_MODE=pgconsole-sidecar` unlocks `EVIDENCE_TOKEN_FILE`,
`CNPG_CLUSTER_NAMESPACE`, `CNPG_CLUSTER_UID`, `CNPG_CLUSTER_NAME`,
`STORE_CREDENTIAL_MODE`, `AWS_WEB_IDENTITY_TOKEN_FILE`, and `AWS_ROLE_ARN` —
and rejects `LISTEN_ADDR`, `TRUSTED_USER_HEADER`, and `ALLOW_DOWNLOAD=true`.
Setting any sidecar variable in standalone mode is an error. See
[the pgConsole sidecar](sidecar.md).
