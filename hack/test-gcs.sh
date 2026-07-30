#!/usr/bin/env bash
set -euo pipefail
image='fsouza/fake-gcs-server@sha256:666f86b873120818b10a5e68d99401422fcf8b00c1f27fe89599c35236f48b4c'
name="objectstoreviewer-test-gcs-$$"
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT
docker run -d --rm --name "$name" -p 4443:4443 "$image" -scheme http >/dev/null
for _ in $(seq 1 30); do curl -fsS http://127.0.0.1:4443/storage/v1/b >/dev/null 2>&1 && break; sleep 1; done
curl -fsS -X POST 'http://127.0.0.1:4443/storage/v1/b?project=proof' -H 'Content-Type: application/json' -d '{"name":"objectstoreviewer-proof"}' >/dev/null
FAKE_GCS_ENDPOINT=http://127.0.0.1:4443 go test -tags=integration ./internal/provider/gcs -run '^TestFakeGCSJourney$' -count=1 -v
