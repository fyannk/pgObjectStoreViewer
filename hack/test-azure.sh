#!/usr/bin/env bash
set -euo pipefail
image='mcr.microsoft.com/azure-storage/azurite@sha256:647c63a91102a9d8e8000aab803436e1fc85fbb285e7ce830a82ee5d6661cf37'
name="objectstoreviewer-test-azure-$$"
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT
docker run -d --rm --name "$name" -p 10000:10000 "$image" azurite-blob --blobHost 0.0.0.0 --inMemoryPersistence --disableTelemetry --skipApiVersionCheck >/dev/null
for _ in $(seq 1 30); do curl -s -o /dev/null http://127.0.0.1:10000/devstoreaccount1?comp=list && break; sleep 1; done
AZURITE_BLOB_ENDPOINT=http://127.0.0.1:10000/devstoreaccount1 go test -tags=integration ./internal/provider/azure -run '^TestAzuriteJourneyWithReadOnlySAS$' -count=1 -v
