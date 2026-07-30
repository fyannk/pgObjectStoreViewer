---
sidebar_position: 3
---

# Repository formats

A repository format owns everything a cloud does not: what a backup is, which
artifacts it must have, how WAL objects are laid out, and what a dependency
means. Formats are **isolated by design** — they share the conservative evidence
envelope and genuinely common primitives, nothing else.

`REPOSITORY_FORMAT` selects exactly one format per scope. There is no
auto-detection, no fallback between formats, and object presence never
compensates for a mismatch.

## Barman Cloud

The first and currently interpreted format.

| Concern | Rule |
|---|---|
| Scope | one Barman *server* per scope name; `BARMAN_SERVER_NAMES` restricts, empty delegates to discovery |
| Backup metadata | per-backup `backup.info` is **required**; nothing else establishes a backup |
| Status | `STARTED` is in progress; `FAILED` is failed; any unrecognized status is unknown |
| Artifacts | expected main-data, tablespace, split, snapshot, incremental, and compression artifacts follow the supported Barman layout |
| Unsupported | unsupported metadata versions or backup types stay unknown — never an anchor |
| WAL layout | `<server>/wals/<timeline+log>/<wal-name>`, with timeline history files at the `wals/` root |
| Compression | gzip, bzip2, xz, snappy, zstd, and lz4 suffixes, normalized **only after** the underlying name and hash placement validate |

`backup.info` is parsed as a **version-tolerant field list**. The catalog retains
only allowlisted facts:

- backup ID, name, status, error, server name, backup type/mode, and parent;
- begin and end timestamps, WAL names, LSNs, timeline, and WAL segment size;
- logical size, deduplicated size when present, compression, encryption, and
  snapshot metadata; and
- unknown field *names* and parse warnings, as diagnostics — never their values.

The parser tolerates older supported metadata and marks newer unsupported
structures explicitly unknown. Compression support differs between base backups
and WAL; see the [compatibility
reference](../reference/compatibility.md#compression-support).

Snapshot and incremental structures, malformed metadata, missing artifacts, and
artifact-confirmation failures remain unknown or unhealthy as appropriate. None
of them becomes a recovery anchor.

### WAL and timeline analysis

Segment arithmetic uses the PostgreSQL version and `xlog_segment_size` taken
from the relevant backup metadata — never a hard-coded 16 MiB default. The
supported context is a power-of-two segment size from 1 MiB through 1 GiB, and
the position is `log * (2^32 / segment_size) + segment`.

Bounded timeline history is parsed into parent identity and switch positions
without erasing PostgreSQL system-ID or version boundaries, then used to connect
structurally usable anchors to per-timeline recovery paths. Each path stops at
the first of: missing ancestry, unknown evidence, a candidate gap, a confirmed
gap, or the observed archive frontier.

Verification: `make test` covers format semantics and timelines, while `make
test-integration` covers the pinned genuine Barman 3.19.1 journey.

## pgBackRest

pgBackRest is a **recognized format** — `REPOSITORY_FORMAT=pgbackrest` validates,
scopes via `PGBACKREST_STANZAS`, and produces inventory — but its metadata and
dependency chains are **not interpreted yet**. That work is Slice 6/7, gated for
milestone v1.5.

The committed profile, once implemented, requires:

- a readable stanza `backup.info`/copy plus the applicable `archive.info`/copy,
  or proven equivalent official JSON;
- encrypted metadata without a valid mounted cipher passphrase
  (`PGBACKREST_CIPHER_PASS_FILE`) to remain **unknown**;
- complete full/differential/incremental reference chains;
- manifests, bundles, block-incremental metadata, archive IDs, and format
  versions to be covered by the declared capability set; and
- a stanza or repository error to be preserved rather than collapsed into a
  healthy backup row.

If the Slice 6 gate accepts the official pgBackRest metadata helper, only the
immutable read-only `info --output=json` operation may ever run: no shell, no
user flags, no mutation or restore command, no unbounded I/O or runtime, no
secret arguments, and no ambient write credentials.

## Why not one backup model

A generic model would have to erase exactly the invariants that make the
evidence honest. A pgBackRest reference chain is not a Barman parent field, and
Barman server layout is not a pgBackRest stanza. So the rule is blunt: a new
shared type must have identical semantics in every format, or it stays
format-owned.

## Capabilities

Each format declares, per scope, what it can currently establish. The registry in
`internal/repository` owns that declaration, and `make test` covers it.

The capability identifiers are format-neutral and closed:

| Capability | Meaning |
|---|---|
| `object-inventory` | complete object inventory within the configured destination root |
| `catalog-listing` | the format's backup catalog could be listed |
| `structural-artifact-validation` | expected artifacts were stat-able |
| `dependency-chain-validation` | reference or parent chains were complete |
| `wal-continuity` | segment-name continuity over an observed range |
| `timeline-history-traversal` | history files parsed into ancestry |
| `encrypted-metadata` | encrypted metadata could be interpreted |
| `observed-recovery-coverage` | position-based coverage from an anchor |
| `retention-expectation-comparison` | observed evidence compared with configured expectations |

Each carries `support` (`supported`, `unsupported`, `unknown`) plus a state and
typed reason. **Unsupported, disabled, or failed capabilities stay visible and
force dependent conclusions to `unknown`.** They never vanish from the output and
are never rendered as zero or healthy — a milestone cannot waive a required
capability.

## Format isolation

The rules that keep one format's trouble from becoming another's:

- one scope is interpreted by exactly one configured format;
- a format parser cannot consume another format's metadata as healthy data;
- cache keys, cursors, downloads, routes, metrics, and diagnostics all carry
  store and format identity;
- failure, throttling, or unsupported metadata in one format or scope never
  blocks cached views or scans for another; and
- common rollups expose mixed capability and state explicitly.

Discovery is validating, not guessing — see [discovery
validation](../reference/compatibility.md#discovery-validation).

## Cross-format isolation

Isolation is asserted, not assumed:

- a **fake second format** proves the shared platform hard-codes no Barman
  terminology or types;
- the same provider contract runs beneath both real formats;
- equivalent healthy/warning/unhealthy/unknown scenarios produce the same
  evidence meaning while keeping format-native detail;
- a Barman prefix configured as pgBackRest, and the inverse, both fail `unknown`;
- store, format, and scope keys and cursors cannot cross boundaries;
- adding a format requires no change to provider adapters or the common HTTP
  security middleware; and
- the output states which capabilities each format actually proved.
