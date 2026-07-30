# AGENTS.md

Instructions for AI coding agents working in this repository. These rules apply
to the entire repository unless a more specific `AGENTS.md` exists below the
path being changed.

## What this repository is

ObjectStoreViewer is a read-only, multi-format Go web application for
inspecting PostgreSQL backup repositories in object storage. Barman Cloud is
the first format and pgBackRest is required in v1.5. pgtoolbox will deploy the
container behind an authentication proxy and an operator-managed network
boundary.

This is an evidence viewer, not a restore verifier. Object presence, metadata,
and WAL-name continuity can establish structural evidence; they cannot prove
that a restore will succeed.

## Code is the truth

**The code and its tests define what this application does. Nothing else does.**

There is no specification set to consult, and there must not be one. The
behavioral authority, in order:

1. the code in `cmd/`, `internal/`, and `api/`; and
2. the tests beside it, which encode the invariants as executable assertions.

The check, integration, scale, container, and release targets in the `Makefile`
are reproducible ways to exercise those tests and structural checks. They do not
create a second acceptance taxonomy.

`web/` is a Docusaurus site that **explains** that truth for humans. It is
documentation, never specification. Concretely:

- If `web/` and the code disagree, **the code is right and the page is a bug.**
  Fix the page.
- Never change code to match a sentence in `web/`. If the behavior is genuinely
  wrong, say so and change it deliberately, with its tests and checks.
- Do not create design, spec, plan, or contract documents. Do not resurrect
  `docs/`. If a rule needs to be normative for contributors, it belongs in this
  file. If a fact needs explaining to a reader, it belongs in `web/`.
- A behavior change updates the code, its tests, the affected checks, and the
  affected `web/` pages in the same change.
- Two exceptions carry knowledge the code cannot: `web/docs/architecture/decisions.md`
  records *why* the code is shaped as it is, and `web/docs/roadmap.md` records
  intent for unbuilt work and says so explicitly.

Before changing behavior, read the code and the tests for the packages you are
touching. `internal/config` defines what a valid deployment is; `internal/store`
plus one provider adapter defines the read boundary;
`internal/formats/barmancloud` defines the semantics; `internal/inventory`
defines scanning and publication; `internal/web` and `internal/evidenceapi`
define output.

The single-repository configuration contract was frozen on 2026-07-27. Change it
only with matching compatibility, documentation, and test updates.

## Hard rules — violating any of these is a bug

1. **Read-only by construction.** Domain code may use only list, bounded
   get/open, and stat/head operations through the narrow read-store interface.
   Do not add write, create, upload, copy, delete, multipart, lifecycle,
   tagging-mutation, bucket-creation, or restore operations. Provider
   credentials must independently deny writes.
2. **Never overstate evidence.** The four states are fixed: `healthy` (all
   required evidence in the stated scope collected, no structural problem),
   `warning` (complete enough to identify a non-fatal or provisional problem),
   `unhealthy` (complete evidence identifies a definite structural failure),
   `unknown` (evidence missing, unsupported, incomplete, truncated, stale, or
   failed to load). Rollups are conservative: any required `unknown` input makes
   the dependent conclusion `unknown`, and a partial scan can never create or
   confirm a healthy result. Incomplete, stale, unsupported, truncated, or failed
   evidence cannot become healthy. Never claim “restore verified,” “restore
   guaranteed,” “exact PITR window,” or “restorable until” from store metadata.
   “Structurally usable” is the strongest permitted statement about a backup.
3. **No secret disclosure.** Credentials, mounted secret values, provider
   authorization headers, signed URLs, Azure SAS parameters, and raw
   credential-bearing SDK errors must not appear in logs, traces, metrics,
   HTTP responses, HTML, links, tests, fixtures, or snapshots. Redaction is
   mandatory at adapter boundaries.
4. **No raw backup content in the normal UI.** Parse bounded format metadata
   such as Barman `backup.info`, pgBackRest info/manifests, and history
   internally, but render allowlisted fields only. Hide arbitrary custom
   metadata and tags by default.
5. **All work is bounded and cancelable.** HTTP handlers never perform full
   catalog scans. Listing is paginated, concurrency is bounded, safety ceilings
   fail to `unknown`, and every provider operation accepts and respects a
   `context.Context`. Do not leak goroutines or retain unbounded object lists.
6. **Keys are opaque.** Never map object keys to local filesystem paths, trust
   path-cleaning as an authorization boundary, or allow a key/cursor to escape
   its configured store, destination prefix, and server.
