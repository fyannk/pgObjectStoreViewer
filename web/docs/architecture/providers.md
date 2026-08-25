---
sidebar_position: 2
---

# Provider adapters

An adapter's whole job is to make three clouds indistinguishable to the domain,
and to make sure nothing sensitive crosses the boundary while doing it.

## The narrow read surface

The domain sees only:

- **list** — paginated, prefix-scoped, with an opaque cursor;
- **get/open** — bounded reads of small metadata objects; and
- **stat/head** — existence plus allowlisted metadata (size, modification time,
  an opaque ETag).

There is no write, create, upload, copy, delete, multipart, lifecycle,
tagging-mutation, bucket-creation, or restore operation anywhere — not
unexported, not behind a flag.
`hack/check-readonly.sh` fails the build if a mutation-shaped identifier appears
in `cmd/`, `internal/`, or `api/`, and `make lint` runs a dedicated
`TestReaderSurface` guard over the interface itself.

## Supported providers

| Provider | `DESTINATION_PATH` | Credential modes | Endpoint override |
|---|---|---|---|
| S3 | `s3://bucket/prefix` | mounted static key pair (+ optional session token), or the SDK workload-identity chain; in sidecar mode an explicit web-identity token/role | `ENDPOINT_URL` (+ `ENDPOINT_CA_FILE`) for MinIO and other S3-compatible stores |
| Azure Blob Storage | `azure://container/prefix` | mounted connection string; or account + exactly one of key/SAS file; or account alone with `DefaultAzureCredential` | not applicable |
| GCS | `gs://bucket/prefix` | mounted service-account JSON, or Application Default Credentials | not applicable |

## Error normalization

Raw SDK errors never leave the adapter. Each one is mapped into the small,
stable `fault` vocabulary shared by logs, HTTP output, and the evidence API:

`unknown`, `canceled`, `timeout`, `invalid_configuration`, `authentication`,
`authorization`, `throttled`, `unavailable`, `not_found`,
`incompatible_format`, `safety_limit`.

This is why a wrong region reads as `unavailable` and a missing policy statement
reads as `authorization` — the adapter refuses to forward a message that might
carry a signed URL, an SAS parameter, or an authorization header. Each adapter
has a dedicated test proving it (`TestS3ContractMapsErrorsWithoutDisclosingSDKDetails`,
`TestSafeErrorRedacts` for Azure and GCS).

## Cursors

Pagination state is an opaque cursor produced by `internal/provider/cursor`. A
cursor is process-local, bounded, and cannot escape its configured store,
destination prefix, and scope. Keys are opaque: they are never mapped onto local
filesystem paths, and path cleaning is never used as an authorization boundary.

## Parity is proven, not assumed

All three adapters implement one shared contract test suite
(`internal/store/storetest`), and a shared six-state Barman journey
(`internal/provider/providertest`) runs against pinned MinIO, Azurite, and
fake-gcs-server images:

```bash
make test-s3        # pinned MinIO
make test-azure     # pinned Azurite
make test-gcs       # pinned fake-gcs-server
make test-provider-parity
```

`make test-provider-parity` requires **byte-identical normalized semantic output** across
the three providers for the same repository. Any place where a provider's
behavior would leak into a conclusion shows up there as a diff.

## Adding a provider

A new adapter must: implement the read surface and pass
`internal/store/storetest`; map every error it can produce into `fault` without
forwarding a message; accept credentials only as mounted files or an ambient
workload identity; keep its SDK types inside the package; produce bounded,
cancelable operations; and join the shared Barman journey with a pinned local
emulator so parity stays provable.
