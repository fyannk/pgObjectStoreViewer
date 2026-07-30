---
sidebar_position: 3
---

# Running the sidecar producer

A local walkthrough of `RUNTIME_MODE=pgconsole-sidecar`: the filesystem profile
is the hard part, so this tutorial builds it explicitly before starting the
producer.

:::warning
This is an integration-development exercise. No pgConsole/ObjectStoreViewer
runtime pair is qualified or advertised as supported — see
[the sidecar profile](../operations/sidecar.md).
:::

## 1. Create the socket directory

The producer refuses to create its socket unless the directory is a real
directory, setgid, at least `02770`, and group-owned by a group the process
belongs to.

```bash
sudo mkdir -p /var/run/objectstoreviewer
sudo chgrp "$(id -g)" /var/run/objectstoreviewer
sudo chmod 2770 /var/run/objectstoreviewer
```

In Kubernetes this is an `emptyDir` with `fsGroup` set; a conformant kubelet
delivers an effective `02777`, which the gate accepts. The surplus world bits are
inert because confinement comes from **which containers mount the volume**, never
from the mode.

## 2. Create the token file

Exactly 32 random bytes, unpadded base64url — 43 characters — in a regular
non-symlink file with mode exactly `0440`:

```bash
mkdir -p /tmp/osv-secrets
head -c 32 /dev/urandom | basenc --base64url | tr -d '=\n' > /tmp/osv-secrets/token
chmod 0440 /tmp/osv-secrets/token
wc -c < /tmp/osv-secrets/token    # must print 43
```

`0400` is rejected on purpose: the console container reads the same file under a
different UID sharing only the supplementary group.

## 3. Credentials

Sidecar mode has no ambient credential fallback — `STORE_CREDENTIAL_MODE` is
required and exact. Using the static-file mode:

```bash
printf '%s' 'local-access'       > /tmp/osv-secrets/aws-access-key-id
printf '%s' 'local-secret-value' > /tmp/osv-secrets/aws-secret-access-key
chmod 0400 /tmp/osv-secrets/aws-*
```

## 4. Start the producer

```bash
RUNTIME_MODE=pgconsole-sidecar \
REPOSITORY_FORMAT=barman-cloud \
PROVIDER=s3 \
DESTINATION_PATH=s3://backups/repository \
ENDPOINT_URL=http://127.0.0.1:9000 \
BARMAN_SERVER_NAMES=orders \
EVIDENCE_TOKEN_FILE=/tmp/osv-secrets/token \
CNPG_CLUSTER_NAMESPACE=database-team \
CNPG_CLUSTER_UID=2f12b7d1-7e8d-4c37-a68f-233efc5f3191 \
CNPG_CLUSTER_NAME=orders \
STORE_CREDENTIAL_MODE=static-files \
AWS_ACCESS_KEY_ID_FILE=/tmp/osv-secrets/aws-access-key-id \
AWS_SECRET_ACCESS_KEY_FILE=/tmp/osv-secrets/aws-secret-access-key \
AWS_REGION=us-east-1 \
./bin/objectstoreviewer
```

Reuse the MinIO from [getting started](getting-started.md). Note that
`BARMAN_SERVER_NAMES` takes **exactly one** name here, and the scanner is
confined to `orders/` beneath the destination even if the credential can read
more.

Confirm there is no TCP listener at all:

```bash
ss -ltnp | grep objectstoreviewer     # expect no output
```

## 5. Call the API

```bash
TOKEN=$(cat /tmp/osv-secrets/token)
SOCK=/var/run/objectstoreviewer/evidence.sock

curl -s --unix-socket "$SOCK" -H "Authorization: Bearer $TOKEN" \
  http://localhost/healthz

curl -s --unix-socket "$SOCK" -H "Authorization: Bearer $TOKEN" \
  http://localhost/api/v1alpha1/snapshot | jq .
```

Without the header you get `401 unauthenticated` and nothing else:

```bash
curl -s -o /dev/null -w '%{http_code}\n' --unix-socket "$SOCK" \
  http://localhost/api/v1alpha1/snapshot     # 401
```

## 6. Page a collection consistently

Collections are bound to the publication identity. Read `revision` from the
snapshot and pass it:

```bash
REV=$(curl -s --unix-socket "$SOCK" -H "Authorization: Bearer $TOKEN" \
  http://localhost/api/v1alpha1/snapshot | jq -r .revision)

curl -s --unix-socket "$SOCK" -H "Authorization: Bearer $TOKEN" \
  "http://localhost/api/v1alpha1/backups?revision=$REV&limit=100" | jq .
```

Wait for a refresh and reuse the stale `revision`: you get `409
publication-changed`, which is the contract telling you to discard the partial
assembly and restart from `/snapshot`. A cursor from one route is never valid on
another, and a restart invalidates every cursor with `invalid-request`.

## 7. The liveness probe

```bash
EVIDENCE_TOKEN_FILE=/tmp/osv-secrets/token ./bin/objectstoreviewer probe
echo $?     # 0 when the authenticated /healthz answered within two seconds
```

It prints no token, no response body, and no raw transport error — by design. Use
it as the kubelet **liveness** command, never as a readiness gate.

## 8. Clean up

```bash
# stop the producer with SIGTERM; the socket is removed on graceful shutdown
ls /var/run/objectstoreviewer/     # expect the socket to be gone
sudo rmdir /var/run/objectstoreviewer
rm -rf /tmp/osv-secrets
```

## Testing the denials

The container test exercises the failure modes this tutorial only touches:

```bash
make test-container
```

It runs the real image under ordinary and OpenShift-style distinct arbitrary
UIDs sharing only a supplementary `fsGroup`, pins the `02770` directory floor
with `0660`/`0440` socket and token modes, and asserts authorized access plus
unmounted, socket-only, and token-only denials, the absence of a TCP listener,
and graceful socket cleanup.
