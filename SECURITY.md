# Security Policy

## Reporting a vulnerability

**Report privately. Do not open a public issue with reproduction details.**

Use GitHub's private vulnerability reporting:
[**Report a vulnerability**](https://github.com/fyannk/pgObjectStoreViewer/security/advisories/new).
It is enabled on this repository, the report stays private to the maintainers,
and it gives us a place to prepare a fix and an advisory before disclosure.

If you cannot use that form, contact the maintainer listed in
[`.github/CODEOWNERS`](.github/CODEOWNERS) through their GitHub profile and ask
for a private channel. Do not include the details in the first message.

### What to include

Follow the same redaction rules as an ordinary bug report — see
[collecting evidence for a bug report](https://fyannk.github.io/pgObjectStoreViewer/docs/operations/troubleshooting/#collecting-evidence-for-a-bug-report):

- the affected version or commit, the provider, and the repository format;
- the deployment shape (standalone, `pgconsole-sidecar`, container, Kubernetes);
- what an attacker can reach and what they gain;
- a minimal reproduction using **synthetic** fixtures.

Never include credentials, SAS parameters, signed URLs, destination URLs,
production bucket names, object keys, or raw provider SDK errors. A report that
contains real secrets is a second incident; we will ask you to rotate them.

### What to expect

This is a small project maintained on a best-effort basis.

| Stage | Target |
|---|---|
| Acknowledgement | 5 business days |
| Initial assessment | 10 business days |
| Fix or documented mitigation for a confirmed issue | 90 days |

We will keep you updated, credit you in the advisory unless you ask otherwise,
and coordinate the disclosure date with you. If a report is declined we will
explain why.

## Supported versions

Security fixes are provided for the latest published release. ObjectStoreViewer
is pre-1.0: only the most recent 0.x minor receives fixes, and there are no
backports to earlier ones.

Every release is a tag on `main`, so a fix ships by landing on `main` and
cutting the next tag. There are no maintenance branches. If you are running an
older 0.x, upgrading to the latest release is the supported path. See
[releases](CONTRIBUTING.md#releases).

## Scope

ObjectStoreViewer is a **read-only evidence viewer**. Its threat model is
described in full on the
[security page](https://fyannk.github.io/pgObjectStoreViewer/docs/operations/security/).
Read it before reporting — it decides whether a finding is a vulnerability or
the documented design.

### In scope

Anything that breaks one of the repository invariants in
[`AGENTS.md`](AGENTS.md#hard-rules--violating-any-of-these-is-a-bug), in
particular:

- **A mutation surface.** Any path by which the application can write, copy,
  delete, tag, restore, or otherwise modify an object store.
- **Secret disclosure.** Credentials, mounted secret values, authorization
  headers, signed URLs, SAS parameters, or raw credential-bearing SDK errors
  reaching a log, response, HTML page, error, or artifact.
- **Overstated evidence.** Input that makes incomplete, stale, truncated,
  failed, or unsupported evidence render as `healthy`.
- **Key or scope escape.** A key, cursor, or path that escapes the configured
  store, destination prefix, or scope — including path traversal through an
  object key.
- **Unbounded work.** Input that defeats a safety ceiling and lets a request or
  scan consume unbounded memory, time, or provider calls.
- **Sidecar channel.** Reaching the `pgconsole-sidecar` Unix socket without the
  pod-local bearer token, or a token comparison weakness.
- **Supply chain.** A flaw in the release, attestation, or container publishing
  pipeline that lets an unverified artifact be published.

### Out of scope

These are documented design, not vulnerabilities:

- **No authentication, authorization, or TLS in the application.** The
  application is not a trust boundary; an authenticating proxy is. Reaching the
  port directly is an operator misconfiguration.
- **`TRUSTED_USER_HEADER` is display-only** and never authorizes a request.
  Spoofing it changes a displayed name and nothing else.
- **`/healthz` and `/readyz` are unauthenticated** by design and expose no
  catalog content.
- Findings that require an attacker who already controls the process,
  credentials, the mounted secret files, or the object store contents that the
  viewer is asked to describe.
- Reports from automated scanners with no demonstrated impact on this code.
- Missing hardening headers or a lack of rate limiting on a port that is
  required to sit behind a proxy.

If you are unsure which side of that line a finding falls on, report it
privately anyway and say so.

## Verifying what you run

Releases ship binaries, `SHA256SUMS`, an SPDX SBOM, a license inventory, a
vulnerability report, and the immutable image digest. Container images and
release binaries carry provenance attestations:

```bash
gh attestation verify oci://ghcr.io/fyannk/pgobjectstoreviewer:v0.1.2 \
  -R fyannk/pgObjectStoreViewer
```

Pin images by digest in production. Grant the deployment credential list/get
only, using the templates under
[`deploy/policies/`](deploy/policies) — that credential is the layer that
survives a bug in the other two.
