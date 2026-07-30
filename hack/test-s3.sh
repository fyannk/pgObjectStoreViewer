#!/bin/sh
set -eu

minio_image='minio/minio@sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e'
mc_image='minio/mc@sha256:fb8f773eac8ef9d6da0486d5dec2f42f219358bcb8de579d1623d518c9ebd4cc'
container="objectstoreviewer-test-minio-$$"
root_access='test-root-access'
root_secret='test-root-secret-canary'
viewer_access='test-viewer-access'
viewer_secret='test-viewer-secret-canary'
barman_image='objectstoreviewer-barman-generator:3.19.1'
postgres_image='postgres@sha256:fbcea1bd13b6a882cd6caa6b58db3ae5c102efe50ec625b3e2a5cbc50db5bfe4'
postgres_container="objectstoreviewer-test-postgres-$$"
fixture_dir=$(mktemp -d)

cleanup() {
    docker exec --user 0 "$postgres_container" chmod -R a+rwX /var/lib/postgresql/data >/dev/null 2>&1 || true
    docker rm -f "$container" >/dev/null 2>&1 || true
    docker rm -f "$postgres_container" >/dev/null 2>&1 || true
    rm -rf "$fixture_dir"
}
trap cleanup EXIT INT TERM

docker run --detach --name "$container" \
    --publish 127.0.0.1::9000 \
    --env "MINIO_ROOT_USER=$root_access" \
    --env "MINIO_ROOT_PASSWORD=$root_secret" \
    "$minio_image" server /data >/dev/null

port=$(docker port "$container" 9000/tcp | sed -n 's/.*://p')
endpoint="http://127.0.0.1:$port"
attempt=0
until curl --fail --silent "$endpoint/minio/health/ready" >/dev/null; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 100 ]; then
        docker logs "$container" >&2
        exit 1
    fi
    sleep 0.1
done

root_host="http://$root_access:$root_secret@127.0.0.1:$port"
docker run --rm --network host --env "MC_HOST_test=$root_host" "$mc_image" mb test/objectstoreviewer-proof >/dev/null
printf '%s' 'outside-root' | docker run --rm --interactive --network host --env "MC_HOST_test=$root_host" "$mc_image" pipe test/objectstoreviewer-proof/outside/ignored >/dev/null
docker build --quiet --file internal/provider/s3/testdata/barman-generator.Dockerfile --tag "$barman_image" . >/dev/null

docker run --detach --name "$postgres_container" \
    --publish 127.0.0.1::5432 \
    --env 'POSTGRES_PASSWORD=test-postgres-password' \
    --volume "$fixture_dir/pgdata:/var/lib/postgresql/data" \
    "$postgres_image" >/dev/null
postgres_port=$(docker port "$postgres_container" 5432/tcp | sed -n 's/.*://p')
attempt=0
until docker exec "$postgres_container" pg_isready --username postgres >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 100 ]; then
        docker logs "$postgres_container" >&2
        exit 1
    fi
    sleep 0.1
done

docker run --rm --network host \
    --volume "$fixture_dir/pgdata:/var/lib/postgresql/data:ro" \
    --env "AWS_ACCESS_KEY_ID=$root_access" \
    --env "AWS_SECRET_ACCESS_KEY=$root_secret" \
    --env 'AWS_DEFAULT_REGION=us-east-1' \
    --env 'PGPASSWORD=test-postgres-password' \
    "$barman_image" barman-cloud-backup \
        --endpoint-url "$endpoint" --addressing-style path --gzip \
        --host 127.0.0.1 --port "$postgres_port" --user postgres \
        s3://objectstoreviewer-proof/repository alpha >/dev/null

completed_id=$(docker run --rm --network host --env "MC_HOST_test=$root_host" "$mc_image" ls test/objectstoreviewer-proof/repository/alpha/base/ | awk '{print $NF}' | tr -d '/')
if [ -z "$completed_id" ]; then
    echo 'Barman did not generate a completed backup' >&2
    exit 1
fi