7. **Provider-neutral, format-isolated domain.** AWS, Azure, and GCS SDK types
   stay inside provider adapters. Barman Cloud and pgBackRest keep their typed
   catalogs/dependency rules inside separate format modules. Shared code uses
   only the conservative evidence envelope and genuinely common primitives.
8. **Deterministic output.** Stable sort orders, UTC timestamps, exact integer
   byte counts, explicit unknown values, and injected clocks are required.
   The same snapshot and configuration produce byte-equivalent semantic data.
9. **The proxy is the trust boundary.** The app performs no authentication or
   authorization. `TRUSTED_USER_HEADER` is display-only. Never add behavior
   that assumes the application port or forwarded identity header is safe when
   directly reachable.
10. **Downloads stay opt-in.** When `ALLOW_DOWNLOAD=false`, no usable download
    route or link exists. Download work must include negative tests for the
    disabled route, prefix confinement, range and size bounds, cancellation,
    safe headers, and audit behavior; do not introduce incidental raw-object
    endpoints earlier.
11. **Health is not catalog health.** `/healthz` is process liveness.
    `/readyz` is valid configuration plus a lightweight, prefix-scoped recent
    reachability result. Neither endpoint runs a scan or exposes sensitive
    topology/error details.
12. **A failed refresh never replaces good evidence.** Publish catalog
    snapshots atomically. Retain the last complete snapshot after failure and
    mark it stale. Partial scans may add diagnostics but cannot confirm gaps or
    healthy coverage.
13. **Repository format is explicit.** `REPOSITORY_FORMAT` selects exactly one
    supported format per scope. Do not silently auto-detect, fall back between
    formats, or let object presence compensate for a format mismatch.
14. **No generic backup model that erases invariants.** A pgBackRest reference
    chain is not a Barman parent field, and Barman server layout is not a
    pgBackRest stanza. New shared types must have identical semantics in every
    format or remain format-owned.
15. **Format helpers are fixed and sandboxed.** If the Slice 6 gate accepts the
    official pgBackRest metadata helper, only the immutable read-only
    `info --output=json` operation may run: no shell, user flags, mutation or
    restore commands, unbounded I/O/runtime, secret arguments, or ambient write
    credentials.

## Work in vertical slices

Deliver the smallest vertical behavior that satisfies the request. A complete
change normally crosses configuration or input, domain behavior, provider/fake
boundary, HTTP/UI output, failure cases, tests, checks, and documentation.
Unbuilt work and its acceptance conditions are sketched in
`web/docs/roadmap.md`, which is intent, not contract.

Before coding:

- identify the behavior and the milestone it belongs to;
- list the evidence rules affected;
- inspect the existing implementation and tests;
- check the working tree and preserve unrelated user changes;
- define success, negative cases, and required check targets; and
- resolve an entry gate before depending on a decision it is meant to freeze.

Do not build speculative layers for later slices. Small internal seams are
welcome when the current slice exercises them end to end.

## Repository and change discipline

- Use `rg` and `rg --files` for discovery.
- Make focused edits; do not format, rename, or reorganize unrelated code.
- Preserve existing user changes. Never reset, discard, or overwrite changes
  that are outside the requested scope.
- Use `apply_patch` for hand edits. Use generators/formatters for generated or
  mechanical output and commit the source plus generated result together.
- Do not edit generated files manually once their generator is established.
- Never commit credentials, real backup/WAL contents, signed URLs, production
  bucket names, or unreviewed large binaries. Synthetic fixtures must be small,
  documented, and demonstrably free of sensitive data.
- New dependencies require a concrete need, an active maintenance/security
  check, compatible licensing, and a note in the change. Prefer the standard
  library and already-selected official SDKs.
- Do not add compatibility shims, feature flags, or fallback behavior without
  a documented lifecycle and tests.
- Do not weaken a test or check merely to make a change pass. Fix the behavior
  or explicitly change the governing contract with rationale.

## Atomic commits

Agents do not create, amend, rebase, squash, or push commits unless the user
explicitly asks. When commits are requested:

- One commit represents one coherent behavior or documentation decision.
- Code, tests, documentation, fixture manifests, and generated output required
  for that behavior belong in the same commit.
- Do not mix refactoring, dependency upgrades, formatting, or drive-by cleanup
  with a feature/fix unless inseparable from it.
- Stage explicit paths after reviewing their diffs; avoid `git add -A` in a
  dirty worktree.
