---
sidebar_position: 4
---

# Evidence API resources

Field-by-field reference for the `v1alpha1` payloads. The transport, envelope,
pagination, and error rules are in the [evidence API
reference](evidence-api.md).

The authoritative machine-readable definition is the generated
[`api/evidence/v1alpha1/schema.json`](https://github.com/fyannk/objectstoreviewer/blob/main/api/evidence/v1alpha1/schema.json)
plus the Go types beside it and the wire goldens under `testdata/wire`. If this
page and the schema ever disagree, the schema is right.

Two rules apply everywhere: a nullable field is **present with a JSON `null`**
when it is unknown, never omitted and never zero; and omitting a field can never
improve a state.

## Capabilities

`capabilities` is a complete, unique, `id`-sorted array.

| Field | Type | Rule |
|---|---|---|
| `id` | string enum | format-neutral capability identifier |
| `support` | `supported`, `unsupported`, `unknown` | unsupported or unknown support requires `state: unknown` |
| `state` | four-state enum | current evidence for this capability |
| `reason` | `Reason` | bounded typed explanation |

The identifiers are exactly:

`object-inventory`, `catalog-listing`, `structural-artifact-validation`,
`dependency-chain-validation`, `wal-continuity`, `timeline-history-traversal`,
`encrypted-metadata`, `observed-recovery-coverage`,
`retention-expectation-comparison`.

An unsupported, disabled, or failed capability stays visible and forces
dependent conclusions to `unknown`. It never disappears from the payload and is
never rendered as zero or healthy.

## Inventory summary

| Field | Type | Rule |
|---|---|---|
| `known` | boolean | false after an incomplete first scan; retained stale evidence stays known and is governed by the top-level `stale`/`state` |
| `object_count` | integer or `null` | exact only when known |
| `stored_bytes` | integer or `null` | exact visible object bytes, only when known |
| `unscoped_object_count` | integer or `null` | exact only when known; sidecar prefix confinement should normally make this zero |
| `pages_examined` | integer | bounded attempt fact |
| `objects_examined` | integer | bounded attempt fact |
| `last_failure_category` | string or `null` | stable redacted category only |

Raw recent objects and custom object metadata are intentionally absent.

## Typed details union

`details` is a closed, validated tagged union:

```json
{
  "type": "barman-cloud/v1alpha1",
  "barman_cloud": {}
}
```

Exactly one payload matching `type` is allowed; `map[string]any` is forbidden.
For an unrecognized `type` the types module keeps only the bounded tag,
**discards** the payload, and marks format details unavailable/unknown while
keeping a valid common envelope.

### `barman_cloud` summary

| Field | Type | Rule |
|---|---|---|
| `backup_items` | unsigned integer or `null` | exact number available through `/backups` |
| `wal_range_items` | unsigned integer or `null` | exact number available through `/wal-ranges` |
| `wal_gap_items` | unsigned integer or `null` | exact number available through `/wal-gaps` |
| `recovery_path_items` | unsigned integer or `null` | exact number available through `/recovery-paths` |
| `structurally_usable_backups` | unsigned integer or `null` | backups proven structurally usable by Barman rules |
| `backup_states` | `StateCounts` or `null` | all four state counts when known |
| `wal_counts` | `BarmanWALCounts` or `null` | all supported WAL class counts when known |
| `wal` | `StateReason` | WAL continuity state |
| `timeline` | `StateReason` | timeline-history state |
| `coverage` | `StateReason` | observed recovery-coverage state |
| `retention` | `BarmanRetentionSummary` | descriptive or expected retention result |
| `latest_archive_receipt_at` | timestamp or `null` | latest provider receipt/modification time, never transaction time |
| `ranges_truncated` | boolean | a safety ceiling was reached building ranges |
| `diagnostics_truncated` | boolean | a safety ceiling was reached retaining diagnostics |

`StateCounts` has required unsigned integers `healthy`, `warning`, `unhealthy`,
`unknown`. `BarmanWALCounts` has required unsigned integers `segment`,
`partial`, `history`, `backup_history`, `unknown`, `duplicate`.

`BarmanRetentionSummary` has `visible_backups` and
`structurally_usable_backups` (unsigned integer or null),
`oldest_completion_at` and `newest_completion_at` (timestamp or null),
`minimum_configured` and `policy_configured` (boolean), `minimum_redundancy`
(unsigned integer or null, and null unless `minimum_configured` is true), plus
required `state` and `reason`. A configured but unrecognized policy forces
retention state to `unknown`.

Collection counts are null whenever the governing evidence is incomplete, and a
truncation flag forces its dependent state to `unknown`.

## Barman backup items

`GET /api/v1alpha1/backups` → `BarmanBackupPage`. Sorted by exact `backup_id`,
then server. Backup IDs are case-sensitive opaque identifiers — do not parse
timestamps out of them.

| Field | Type | Rule |
|---|---|---|
| `server` | string | exact configured Barman server |
| `backup_id` | string | exact case-sensitive Barman backup ID, ≤ 256 bytes |
| `status` | string or `null` | allowlisted format-native status, ≤ 64 bytes |
| `backup_type` | string or `null` | allowlisted format-native type, ≤ 64 bytes; an unknown type is null and forces state unknown |
| `state` | four-state enum | structural evidence for this backup |
| `reason` | `Reason` | required typed reason |
| `system_id` | decimal string or `null` | PostgreSQL system identifier, never a JSON number |
| `postgresql_version` | unsigned integer or `null` | format-declared version |
| `timeline` | unsigned integer or `null` | timeline ID; zero is invalid |
| `wal_segment_size_bytes` | unsigned integer or `null` | format-declared; no 16 MiB default |
| `begin_wal` / `end_wal` | string or `null` | allowlisted WAL segment names, not object keys |
| `begin_lsn` / `end_lsn` | string or `null` | validated uppercase PostgreSQL LSN |
| `begin_at` / `end_at` | timestamp or `null` | format-declared begin and completion time |
| `logical_bytes` | unsigned integer or `null` | logical backup size |
| `deduplicated_bytes` | unsigned integer or `null` | only when explicitly declared by the format |
| `stored_artifact_bytes` | unsigned integer or `null` | exact visible bytes of validated artifacts |
| `compression` | string or `null` | allowlisted classification, not provider metadata |
| `encryption` | string or `null` | allowlisted classification; unknown never becomes `none` |
| `artifact_count` | unsigned integer or `null` | count only — artifact keys are forbidden |
| `tablespace_count` | unsigned integer or `null` | count only — OIDs and paths are forbidden |

An item's state is the last complete evidence for that backup. **If the parent
snapshot is stale or unknown, render the item as retained historical evidence,
not as currently healthy.**

## WAL range items

`GET /api/v1alpha1/wal-ranges` → `BarmanWALRangePage`. Sorted by server,
timeline, then start position.

| Field | Type | Rule |
|---|---|---|
| `server` | string | exact configured Barman server |
| `timeline` | unsigned integer | timeline ID; zero is invalid |
| `start_position` | unsigned integer | format-derived segment position |
| `end_position` | unsigned integer | inclusive position, **not** an LSN |
| `segment_count` | unsigned integer | exact count in the compact range |
| `first_wal` / `last_wal` | string | allowlisted WAL names |
| `latest_receipt_at` | timestamp or `null` | latest provider receipt/modification time in the range |
| `end_receipt_at` | timestamp or `null` | receipt/modification time of the ending segment |

## WAL gap items

`GET /api/v1alpha1/wal-gaps` → `BarmanWALGapPage`. Sorted by server, timeline,
then start position.

| Field | Type | Rule |
|---|---|---|
| `server` | string | exact configured Barman server |
| `timeline` | unsigned integer | timeline ID; zero is invalid |
| `start_position` | unsigned integer | first missing position |
| `end_position` | unsigned integer | last missing position, inclusive |
| `segment_count` | unsigned integer | exact number of missing positions |
| `first_wal` / `last_wal` | string | first and last missing WAL name |
| `status` | string enum | exactly `candidate` or `confirmed` |
| `first_observed_generation` | unsigned integer | complete generation that first observed the candidate |
| `last_observed_generation` | unsigned integer | most recent complete generation observing it |

A confirmed gap must have passed the [gap
lifecycle](../overview/evidence-model.md#gap-lifecycle); a failed or incomplete
scan cannot advance either generation.

WAL names are format-native segment identifiers, never object keys. Provider
receipt timestamps are never transaction timestamps or PITR endpoints.
Diagnostics that contain object keys stay in the standalone viewer and are not
part of this API version.

## Recovery-path items

`GET /api/v1alpha1/recovery-paths` → `BarmanRecoveryPathPage`. Sorted by backup
ID, then target timeline.

| Field | Type | Rule |
|---|---|---|
| `server` | string | exact configured Barman server |
| `backup_id` | string | exact case-sensitive Barman backup ID |
| `target_timeline` | unsigned integer | requested or derived target timeline |
| `state` | four-state enum | conservative observed-coverage state |
| `reason` | `Reason` | required typed reason |
| `stop` | string enum | `frontier`, `candidate-limited`, `gap-limited`, or `unknown-limited` |
| `lower_bound_at` | timestamp or `null` | conservative lower bound, never an exact PITR endpoint |
| `start_timeline` | unsigned integer or `null` | proven starting timeline |
| `start_position` | unsigned integer or `null` | proven starting segment position |
| `start_wal` | string or `null` | proven starting WAL name |
| `frontier_timeline` | unsigned integer or `null` | timeline at the observed frontier |
| `frontier_position` | unsigned integer or `null` | position at the observed frontier |
| `frontier_wal` | string or `null` | WAL name at the observed frontier |
| `frontier_receipt_at` | timestamp or `null` | provider receipt/modification time, not transaction time |
| `assumption_codes` | array of string enums | sorted, unique, ≤ 32 allowlisted codes of ≤ 64 bytes each |

Timeline-history keys, content, and graph edges are **not** exported in
`v1alpha1`. The summary still exposes timeline capability and state, and every
stop caused by missing or invalid ancestry is preserved in the path.

The API and its consumers say "observed recovery coverage". They never return or
render "exact PITR window", "restorable until", "restore guaranteed", or
"restore verified".
