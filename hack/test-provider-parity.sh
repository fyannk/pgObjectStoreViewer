#!/bin/sh
set -eu

result_dir=$(mktemp -d)
cleanup() {
    rm -rf "$result_dir"
}
trap cleanup EXIT INT TERM

go test ./internal/provider/providertest ./internal/provider/s3 ./internal/provider/azure ./internal/provider/gcs ./internal/provider/cursor ./internal/store/storetest

OBJECTSTOREVIEWER_NORMALIZED_RESULT="$result_dir/s3-normalized.json" ./hack/test-s3.sh
OBJECTSTOREVIEWER_NORMALIZED_RESULT="$result_dir/azure-normalized.json" ./hack/test-azure.sh
OBJECTSTOREVIEWER_NORMALIZED_RESULT="$result_dir/gcs-normalized.json" ./hack/test-gcs.sh

cmp "$result_dir/s3-normalized.json" "$result_dir/azure-normalized.json"
cmp "$result_dir/s3-normalized.json" "$result_dir/gcs-normalized.json"
normalized_sha256=$(sha256sum "$result_dir/s3-normalized.json" | awk '{print $1}')

printf 'provider parity passed: S3, Azure, and GCS normalized output sha256:%s\n' "$normalized_sha256"
