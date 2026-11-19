#!/usr/bin/env bash
# Run formatting, vet and tests in the pinned Go image.
#
# The development machine and the CI runner are not guaranteed a Go toolchain,
# so the module is streamed into the same image the Dockerfile builds from. The
# source is sent on stdin rather than bind-mounted: on the self-hosted runner
# the checkout lives in a container and the host Docker daemon cannot resolve
# that path. One command keeps local runs and the pipeline identical.
set -euo pipefail

GO_VERSION="${GO_VERSION:-1.27.4}"
IMAGE="golang:${GO_VERSION}-alpine"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# COPYFILE_DISABLE stops macOS tar from emitting extended attributes, which GNU
# tar inside the image turns back into ._* files that break tooling.
COPYFILE_DISABLE=1 tar -cf - --exclude='._*' --exclude='.DS_Store' -C "${ROOT}" . \
  | docker run --rm -i \
  -w /src \
  "${IMAGE}" \
  sh -ec '
    tar -xf - -C /src
    unformatted="$(gofmt -l .)"
    if [ -n "$unformatted" ]; then
      echo "gofmt needed:" >&2
      echo "$unformatted" >&2
      exit 1
    fi
    go vet ./...
    go test ./...
  '
