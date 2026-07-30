#!/bin/sh
set -eu

image='objectstoreviewer-barman-generator:3.19.1'
generated=$(mktemp)
cleanup() {
    rm -f "$generated"
}
trap cleanup EXIT INT TERM

docker build --quiet --file internal/provider/s3/testdata/barman-generator.Dockerfile --tag "$image" . >/dev/null
docker run --rm --volume "$PWD:/src:ro" "$image" \
    python /src/internal/formats/barmancloud/testdata/generate-completed.py > "$generated"
cmp "$generated" internal/formats/barmancloud/testdata/barman-3.19.1/completed/backup.info
printf 'Barman fixture check passed: committed metadata is byte-identical to Barman %s output\n' \
    "$(docker run --rm "$image" barman-cloud-backup --version)"
