---
sidebar_position: 9
---

# Compatibility and authorities

What ObjectStoreViewer has actually been checked against, and which upstream
source decides each question.

## Format authorities

| Question | Authority |
|---|---|
| Barman Cloud layout, metadata, and compression | Barman source, documentation, and pinned generated repositories |
| WAL naming and segment arithmetic | PostgreSQL source |
| pgBackRest layout, info files, and reference chains | pgBackRest source, release policy, its documented stable JSON output, and pinned generated repositories |
| CNPG object-store topology | CloudNativePG and the Barman Cloud Plugin documentation |

The format behavior is **reimplemented in Go**. Implementation code is not
copied from Barman or pgBackRest, which keeps this project's Apache-2.0
licensing intact.

## Verified versions

| Component | Checked against |
|---|---|
| Barman | 3.19.1 — a genuine `barman-cloud-backup` and WAL archive journey runs inside `make test-integration` |
| PostgreSQL WAL grammar | 18 (`src/include/access/xlog_internal.h`): 24-hex-digit names, 1 MiB – 1 GiB power-of-two segments, `2^32 / wal_segment_size` segments per log ID, `.partial`, timeline `.history`, backup-history names |
| Barman WAL grammar and hash directory | 3.19.1 (`barman/xlog.py`) |
| Barman compression suffixes | 3.19.1 (`barman/compression.py`): `.gz`, `.bz2`, `.xz`, `.snappy`, `.zst`, `.lz4` |
| Go toolchain | 1.27.0 |
| Object stores | S3/MinIO, Azure/Azurite, GCS/fake-gcs-server — all pinned by digest |
| Architectures | `linux/amd64`, `linux/arm64` |
| Restricted runtime | distroless non-root; arbitrary UID/GID, read-only root, dropped capabilities, no-new-privileges |
| OpenShift | 4.20 under `restricted-v2` (live sidecar Pod and channel mechanics, 2026-07-29) |

Arithmetic detail worth knowing: lowercase hexadecimal WAL input is accepted, as
Barman does, while rendered range boundaries are normalized to uppercase.

## Compression support

Barman Cloud distinguishes base-backup from WAL compression, and so does the
viewer:

| Object | Supported compression |
|---|---|
| base backup | uncompressed, gzip, bzip2, snappy, lz4 |
| WAL | uncompressed, gzip, bzip2, snappy, lz4, xz, zstd |

A suffix is normalized **only after** the underlying WAL/history name and its
hash-directory placement validate. Anything else is bounded `unknown`
diagnostic evidence, not a guess.

## CloudNativePG basis

Checked against the Barman Cloud Plugin 0.13.0 documentation and Barman 3.19:

- the plugin requires CloudNativePG 1.26 or later and recommends 1.27 or later;
- it runs the Barman Cloud client in a database-pod sidecar; and
- base backups are tarballs under the Barman Cloud `base` layout.

Both CNPG integrations are in scope: the legacy in-tree `barmanObjectStore`
configuration and the Barman Cloud Plugin's `ObjectStore` CRD in API group
`barmancloud.cnpg.io`.

:::note The plugin keeps `ObjectStore.serverName` empty
A server name can instead be selected through Cluster plugin parameters, so an
Kubernetes operator composing this viewer must pass the **effective** server names
or rely on validated discovery.
:::

### Explicitly out of scope

Barman 3.19 supports block-level incremental backups streamed to cloud through a
configured *Barman server*. That is not the Barman Cloud client topology the CNPG
plugin exposes, so such a repository is reported as
`incompatible_format`/`unknown` — never as ordinary full-backup evidence.

This is not a permanent exclusion. If an accepted CNPG or CNPG-I integration
exposes Barman server streaming incrementals, support becomes a release gate:
format-owned parent/chain fields, dependency validation, fixtures, and
failure-to-unknown tests must land before that CNPG/Plugin/Barman tuple is
called supported.

Also outside the current profile: Kubernetes/CSI `VolumeSnapshot` evidence (a
consumer-side source), pgBackRest interpretation, multiple repositories or scopes
in one process, and raw downloads.

## Discovery validation

When a scope list is empty, discovery is delegated to the format — and it
validates rather than guesses:

- **Barman** accepts only prefixes with a recognizable `base/` or `wals/`
  subtree.
- **pgBackRest** accepts only stanzas represented consistently under `backup/`
  or `archive/`.

Invalid candidates are ignored and reported as diagnostics. Names are trimmed,
deduplicated, and sorted deterministically.

## Upstream references

**Barman**

- [Barman Cloud documentation](https://docs.pgbarman.org/release/3.19.1/user_guide/barman_cloud.html)
- [Barman Cloud catalog source](https://github.com/EnterpriseDB/barman/blob/master/barman/cloud.py)
- [Barman backup metadata source](https://github.com/EnterpriseDB/barman/blob/master/barman/infofile.py)

**CloudNativePG**

- [Barman Cloud Plugin concepts](https://cloudnative-pg.io/plugin-barman-cloud/docs/concepts/)
- [Barman Cloud Plugin compression](https://cloudnative-pg.io/plugin-barman-cloud/docs/next/compression/)
- [Barman Cloud Plugin parameters](https://cloudnative-pg.io/plugin-barman-cloud/docs/parameters/)
- [CloudNativePG API reference](https://cloudnative-pg.io/docs/devel/cloudnative-pg.v1/)

**pgBackRest**

- [pgBackRest user guide](https://pgbackrest.org/user-guide.html)
- [pgBackRest command reference](https://pgbackrest.org/command.html)
- [pgBackRest release and repository-format policy](https://pgbackrest.org/release.html)
- [pgBackRest source repository](https://github.com/pgbackrest/pgbackrest)

**PostgreSQL**

- [`xlog_internal.h`](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/include/access/xlog_internal.h)
