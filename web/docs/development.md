---
sidebar_position: 6
---

# Development

Contribution rules live in
[`CONTRIBUTING.md`](https://github.com/fyannk/objectstoreviewer/blob/main/CONTRIBUTING.md),
and the complete normative rules for human and automated contributors are in
[`AGENTS.md`](https://github.com/fyannk/objectstoreviewer/blob/main/AGENTS.md).
This page is the short version, plus how this site relates to the code.

## Code is the truth

This site is **documentation, not specification**. The code and its tests define
what ObjectStoreViewer does; these pages explain that behavior in a readable
form.

So, when the two disagree:

- the code is right and the page is a bug — fix the page;
- never "fix" code to match a sentence here;
- a behavior change lands with its documentation update in the same change; and
- prose that cannot be traced to code or a test does not belong here. The one
  exception is [Decisions](architecture/decisions.md), which records *why* the
  code is shaped the way it is, and the [Roadmap](roadmap.md), which is
  explicitly labelled as intent.

There is no separate design-document set to keep in sync. That is deliberate: two
sources of truth is one too many.

## Toolchain

- Go 1.26+ (both modules pin toolchain 1.26.5)
- `make`, `git`, `ripgrep`
- Docker for the container, fixture, and provider checks
- Network access for pinned test images, the Go vulnerability database, and
  `npm ci` when building this site

## Two modules

| Module | Path | Rule |
|---|---|---|
| `github.com/fyannk/objectstoreviewer` | root | the application |
| `github.com/fyannk/objectstoreviewer/api` | `api/` | the wire vocabulary — **zero external module dependencies**, asserted by `make check-api` |

Never import a provider SDK, format parser, HTTP server, or credential
implementation from the `api` module.

## The loop

```bash
make build
make test
make check       # lint + unit + race + stress + API drift + vuln
```

`make lint` runs gofmt, `go vet`, the license-boilerplate check, the read-only
source and policy scan, the read-store surface guard, and **golangci-lint with
gosec enforced** across both modules.

If you touched the evidence wire types:

```bash
make generate-evidence-artifacts   # regenerate schema.json and wire goldens
make check-api         # fails on committed drift
```

Commit regenerated files with the change that caused them.

## Definition of done

A change is done when all of the applicable items hold. This is the checklist
reviews actually apply.

**Behavior** — acceptance cases cover successful, empty, malformed, timeout,
permission-denied, throttled, canceled, and incomplete paths; output is
deterministic (stable sorting, UTC, explicit unknowns, exact bytes); no handler
performs an unbounded list or waits for a scan; cancellation reaches every
provider request and background worker; new configuration has validation,
documentation, and redacted errors; format mismatch and unsupported capabilities
fail `unknown` without fallback.

**Security** — domain code sees only the narrow read-store interface; the static
forbidden-operation scan still rejects every mutation-shaped symbol; integration
credentials deny mutation; canary credentials injected into provider failures
appear in no body, header, log, metric, page, or link; HTML comes from
`html/template` with no unsafe HTML derived from store values; sensitive routes
send the no-store and security headers; download routes do not exist while
`ALLOW_DOWNLOAD=false`.

**Quality** — gofmt, lint, unit, race, integration, end-to-end, and vulnerability
checks pass for affected code; new domain decisions have table-driven tests and
pathological fixtures; provider adapters pass the shared contract suite; format
modules pass the evidence-envelope and isolation contract plus their own
compatibility suite; behavior and configuration are documented in the same
change; no TODO remains that could turn a healthy result into a false positive.

**Runtime and supply chain** — the binary serves health endpoints under an
arbitrary UID with a read-only root and only `/tmp` writable; SIGTERM drains HTTP
and cancels scans; the container drops capabilities and forbids privilege
escalation; the image contains no credentials, source tree, package manager, or
writable application directory; SBOM and vulnerability results are retained as CI
artifacts; amd64 and arm64 builds succeed for release milestones.

**Verification** — the affected [check layers](reference/checks.md) pass in CI,
and a reviewer can reproduce each one through a documented `make` target.

## Delivery principles

1. Build vertical behavior, not isolated layers: a change normally crosses
   configuration, domain model, provider boundary, HTTP or API output, tests, and
   docs.
2. Preserve uncertainty from the provider to the output. No layer may turn
   missing evidence into a zero value or a healthy state.
3. Keep provider SDK types at adapter boundaries.
4. Keep format semantics in isolated format modules; share only the evidence
   envelope and genuinely common primitives.
5. Make the cached path the normal path. Provider latency must not determine page
   latency.
6. Test negative security properties continuously, not only at release time.
7. Pin generated-fixture tooling and record versions with the fixtures.

## What a pull request states

The user-visible behavior it changes; the evidence rule it affects; acceptance
examples added or changed; checks run; and any intentionally deferred item —
which needs a follow-up issue and may never weaken a healthy or unhealthy
conclusion.

A milestone is not complete because the code merged. It is complete when the
journey is demonstrable, the negative cases fail safely, the relevant checks
are reproducible, and the documentation matches shipped behavior.

## This documentation site

```bash
cd web
npm ci
npm start        # dev server with hot reload
npm run build    # production build; broken links fail it
```

Or from the repository root, `make docs` runs `npm ci`, the TypeScript check, and
a production build.

Docs live in `web/docs/`, grouped by audience: `overview/`, `operations/`,
`architecture/`, `reference/`, and `tutorials/`. Category order comes from
`_category_.json`; page order from `sidebar_position` front matter. Pushing to
`main` publishes the site to GitHub Pages.

Prose rules that keep this site honest: never write a claim the evidence model
prohibits, always say which layer a statement belongs to, and prefer "observed"
and "structurally usable" over "valid" and "restorable".
