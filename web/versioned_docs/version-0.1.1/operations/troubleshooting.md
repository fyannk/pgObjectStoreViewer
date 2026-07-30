---
sidebar_position: 5
---

# Troubleshooting

Two rules explain most of what you will see.

:::info Errors never disclose values
Startup and provider errors name a variable and a reason — never a credential,
a destination URL, an object key, a signed URL, or a raw SDK message. Diagnosis
starts from your manifest, not from an error string.
:::

:::info `unknown` is a result, not a bug
Incomplete, stale, unsupported, truncated, or failed evidence is reported as
`unknown` by design, and is never silently upgraded to `healthy`.
:::

## The process exits at startup

The message is `configuration invalid: <VARIABLE>: <reason>`.

| Reason | What to fix |
|---|---|
| `must be barman-cloud or pgbackrest` | `REPOSITORY_FORMAT` missing or misspelled — there is no auto-detection |
| `must be s3, azure, or gcs` | `PROVIDER` missing or misspelled |
| `must be a credential-free object-store URI` | `DESTINATION_PATH` scheme disagrees with `PROVIDER`, or embeds credentials or a query |
| `contains an invalid prefix` | control characters, `.`/`..` segments, or another unsafe prefix |
| `must be a credential-free http or https origin` | `ENDPOINT_URL` has a path, userinfo, or query — give an origin only, and only for S3 |
| `contains an invalid scope name` | a scope entry is empty, `.`/`..`, contains a path separator or control character, or exceeds 128 bytes |
| `requires pgconsole-sidecar` | a sidecar-only variable was set in standalone mode |
| `must be host:port` / `port must be between 1 and 65535` | `LISTEN_ADDR` |
| `must be an HTTP field name or empty` | `TRUSTED_USER_HEADER` |
| `must be between …` | a bounded numeric or duration variable is out of range |
| `must be true or false` | a boolean variable got something else |
| `access-key and secret-key files must be configured together` | S3 static credentials need both `*_FILE` variables |
| `requires access-key and secret-key files` | `AWS_SESSION_TOKEN_FILE` without the pair |

Two frequent surprises:

- **Direct credential variables are rejected.** `AWS_ACCESS_KEY_ID`,
  `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` as values fail startup.
- **Cross-provider and cross-format variables are rejected**, not ignored —
  Azure variables with `PROVIDER=s3`, or `PGBACKREST_STANZAS` with
  `REPOSITORY_FORMAT=barman-cloud`.

## `/readyz` never becomes ready

Readiness means *valid configuration plus a recent lightweight, prefix-scoped
list that succeeded*. It never runs a scan and never reflects catalog health.

1. **Authentication / authorization** — the credential cannot list the
   destination prefix. Verify the mounted files are the ones you think, the
   role trust relationship matches the pod's service account, and the policy
   grants `ListBucket` **on the bucket** with a prefix condition, not only
   `GetObject` on objects.
2. **Unavailable / timeout** — the endpoint is unreachable: a missing
   `ENDPOINT_URL` for MinIO, a NetworkPolicy blocking egress, or a private CA
   needing `ENDPOINT_CA_FILE`.
3. **Region mismatch** — a wrong `AWS_REGION` surfaces as unavailable, not as a
   helpful redirect.

An empty prefix is reachable, so it becomes ready and reports a complete
inventory with zero totals. Ready plus an empty page usually means the prefix is
wrong, not that the store is broken.

## Everything says `unknown`

| Symptom | Cause |
|---|---|
| everything unknown right after start | the first scan has not finished; there is no retained generation yet, so totals are legitimately null |
| totals unknown, "incomplete scan" | the scan hit `MAX_OBJECTS_PER_SCAN` or a listing did not complete |
| evidence marked stale | the latest refresh failed; the last complete snapshot is retained and shown as stale |
| backups unknown / metadata unreadable | `backup.info` missing, unreadable, or unparseable; encrypted or unsupported metadata stays unknown |
| backups unknown / unsupported | a Barman status, backup type, or capability this version does not recognize |
| WAL continuity unknown | unavailable segment size or version context, conflicting contexts within one server, multiple complete representations of one position, or a truncated diagnostic set |
| recovery coverage unknown | missing or malformed timeline history, impossible/cyclic ancestry, or a required input that was itself unknown |

