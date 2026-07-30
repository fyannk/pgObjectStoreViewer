---
sidebar_position: 3
---

# The evidence model

These are the rules the code implements and the semantic tests assert
(`make test`). Where wording anywhere else is
ambiguous, this page is the one to read — and where this page and the code
disagree, the code is right.

## Confidence layers

Each layer may make exactly one claim, and only on the evidence listed.

| Layer | Claim permitted | Minimum evidence | Claim **not** permitted |
|---|---|---|---|
| Inventory observed | an object or parsed metadata record was visible during a named scan generation | a successful provider list/get/stat | that the object existed before or after that scan |
| Structurally usable backup | a completed backup's metadata parsed and its required artifacts and dependency backups were visible and stat-able | complete scan, supported format, terminal successful status, complete dependency graph, expected artifacts present | that artifacts are uncorrupted, decryptable, or restorable |
| Observed WAL continuity | no segment-name gap was observed over a defined position range and timeline ancestry | complete scan, known segment size, classified WALs, required history metadata | that WAL bytes are valid or contain a particular timestamp |
| Observed recovery coverage | a structurally usable backup is connected to an observed contiguous WAL frontier | all the layers above, plus an explicit anchor and timeline graph | an exact PITR wall-clock endpoint, or a guaranteed restore |
| Restore verified | **never produced by this application** | an external restore drill | that any local inference substitutes for a drill |

"Structurally usable" is deliberately weaker than "valid" or "restorable".
Those stronger words are prohibited in the UI.

## Status vocabulary

| State | Meaning |
|---|---|
| `healthy` | all required evidence in the stated scope was collected, and no structural problem was found |
| `warning` | evidence is complete enough to identify a non-fatal or provisional problem — a candidate gap, a stale archive |
| `unhealthy` | complete evidence identifies a definite structural failure — a failed backup, a confirmed required-WAL gap |
| `unknown` | evidence is missing, unsupported, incomplete, truncated, stale beyond policy, or failed to load |

Rollups are conservative, in this order:

1. any required `unknown` input makes the dependent conclusion `unknown`;
2. `unhealthy` beats `warning` only when the unhealthy observation is complete
   and relevant to the stated scope;
3. healthy evidence about one backup or timeline may not hide unhealthy or
   unknown evidence about another — both the selected recovery path and the
   complete inventory stay visible; and
4. a partial scan may add diagnostics but can never create or confirm a healthy
   semantic result.

## Backup classification

| Observation | Classification | Recovery use |
|---|---|---|
| required catalog metadata missing | `unknown` / metadata missing | never an anchor |
| metadata cannot be decrypted, read, or parsed | `unknown` / metadata unreadable | never an anchor |
| format-native in-progress status | `warning` / in progress | never an anchor |
| format-native failed terminal status | `unhealthy` / failed | never an anchor |
| successful terminal status, supported structure | continue to artifact validation | candidate anchor |
| missing dependency/reference backup | `unhealthy` when complete evidence proves absence, otherwise `unknown` | never an anchor |
| unknown status, unsupported type or capability | `unknown` / unsupported | never an anchor until supported |

Completion is never inferred from an end timestamp, a label, a manifest name, or
the presence of a data object. New or unrecognized values are unknown.

Artifact validation proves only that the expected key was present in a complete
listing or direct stat, that the provider returned bounded allowlisted metadata,
and that the size was non-negative and matched a format-declared expectation
where one exists. It proves nothing about checksums, archive readability,
decryption permission, or tar contents. ETags are displayed as opaque
identifiers, never treated as content hashes.

## Sizes are not interchangeable

- **Logical size** — declared by supported format metadata.
- **Changed/deduplicated size** — a format-declared value, labeled with its
  format-native meaning.
- **Stored bytes** — the sum of visible object sizes in a complete, bounded
  scope.
- **Unknown total** — used whenever listing was incomplete or any relevant size
  was unavailable.

These are never silently substituted for one another. Do not add or compare
them in a dashboard.

## WAL classification

Objects are classified without trusting the suffix alone: ordinary complete
segment, partial segment, timeline history, backup-history, archive-ID
suffixed object, supported compressed variant, or unknown/malformed.

For Barman Cloud a classified archive object must sit in the format-owned
`<server>/wals/<timeline+log>/<wal-name>` hash directory, with history files at
the `wals/` root. Gzip, bzip2, xz, snappy, zstd, and lz4 suffixes are
normalized only after the underlying name and hash placement validate.

