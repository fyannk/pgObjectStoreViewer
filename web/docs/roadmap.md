---
sidebar_position: 8
---

# Roadmap

What is not built yet, and what each piece will have to prove before it counts
as built. Shipped behavior is in the [release notes](release-notes.md).

:::note This page describes intent, not behavior
Nothing here exists in the code. If a page in this site and the code disagree,
the code is right — see [how this documentation
works](development.md#code-is-the-truth).
:::

## pgBackRest support

`REPOSITORY_FORMAT=pgbackrest` currently validates, scopes via
`PGBACKREST_STANZAS`, and produces inventory. Metadata and dependency chains are
not interpreted.

**Backup catalog** will add stanza discovery, `backup.info`/copy and
`archive.info`/copy handling, version/format/cipher/status capability reporting,
full/differential/incremental reference graphs, and manifest/bundle/block
capability checks. To count as done:

- encrypted repositories work with a mounted passphrase and fail *redacted
  unknown* with a missing or wrong one;
- every incremental or differential candidate proves its complete reference
  chain before becoming structurally usable;
- ordinary, bundled, and block-incremental repositories each have proven support
  or a visible unknown capability — an unsupported configuration can never be
  called healthy;
- `barman-cloud` data configured as `pgbackrest` and the inverse both fail
  unknown, with no fallback; and
- shared adapters and pages need no pgBackRest-specific branches beyond the
  registered format interface.

**Archive continuity and coverage** will add archive-ID and database-history
parsing, checksum-suffixed/compressed WAL normalization, and per-chain recovery
frontiers reusing the same compact-range and two-scan gap lifecycle. Retention
gaps outside a selected path must stay visible without invalidating that path,
and `pgbackrest info` min/max alone must never be presented as continuity proof.

### The metadata source decision

The official `pgbackrest info --output=json` output is the preferred first
candidate, because pgBackRest documents that JSON as machine-readable and
stable. A decision gate must accept or reject a fixed read-only subprocess
integration first, proving: arbitrary UID, read-only root, writable `/tmp` only,
multi-architecture packaging; S3/Azure/GCS credential translation with no secrets
in arguments, process listings, logs, or errors; bounded stdout/stderr, timeout,
cancellation, and no shell; encrypted and unencrypted repositories;
full/differential/incremental, bundle, and block configurations; read-only
credentials with no provider mutation calls; and pinned version/format
compatibility.

If accepted, **only** the fixed `info --output=json` operation may ever run — no
shell, user flags, restore/verify/expire/stanza commands, unbounded output,
mutation permissions, or secret-bearing arguments. If rejected, a native Go
reader must separately scope encryption, info-file checksum and copy behavior,
manifests, bundles, blocks, and compatibility. Unsupported structures stay
`unknown` either way.

A fixed upstream helper would also need more than a compile-time interface
restriction: an immutable command and argument allowlist, credentials that deny
writes, a provider audit check showing no mutation requests, a subprocess sandbox
with bounded resources, canary redaction tests, and a failure-closed response
when the executable, version, or configuration differs from the supported
contract.

## Freshness and retention expectations

Cross-format backup and archive age, plus descriptive retention inventory, with
explicit health only when format-appropriate expectations are configured.
Constraints that already hold and must keep holding: unconfigured retention
stays descriptive and cannot become unhealthy just because an arbitrary age
threshold passed; scan staleness makes freshness conclusions unknown; and mixed
format capabilities are never compared as if retention semantics were identical.

## Metrics

A `/metrics` endpoint exposing completeness, staleness, last success, format
capability, structurally usable backup count, newest-backup age, frontier
receipt age, and gap state — from the same evidence model the pages use. Metric
values and snapshot generation must agree in tests; stale, incomplete, or
unsupported evidence must not stay silently healthy; and no object key, backup
ID, identity, raw error, or secret may become a label. Cardinality stays bounded
by configured stores, formats, and scopes.

## Controlled raw download

Opt-in via `ALLOW_DOWNLOAD=true`, scoped to the configured store/format/scope
prefix. Default-false must return not found and emit no links. Prefix escape,
tampered keys, header injection, and unsupported ranges all fail closed;
streaming stays within a memory bound; failures are redacted and stop promptly
on client disconnect. This needs its own accepted threat model and audit-event
schema before it ships.

Base backups and WALs contain every byte of every database while bypassing
PostgreSQL authorization. That is why this is opt-in and last.

## Multiple stores and formats

One deployment presenting several explicitly configured stores and formats
without mixing credentials, cache generations, capabilities, or evidence. A
failure or throttle in one store must not block refresh or cached views for
another; keys, cursors, downloads, scope names, and cipher secrets must not cross
boundaries; concurrency and metric cardinality stay globally bounded; and
single-store configuration stays backward compatible.

## Cross-project qualification

The pgConsole sidecar producer is executable and proven at the producer
boundary. Remaining gates before any runtime pair is advertised as supported: a
live cross-check demonstration, published per-profile resource sizing from the
composing operator, and the first complete qualification profile.

That profile also needs an end-to-end test: a pinned CNPG/Barman Cloud Plugin
creates a backup in MinIO or S3, and the consumer correlates the exact backup ID
and renders equivalent evidence without stronger recovery claims.

## Rules no future work may weaken

- No later feature may weaken an earlier confidence or security rule.
- A new format capability is either proven or visibly unknown; a milestone
  cannot waive a required capability.
- Absence of an out-of-scope source or backup type is never a healthy result.
- Every addition keeps the read-only, redaction, boundedness, determinism, and
  no-overstatement invariants intact.
