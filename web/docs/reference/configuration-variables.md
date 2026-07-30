---
sidebar_position: 1
---

# Configuration variables

The complete frozen contract. Invalid or ambiguous combinations fail before the
process listens; values and secret-file paths never appear in startup errors.

## Common

| Variable | Default | Validation and meaning |
|---|---:|---|
| `RUNTIME_MODE` | standalone | Empty selects the standalone HTTP/UI runtime; exact `pgconsole-sidecar` selects the private producer profile |
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
| `EXPECTED_RETENTION_POLICY` | empty | Frozen bounded string (≤ 256 characters, no control characters); shown as configured but unknown until Slice 8 |
| `EXPECTED_MINIMUM_REDUNDANCY` | empty | Optional integer 0 through 100,000; compared with the visible structurally usable backup count |

### Scope names

Trimmed, deduplicated, and byte-sorted. A name may not be empty, `.`/`..`,
contain path separators or control characters, or exceed 128 bytes. An empty
applicable list delegates discovery to the selected format. Setting a scope or
cipher variable belonging to the other format is an error.

## S3 credentials

| Variable | Meaning |
|---|---|
| `AWS_ACCESS_KEY_ID_FILE` | Mounted access key; required together with the secret-key file |
| `AWS_SECRET_ACCESS_KEY_FILE` | Mounted secret key; required together with the access-key file |
| `AWS_SESSION_TOKEN_FILE` | Optional; requires the pair above |
| `AWS_REGION` | Non-secret region, at most 128 characters without control characters |

Without the file pair, the adapter uses the AWS SDK workload-identity chain.
Direct `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN`
values are **rejected**.

## Azure credentials

| Variable | Meaning |
|---|---|
| `AZURE_STORAGE_CONNECTION_STRING_FILE` | Mounted connection string |
| `AZURE_STORAGE_ACCOUNT` | Account name, at most 256 characters; identifies the Blob endpoint |
| `AZURE_STORAGE_ACCOUNT_KEY_FILE` | Mounted account key; exclusive with the SAS file |
| `AZURE_STORAGE_SAS_TOKEN_FILE` | Mounted SAS token; exclusive with the key file |

Accepted combinations: the connection-string file; or the account plus exactly
one of key/SAS file; or the account alone with workload identity through
`DefaultAzureCredential`.

## GCS credentials

| Variable | Meaning |
|---|---|
| `GOOGLE_APPLICATION_CREDENTIALS` | Mounted, valid JSON service-account file; absent selects Application Default Credentials / workload identity |

Credential variables for a provider other than `PROVIDER` are rejected.

## Sidecar-only

Accepted only with `RUNTIME_MODE=pgconsole-sidecar`. In that mode `LISTEN_ADDR`
and `TRUSTED_USER_HEADER` are rejected even when explicitly empty, and
`ALLOW_DOWNLOAD=true` is rejected.

| Variable | Requirement |
|---|---|
| `EVIDENCE_TOKEN_FILE` | Required absolute, clean path; startup accepts only a non-symlink regular `0440` file containing one canonical 32-byte unpadded-base64url token (43 characters, at most 128 bytes of file) |
| `CNPG_CLUSTER_NAMESPACE` | Required bounded namespace identity (≤ 63 bytes) |
| `CNPG_CLUSTER_UID` | Required bounded immutable Cluster UID (≤ 128 bytes) |
| `CNPG_CLUSTER_NAME` | Optional bounded display name (≤ 253 bytes) |
| `STORE_CREDENTIAL_MODE` | Required exact `static-files` or `aws-web-identity`; no ambient/default-chain fallback |
| `AWS_WEB_IDENTITY_TOKEN_FILE` | Required absolute, clean path in `aws-web-identity` mode |
| `AWS_ROLE_ARN` | Required bounded role ARN in `aws-web-identity` mode |
| `AWS_REGION` | Required in `aws-web-identity` mode; optional S3 coordinate in `static-files` mode |

`static-files` requires exactly the access-key/secret-key file pair with an
optional session-token file. `aws-web-identity` rejects those static files and
constructs only the explicit token-file/role provider. The remaining profile
restrictions — Barman Cloud, S3, and exactly one configured Barman server —
fail before the socket is created.

## Startup error format

```text
configuration invalid: <VARIABLE>: <reason>
```

The reason is one of a fixed set — `must be barman-cloud or pgbackrest`,
`must be a credential-free object-store URI`, `contains an invalid scope name`,
`requires pgconsole-sidecar`, `must be between …`, and so on. See
[troubleshooting](../operations/troubleshooting.md#the-process-exits-at-startup)
for the mapping to fixes.