Continuity becomes `unknown` — not "probably fine" — when:

- the segment size or PostgreSQL version context is unavailable (no hard-coded
  16 MiB default is ever assumed);
- conflicting version/segment-size contexts exist within one Barman server;
- multiple complete representations of the same WAL position exist, because the
  application will not guess which one a restore would choose; or
- a compact-range or retained-diagnostic safety ceiling truncated the evidence.

Position arithmetic is `log * (2^32 / segment_size) + segment`, with a supported
power-of-two segment size from 1 MiB through 1 GiB.

## Gap lifecycle

A gap is identified by store, format, scope, archive identity, timeline,
start/end position, scan generation, and recovery relevance.

1. Seen in one complete scan → **candidate** → `warning`.
2. Seen in two consecutive complete scans → **confirmed**.
3. Confirmed **and required by a selected recovery path** → `unhealthy`.
4. Entirely before the path's anchor → historical inventory; it does not make
   that path unhealthy, though it still makes the WAL-continuity capability
   unhealthy.
5. An incomplete scan in between neither confirms nor clears a candidate.
6. After a process restart there is no persisted confirmation, so gaps begin as
   candidates again.
7. A missing range is a gap only when a later observed complete segment bounds
   it. Absence *after* the newest segment is the archive frontier — that WAL may
   simply not be archived yet.

## Timelines

The format module turns supported history files into parent identity and switch
positions without erasing PostgreSQL system-ID or version boundaries. The graph
analyzer must detect and report a missing history file needed to connect a
selected child timeline, malformed history content, impossible or cyclic
ancestry, a child range disagreeing with its switch point, and independent
timelines claiming no ancestry. A timeline with missing ancestry can still have
an inventory, but coverage across the missing edge is unknown.

## How recovery coverage is computed

Coverage is position-based and computed per possible backup anchor:

1. select only structurally usable backups;
2. derive the WAL range required to bring each backup to a consistent recovery
   state, from its begin/end WAL, PostgreSQL version, and segment size;
3. require **every** segment in that range;
4. follow contiguous segments on the backup's timeline;
5. traverse a timeline switch only through a valid history edge;
6. for dependent backup formats, require every referenced backup and applicable
   catalog or manifest edge before using the anchor; and
7. stop at the first candidate, confirmed, unknown, or incomplete position, and
   record the reason.

Each result carries the backup ID and a conservative lower wall-clock bound
(normally backup end time); start and frontier positions and timelines; whether
the frontier is complete, candidate-limited, gap-limited, or scan-limited; the
archive receipt time of the frontier object, explicitly labelled as an
archive-time estimate; and every assumption needed to derive the range.

## Freshness

Archive freshness is descriptive: the latest provider modification or receipt
timestamp among classified complete segments. It has **no default health
threshold**, and it is not transaction time. A recovery path's frontier time
comes from the same source and is not a PITR endpoint.

Any threshold must be explicit. A presentation default may be offered, but
health and alert semantics require an operator-configured expectation.

:::caution Do not infer activity from WAL age
Low-write PostgreSQL clusters archive WAL mainly because of `archive_timeout`, so
WAL age says nothing reliable about workload activity.
:::

## Retention signals

Without configured expectations, the viewer reports descriptive facts only: the
number of visible backups, the number structurally usable, oldest and newest
completion time, and observed coverage age.

With `EXPECTED_RETENTION_POLICY` or `EXPECTED_MINIMUM_REDUNDANCY` set, observed
evidence is compared against those expectations — as a **sanity signal**. It is
not a reimplementation of, and not proof of, the configured tool's retention
policy execution. Barman and pgBackRest retention semantics belong to their own
modules. A configured but unrecognized policy forces retention state to
`unknown`.

Retention deletion during a scan can produce transient inventory changes;
incomplete observations become unknown.

## What every semantic view shows

So that a state is never presented without its scope:

- the four-state label **in text**, not only as a colour;
- the evidence generation and its age;
- complete, stale, or incomplete status;
- the exact scope of the conclusion;
- a short "what this proves" explanation; and
- a disclosure for diagnostics and assumptions.

Object names, Barman server names, and metadata values are escaped. Arbitrary
provider metadata, raw catalog or info files, timeline history content, and
object content are never rendered.
