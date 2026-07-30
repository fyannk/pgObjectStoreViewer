# ObjectStoreViewer evidence API types

This separate Go module contains the dependency-light
`evidence.objectstoreviewer.io/v1alpha1` wire vocabulary shared by
ObjectStoreViewer, pgConsole, and pgtoolbox. It intentionally imports no main
ObjectStoreViewer package, cloud provider SDK, compression library, repository
parser, HTTP server, or credential implementation.

The module currently provides the producer contract artifacts behind tests:

- exact health, readiness, snapshot, error, and Barman collection wire types;
- conservative validators for states, nullable evidence, deterministic order,
  bounds, and the initial S3/Barman profile;
- unknown-details-tag handling that discards unrecognized payloads; and
- the shared S3 canonicalization and destination fingerprint algorithm with
  cross-project golden vectors;
- the generated Draft 2020-12 [`schema.json`](evidence/v1alpha1/schema.json);
  and
- deterministic JSON wire goldens under
  [`testdata/wire`](evidence/v1alpha1/testdata/wire).

The main module's provider-free `internal/evidenceapi` package projects its
validated immutable inventory into these types and owns the process-local
immutable publication and pagination engine plus the authenticated private
channel. It is kept outside this module so the cross-project contract cannot
import scanners, format parsers, HTTP, or runtime implementations.

This module and its committed goldens are the authority on the wire contract.
The reader-facing description lives at
[`web/docs/reference/evidence-api.md`](../web/docs/reference/evidence-api.md)
and its [resource reference](../web/docs/reference/evidence-api-resources.md).

The producer endpoint is exposed only in explicit `pgconsole-sidecar` mode, with
scanner generations wired into the immutable publication engine, and
`make test-container` checks the producer-owned container
filesystem profiles. A semantic version tag, a pgConsole consumer, an operator
Pod profile, and a listed runtime pair still belong to cross-project
integration; the executable producer alone does not claim a qualified or
shippable pair.

Regenerate the schema and wire goldens from the repository root with:

```sh
make generate-evidence-artifacts
```

`make check-api` fails when committed generated output has drifted. The unit
suite compiles the schema, validates every golden, and checks Go JSON round
trips and additive-field behavior.

Run the module tests with:

```sh
go test ./...
```

From the repository root, `make check-api` additionally checks that the module
has no external module dependencies. `make test` checks that
ObjectStoreViewer's live evidence vocabulary still uses these definitions.
