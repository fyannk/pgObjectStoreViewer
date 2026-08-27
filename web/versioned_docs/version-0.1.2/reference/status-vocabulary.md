---
sidebar_position: 5
---

# Status and failure vocabulary

Every stable string an API consumer or a monitoring rule is allowed to branch on.
Branch on these, never on a message.

## Four states

| State | Meaning |
|---|---|
| `healthy` | all required evidence in the stated scope was collected, and no structural problem was found |
| `warning` | evidence is complete enough to identify a non-fatal or provisional problem |
| `unhealthy` | complete evidence identifies a definite structural failure |
| `unknown` | evidence is missing, unsupported, incomplete, truncated, stale beyond policy, or failed to load |

Rollup rules are in [the evidence model](../overview/evidence-model.md#status-vocabulary).

## Completeness

| Value | Meaning |
|---|---|
| `complete` | discovery and every required listing finished |
| `incomplete` | a listing did not finish, or a safety ceiling was reached |
| `no-completed-scan` | no complete generation exists yet |

## Failure categories

The redacted vocabulary shared by logs, HTTP output, and the evidence API. An
absent failure is `null`, never `unknown`.

| Category | Typical cause |
|---|---|
| `canceled` | shutdown or a canceled request context |
| `timeout` | `STORE_REQUEST_TIMEOUT` elapsed |
| `invalid_configuration` | configuration rejected at or after startup |
| `authentication` | the credential was not accepted |
| `authorization` | the credential is valid but not permitted on the prefix |
| `throttled` | provider rate limiting |
| `unavailable` | endpoint unreachable, wrong region, transport failure |
| `not_found` | the prefix or object was absent |
| `incompatible_format` | the repository does not match the selected format |
| `safety_limit` | a bounded ceiling was reached |
| `unknown` | nothing safe could be established |

No category ever carries an upstream message.

## Backup classifications

| Classification | State | Anchor? |
|---|---|---|
| metadata missing | `unknown` | never |
| metadata unreadable | `unknown` | never |
| in progress | `warning` | never |
| failed | `unhealthy` | never |
| unsupported status / type / capability | `unknown` | not until supported |
| missing dependency backup | `unhealthy` if absence is proven, else `unknown` | never |
| structurally usable | `healthy` contribution | candidate anchor |

## WAL classes

`segment` (ordinary complete), `partial`, `history`, `backup-history`,
`duplicate`, `unknown`. Partial WALs never fill a required complete-segment
position, and unknown files never contribute to continuity.

## Gap lifecycle states

| State | Meaning | Effect |
|---|---|---|
| candidate | seen in one complete scan | `warning` |
| confirmed | seen in two consecutive complete scans | `unhealthy` when required by a selected path |
| historical | entirely before the path's anchor | does not make that path unhealthy; still makes the WAL-continuity capability unhealthy |

Confirmation is process-local: after a restart, gaps begin as candidates again.

## Recovery-path stop reasons

A path stops conservatively at the first of:

- missing timeline ancestry;
- unknown evidence on the path;
- a candidate gap;
- a confirmed gap; or
- the current observed archive frontier.

The frontier time is the provider receipt/modification time of the latest
contiguous archived WAL. It is **not** transaction time and **not** a PITR
endpoint.

## Size vocabulary

| Term | Source |
|---|---|
| logical size | declared by supported format metadata |
| changed / deduplicated size | format-declared, labeled with its format-native meaning |
| stored bytes | sum of visible object sizes in a complete, bounded scope |
| unknown total | listing was incomplete, or a relevant size was unavailable |

These are never substituted for one another.

## Prohibited claims

The following never appear, in any surface, for any input:

- "restore verified"
- "restore guaranteed"
- "exact PITR window"
- "restorable until"

"Structurally usable" is the strongest available statement about a backup, and it
is deliberately weaker than "valid" or "restorable".
