#!/bin/sh
set -eu

artifact_dir=${1:-artifacts/release}
binfmt_image='tonistiigi/binfmt@sha256:8f58e6214f4cc9dc83ce8f5acad1ece508eb6b20e696a8c1e9f274481982c541'
alpine_image='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce'

mkdir -p "$artifact_dir"
docker run --privileged --rm "$binfmt_image" --install arm64 >/dev/null
docker run --rm --platform linux/arm64 "$alpine_image" true

for architecture in amd64 arm64; do
    tag="objectstoreviewer:test-$architecture"
    docker buildx build --platform "linux/$architecture" --load --tag "$tag" .
    test "$(docker image inspect --format '{{.Architecture}}' "$tag")" = "$architecture"
    ./hack/test-container.sh "$tag"
done

amd64_id=$(docker image inspect --format '{{.Id}}' objectstoreviewer:test-amd64)
arm64_id=$(docker image inspect --format '{{.Id}}' objectstoreviewer:test-arm64)
printf '{"linux/amd64":"%s","linux/arm64":"%s"}\n' "$amd64_id" "$arm64_id" > "$artifact_dir/multiarch-digests.json"
printf 'multi-architecture container tests passed: linux/amd64=%s linux/arm64=%s\n' "$amd64_id" "$arm64_id"
