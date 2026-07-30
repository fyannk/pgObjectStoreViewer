---
sidebar_position: 2
---

# HTTP endpoints

The standalone runtime serves exactly four routes on `LISTEN_ADDR`. There is no
authentication, no TLS, and no other route — see
[security](../operations/security.md).

| Route | Purpose |
|---|---|
| `GET /` | Inventory page: totals, validated scopes, recent objects, freshness, Barman catalog, recovery coverage |
| `GET /wals` | Cached WAL evidence browser, filtered by server, class, timeline, and WAL-name range |
| `GET /healthz` | Process liveness |
| `GET /readyz` | Configuration validity plus a recent lightweight, prefix-scoped reachability result |

Any other method or path is not served. Neither page performs provider I/O:
both render from the last published snapshot.

## `GET /`

Shows, for the configured repository:

- **Inventory** — exact object and stored-byte totals **only after a complete
  scan**; unknown otherwise, never zero as a substitute.
- **Scopes** — validated Barman server or pgBackRest stanza names.
- **Recent objects** — a bounded list, never the full listing.
- **Freshness** — the generation ID, start and completion timestamps,
  completeness, staleness, and any reached safety limits.
- **Barman catalog** — completed, in-progress, failed, malformed, unsupported,
  and structurally incomplete backups, kept separate.
- **Recovery coverage** — per-timeline observed coverage, labelled
  conservatively, with the stop reason for each path.
- **Configured expectations** — `EXPECTED_RETENTION_POLICY` as configured, and
  `EXPECTED_MINIMUM_REDUNDANCY` compared with the visible structurally usable
  backup count.
- **Displayed identity** — the value of `TRUSTED_USER_HEADER`, if configured.

## `GET /wals`

Filters the cached compact WAL evidence. Query parameters are bounded and
validated; an invalid value yields a rejected filter rather than a partial scan.

| Filter | Meaning |
|---|---|
| server | Barman server scope |
| class | segment, partial, history, backup-history, duplicate, unknown |
| timeline | timeline identifier |
| WAL-name range | inclusive start/end WAL names |

Paging uses `WAL_PAGE_SIZE` (1 – 1,000, default 200). Enormous missing ranges are
shown compactly and never expanded into unbounded rows.

## `GET /healthz`

Process liveness only. It does not check the store, the catalog, or the
configuration. A `200` here means the process is running — nothing more.

## `GET /readyz`

Ready requires **both**:

1. valid configuration; and
2. a lightweight, prefix-scoped list that succeeded recently (within the
   freshness window).

It never runs a scan and never exposes topology or error detail. An empty prefix
is reachable and therefore ready — producing a complete inventory with zero
totals.

:::caution
`/readyz` is not catalog health. A stale, incomplete, or unknown catalog is a
correct state to serve.
:::

## Response hardening

Sensitive responses are non-cacheable and carry restrictive CSP, referrer,
framing, and content-type headers. Server timeouts are: 5 s read-header, 15 s
read, 30 s write, 60 s idle, 16 KiB maximum headers, and a 15 s graceful-shutdown
deadline.

Request logs use stable route names — `/`, `/healthz`, `/readyz`, `/wals` — and
never raw URLs, queries, identity headers, or authorization headers.

## The sidecar surface

In `pgconsole-sidecar` mode none of the above exists: there is no TCP listener,
and the only surface is the authenticated evidence API over a Unix socket. See
the [evidence API reference](evidence-api.md).
