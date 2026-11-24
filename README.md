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

## Deploy the subscription panel

Create a private bucket in MinIO or another S3-compatible service. Give the
application credentials permission to put, get, and delete objects under its
configured prefix. Enable Redis persistence (for example, `appendonly yes` on a
persistent volume) and use `maxmemory-policy noeviction`. Redis stores drafts,
download permissions, publication state, and items waiting for cleanup without
expiration. Keep both data volumes across restarts.

Copy `config.yaml` to a private deployment configuration and fill in these
settings. The values below are fictional.

```yaml
environment: production
base_url: https://subscriptions.example.net
admin_token: replace-with-a-long-random-secret
redis_url: redis://redis:6379/0
object_store:
  endpoint: http://minio:9000
  bucket: clash-subscriptions
  region: us-east-1
  prefix: clash-gen
  access_key_id: example-access-key
  secret_access_key: replace-with-the-s3-secret
  path_style: true
```

`base_url` is the public address of this Go service. Serve it over HTTPS in
production. It supplies the origin for client links and enables secure admin
cookies. Keep the storage API on the private network. If a reverse proxy logs
requests, configure it to omit query strings for `/subscription/` because they
contain download Tokens. Path-style addressing is enabled by default.

For a container deployment, mount the file and point Butterfly at it. Replace
`your-app-network` with the network that reaches Redis and MinIO.

```sh
docker build -t clash-gen .
docker run --rm --network your-app-network -p 127.0.0.1:8080:8080 \
  -v "$PWD/deploy.yaml:/app/config.yaml:ro" \
  -e BUTTERFLY_CONFIG_TYPE=file \
  -e BUTTERFLY_CONFIG_FILE_PATH=/app/config.yaml \
  -e BUTTERFLY_TRACING_DISABLE=true clash-gen
```

Open `/` and sign in with `admin_token`. Save and preview a draft, then publish
it to make its download link available. Later publications keep the same link.
Resetting the link revokes the previous link while serving the current YAML
through the new one. Confirming deletion removes download access immediately,
and the service retries object cleanup in the background. Changing
`admin_token` and restarting the service invalidates existing admin sessions.

## Acceptance tests

`make test` runs the Gin HTTP suite. To include the Redis and object-store
acceptance tests, point the suite at an isolated Redis database and a private
MinIO bucket with test credentials. Set `CLASH_GEN_TEST_MIHOMO` to the local
Mihomo executable to have it load the generated YAML.

```sh
CLASH_GEN_TEST_REDIS_URL=redis://127.0.0.1:6379/8 \
CLASH_GEN_TEST_S3_ENDPOINT=http://127.0.0.1:9000 \
CLASH_GEN_TEST_S3_BUCKET=clash-gen-test \
CLASH_GEN_TEST_S3_ACCESS_KEY_ID=example-test-key \
CLASH_GEN_TEST_S3_SECRET_ACCESS_KEY=example-test-secret \
CLASH_GEN_TEST_MIHOMO=/path/to/mihomo \
go test -race -count=1 ./...
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
internal/http/     # Gin routes and admin panel
internal/subscription/ # draft and publication state, and Mihomo YAML generation
```