- Run the applicable verification before committing and record what passed.
- Use an imperative Conventional Commit subject:
  `feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `build:`, `ci:`, or `chore:`.
- Keep the subject concise (prefer at most 72 characters). The body explains
  why and semantic/security tradeoffs when non-obvious.
- Never amend, rewrite history, force-push, or bypass hooks unless the user
  explicitly requests that exact operation.
- If a logically atomic change cannot pass independently, do not commit it as
  an intermediate broken state; explain the dependency and regroup the work.

## Go conventions

- Target Go 1.26 as specified by `go.mod`. The normal path is a static Go binary;
  a pinned pgBackRest helper may be packaged only after its required decision
  and security checks pass.
- Every Go source file uses the repository's Apache-2.0 boilerplate once Slice
  0 establishes it.
- `gofmt` clean is mandatory. Follow the repository lint configuration.
- Prefer standard-library `net/http`, `html/template`, `embed`, `log/slog`, and
  ordinary Go errors over frameworks or abstraction-heavy helpers.
- Packages are cohesive and named for domain responsibility, not generic
  layers such as `util`, `common`, or `helpers`.
- Interfaces are small and owned by their consumer. Do not mirror entire cloud
  SDK clients in interfaces or mocks.
- `context.Context` is the first parameter for blocking/I/O operations. Never
  replace a caller context with `context.Background()` in an active request or
  scan path.
- Wrap errors with `%w` internally. Convert provider errors to stable,
  redacted categories at the adapter boundary. Do not branch on error strings
  when the SDK provides typed errors.
- Constructors validate invariants and return errors. Avoid mutable package
  globals and hidden `init` behavior.
- Use `time.Time` in UTC at boundaries and inject a clock where age,
  staleness, gap confirmation, or retention behavior is tested.
- Use `int64` for object sizes and counts unless a narrower type is proven.
  Never calculate exact bytes through floating point.
- Bound channels, worker pools, response bodies, metadata reads, decompression,
  pagination, and rendered rows. Document every safety limit and its
  `unknown` behavior.
- Close response bodies and streams on every path. Propagate cancellation and
  test client disconnects.
- Comments explain invariants, evidence limits, security constraints, and
  non-obvious repository-format/PostgreSQL behavior—not what the next line
  says.

## Repository-format and semantic conventions

- Provider and repository format are independent axes. Adding a format must
  not modify provider adapters; adding a provider must not change format
  semantics.
- Every snapshot states format, format compatibility, scope kind/name, and
  proven capabilities. Unsupported capabilities remain visible and unknown.
- Barman and pgBackRest source plus pinned generated output are their format
  authorities. Cite upstream versions in fixture manifests and compatibility
  changes.
- Parse format metadata version-tolerantly. Unknown status, fields,
  compression, encryption, or structure remains unknown; never infer a default
  that improves health.
- Barman owns per-backup `backup.info`, server prefixes, artifact derivation,
  hashed WAL layout, and timeline history.
- pgBackRest owns stanza `backup.info`/copy, `archive.info`/copy, manifests,
  full/diff/incr references, bundles, blocks, archive IDs, encryption, and
  checksum-suffixed WAL names.
- An incremental/dependent backup cannot be structurally usable until every
  required reference and supported manifest/catalog edge is proven.
- If the pgBackRest helper is accepted, execute it directly without a shell,
  through fixed arguments and a bounded context. Generate sensitive config only
  under `/tmp` with restrictive permissions, bound stdout/stderr, verify the
  executable/version, and redact every error/output boundary.
- Distinguish logical size, deduplicated size, and stored bytes.
- WAL arithmetic uses the relevant PostgreSQL version and
  `xlog_segment_size`; never hard-code 16 MiB as universal.
- Partial, duplicate, malformed, history, and backup-history WAL objects have
  explicit classifications and test cases.
- A gap is candidate after one complete scan and confirmed only under the
  lifecycle in the evidence document.
- Timeline ancestry comes only from valid bounded history metadata. Missing,
  malformed, cyclic, or contradictory history stops coverage as unknown.
- Archive receipt/modification time is not transaction time. Label it
  accordingly in UI and metrics.
- Retention is descriptive unless an operator expectation is configured and is
  interpreted by the selected format. pgBackRest retention gaps outside a
  selected required path may be intentional.

## HTTP, UI, logging, and metrics

- Configure server read-header, read, write/streaming, idle, and shutdown
  timeouts deliberately. Test slow and canceled clients where relevant.
- Render store-derived values only through `html/template`; do not use
  `template.HTML` for dynamic data.
- Sensitive pages send `Cache-Control: no-store`, restrictive CSP,
  `X-Content-Type-Options: nosniff`, and restrictive referrer policy headers.
- Serve no third-party scripts, fonts, styles, trackers, or telemetry.
- Error pages are useful but redacted. Detailed provider causes remain in
  categorized internal diagnostics only when safe.
- Structured logs use stable field names and bounded values. Do not log raw
  request headers, destination URLs with credentials, object content, or
  arbitrary metadata.
- Metrics mirror the same immutable evidence generation as the UI and expose
  staleness/completeness. Labels are bounded; object keys, backup IDs, user
  identities, raw errors, and timeline IDs are not labels.
- Status is conveyed through text and semantics, not color alone.

## Testing conventions

- Tests are table-driven where cases share behavior and name the scenario and
  expected evidence state.
- Unit tests are hermetic: no real network, current time, ambient credentials,
  home directory, or execution-order dependence.
- Provider adapters pass one shared contract suite. Use MinIO, Azurite, and
  fake-gcs-server only for provider behavior they actually emulate; document
  gaps rather than pretending emulator coverage is cloud coverage.
- Repository-format modules pass a shared isolation/evidence-envelope contract
  plus a format-specific compatibility suite. Wrong-format repositories must
  fail unknown.
- Keep small committed golden fixtures for parser speed and reproducibility.
  Generate end-to-end fixtures with pinned Barman and pgBackRest tooling. Never
  hand-author a “realistic” fixture without documenting which format rules it
  covers.
- Every semantic change adds or updates pathological cases in the semantic test
  suites. The required minimum fixture matrix is listed in
  `web/docs/reference/checks.md`; the suites themselves are authoritative.
- Run the race detector for concurrency changes and deterministic repeated
  tests for snapshot/gap state machines.
- Scale tests prove bounded retention and failure-to-unknown, not merely that a
  happy scan eventually completes.
- Negative tests are mandatory for redaction, prefix isolation, incomplete
  scans, unsupported metadata, disabled downloads, and restricted runtime.
- Add or update the narrowest relevant test and its reproducible Make target in
  the same change. Do not create milestone-specific aliases for checks that
  already exist.

## Commands and verification

```bash
make build             # build the binary
make test              # unit tests, both modules
make test-race         # race detector, both modules
make test-stress       # repeated concurrency and lifecycle tests
make check-api         # dependency boundary and generated API drift
make test-integration  # pinned Barman and provider-emulator journeys
make test-scale        # bounded million-object scan and render benchmark
make test-container    # restricted standalone and sidecar image profiles
make lint              # gofmt, vet, boilerplate, read-only scan, reader surface,
                       # and golangci-lint with gosec enforced
