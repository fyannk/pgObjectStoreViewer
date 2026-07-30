<!--
Thanks for the contribution. CONTRIBUTING.md explains the build and the checks;
AGENTS.md is the normative list of invariants. A change that violates one of
them is a bug, not a tradeoff.
-->

## What changes, and why

<!-- The user-observable behavior before and after. Link the issue it closes. -->

## Evidence semantics

<!--
Delete this section only if the change cannot affect a reported conclusion.

Otherwise: which statuses, classifications, completeness values, failure
categories, or stop reasons change, and what happens when the required input is
missing, truncated, stale, or unsupported?
-->

## Verification

<!-- Record what you ran and what passed. Say what you did not run and why. -->

```
make check
```

## Checklist

- [ ] `make check` passes.
- [ ] Tests cover the new behavior **and** its negative cases.
- [ ] Read-only: no write, copy, delete, multipart, lifecycle, tagging, or
      restore operation was added anywhere in `cmd/` or `internal/`.
- [ ] No evidence is overstated: incomplete, stale, truncated, unsupported, or
      failed input still yields `unknown`, and a partial scan cannot produce a
      healthy result.
- [ ] No credentials, signed URLs, SAS parameters, destination URLs, real
      object keys, or raw provider SDK errors reach logs, responses, HTML,
      errors, tests, or fixtures.
- [ ] Provider SDK types stay inside `internal/provider/<name>`; format parsing
      stays inside `internal/formats/<name>`.
- [ ] New Go files carry the license boilerplate (`hack/boilerplate.go.txt`).
- [ ] Documentation in `web/docs/` and the root `README.md` matches the shipped
      behavior, in this same change.

<!-- Delete any that do not apply: -->

- [ ] Wire types changed: `make generate-evidence-artifacts` was run and the
      regenerated `api/evidence/v1alpha1/schema.json` and wire goldens are
      committed here.
- [ ] The `api/` module still has zero external dependencies (`make check-api`).
- [ ] Configuration changed: the reference table, `README.md` usage, and the
      operator mapping were updated together.
- [ ] Docker-backed checks that this change touches were run:
      `make test-integration` / `make test-scale` / `make test-container`.
