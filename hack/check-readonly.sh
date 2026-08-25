#!/bin/sh
set -eu

forbidden='\b(Put|PutObject|Create|CreateBucket|Upload|Copy|CopyObject|Delete|DeleteObject|Batch|Multipart|Lifecycle|PutObjectTagging|CreateMultipartUpload)\b'
if rg -n --glob '*.go' --glob '!**/*_test.go' "$forbidden" cmd internal api; then
  echo "forbidden mutation-shaped identifier found in application source" >&2
  exit 1
fi

if rg -n 's3:(Put|Delete|Create|Replicate|Restore|AbortMultipart)' deploy/policies/s3-read-only.json; then
  echo "forbidden S3 action found in read-only policy" >&2
  exit 1
fi

if rg -n 'storage\.objects\.(create|delete|update|restore|setIamPolicy)' deploy/policies/gcs-read-only-role.json; then
  echo "forbidden GCS permission found in read-only policy" >&2
  exit 1
fi

if rg -ni 'containers/blobs/(write|delete|add/action)' deploy/policies/azure-read-only-role.json; then
  echo "forbidden Azure data action found in read-only policy" >&2
  exit 1
fi
