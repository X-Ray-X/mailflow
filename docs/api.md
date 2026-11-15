# Mailflow API

The Go service that publishes Mailflow's HTTP contract. It adapts to the mail
server through JMAP and translates the result into Mailflow identifiers and
vocabulary; nothing in a response is vendor-shaped.

## Runtime and build

| Item | Value |
|---|---|
| Go release | 1.27.4 (stable on 2026-11-15) |
| Builder image | `golang:1.27.4-alpine` |
| Runtime image | `alpine:3.23`, non-root uid 10001 |
| Platform | `linux/arm64` (built on the host architecture, cross-compiled) |

Neither the development machine nor the CI runner is assumed to have a Go
toolchain. `app/scripts/go.sh` runs `gofmt`, `go vet` and `go test` inside the
pinned builder image:

```sh
./app/scripts/go.sh
```

Build the image:

```sh
docker buildx build --platform linux/arm64 -t mailflow-api:dev app/
```
