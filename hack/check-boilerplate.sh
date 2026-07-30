#!/bin/sh
set -eu

missing=0
for file in $(find cmd internal -type f -name '*.go' -print); do
  if ! head -n 1 "$file" | grep -q '^// Copyright 2026 The ObjectStoreViewer Authors$'; then
    echo "$file: missing Apache-2.0 boilerplate" >&2
    missing=1
  fi
done
exit "$missing"