See [the evidence model](../overview/evidence-model.md) for why each of these
must stay unknown rather than degrade to a guess.

## Backups look complete but are not "structurally usable"

That class requires supported terminal metadata **and** stat-able expected
artifacts. A backup leaves it when a required data or tablespace artifact is
missing, a size disagrees with the format-declared expectation, or a dependency
backup is absent.

What the label never means: uncorrupted, decryptable, readable, or restorable.
Only a restore drill proves that.

Logical size, changed/deduplicated size, and stored bytes are three different
values. Never compare or add them.

## A gap appeared, or will not clear

1. one complete scan → **candidate** → `warning`;
2. two consecutive complete scans → **confirmed**;
3. confirmed and required by a selected path → `unhealthy`;
4. entirely before the path's anchor → historical inventory: it does not make
   that path unhealthy, though it does make the WAL-continuity capability
   unhealthy — which is why the two indicators can legitimately disagree;
5. an incomplete scan in between neither confirms nor clears a candidate;
6. **after a restart, confirmation history is gone** — gaps start as candidates
   again, by design.

Absence *after* the newest observed segment is the archive frontier, not a gap.

Archive freshness is the latest provider modification/receipt timestamp among
classified complete segments. It has no health threshold and is not transaction
time — do not alert on it as if it were replication lag.

## Sidecar: the process exits before serving

The errors are intentionally opaque (`evidence socket path is invalid`,
`evidence token file is invalid`). Check in this order:

**Socket directory** — a real directory (not a symlink), setgid, at least
`02770`, with the process's effective or supplementary GID equal to its group. A
directory without the setgid bit fails even when the mode looks permissive:
without setgid the socket would not inherit the shared group. A kubelet applying
`fsGroup` delivers an effective `02777`, which is accepted.

**Socket path** — a symlink or non-socket at
`/var/run/objectstoreviewer/evidence.sock` is refused outright. A stale socket
is removed only when a dial confirms nothing is listening; if something *is*
listening, startup fails rather than stealing the path.

**Token file** — non-symlink regular file, mode exactly `0440` (not `0400`, no
setuid/setgid/sticky), at most 128 bytes, one canonical 43-character
unpadded-base64url token. Mount the Secret key with a `subPath` so the path is a
real file rather than a projected symlink.

**Profile restrictions** — `barman-cloud`, `s3`, exactly one
`BARMAN_SERVER_NAMES` entry, an explicit `STORE_CREDENTIAL_MODE`, no
`LISTEN_ADDR`, no `TRUSTED_USER_HEADER`, no `ALLOW_DOWNLOAD=true`.

## Sidecar: the liveness probe fails

`./bin/objectstoreviewer probe` reports only success or failure — never the
token, the body, or the transport error. A failure means the viewer is not
listening, the token file is unreadable or does not match the token the server
loaded, or the probe container does not carry both volumes.

If the console gets `401 unauthenticated` while the probe succeeds, the two
containers are reading different token revisions. Tokens are not rotated in
place: generate once, and restart the Pod to adopt a new revision.

## Nothing is reachable from a browser

That is the intended state. In standalone mode the port is reachable only
through your authentication proxy; the example `NetworkPolicy` admits only pods
labeled `app.kubernetes.io/component: auth-proxy`. In sidecar mode there is no
TCP listener at all, so a `curl` against a port will always fail.

If the identity shown in the UI looks wrong, remember `TRUSTED_USER_HEADER` is
display-only: a wrong name means the proxy forwarded a wrong or unstripped
header, never that someone gained access.

## Collecting evidence for a bug report

Include the redacted startup log, the readiness state, the scan-generation
facts shown in the UI (generation ID, start/completion timestamps, completeness
flags, safety limits reached), the provider and repository format, and the
Barman version that wrote the repository.

Do **not** include credentials, destination URLs, object keys, or raw provider
errors — and note that the process will not have produced any of those anyway.