make golangci-lint     # golangci-lint alone, both modules
make vuln              # pinned govulncheck
make check             # complete local verification without Docker
make package           # amd64/arm64 binaries plus SHA256SUMS
make docker-build      # local container image
make docs              # build the documentation site
make release-check     # all checks plus release artifacts
```

Prefer the Make targets over private command variants so local and CI checks
match. Never invent successful command output.

Before declaring work complete, run the narrowest applicable tests during
development and the full affected target set at handoff. At minimum:

- documentation-only: resolve local links, inspect terminology/traceability,
  and run configured Markdown checks when present;
- Go changes: `gofmt`, affected tests, `make test`, and `make lint`;
- concurrency/scanner changes: `make test-race`, `make test-stress`, and
  `make test-scale` when resource bounds are affected;
- provider or format changes: shared contracts, format tests, and
  `make test-integration`;
- semantic changes: affected format, inventory, API, and rendering tests;
- runtime/image changes: redaction tests and `make test-container`;
- release work: `make release-check`.

If a required command cannot run, report exactly what ran, what did not, and
why. Never describe an unrun check as passing.

## Documentation conventions

- Documentation lives in `web/` (reader-facing) and this file (contributor
  rules). Nowhere else. Do not add a `docs/` tree.
- Lead with user-observable behavior and evidence limits.
- Use “structurally usable backup” and “observed recovery coverage”. Do not
  introduce stronger synonyms casually.
- Keep examples synthetic and credentials obviously fake.
- Prefer describing behavior and naming the package or test that establishes
  it, over restating semantics that can drift.
- Time-sensitive upstream statements cite official Barman, pgBackRest,
  PostgreSQL, CloudNativePG, provider, or Go sources and record the checked
  version/date where compatibility depends on it.
- Update configuration tables, README usage, and operator mapping together.
- A milestone or slice is not done while documentation describes behavior that
  the binary does not ship.

## Handoff expectations

At the end of a task, report:

- the user-visible outcome;
- files and contracts changed;
- tests and checks that passed;
- tests/checks not run and why;
- remaining `unknown`, risk, or follow-up decision; and
- commits created only if explicitly requested.

Do not call work complete merely because code compiles. Completion means the
requested vertical behavior is present, negative cases fail safely, applicable
checks pass, and documentation matches reality.
