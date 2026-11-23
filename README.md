# clash-gen

Clash configuration generator service written in Go.

Module path: `github.com/kongken/clash-gen`. Built on the
`butterfly.orx.me/core` app framework (config loading, HTTP/gRPC server
wiring, logging, OTel), same as [urlo](https://github.com/kongken/urlo).

## Quick start

```sh
make run     # runs with ./config.yaml, listens on :8080
curl localhost:8080/ping
```

## Commands

| Task | Command |
|------|---------|
| Run locally | `make run` |
| Build binary | `make build` → `bin/clash-gen` |
| Tests | `make test` |
| Tidy modules | `make tidy` |
| Docker build | `docker build -t clash-gen .` |

## Layout

```
cmd/clash-gen/     # main entrypoint
internal/config/   # service config (YAML)
internal/http/     # Gin REST routes
```
