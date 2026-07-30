#!/bin/sh
set -eu

image=${1:-objectstoreviewer:dev}
helper_image=${HELPER_IMAGE:-alpine:3.22@sha256:7c8cb692ae09657cbc4a3f3cbd0e8d5a2690ba38386aaaf252dbb060bf5eb2e6}
helper_platform=${HELPER_PLATFORM:-linux/amd64}
test_prefix="osv-evidence-sidecar-$$"
containers=""
volumes=""

cleanup() {
  for container_name in $containers; do
    docker rm -f "$container_name" >/dev/null 2>&1 || true
  done
  for volume_name in $volumes; do
    docker volume rm "$volume_name" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT INT TERM

run_profile() {
  profile=$1
  viewer_uid=$2
  viewer_gid=$3
  consumer_uid=$4
  consumer_gid=$5
  fs_group=$6

  viewer_name="$test_prefix-$profile-viewer"
  socket_volume="$test_prefix-$profile-socket"
  token_volume="$test_prefix-$profile-token"
  credential_volume="$test_prefix-$profile-credentials"
  containers="$viewer_name $containers"
  volumes="$socket_volume $token_volume $credential_volume $volumes"

  docker volume create "$socket_volume" >/dev/null
  docker volume create "$token_volume" >/dev/null
  docker volume create "$credential_volume" >/dev/null

  docker run --rm \
    --platform "$helper_platform" \
    --network none \
    --user 0:0 \
    --env PROOF_FS_GROUP="$fs_group" \
    --mount "type=volume,source=$socket_volume,target=/socket" \
    --mount "type=volume,source=$token_volume,target=/token" \
    --mount "type=volume,source=$credential_volume,target=/credentials" \
    "$helper_image" /bin/sh -eu -c '
      chown root:"$PROOF_FS_GROUP" /socket /token /credentials
      chmod 2770 /socket
      chmod 0750 /token /credentials
      dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64 | tr "+/" "-_" | tr -d "=\n" > /token/evidence-token
      dd if=/dev/urandom bs=24 count=1 2>/dev/null | base64 | tr -d "\n" > /credentials/aws-access-key-id
      dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64 | tr -d "\n" > /credentials/aws-secret-access-key
      chown root:"$PROOF_FS_GROUP" /token/evidence-token /credentials/aws-access-key-id /credentials/aws-secret-access-key
      chmod 0440 /token/evidence-token /credentials/aws-access-key-id /credentials/aws-secret-access-key
    '

  docker run --detach \
    --name "$viewer_name" \
    --read-only \
    --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m \
    --network none \
    --user "$viewer_uid:$viewer_gid" \
    --group-add "$fs_group" \
    --cap-drop ALL \
    --security-opt no-new-privileges \
    --mount "type=volume,source=$socket_volume,target=/var/run/objectstoreviewer" \
    --mount "type=volume,source=$token_volume,target=/var/run/secrets/objectstoreviewer,readonly" \
    --mount "type=volume,source=$credential_volume,target=/var/run/secrets/objectstoreviewer-store,readonly" \
    --env RUNTIME_MODE=pgconsole-sidecar \
    --env REPOSITORY_FORMAT=barman-cloud \
    --env PROVIDER=s3 \
    --env "DESTINATION_PATH=s3://synthetic-$profile/repository" \
    --env BARMAN_SERVER_NAMES=cluster \
    --env EVIDENCE_TOKEN_FILE=/var/run/secrets/objectstoreviewer/evidence-token \
    --env CNPG_CLUSTER_NAMESPACE=database-team \
    --env "CNPG_CLUSTER_UID=00000000-0000-0000-0000-$profile" \
    --env CNPG_CLUSTER_NAME=cluster \
    --env STORE_CREDENTIAL_MODE=static-files \
    --env AWS_ACCESS_KEY_ID_FILE=/var/run/secrets/objectstoreviewer-store/aws-access-key-id \
    --env AWS_SECRET_ACCESS_KEY_FILE=/var/run/secrets/objectstoreviewer-store/aws-secret-access-key \
    --env AWS_REGION=us-east-1 \
    --env STORE_REQUEST_TIMEOUT=1s \
    "$image" >/dev/null

  attempt=0
  while [ "$attempt" -lt 40 ]; do
    if docker exec "$viewer_name" /objectstoreviewer probe >/dev/null 2>&1; then
      break
    fi
    attempt=$((attempt + 1))
    sleep 0.25
  done
  if [ "$attempt" -eq 40 ]; then
    docker logs "$viewer_name" >&2
    return 1
  fi

  test "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$viewer_name")" = "true"
  test "$(docker inspect --format '{{.Config.User}}' "$viewer_name")" = "$viewer_uid:$viewer_gid"
  test "$(docker inspect --format '{{.HostConfig.NetworkMode}}' "$viewer_name")" = "none"
  test "$(docker inspect --format '{{.HostConfig.PidMode}}' "$viewer_name")" = ""
  test "$(docker inspect --format '{{.HostConfig.SecurityOpt}}' "$viewer_name")" = "[no-new-privileges]"
  test "$(docker inspect --format '{{.HostConfig.CapDrop}}' "$viewer_name")" = "[ALL]"
  test -n "$(docker inspect --format '{{index .HostConfig.Tmpfs "/tmp"}}' "$viewer_name")"
  test "$(docker inspect --format '{{range .Mounts}}{{println .Destination}}{{end}}' "$viewer_name" | wc -l)" -eq 4
  docker inspect --format '{{range .HostConfig.GroupAdd}}{{println .}}{{end}}' "$viewer_name" | grep -qx "$fs_group"
  test "$(docker inspect --format '{{json .HostConfig.PortBindings}}' "$viewer_name")" = "{}"
  test "$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/run/objectstoreviewer"}}{{.RW}}{{end}}{{end}}' "$viewer_name")" = "true"
  test "$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/run/secrets/objectstoreviewer"}}{{.RW}}{{end}}{{end}}' "$viewer_name")" = "false"
  test "$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/run/secrets/objectstoreviewer-store"}}{{.RW}}{{end}}{{end}}' "$viewer_name")" = "false"

  docker run --rm \
    --platform "$helper_platform" \
    --network none \
    --user 0:0 \
    --env PROOF_FS_GROUP="$fs_group" \
    --mount "type=volume,source=$socket_volume,target=/socket,readonly" \
    --mount "type=volume,source=$token_volume,target=/token,readonly" \
    "$helper_image" /bin/sh -eu -c '
      test "$(stat -c %a /socket)" = 2770
      test "$(stat -c %g /socket)" = "$PROOF_FS_GROUP"
      test -S /socket/evidence.sock
      test "$(stat -c %a /socket/evidence.sock)" = 660
      test "$(stat -c %g /socket/evidence.sock)" = "$PROOF_FS_GROUP"
      test -f /token/evidence-token
      test ! -L /token/evidence-token
      test "$(stat -c %a /token/evidence-token)" = 440
      test "$(stat -c %g /token/evidence-token)" = "$PROOF_FS_GROUP"
    '

  docker run --rm \
    --read-only \
    --network none \
    --user "$consumer_uid:$consumer_gid" \
    --group-add "$fs_group" \
    --cap-drop ALL \
    --security-opt no-new-privileges \
    --mount "type=volume,source=$socket_volume,target=/var/run/objectstoreviewer" \
    --mount "type=volume,source=$token_volume,target=/var/run/secrets/objectstoreviewer,readonly" \
    --env EVIDENCE_TOKEN_FILE=/var/run/secrets/objectstoreviewer/evidence-token \
    --entrypoint /objectstoreviewer \
    "$image" probe >/dev/null

  if docker run --rm --platform "$helper_platform" --network "container:$viewer_name" "$helper_image" /bin/sh -eu -c 'nc -z -w 1 127.0.0.1 3000'; then
    echo "sidecar profile exposed a TCP listener" >&2
    return 1
  fi

  if docker run --rm --read-only --network none --user "$consumer_uid:$consumer_gid" --group-add "$fs_group" --cap-drop ALL --security-opt no-new-privileges --entrypoint /objectstoreviewer "$image" probe >/dev/null 2>&1; then
    echo "unmounted caller reached evidence API" >&2
    return 1
  fi
  if docker run --rm --read-only --network none --user "$consumer_uid:$consumer_gid" --group-add "$fs_group" --cap-drop ALL --security-opt no-new-privileges --mount "type=volume,source=$socket_volume,target=/var/run/objectstoreviewer" --env EVIDENCE_TOKEN_FILE=/var/run/secrets/objectstoreviewer/evidence-token --entrypoint /objectstoreviewer "$image" probe >/dev/null 2>&1; then
    echo "socket-only caller reached evidence API" >&2
    return 1
  fi
  if docker run --rm --read-only --network none --user "$consumer_uid:$consumer_gid" --group-add "$fs_group" --cap-drop ALL --security-opt no-new-privileges --mount "type=volume,source=$token_volume,target=/var/run/secrets/objectstoreviewer,readonly" --env EVIDENCE_TOKEN_FILE=/var/run/secrets/objectstoreviewer/evidence-token --entrypoint /objectstoreviewer "$image" probe >/dev/null 2>&1; then
    echo "token-only caller reached evidence API" >&2
    return 1
  fi

  docker stop --signal TERM --time 10 "$viewer_name" >/dev/null
  test "$(docker inspect --format '{{.State.ExitCode}}' "$viewer_name")" = "0"
  docker run --rm --platform "$helper_platform" --network none --user 0:0 --mount "type=volume,source=$socket_volume,target=/socket,readonly" "$helper_image" /bin/sh -eu -c 'test ! -e /socket/evidence.sock'

  echo "producer sidecar container profile $profile passed"
}

run_profile ordinary 12345 12345 12346 12346 23456
run_profile openshift 1000710000 0 1000710001 0 1000711000

image_id=$(docker image inspect --format '{{.Id}}' "$image")
echo "sidecar restricted-runtime container tests passed for $image_id"
