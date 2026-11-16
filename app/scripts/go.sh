#!/usr/bin/env bash
# Run formatting, vet and tests in the pinned Go image.
#
# The development machine and the CI runner are not guaranteed a Go toolchain,
# so the module is mounted into the same image the Dockerfile builds from. One
# command keeps local runs and the pipeline identical.
set -euo pipefail

GO_VERSION="${GO_VERSION:-1.27.4}"
IMAGE="golang:${GO_VERSION}-alpine"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

exec docker run --rm \
  -v "${ROOT}:/src" \
  -w /src \
  "${IMAGE}" \
  sh -ec '
    unformatted="$(gofmt -l .)"
    if [ -n "$unformatted" ]; then
      echo "gofmt needed:" >&2
      echo "$unformatted" >&2
      exit 1
    fi
    go vet ./...
    go test ./...
  '
