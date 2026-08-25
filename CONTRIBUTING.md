# Contributing

Thanks for considering a contribution! This page explains how to build the
project, what the tests and checks expect, and the invariants every change must
keep.

ObjectStoreViewer is an *evidence viewer*, not a restore verifier. Most review
comments you will receive are about the difference between the two — read the
[evidence model](web/docs/overview/evidence-model.md) and the semantic tests in
`internal/formats/barmancloud` before you argue with one.

**The code is the truth.** There is no specification set. The code and its tests
define the behavior; `web/` explains that behavior for
readers. Where they disagree, the code is right and the page is a bug. Do not add
a `docs/` tree — contributor rules go in [`AGENTS.md`](AGENTS.md), reader-facing
explanation goes in `web/`.

## Releases

`main` is the only long-lived branch. A release is a `vX.Y.Z` tag on `main`,
and the release workflow builds every artifact from that tag, so the tag is the
complete record of what shipped.

There are no maintenance branches and no backports: a fix reaches users by
landing on `main` and appearing in the next tag. Older tags stay readable and
buildable, but they do not receive further fixes. The normative policy is in
[`AGENTS.md`](AGENTS.md#releases).

## Development environment

- Go 1.26+ (both modules pin the toolchain to 1.26.6)
- `make`, `git`, and `ripgrep` (`rg`, used by the read-only and boilerplate
  checks)
- `docker` for the container, fixture, and provider tests
- network access for the pinned test images, the Go vulnerability database,
  and `npm ci` when building the documentation site

The repository contains two Go modules:

| Module | Path | Purpose |
|---|---|---|
| `github.com/fyannk/pgObjectStoreViewer` | repository root | the application, providers, formats, and runtimes |
| `github.com/fyannk/pgObjectStoreViewer/api` | [`api/`](api) | the dependency-light `evidence.objectstoreviewer.io/v1alpha1` wire vocabulary |

The `api` module must keep **zero external module dependencies**;
`make check-api` asserts it. Never import a provider SDK, a format
parser, an HTTP server, or a compression library from it.

## Everyday commands

```bash
make build                # build bin/objectstoreviewer
make test                 # unit tests, both modules
make test-race            # race detector, both modules
make test-stress          # repeated concurrency and lifecycle tests
make check-api            # dependency boundary and generated API drift
make test-integration     # pinned Barman and provider-emulator journeys
make test-scale           # million-object bound and render benchmark
make test-container       # restricted standalone and sidecar profiles
make lint                 # gofmt, vet, boilerplate, read-only scan, reader
                          # surface, and golangci-lint with gosec enforced
make golangci-lint        # golangci-lint alone, both modules
make vuln                 # pinned govulncheck v1.6.0 (needs network)
make check                # complete local verification without Docker
make package              # amd64/arm64 binaries plus SHA256SUMS
make docker-build         # build the distroless image
make docs                 # build the Docusaurus site under web/
make release-check        # all checks plus release artifacts
```

`make check` must pass before you consider a change done. If you touched the
evidence wire types, also run `make generate-evidence-artifacts` and commit the
regenerated `api/evidence/v1alpha1/schema.json` and wire goldens in the same
change.

## Check layers

The Makefile exposes capability-based layers instead of slice or milestone
aliases. The code and tests remain authoritative; these targets provide stable,
reproducible entry points for local development and CI.

```bash
make check              # hermetic checks, including repeated race cases
make test-integration   # generated Barman fixture plus all provider journeys
make test-scale         # resource ceilings
make test-container     # restricted runtime profiles
make release-check      # all checks, multi-arch smoke tests, and supply chain
```

The provider, fixture, container, multi-arch, and supply-chain checks need Docker,
network access to the pinned images, and (for local arm64 emulation)
privileged `binfmt` setup. Release outputs are retained under `dist/` and
`artifacts/release/`; ordinary CI logs stay attached to their exact commit by
the CI system.

A change that alters behavior updates, in the same commit: the code, the tests,
the affected check target, and the documentation (`web/docs/` and the root
`README.md`).

## Repository invariants

These come from [`AGENTS.md`](AGENTS.md), which is the complete and normative
list. A change that violates one is a bug:

1. **Read-only by construction.** Domain code may use only list, bounded
   get/open, and stat/head through the narrow read-store interface. No write,
   copy, delete, multipart, lifecycle, tagging, or restore operation may exist
   anywhere in `cmd/` or `internal/` — `hack/check-readonly.sh` fails the build
   on mutation-shaped identifiers and on forbidden actions in the IAM policy
   templates.
2. **Never overstate evidence.** `healthy`, `warning`, `unhealthy`, and
   `unknown` keep exactly the meanings the semantic tests assert. Any required
   `unknown` input makes the dependent conclusion `unknown`, and a partial scan
   can never produce a healthy result. Incomplete, stale, unsupported,
   truncated, or failed evidence can never become healthy. The phrases "restore
   verified", "restore guaranteed", "exact PITR window", and "restorable until"
   are prohibited.
3. **No secret disclosure.** Credentials, mounted secret values, provider
   authorization headers, signed URLs, SAS parameters, and raw
   credential-bearing SDK errors never reach logs, responses, HTML, errors,
   tests, fixtures, or snapshots. Redaction happens at the adapter boundary.
4. **No raw backup content in the UI.** Parse bounded format metadata
   internally; render allowlisted fields only.
5. **Bounded and cancelable.** HTTP handlers never scan. Listing is paginated,
   concurrency is bounded, safety ceilings fail to `unknown`, and every
   provider operation respects a `context.Context`.
6. **Keys are opaque.** Never map object keys onto filesystem paths, and never
   let a key or cursor escape its configured store, destination prefix, and
   scope.
7. **Provider-neutral, format-isolated.** AWS/Azure/GCS SDK types stay inside
   `internal/provider/<name>`. Barman Cloud and pgBackRest keep their typed
   catalogs inside `internal/formats/<name>`.
8. **Deterministic output.** Stable sort orders, UTC timestamps, exact integer
   byte counts, explicit unknowns, injected clocks.
9. **The proxy is the trust boundary.** The application authenticates nothing.
   `TRUSTED_USER_HEADER` is display-only.
10. **Downloads stay opt-in** and are not built yet; no incidental raw-object
    route may appear before that feature ships with its own threat model.
11. **Health is not catalog health.** `/healthz` is process liveness; `/readyz`
    is valid configuration plus a recent lightweight prefix-scoped reachability
    result. Neither runs a scan.
12. **A failed refresh never replaces good evidence.** Publish snapshots
    atomically; keep the last complete one and mark it stale.
13. **Repository format is explicit.** No auto-detection, no fallback between
    formats.
14. **No generic backup model that erases invariants.** A pgBackRest reference
    chain is not a Barman parent field.
15. **License boilerplate** (`hack/boilerplate.go.txt`) on every new Go file.

16. **Enforced static analysis.** `golangci-lint` with `gosec` must pass on both
    modules. A guarded integer conversion carries a `#nosec G115 -- <invariant>`
    comment naming the bound that makes it safe; do not silence a finding you
    have not actually verified.

The single-repository configuration contract was frozen on 2026-07-27. Changing
it requires matching compatibility, documentation, and test updates.

## Work in vertical slices

Deliver the smallest vertical behavior that satisfies the request. A complete
change normally crosses configuration, domain behavior, the provider/fake
boundary, HTTP or API output, failure cases, tests, checks, and documentation.

Before coding: read the existing implementation and its tests, and list the
evidence rules affected. The full definition of done is in
[Development](web/docs/development.md#definition-of-done); unbuilt work and its
acceptance conditions are sketched in the [roadmap](web/docs/roadmap.md), which
is intent, not contract.

## Layout

- `api/evidence/v1alpha1/` — cross-project wire types, schema, wire goldens.
- `cmd/objectstoreviewer/` — the single binary: server runtimes and `probe`.
- `internal/application/` — runtime composition for both modes.
- `internal/config/` — the frozen configuration contract and its validation.
- `internal/store/` — the narrow read-only store interface and its contract
  tests.
- `internal/provider/{s3,azure,gcs,cursor}/` — provider adapters; SDK types stop
  here.
- `internal/formats/{barmancloud,pgbackrest}/` — format-owned catalogs, WAL
  classification, timelines, recovery.
- `internal/inventory/` — the background scanner and the atomically published
  snapshot.
- `internal/evidence/`, `internal/evidenceapi/` — evidence projection, the
  immutable publication engine, the authenticated Unix-socket channel, and the
  probe.
- `internal/web/` — the standalone HTML surface (`/`, `/wals`).
- `internal/redact/`, `internal/fault/`, `internal/readiness/` — redaction, the
  non-sensitive failure vocabulary, and probe state.
- `deploy/` — the Kubernetes example and the read-only IAM policy templates.
- `hack/` — integration/container harnesses and repository checks.
- `web/` — the Docusaurus documentation site (reader-facing explanation, not specification).

## Documentation

User-facing behavior changes must come with doc updates in `web/docs/`. Build the
site locally:

```bash
cd web
npm ci
npm start
```

`make docs` runs `npm ci`, the TypeScript check, and a production build; broken
links fail it.

## Git workflow

- One focused change per commit; keep generated files with their source
  changes.
- Commit messages explain the user-visible behavior or engineering change.
- Open a merge request against `main`.

## License

By contributing you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
