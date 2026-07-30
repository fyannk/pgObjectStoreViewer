---
sidebar_position: 2
---

# Azure and GCS

The same repository, the same semantics, a different adapter. This walkthrough
uses the emulators the integration tests use, so you can see parity rather than take it on
faith.

## Azure Blob Storage, with Azurite

```bash
docker run --rm --detach --name osv-azurite \
  --publish 10000:10000 \
  mcr.microsoft.com/azure-storage/azurite \
  azurite-blob --blobHost 0.0.0.0
```

Azurite's well-known development account works with the connection-string form.
Write it to a file — connection strings are secrets, so a file is the only
accepted channel:

```bash
mkdir -p /tmp/osv-credentials
cat > /tmp/osv-credentials/azure-connection-string <<'EOF'
DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;
EOF
chmod 0400 /tmp/osv-credentials/azure-connection-string
```

Create the container and upload a Barman layout under
`repository/`, then run:

```bash
REPOSITORY_FORMAT=barman-cloud \
PROVIDER=azure \
DESTINATION_PATH=azure://backups/repository \
AZURE_STORAGE_CONNECTION_STRING_FILE=/tmp/osv-credentials/azure-connection-string \
CATALOG_REFRESH_INTERVAL=30s \
./bin/objectstoreviewer
```

Note what is **not** there: no `ENDPOINT_URL`. That variable is S3-only and is
rejected for Azure — the Blob endpoint comes from the account or the connection
string.

### Production credentials

| Form | Variables |
|---|---|
| connection string | `AZURE_STORAGE_CONNECTION_STRING_FILE` |
| account + key | `AZURE_STORAGE_ACCOUNT` + `AZURE_STORAGE_ACCOUNT_KEY_FILE` |
| account + SAS | `AZURE_STORAGE_ACCOUNT` + `AZURE_STORAGE_SAS_TOKEN_FILE` |
| workload identity | `AZURE_STORAGE_ACCOUNT` alone (`DefaultAzureCredential`) |

Key and SAS are mutually exclusive. Workload identity is preferred.

## GCS

Against real GCS, configuration is just the coordinates plus a credential:

```bash
REPOSITORY_FORMAT=barman-cloud \
PROVIDER=gcs \
DESTINATION_PATH=gs://backups/repository \
GOOGLE_APPLICATION_CREDENTIALS=/tmp/osv-credentials/gcs.json \
CATALOG_REFRESH_INTERVAL=30s \
./bin/objectstoreviewer
```

Omit `GOOGLE_APPLICATION_CREDENTIALS` entirely to use Application Default
Credentials / GKE workload identity, which is the preferred mode.

:::note There is no GCS endpoint override
`ENDPOINT_URL` is **S3-only** and is rejected for GCS — the configuration
contract deliberately exposes no endpoint override for Azure or GCS. So you
cannot point the shipped binary at fake-gcs-server through configuration the way
you can at MinIO.
:::

The supported way to exercise the GCS adapter locally is therefore the
repository's own integration test, which starts a pinned fake-gcs-server and
drives the adapter with an internal endpoint:

```bash
make test-gcs
```

Under the hood it runs the pinned `fsouza/fake-gcs-server` image and executes
the journey test with `FAKE_GCS_ENDPOINT=http://127.0.0.1:4443`. The same
pattern applies to Azure, whose test uses
`AZURITE_BLOB_ENDPOINT=http://127.0.0.1:10000/devstoreaccount1` — the Azurite
walkthrough above works with the plain binary only because a *connection string*
legitimately carries its own `BlobEndpoint`.

### Production credentials

| Form | Variables |
|---|---|
| service-account JSON | `GOOGLE_APPLICATION_CREDENTIALS` pointing at a mounted, valid JSON file |
| workload identity | nothing — Application Default Credentials |

## Parity is a build gate, not a hope

All three adapters implement one shared contract suite, and one shared six-state
Barman journey runs against pinned MinIO, Azurite, and fake-gcs-server images.
The parity test then requires the normalized semantic output to be
**byte-identical**:

```bash
make test-provider-parity
```

It writes `s3-normalized.json`, `azure-normalized.json`, and
`gcs-normalized.json` into a temporary directory, compares them byte for byte,
and prints the shared SHA-256. Any place where a provider's behavior leaked into
a conclusion appears there as a diff.

Run individual journeys with:

```bash
make test-s3      # pinned MinIO
make test-azure   # pinned Azurite
make test-gcs     # pinned fake-gcs-server
```

## Cleanup

```bash
docker rm -f osv-azurite osv-fake-gcs
rm -rf /tmp/osv-credentials
```
