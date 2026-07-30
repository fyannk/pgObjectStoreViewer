---
sidebar_position: 6
---

# Limits and ceilings

Every bound below exists so that failure degrades to `unknown` instead of to
unbounded memory, an unbounded page, or a wrong answer. They are enforced in
code and asserted by tests; this page collects them in one place.

## Store requests

| Bound | Value | Effect when exceeded |
|---|---:|---|
| objects per list page | 1,000 | request rejected as invalid |
| object key length | 16 KiB | request rejected as invalid |
| provider cursor length | 16 KiB | request rejected as invalid |
| bounded get/open size | per call, always set | read stops; evidence becomes unknown |
| per-operation timeout | `STORE_REQUEST_TIMEOUT`, 1 s – 5 min | `timeout` failure category |
| concurrent provider operations | `SCAN_CONCURRENCY`, 1 – 64 | queued |

## Scan

| Bound | Value | Effect when exceeded |
|---|---:|---|
| objects per scan | `MAX_OBJECTS_PER_SCAN`, 1,000 – 10,000,000 | scan incomplete; totals unknown |
| retained recent objects | bounded index, 200 per scope | older entries dropped from the list, never from the totals |
| compact WAL ranges | 10,000 per Barman server | WAL continuity becomes `unknown` |
| retained WAL diagnostics | 200 rows per server | diagnostics truncated, which forces `unknown` |
| configured scopes | bounded set | configuration rejected |
| timeline history entries | bounded per history file | `malformed timeline history` → unknown |
| decompressed history bytes | bounded per file | history rejected → unknown |

A truncated diagnostic set forces `unknown` **even when every visible complete
segment is contiguous**. Contiguity you cannot see all of is not contiguity you
can claim.

## WAL arithmetic

| Bound | Value |
|---|---|
| supported segment size | power of two, 1 MiB through 1 GiB |
| segment position formula | `log * (2^32 / segment_size) + segment` |
| segments per log ID | `2^32 / segment_size` (at most 4,096 for a 1 MiB segment) |
| WAL name | exactly 24 hex digits |
| PostgreSQL version floor | 9.3 (90300) for arithmetic context |

There is no 16 MiB default. Missing or conflicting version/segment-size context
makes continuity `unknown` rather than calculated.

## HTTP server (standalone)

| Bound | Value |
|---|---:|
| read-header timeout | 5 s |
| read timeout | 15 s |
| write timeout | 30 s |
| idle timeout | 60 s |
| maximum header bytes | 16 KiB |
| graceful-shutdown deadline | 15 s |
| WAL browser rows per page | `WAL_PAGE_SIZE`, 1 – 1,000 (default 200) |

## Evidence API (sidecar)

| Bound | Value | Effect when exceeded |
|---|---:|---|
| handler deadline | 5 s, every route | request fails; no partial body |
| snapshot response | 256 KiB | `413 response-limit` |
| collection response | 1 MiB | `413 response-limit` |
| items per page | default 100, maximum 200 | `400 invalid-request` |
| concurrent API requests | 16 | `429 busy` with bounded retry guidance |
| cursor length | 4,096 bytes | `400 invalid-request` |
| bearer token file | 128 bytes, exactly 43 encoded characters | startup refuses |
| message strings | 256 bytes, no control characters | validation failure |
| reason code | 64 bytes, lower-kebab-case | validation failure |
| assumption codes per path | 32, each ≤ 64 bytes | validation failure |

A safety-limit failure never returns a truncated success page. It returns a
typed error, and the affected collection becomes `unknown`.

## Deployment budget

| Resource | Value |
|---|---:|
| CPU request / limit | 25m / 500m |
| memory request / limit | 64 MiB / 256 MiB |
| `/tmp` volume | 16 MiB |

Recorded scale acceptance: a synthetic 1,000,000-object scan (999,998
contiguous WAL segments) completes within a 128 MiB heap and 256 MiB RSS budget,
retains a single WAL range, and keeps at most 200 recent objects. A cached
summary page performs zero provider operations and renders within 200 ms in the
reference environment.

Raising `MAX_OBJECTS_PER_SCAN` or `SCAN_CONCURRENCY` raises what one scan can
hold and do. Raise the pod limits with them.

## Configuration bounds

| Variable | Range |
|---|---|
| `CATALOG_REFRESH_INTERVAL` | 30 s – 24 h |
| `STORE_REQUEST_TIMEOUT` | 1 s – 5 min |
| `SCAN_CONCURRENCY` | 1 – 64 |
| `MAX_OBJECTS_PER_SCAN` | 1,000 – 10,000,000 |
| `WAL_PAGE_SIZE` | 1 – 1,000 |
| `EXPECTED_RETENTION_POLICY` | ≤ 256 characters, no control characters |
| `EXPECTED_MINIMUM_REDUNDANCY` | 0 – 100,000 |
| scope name | ≤ 128 bytes, no separators or control characters |
| `ENDPOINT_CA_FILE` | ≤ 1 MiB, ≥ 1 PEM certificate |
| `PGBACKREST_CIPHER_PASS_FILE` | ≤ 64 KiB |
| `CNPG_CLUSTER_NAMESPACE` | ≤ 63 bytes |
| `CNPG_CLUSTER_UID` | ≤ 128 bytes |
| `CNPG_CLUSTER_NAME` | ≤ 253 bytes |
| `AWS_REGION` | ≤ 128 bytes |
| `AZURE_STORAGE_ACCOUNT` | ≤ 256 bytes |
