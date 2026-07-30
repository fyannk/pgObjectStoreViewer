---
sidebar_position: 6
---

# Decisions

Why the code looks the way it does. Each entry records what was decided, when,
and what it rules out — the reasoning that a reader cannot recover from the code
alone.

The code is the authority on *what happens*. This page is the authority on
*why*, and nothing more.

## One application, many formats

**Decided with the initial architecture.** ObjectStoreViewer models two
independent axes — object-store provider and repository format — instead of
shipping sibling `barmanviewer` and `pgbackrestviewer` applications.

Splitting per format would duplicate the security boundary, provider clients,
scanner, cache, UI, metrics, container, and operator integration. Adding Azure
must not change Barman semantics; adding pgBackRest must not duplicate HTTP,
cache, redaction, or provider adapters.

Splitting a format into its own application would require a new decision showing
a materially different deployment or security boundary, team lifecycle, or
runtime that cannot safely coexist here.

## No shared deep backup model

**Decided with the format boundary.** The shared envelope carries only concepts
with identical meaning in every format: format and compatibility status, scope
kind and display name, scan generation/completeness/freshness, normalized
evidence state plus format-native details, WAL positions, observed coverage with
assumptions and stop reason, capability flags, and the observed/unknown/verified
distinction.

Everything richer stays behind the format's own analyzer. A pgBackRest reference
chain is not a Barman parent field; Barman server layout is not a pgBackRest
stanza. Conversion into the envelope is conservative: a fact that cannot be
represented without losing an invariant stays format-owned or becomes `unknown`.
It is never approximated into healthy common data.

## Explicit format selection, no detection

**Decided with the configuration contract.** `REPOSITORY_FORMAT` is required.
Optional detection could only ever be a redacted diagnostic, because an empty,
encrypted, partially retained, or mixed-prefix repository makes automatic
detection unsafe. A mismatch between configured format and observed layout is
`unknown`/configuration-invalid, never a fallback.

## The configuration contract is frozen

**Frozen 2026-07-27**, deliberately before the composing operator's CRD existed,
so that both sides could be built against one stable contract. Changing it
requires matching compatibility, documentation, and test updates in the same
change. A removed or re-validated variable fails startup rather than being
ignored — a misconfiguration must never become a silently wrong answer.

## Static credentials only as files

**Decided with the configuration contract.** Direct `AWS_ACCESS_KEY_ID`,
`AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` *values* are rejected. Secrets
arrive as mounted files or ambient workload identity, are read with a size
ceiling, and are held in an opaque type that cannot be printed. Environment
variables leak through process listings, crash dumps, and child processes; a
mounted file has a permission model.

## Evidence as a sidecar, not a library

**Decided by the consumer team, 2026-07-28.** Repository evidence reaches
pgConsole from a **pod-private sidecar** holding the only object-store
credentials, over a versioned read-only JSON API.

In-process reuse was **permanently rejected**: it would have put object-store
credentials and Kubernetes credentials in the same process, and coupled two
release cycles through Go `internal` visibility. The sidecar keeps a hard rule
intact — S3 credentials and Kubernetes credentials never coexist in one
container, the sidecar holds no credential valid against the Kubernetes API, and
the consumer holds no object-store credential.

## A Unix socket, not loopback TCP

**Decided in the evidence API contract.** `v1alpha1` uses HTTP/1.1 over a fixed
pod-private Unix socket. Authenticated loopback TCP was considered as a fallback
and is not part of the version.

The bearer token authenticates the *caller*; the socket path — in a volume
mounted only into the two intended containers — authenticates the *server
boundary*. No NetworkPolicy mistake can expose the channel, and the auth proxy
cannot reach it at all. Confinement comes from the mount set, never from a file
mode.

## Liveness only, never readiness

**Decided 2026-07-28, after review.** The image ships a `probe` subcommand for an
`exec` **liveness** probe. Configuring `/readyz` as a kubelet readiness probe is
forbidden: a store failure would make the whole Pod unavailable, contradicting
the consumer's source-independent readiness. Repository evidence is one source
among several, not a dependency.

## Correlation is observed-UID-only

**Decided 2026-07-28, after review.** The producer never reads the Kubernetes
API. It exports repository identity and exact backup IDs; the consumer correlates
using only its own Kubernetes observation, and there is no injected fallback.

Zero matches means "counterpart not observed". More than one match means
"ambiguous". Neither is guessed or collapsed into agreement. Cluster name,
Backup object name, scheduled-backup name, Barman server name, timestamps, and
object prefixes are explicitly *not* substitute correlation keys.

## A types-only module

**Decided in the evidence API contract.** The wire vocabulary lives in
`github.com/fyannk/objectstoreviewer/api` with **zero external module
dependencies**, so consumers can depend on the contract without inheriting a
cloud SDK, a compression library, or a repository parser. Collector, publisher,
engine, provider, and analyzer interfaces stay implementation details that
consumers must not import.

## Two counters, not one

**Decided in the evidence API contract.** `evidence_generation` identifies the
last complete immutable evidence; `revision` advances on every published attempt,
including a failed refresh that retains previous evidence as stale. One counter
could not express "the evidence is unchanged but the last attempt failed", which
is exactly the state an operator most needs to see.

## Typed failure over truncated success

**Decided in the evidence API contract.** A safety-limit failure returns a typed
error rather than a short page. A truncated success is indistinguishable from a
complete small answer, which is how a bounded read turns into a false healthy
conclusion.

## Gap confirmation is not persisted

**Decided with the gap lifecycle.** A gap needs two consecutive complete scans to
be confirmed, and confirmation lives only in the process. After a restart, gaps
begin as candidates again.

Persisting confirmation would mean durable state — a volume, a schema, a
migration path, and a new way to be wrong after a restore. Re-confirming costs
one refresh interval.

## Downloads last, and opt-in

**Decided with the security posture.** Base backups and WALs contain every byte
of every database while bypassing PostgreSQL authorization. `ALLOW_DOWNLOAD`
defaults to `false`, no usable route exists until the feature ships with its own
threat model, and no incidental raw-object endpoint may appear earlier.

## Reimplement, never copy

**Decided with the format profiles.** Barman and pgBackRest source, documentation,
and pinned generated repositories are the compatibility *authority*, and the
behavior is reimplemented in Go. Implementation code is not copied, which keeps
this project's Apache-2.0 licensing intact.
