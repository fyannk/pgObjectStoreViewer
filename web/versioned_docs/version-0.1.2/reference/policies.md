---
sidebar_position: 7
---

# Read-only policy templates

The deployment credential must **independently** deny writes. The application
has no mutation operation in its domain or any provider adapter, but the
credential is the layer that survives a bug.

Templates live in
[`deploy/policies`](https://github.com/fyannk/pgObjectStoreViewer/tree/v0.1.2/deploy/policies)
and are guarded by `hack/check-readonly.sh`, which fails the build if a
forbidden action ever appears in them.

| File | Grants |
|---|---|
| `s3-read-only.json` | `s3:ListBucket` and `s3:GetObject`, prefix-scoped |
| `azure-read-only-role.json` | blob read/list data actions only |
| `gcs-read-only-role.json` | `storage.objects.get` and `storage.objects.list` only |

Scope the resource ARNs, scopes, and conditions to the exact destination prefix
before applying them.

## What the application needs

| Operation | Why |
|---|---|
| list, prefix-scoped and paginated | inventory, scope discovery, WAL classification |
| bounded get/open | small metadata objects: `backup.info`, timeline history, pgBackRest info/manifests |
| stat/head | artifact existence and allowlisted metadata (size, modification time, opaque ETag) |

Nothing else. In particular it never needs write, create, upload, copy, delete,
multipart, lifecycle, tagging-mutation, bucket-creation, or restore permission.

## S3

Two statements are required, and the split matters:

- **`s3:ListBucket` on the bucket ARN**, with a
  `s3:prefix` condition limiting it to the destination prefix; and
- **`s3:GetObject` on the object ARNs** under that prefix.

A policy that grants only `GetObject` produces a viewer that never becomes
ready — listing is what readiness probes. Denied actions to keep out: any
`s3:Put*`, `s3:Delete*`, `s3:Create*`, `s3:Replicate*`, `s3:Restore*`, and
`s3:AbortMultipart*`.

For MinIO, the equivalent read-only policy used by the integration tests is
`internal/provider/s3/testdata/minio-readonly-policy.json`.

## Azure

Grant a custom role with blob **read** and **list** data actions on the
container or a narrower scope. Never grant
`.../containers/blobs/write`, `.../delete`, or `.../add/action`.

If you use a SAS token instead of a role, issue it read+list only, scoped to the
container and prefix, and mount it with `AZURE_STORAGE_SAS_TOKEN_FILE`.

## GCS

Grant a custom role with exactly `storage.objects.get` and
`storage.objects.list`, bound at the bucket (or a prefix-conditioned) scope.
Never grant `storage.objects.create`, `.delete`, `.update`, `.restore`, or
`.setIamPolicy`.

## Verifying the boundary

The integration tests run against pinned local emulators using read-only credentials, so a
regression that starts requiring a write permission fails there first:

```bash
make test-s3      # MinIO with the read-only policy applied
make test-azure   # Azurite
make test-gcs     # fake-gcs-server
make lint          # includes the read-only source and policy checks
```