for mutation in started failed malformed; do
    docker run --rm --network host --env "MC_HOST_test=$root_host" "$mc_image" cp --recursive \
        "test/objectstoreviewer-proof/repository/alpha/base/$completed_id/" \
        "test/objectstoreviewer-proof/repository/alpha/base/$mutation/" >/dev/null
done
docker run --rm --network host --env "MC_HOST_test=$root_host" "$mc_image" cat \
    "test/objectstoreviewer-proof/repository/alpha/base/$completed_id/backup.info" \
    | sed 's/^status=DONE$/status=STARTED/' \
    | docker run --rm --interactive --network host --env "MC_HOST_test=$root_host" "$mc_image" pipe \
        test/objectstoreviewer-proof/repository/alpha/base/started/backup.info >/dev/null
docker run --rm --network host --env "MC_HOST_test=$root_host" "$mc_image" cat \
    "test/objectstoreviewer-proof/repository/alpha/base/$completed_id/backup.info" \
    | sed 's/^status=DONE$/status=FAILED/' \
    | docker run --rm --interactive --network host --env "MC_HOST_test=$root_host" "$mc_image" pipe \
        test/objectstoreviewer-proof/repository/alpha/base/failed/backup.info >/dev/null
printf '%s\n' 'malformed Barman metadata' \
    | docker run --rm --interactive --network host --env "MC_HOST_test=$root_host" "$mc_image" pipe \
        test/objectstoreviewer-proof/repository/alpha/base/malformed/backup.info >/dev/null
docker run --rm --network host --env "MC_HOST_test=$root_host" "$mc_image" cp \
    "test/objectstoreviewer-proof/repository/alpha/base/$completed_id/backup.info" \
    test/objectstoreviewer-proof/repository/alpha/base/missing-artifact/backup.info >/dev/null
docker run --rm --network host --env "MC_HOST_test=$root_host" "$mc_image" cp \
    "test/objectstoreviewer-proof/repository/alpha/base/$completed_id/data.tar.gz" \
    test/objectstoreviewer-proof/repository/alpha/base/missing-info/data.tar.gz >/dev/null

docker run --rm --network host \
    --env "AWS_ACCESS_KEY_ID=$root_access" \
    --env "AWS_SECRET_ACCESS_KEY=$root_secret" \
    --env 'AWS_DEFAULT_REGION=us-east-1' \
    --env "TEST_ENDPOINT=$endpoint" \
    "$barman_image" sh -c 'truncate -s 16777216 /tmp/000000010000000000000001 && barman-cloud-wal-archive --endpoint-url "$TEST_ENDPOINT" --addressing-style path s3://objectstoreviewer-proof/repository alpha /tmp/000000010000000000000001' >/dev/null
printf 'Barman fixture generator: %s\n' "$(docker run --rm "$barman_image" barman-cloud-backup --version)"
printf 'PostgreSQL fixture image: %s\n' "$postgres_image"
docker run --rm --network host --env "MC_HOST_test=$root_host" "$mc_image" admin user add test "$viewer_access" "$viewer_secret" >/dev/null
policy_file="$PWD/internal/provider/s3/testdata/minio-readonly-policy.json"
docker run --rm --network host --env "MC_HOST_test=$root_host" --volume "$policy_file:/policy.json:ro" "$mc_image" admin policy create test objectstoreviewer-readonly /policy.json >/dev/null
docker run --rm --network host --env "MC_HOST_test=$root_host" "$mc_image" admin policy attach test objectstoreviewer-readonly --user "$viewer_access" >/dev/null

OBJECTSTOREVIEWER_S3_INTEGRATION_ENDPOINT="$endpoint" \
OBJECTSTOREVIEWER_S3_INTEGRATION_ACCESS_KEY="$viewer_access" \
OBJECTSTOREVIEWER_S3_INTEGRATION_SECRET_KEY="$viewer_secret" \
go test -tags=integration ./internal/provider/s3 -run '^TestS3MinIOJourney$' -count=1 -v
