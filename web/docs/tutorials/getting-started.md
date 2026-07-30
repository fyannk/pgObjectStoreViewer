---
sidebar_position: 1
---

# Getting started

A first inventory against a local MinIO, on your machine, in a few minutes. No
cluster, no cloud account.

## 1. Build the binary

```bash
git clone https://github.com/fyannk/objectstoreviewer
cd objectstoreviewer
make build
```

You need Go 1.26+. Docker is needed for step 2 and for integration tests, not for the
viewer itself.

## 2. Start a local S3

```bash
docker run --rm --detach --name osv-minio \
  --publish 9000:9000 \
  --env MINIO_ROOT_USER=local-access \
  --env MINIO_ROOT_PASSWORD=local-secret-value \
  minio/minio server /data
```

Create a bucket and a read-only user. Using `mc` from a container:

```bash
export MC_HOST_local=http://local-access:local-secret-value@127.0.0.1:9000
docker run --rm --network host --env MC_HOST_local \
  minio/mc mb local/backups
```

## 3. Put a Barman repository in it

If you already have a Barman Cloud repository, mirror a copy of it under
`backups/repository/`. Otherwise, the repository's integration harness builds a
genuine Barman 3.19.1 repository from a real PostgreSQL instance:

```bash
make test-s3
```

That target starts pinned MinIO and PostgreSQL containers, runs a real
`barman-cloud-backup` and WAL archive journey, and asserts the resulting
inventory. It is the fastest way to see honest evidence end to end, and it is
also the reference for what a valid layout looks like:

```text
backups/repository/<server>/base/<backup-id>/backup.info
backups/repository/<server>/base/<backup-id>/data.tar.gz
backups/repository/<server>/wals/<timeline+log>/<wal-name>
backups/repository/<server>/wals/<timeline>.history
```

## 4. Mount credentials as files

Static credentials are accepted **only** as mounted files — that is not
configurable:

```bash
mkdir -p /tmp/osv-credentials
printf '%s' 'local-access'       > /tmp/osv-credentials/access-key-id
printf '%s' 'local-secret-value' > /tmp/osv-credentials/secret-access-key
chmod 0400 /tmp/osv-credentials/*
```

In a real deployment these are keys of a read-only Secret volume, and the
credential itself is scoped to list and get under the prefix only — see the
[policy reference](../reference/policies.md).

## 5. Run the viewer

```bash
REPOSITORY_FORMAT=barman-cloud \
PROVIDER=s3 \
DESTINATION_PATH=s3://backups/repository \
ENDPOINT_URL=http://127.0.0.1:9000 \
AWS_REGION=us-east-1 \
AWS_ACCESS_KEY_ID_FILE=/tmp/osv-credentials/access-key-id \
AWS_SECRET_ACCESS_KEY_FILE=/tmp/osv-credentials/secret-access-key \
CATALOG_REFRESH_INTERVAL=30s \
./bin/objectstoreviewer
```

Then:

```bash
curl -s localhost:3000/healthz   # process liveness
curl -s localhost:3000/readyz    # ready once a prefix-scoped list succeeds
open http://localhost:3000/      # the inventory page
```

`CATALOG_REFRESH_INTERVAL=30s` is the minimum accepted value and makes
experimenting quicker; the default is `5m`.

## 6. Read the page honestly

Things worth noticing, because they are the product rather than rough edges:

- **The first render may be unknown.** Until the first background scan
  completes there is no generation, so totals are `null` — not `0`.
- **Totals are exact only after a complete scan.** If you set
  `MAX_OBJECTS_PER_SCAN` low enough to truncate, totals become unknown.
- **An empty prefix is ready.** Point `DESTINATION_PATH` at a prefix that does
  not exist and the viewer becomes ready and reports a complete, empty
  inventory. Ready plus empty usually means a wrong prefix.
- **A stopped MinIO does not erase evidence.** Stop the container and watch the
  next refresh fail: the last complete snapshot is retained and marked stale,
  and dependent states go unknown.

```bash
docker stop osv-minio     # watch /readyz and the freshness panel
docker start osv-minio    # the next complete scan clears the staleness
```

## 7. Explore WAL evidence

Open `http://localhost:3000/wals` and filter by server, class, timeline, and
WAL-name range. Classes are `segment`, `partial`, `history`, `backup-history`,
`duplicate`, and `unknown`.

If continuity reads `unknown`, that is usually a *correct* answer — the segment
size or PostgreSQL version context was unavailable, two complete
representations of one position exist, or a safety ceiling truncated the
evidence. See [the evidence model](../overview/evidence-model.md#wal-classification).

## 8. Clean up

```bash
docker rm -f osv-minio
rm -rf /tmp/osv-credentials
```

## Next steps

- [Other providers](other-providers.md) — the same walkthrough on Azurite and
  fake-gcs-server.
- [The pgConsole sidecar](pgconsole-sidecar.md) — run the producer profile
  locally.
- [Installation](../operations/installation.md) — the real deployment, with its
  proxy and network boundary.
