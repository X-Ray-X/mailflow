# syntax=docker/dockerfile:1

# Pinned toolchain. 1.27.4 is the stable release current on 2026-11-15 and is
# recorded in docs/api.md. Bump the builder, scripts/go.sh and that record
# together; nothing else may float.
ARG GO_VERSION=1.27.4
ARG ALPINE_VERSION=3.23

# Build on the host architecture and cross-compile: the homelab runner is
# arm64, but a developer machine may not be, and this keeps the build free of
# emulation either way.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/mailflow-api ./cmd/mailflow-api

FROM alpine:${ALPINE_VERSION}
# No toolchain in the runtime image. BusyBox supplies wget for the health check.
RUN adduser -D -H -u 10001 mailflow
COPY --from=build /out/mailflow-api /usr/local/bin/mailflow-api
USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/mailflow-api"]
