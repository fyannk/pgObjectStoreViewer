---
sidebar_position: 3
---

# Evidence API `v1alpha1`

The private producer API served in `pgconsole-sidecar` mode over
`/var/run/objectstoreviewer/evidence.sock`. The authoritative definition is the
generated schema, Go types, and wire goldens in the
[`api` module](https://github.com/fyannk/pgObjectStoreViewer/tree/v0.1.2/api), all
checked by `make check`.

## Routes

The route set is **closed**.

| Method and route | Result |
|---|---|
| `GET /healthz` | Process liveness only, over the authenticated socket |
| `GET /readyz` | Sidecar configuration plus recent lightweight store reachability; never a scan |
| `GET /api/v1alpha1/snapshot` | Current immutable envelope, identity, inventory, and Barman summaries |
| `GET /api/v1alpha1/backups` | Generation-consistent page of typed Barman backup evidence |
| `GET /api/v1alpha1/wal-ranges` | Generation-consistent page of compact WAL ranges |
| `GET /api/v1alpha1/wal-gaps` | Generation-consistent page of candidate/confirmed gaps |
| `GET /api/v1alpha1/recovery-paths` | Generation-consistent page of observed recovery paths |

No route accepts an object key. No route returns arbitrary metadata, artifact
names, history-file keys or content, WAL object keys, provider URLs, signed URLs,
credentials, or raw backup content. Other methods return `405`, unknown routes
return `404`, CORS is disabled, and proxy/forwarding headers are ignored.

Every handler has a five-second hard deadline, honors cancellation, and performs
no provider I/O.

## Authentication

```http
Authorization: Bearer <pod-local-token>
```

The token authenticates the caller; the fixed socket path authenticates the
server boundary. Missing, malformed, or oversized credentials get `401
unauthenticated` with no further detail.

## Response headers

```http
Content-Type: application/vnd.objectstoreviewer.evidence.v1alpha1+json
Cache-Control: no-store
X-Content-Type-Options: nosniff
Referrer-Policy: no-referrer
```

Responses are not compressed on the pod-private channel.

## Health and readiness bodies

Fixed JSON with `api_version`, `kind` (`HealthStatus` / `ReadinessStatus`), and
`status`:

- `/healthz` → `200` with `status: "live"`;
- `/readyz` → `200` with `status: "ready"`, or `503` with
  `status: "not-ready"`.

Neither body contains identity, destination, provider cause, or evidence
summary.

## Common wire rules

- Field names are `snake_case`.
- Timestamps are UTC RFC 3339 with optional fractional seconds; unknown times
  are `null`, never a zero timestamp.
- Byte and object counts are exact JSON integers; unknown counts are `null`,
  never `0`.
- WAL positions and generations are non-negative integers; decode into the
  provided Go types, not floating point.
- Arrays are non-null and deterministically sorted. An empty array means a known
  empty collection only when its associated `known` flag is true.
- Messages are UTF-8, control-character-free, and at most 256 bytes. **Branch on
  typed states and codes, never on messages.**
- Identifiers are bounded typed strings; arbitrary object keys and provider
  errors can never become identifiers.
- Unknown and unsupported facts stay explicit — omitting a field cannot improve a
  state.

`completeness` is exactly `complete`, `incomplete`, or `no-completed-scan`.
Redacted failure categories are exactly `canceled`, `timeout`,
`invalid_configuration`, `authentication`, `authorization`, `throttled`,
`unavailable`, `not_found`, `incompatible_format`, `safety_limit`. An absent
failure is `null` — not `unknown`.

## State and reason

```json
{
  "state": "unknown",
  "reason": {
    "code": "no-completed-scan",
    "message": "no completed repository scan"
  }
}
```

`state` is exactly `healthy`, `warning`, `unhealthy`, or `unknown`. `code` is a
stable lower-kebab-case identifier of at most 64 bytes. An unrecognized code
does not change the supplied state — render a safe generic explanation instead of
guessing.

## Publication identity

Two counters distinguish evidence from refresh attempts:

| Counter | Advances when |
|---|---|
| `evidence_generation` | a new **complete** immutable generation is published; collection pages are bound to it |
| `revision` | **every** published attempt result, including a failed refresh that retains the previous evidence as stale |

After a failed refresh: `evidence_generation` is unchanged, `revision` advances,
`stale` is `true`, and dependent states are `unknown`. Treat
`(revision, evidence_generation)` as **one** publication identity.

## Snapshot

`GET /api/v1alpha1/snapshot` returns one `RepositoryEvidenceSnapshot`:

```json
{
  "api_version": "evidence.objectstoreviewer.io/v1alpha1",
  "kind": "RepositoryEvidenceSnapshot",
  "producer": {"name": "objectstoreviewer", "version": "1.0.0"},
  "identity": {
    "cluster": {
      "namespace": "database-team",
      "uid": "2f12b7d1-7e8d-4c37-a68f-233efc5f3191",
      "name": "orders"
    },
    "repository": {
      "provider": "s3",
      "format": "barman-cloud",
      "destination_fingerprint": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      "scope": {"kind": "barman-server", "name": "orders"}
    }
  },
  "revision": 43,
  "evidence_generation": 42,
  "started_at": "2026-07-28T10:00:00Z",
  "completed_at": "2026-07-28T10:00:03Z",
  "last_attempt_at": "2026-07-28T10:05:00Z",
  "completeness": "complete",
  "stale": true,
  "state": "unknown",
  "reason": {"code": "refresh-throttled", "message": "last refresh failed"},
  "capabilities": [],
  "inventory": {},
  "details": {}
}
```

All fields are required; a field whose type includes `null` is present with a
JSON `null` when unknown.

| Field | Rule |
|---|---|
| `producer` | `name` fixed to `objectstoreviewer`; `version` sanitized, both ≤ 64 bytes |
| `identity.cluster` | required `namespace` and `uid`, optional display `name` |
| `identity.repository` | provider, format, shared `destination_fingerprint`, and the format-native scope |
| `started_at` / `completed_at` | of the **generation**, not the latest failed attempt |
| `last_attempt_at` | start of the most recent refresh attempt |
| `completeness` | of the evidence this publication represents |
| `stale` | last attempt failed, or the freshness threshold elapsed |
| `state` / `reason` | conservative top-level state and its typed reason |
| `capabilities` | complete, unique, deterministically sorted |
| `inventory` | provider-neutral allowlisted facts |
| `details` | exactly one format-owned tagged payload |

When `evidence_generation` is `0`: both generation timestamps are `null`,
`completeness` is `no-completed-scan` or `incomplete`, and `state` is `unknown`.
When it is non-zero, both timestamps are non-null and ordered.

Unrecognized `details` tags are **discarded**, never guessed at.

## Pagination

```text
GET /api/v1alpha1/backups?revision=43&limit=100
GET /api/v1alpha1/backups?revision=43&limit=100&cursor=<opaque>
```

Every collection response carries `api_version`, `kind` (`BarmanBackupPage`,
`BarmanWALRangePage`, `BarmanWALGapPage`, or `BarmanRecoveryPathPage`, matching
the route), `revision`, `evidence_generation`, nullable exact `total_items`,
`items`, and nullable `next_cursor`.

Rules:

- default page size 100, maximum 200;
- cursors are opaque, authenticated with a process-random secret **distinct from
  the bearer token**, process-local, at most 4096 bytes, and bind route,
  revision, generation, sort position, and limit;
- the cursor secret is never persisted — a restart invalidates every cursor,
  returns `invalid-request`, and requires restarting from `/snapshot` rather
  than a best-effort resume;
- a cursor cannot be reused on another route or after a publication change;
- a no-longer-current `revision` returns `409 publication-changed`; discard the
  partial assembly and restart from `/snapshot` after bounded backoff;
- publish assembled details atomically only after every requested page for one
  revision validates; and
- a safety-limit failure never returns a truncated success page — it returns a
  typed error, and the affected collection becomes `unknown`.

Limits: snapshot ≤ 256 KiB, each collection response ≤ 1 MiB and ≤ 200 items, at
most 16 concurrent API requests (excess gets `429 busy` with bounded retry
guidance).

## Errors

```json
{
  "api_version": "evidence.objectstoreviewer.io/v1alpha1",
  "kind": "EvidenceAPIError",
  "code": "publication-changed",
  "message": "repository evidence changed; restart from snapshot"
}
```

| HTTP | Code | Meaning |
|---:|---|---|
| 400 | `invalid-request` | Invalid bounded query, limit, or cursor |
| 401 | `unauthenticated` | Missing or invalid pod-local token |
| 404 | `not-found` | Route not present; no object lookup implied |
| 405 | `method-not-allowed` | The API is read-only |
| 409 | `publication-changed` | Requested revision is no longer current |
| 413 | `response-limit` | A bounded response cannot be represented safely |
| 429 | `busy` | API concurrency budget reached |
| 500 | `invalid-publication` | Internal snapshot failed validation; no partial data returned |

Messages are fixed or generated from allowlisted values. Provider errors,
destination URLs, object keys, credentials, token values, raw metadata, and
stack traces never cross this boundary.

## Consumer behavior

Transport failure, timeout, authentication failure, schema incompatibility,
invalid publication, and repeated publication changes all make the consumer's
repository source unavailable/unknown. **None of them may make the consumer's own
`/readyz` fail** — evidence is a source, not a dependency.

## Generated artifacts

```bash
make generate-evidence-artifacts   # regenerate schema.json + wire goldens
make check-api                    # fail on generated drift or new API dependencies
make test                         # compile the schema and validate wire behavior
```

`api/evidence/v1alpha1/schema.json` is a generated Draft 2020-12 schema, and
`api/evidence/v1alpha1/testdata/wire/` holds deterministic goldens for every
response kind. Both are committed and checked.
