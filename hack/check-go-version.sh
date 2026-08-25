#!/bin/sh
# Go toolchain agreement across every place the version has to be written.
# It lives in go.mod, but a container image tag cannot be read from a module
# file and this repository ships two modules, so the same version is spelled
# three times. Dependabot moves the Dockerfile on its own schedule and moves
# neither module file with it, so the three drift silently — and a release
# whose binaries and container were built by different toolchains is not one
# the supply-chain attestations can honestly call a single build.
set -eu

status=0

read_toolchain() {
  v=$(sed -n 's/^toolchain go\([0-9][0-9.]*\)$/\1/p' "$1")
  if [ -z "$v" ]; then
    echo "no toolchain directive in $1" >&2
    exit 2
  fi
  echo "$v"
}

root=$(read_toolchain go.mod)
api=$(read_toolchain api/go.mod)

# "FROM ... golang:1.27.0-alpine@sha256:..." -> "1.27.0"
img=$(sed -n 's/.*golang:\([0-9][0-9.]*\)-alpine.*/\1/p' Dockerfile)
if [ -z "$img" ]; then
  echo "no golang builder image in Dockerfile" >&2
  exit 2
fi

if [ "$root" != "$api" ]; then
  echo "Go toolchain drift: go.mod says $root, api/go.mod says $api" >&2
  status=1
fi

if [ "$root" != "$img" ]; then
  echo "Go toolchain drift: go.mod says $root, Dockerfile says $img" >&2
  status=1
fi

if [ "$status" -ne 0 ]; then
  echo "bump whichever is behind so both modules and the container share a toolchain" >&2
  exit "$status"
fi

echo "Go toolchain agrees at $root"
