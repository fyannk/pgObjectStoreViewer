---
sidebar_position: 7
---

# Release notes

## v0.1.2 — 2026-08-25

A security release. Upgrading is recommended for every v0.1.1 and v0.1.0
deployment.

**Standard-library vulnerabilities.** v0.1.1 was built with Go 1.26.5 and is
affected by seven vulnerabilities reachable from this code: GO-2026-6218
(`net/url`), GO-2026-6091 (`html/template`), GO-2026-6090 (`crypto/tls`),
GO-2026-6089 and GO-2026-5026 (`net/http`), GO-2026-6088 (`encoding/xml`), and
GO-2026-5972 (`encoding/asn1`). The `encoding/xml` and `encoding/asn1` paths are
reached through ordinary object-store listing and credential setup. This release
is built with Go 1.27.0 and `govulncheck` reports none.

**Continuation cursors accept only their canonical spelling.** `Decode` verified
the signature over the decoded bytes without checking that the value it received
was the only string producing them. Go's base64 decoder ignores carriage returns
and newlines and, outside strict mode, unused bits in the final character, so one
issued cursor had several accepted spellings. Confinement was never broken —
every variant verified under the same key and stayed inside the same prefix — but
the codec was not the one-to-one mapping it documents, and a caller comparing or
caching cursors as opaque strings could treat one position as several. Cursors
are ephemeral, so this is not a compatibility break in practice.

No evidence-model, API, or configuration changes: `internal/provider/cursor` is
the only non-test source file that differs from v0.1.1.

**Maintenance branches are gone.** Releases are tags on `main` and there are no
backports. Fixes reach a deployment by upgrading to the next tag, which is why
this release exists rather than a patch to the 0.1 line. See
[`SECURITY.md`](https://github.com/fyannk/pgObjectStoreViewer/blob/v0.1.2/SECURITY.md).

## v0.1.1 — 2026-07-30

This maintenance release updates the AWS and Google Cloud Go dependencies and
the GitHub Pages actions used by CI. It contains no functional or evidence-model
changes from v0.1.0.

## v0.1.0 — 2026-07-30

The first release includes the currently shipped Barman Cloud inventory, backup
and WAL evidence, provider parity, conservative recovery coverage, and the
pgConsole evidence producer. The tag records exactly what shipped; later fixes
arrive in later tags rather than as maintenance on this one.

## Milestone status

| Milestone | State | Scope |
|---|---|---|
| **v0** | shipped | honest Barman Cloud inventory over S3 |
| **v0.5** | shipped | the same normalized catalog and rendered evidence over S3, Azure Blob Storage, and GCS, byte-identical |
| **v1** | shipped | Barman timeline ancestry, backup consistency ranges, conservative observed recovery coverage |
| **v1.5** | not shipped | pgBackRest metadata source, backup catalog, archive continuity, and recovery coverage |
| **v2** | not shipped | controlled operational extras: metrics, opt-in raw download, multiple stores |

The production runtime currently includes **Slices 0 through 5**.

## What shipped, by slice

| Slice | Delivered |
|---|---|
| 0 | secure multi-format runtime foundation: the frozen configuration contract, the read-only store surface, redaction, health/readiness, the restricted container |
| 1 | S3 inventory from store to browser: the bounded background scanner, the atomic snapshot, the cached inventory page, million-object scale evidence |
| 2 | honest Barman Cloud backup catalog: `backup.info` parsing, artifact validation, separate reporting of completed/in-progress/failed/malformed/unsupported/incomplete backups |
| 3 | provider parity: the Azure and GCS adapters behind the same contract, with pinned Azurite and fake-gcs-server journeys |
| 4 | Barman WAL continuity and the gap lifecycle: metadata-sized segment arithmetic, compact ranges, candidate/confirmed gaps, the cached WAL browser |
| 5 | Barman timeline graph and observed recovery coverage: bounded history parsing for every supported compression, conservative path rendering |
| 13 (steps 1–5) | the pgConsole producer: the dependency-light `api` module, the generated schema and wire goldens, the immutable publication engine, the authenticated Unix-socket channel, the executable sidecar runtime, and the producer container filesystem profiles |

## Not yet available

- **pgBackRest interpretation.** `REPOSITORY_FORMAT=pgbackrest` validates and
  produces inventory, but metadata and dependency chains are not interpreted.
  Milestone v1.5.
- **Retention policy interpretation.** `EXPECTED_RETENTION_POLICY` is displayed
  as configured; the derived status stays unknown until Slice 8.
- **Metrics.** Slice 9.
- **Raw object download.** Slice 10, opt-in via `ALLOW_DOWNLOAD`. No usable
  download route or link exists today.
- **Multiple stores or formats per process.** Slice 11.
- **A qualified pgConsole pair.** The producer is executable and proven at its
  own boundary; a live cross-check demonstration, published per-profile resource
  sizing, and the first complete pgtoolbox qualification profile remain release
  gates.

## Known behaviors that look like bugs

These are intended, and each has a test asserting it:

- **Gap confirmation is lost on restart.** Confirmation is process-local, so
  confirmed gaps return to candidate (`warning`) after a restart until a second
  consecutive complete scan re-confirms them.
- **Truncated WAL diagnostics force `unknown`** even when every visible complete
  segment is contiguous.
- **Multiple complete representations of one WAL position force `unknown`.** The
  application will not guess which one a restore command would choose.
- **An empty prefix is ready** and reports a complete inventory with zero
  totals.
- **A failed refresh keeps the previous evidence** and marks it stale; on the
  *first* failed refresh, totals are `null` rather than `0`.
- **Startup errors name no values.** Not even the offending path.

## Compatibility

The single-repository configuration contract was frozen by the Slice 0 decision
gate on **2026-07-27**. A removed or re-validated variable fails startup rather
than being silently ignored.

The evidence API is `evidence.objectstoreviewer.io/v1alpha1` and is additive-only
within the version: new fields may appear, and consumers must ignore what they do
not recognize. Unrecognized typed-details tags are discarded.

On **2026-07-30**, the repository, Go modules, and JSON Schema identifier moved
to `github.com/fyannk/pgObjectStoreViewer`. Go consumers must update their import
paths; the evidence API version and wire vocabulary are unchanged.

## Verified environments

| Environment | Evidence |
|---|---|
| Barman Cloud 3.19.1 repositories | genuine backup and WAL journey in pinned MinIO, inside `make test-integration` |
| S3 / MinIO, Azure / Azurite, GCS / fake-gcs-server | `make test-provider-parity`, byte-identical normalized output |
| linux/amd64 and linux/arm64 images | `make test-multiarch` |
| Restricted container runtime | `make test-container`: arbitrary UID/GID, read-only root, dropped capabilities, no-new-privileges, SIGTERM |
| OpenShift 4.20 under `restricted-v2` | live sidecar Pod and channel mechanics, 2026-07-29 |
