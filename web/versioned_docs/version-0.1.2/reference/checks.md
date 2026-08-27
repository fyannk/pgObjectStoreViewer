---
sidebar_position: 8
---

# Verification

The code and its tests define ObjectStoreViewer's behavior. The Makefile groups
those tests and structural checks by the environment they need; it does not
maintain separate slice, milestone, or acceptance-ID taxonomies.

## Local checks

| Target | What it runs |
|---|---|
| `make build` | build `bin/objectstoreviewer` |
| `make test` | hermetic unit tests in both Go modules |
| `make test-fuzz` | mutate bounded Barman metadata, timeline, WAL-name, and signed-cursor inputs for five seconds per target with one worker |
| `make test-race` | the complete unit suite under the race detector |
| `make test-stress` | repeated publication, channel, runtime, and probe lifecycle tests under the race detector |
| `make check-api` | the API module's zero-dependency boundary and generated-artifact drift check |
| `make lint` | gofmt, vet, license boilerplate, read-only source/policy scan, read-store surface check, golangci-lint, and gosec |
| `make vuln` | `govulncheck`, pinned by `GOVULNCHECK_VERSION` in the `Makefile` |
| `make check` | every non-Docker check above |
| `make docs` | documentation type checking, broken-link checking, and production build |

`make check` is the normal completion gate for a code change. Run the narrowest
affected package tests while developing, then run `make check` before handoff.

## Docker and resource checks

| Target | What it runs |
|---|---|
| `make test-barman-fixtures` | regenerates the committed metadata with pinned Barman 3.19.1 and requires byte-identical output |
| `make test-s3` / `test-azure` / `test-gcs` | runs one pinned provider-emulator journey |
| `make test-provider-parity` | runs the shared journey over pinned MinIO, Azurite, and fake-gcs-server environments and compares byte-identical normalized output |
| `make test-integration` | the Barman fixture and provider-parity checks together |
| `make test-scale` | the million-object bounded-memory scan plus the cached-render benchmark |
| `make test-container` | standalone and sidecar images under restricted ordinary/OpenShift-style profiles |
| `make test-multiarch` | amd64 and arm64 image builds and restricted-runtime smoke tests |
| `make supply-chain` | SPDX SBOM, image digest, license report, and high/critical vulnerability result |
| `make release-check` | local checks, docs, integration, scale, container, packaging, multi-architecture, and supply-chain checks |

Docker and network access to the pinned images are required for integration,
container, multi-architecture, and supply-chain work. Local arm64 emulation also
needs privileged `binfmt` setup. Release binaries are written to `dist/`, while
retained image and supply-chain output is written to `artifacts/release/`.
Ordinary CI logs are already tied to the exact commit by the CI system and are
not copied into a second checksummed log bundle.

## Required semantic cases

The semantic suites include at least: successful, in-progress, failed, missing,
malformed, and unsupported catalog outcomes; missing expected artifacts or
dependencies and duplicate objects; supported and unknown compressed
classifications; partial WALs, malformed names, one missing segment, and a large
missing range; a non-default WAL segment size; a valid timeline switch, missing
history, malformed history, and a cycle; an object appearing between scan pages;
retention deletion during a scan; provider pagination failure and
maximum-object truncation; and a stale previous snapshot after a failed
refresh.

Barman cases additionally cover completed, `STARTED`, `FAILED`, missing, and
malformed per-backup `backup.info`; missing main data or tablespaces; supported
split and compression variants; timeline history; and supported backup types.

Incomplete, stale, unsupported, truncated, or failed evidence must remain
`unknown`. A first complete gap observation is only a candidate; an incomplete
refresh neither confirms nor clears it; the next complete observation confirms
it; and a filled range clears it. These assertions live in the package tests,
where new behavior must add its pathological cases.

## CI

Every pull request runs build/unit/fuzz/race/stress/API checks, lint and static
analysis, vulnerability scanning, documentation, Barman/provider integration,
the bounded-resource suite, and both restricted container profiles. A push to
`main` additionally builds release binaries, checks amd64 and arm64 images, and
retains the SBOM, image digest, license report, and vulnerability result. After
that complete CI workflow succeeds for an annotated `v*` tag, the release
workflow publishes the binaries and reports to GitHub Releases and a
multi-architecture image to `ghcr.io/fyannk/pgobjectstoreviewer`. The registry
image carries OCI SBOM and provenance attestations; GitHub attestations cover
the image and standalone binaries. It also publishes the nested Go module as
the annotated `api/vX.Y.Z` tag at the exact root release commit.
