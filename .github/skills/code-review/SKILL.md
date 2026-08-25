---
name: code-review
description: Review pgObjectStoreViewer changes against the repository's hard invariants — the read-only object-store boundary, evidence that never overstates itself, secret redaction at the adapter edge, bounded and cancelable work, provider and format isolation, and deterministic output. Use this when reviewing any pull request in this repository.
license: Apache-2.0
---

pgObjectStoreViewer reads PostgreSQL backup repositories out of object
storage — S3, Azure Blob, GCS — and reports what the evidence supports.
It holds credentials to buckets containing production backups and never
writes to them. Most of what matters in a review here is not style: it is
whether a change widens the read-only boundary, leaks a credential, or
lets a conclusion claim more than the evidence proves.

`make lint` already enforces style (`golangci-lint` with `gosec` on both
modules, `gofmt`, `go vet`). Do not spend review on what the linter fails
on its own.

## The canonical source

[`AGENTS.md`](../../../AGENTS.md) is the complete and normative invariant
list; [`CONTRIBUTING.md`](../../../CONTRIBUTING.md) summarises it. **Read
the invariant in `AGENTS.md` before asserting a change violates it**, and
quote the number. Where the two disagree, `AGENTS.md` wins and the
summary is the defect.

## What to look for

### 1. Read-only by construction

The invariant has two independent layers, and a review has to check both.

**The code layer.** Domain code may use only list, bounded get/open, and
stat/head through the narrow read-store interface. No write, create,
upload, copy, delete, multipart, lifecycle, tagging-mutation,
bucket-creation, or restore operation may exist anywhere in `cmd/`,
`internal/`, or `api/`.

**The credential layer.** *"Provider credentials must independently deny
writes."* The word is **independently**: the deny must hold even if the
code layer were wrong. So a change to an IAM policy template, a role, a
trust relationship, or the documented least-privilege grant is a
read-only boundary change even when no Go code moves — and one that
merely widens a policy without adding a call is easy to wave through
because nothing in `cmd/` or `internal/` looks different.

`hack/check-readonly.sh` covers both: it fails on mutation-shaped
identifiers **and** on forbidden actions in the IAM policy templates.
Treat an edit that narrows that scan as a change to the boundary itself.

### 2. Evidence never overstates itself

This is the invariant most often broken by accident, because the broken
version reads better.

- `healthy`, `warning`, `unhealthy`, `unknown` keep exactly the meanings
  the semantic tests assert.
- **Any required `unknown` input makes the dependent conclusion
  `unknown`.** A partial scan can never produce a healthy result.
- Incomplete, stale, unsupported, truncated, or failed evidence can never
  become healthy.
- The phrases **"restore verified"**, **"restore guaranteed"**, **"exact
  PITR window"**, and **"restorable until"** are prohibited outright — in
  UI text, comments, and test fixtures.

Look for an error path falling back to a zero value that renders healthy,
a truncation flag dropped while mapping to a view model, and a count
presented as a total when the source could only supply a floor.

### 3. No secret disclosure

Credentials, mounted secret values, provider authorization headers, signed
URLs, SAS parameters, and raw credential-bearing SDK errors never reach
logs, responses, HTML, errors, tests, fixtures, or snapshots. **Redaction
happens at the adapter boundary**, so a raw provider error wrapped with
`%w` further up is the classic leak — a signed URL or SAS parameter often
rides inside the SDK error string. A new fixture captured from a real
provider response is worth reading closely.

### 4. No raw backup content in the UI

Parse bounded format metadata internally; render allowlisted fields only.
A new field reaching a template needs to be on the allowlist, not merely
available.

### 5. Bounded and cancelable

HTTP handlers never scan. Listing is paginated, concurrency is bounded,
safety ceilings fail to `unknown` rather than to a partial answer that
looks whole, and every provider operation respects a `context.Context`. A
new call without a context, or a loop over pages with no ceiling, is a
finding.

### 6. Keys are opaque

Never map object keys onto filesystem paths — that is a path-traversal
sink — and never let a key or cursor escape its configured store,
destination prefix, and scope.

### 7. Provider-neutral, format-isolated

AWS, Azure, and GCS SDK types stay inside `internal/provider/<name>`.
Barman Cloud and pgBackRest keep their typed catalogs inside
`internal/formats/<name>`. An SDK type or a format-specific field crossing
into shared code is a finding even when it compiles — invariant 14 exists
because a generic backup model erases the distinctions: a pgBackRest
reference chain is not a Barman parent field.

### 8. Deterministic output

Stable sort orders, UTC timestamps, exact integer byte counts, explicit
unknowns, injected clocks. No naked `time.Now()` in code computing an age
or a staleness.

### 9. The proxy is the trust boundary

The application authenticates nothing. `TRUSTED_USER_HEADER` is
display-only — a change treating it as an authorization input is a
security change, not a feature.

### 10. Routes, health, and snapshots

Downloads stay opt-in and are **not built yet**: no incidental raw-object
route may appear before that feature ships with its own threat model.
`/healthz` is process liveness and `/readyz` is valid configuration plus a
recent lightweight prefix-scoped reachability result — neither runs a
scan. A failed refresh never replaces good evidence: publish snapshots
atomically, keep the last complete one, and mark it stale.

### 11. Repository format is explicit

No auto-detection, no fallback between formats. A heuristic that guesses
the format is a finding however convenient.

### 12. `#nosec` needs a named bound

A guarded integer conversion carries `#nosec G115 -- <invariant>` naming
the bound that makes it safe. A `#nosec` without a named, checkable
invariant is silenced analysis, not verified analysis — flag it. License
boilerplate (`hack/boilerplate.go.txt`) is required on every new Go file.

## Documentation is not specification

The code and tests are the source of truth. When prose and code disagree,
**the code is right and the prose is the defect** — do not report a code
change as wrong because a README, a doc page, or a badge says otherwise.
Report the stale prose instead.

Apply the same standard to prose *about* configuration. If a comment or
document paraphrases a condition, a filename, or a flag, ask for the exact
string so a reader can grep for it. A paraphrase that cannot be verified
is the same class of defect as a stale one.

The single-repository configuration contract was frozen on 2026-07-27.
Changing it requires matching compatibility, documentation, and test
updates — a change touching it with only one of the three is incomplete.

## Two modules

The repository ships `.` and `api/`. Both are linted, both are vetted, and
both carry their own `go.mod`. A change to shared types usually needs to
land in both, and `hack/check-go-version.sh` requires their toolchains to
agree with the `Dockerfile`.

## How to report

Lead with the invariant number and what the change does to it. Prefer one
well-evidenced finding over several speculative ones — quote the line and
say what input reaches it. If the reasoning depends on a file you have not
read, read it or say the finding is uncertain. State plainly when a change
is clean; "no findings" is a useful review here.
