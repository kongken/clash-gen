# CLAUDE.md

Guidance for Claude Code working in this repo.

## Project

`clash-gen` — Clash config generator service written in Go. Module path:
`github.com/kongken/clash-gen`. Layout and conventions follow `kongken/urlo`.

Built on the `butterfly.orx.me/core` app framework (config loading,
HTTP/gRPC server wiring, logging, OTel). Exposes REST/JSON via Gin.

## Layout

```
cmd/clash-gen/     # main entrypoint; wires app.Config
internal/config/   # ServiceConfig (loaded from config.yaml by butterfly)
internal/http/     # Gin REST routes
```

## Commands

| Task | Command |
|------|---------|
| Run locally | `make run` (uses `config.yaml`) |
| Build binary | `make build` → `bin/clash-gen` |
| Tests | `go test ./...` |
| Tidy modules | `make tidy` |
| Docker build | `docker build -t clash-gen .` |

## Conventions

- Prefer table-driven tests.
- Go version: 1.26 (see `go.mod`); Dockerfile and CI pin to 1.26.
- Add new HTTP routes in `internal/http` and register them in `RegisterRoutes`.

## CI

`.github/workflows/go.yml` runs build + tests with the race detector on
push/PR to `main`. Docker images are published via `docker-publish.yml`.

## Agent skills

### Issue tracker

Read or publish issues in GitHub Issues for `kongken/clash-gen`. See `docs/agents/issue-tracker.md` for commands and conventions.

### Triage labels

Use the default Matt Pocock triage labels when triaging issues. See `docs/agents/triage-labels.md` for the role mapping.

### Domain docs

Use the single-context glossary and ADR layout when exploring domain concepts or decisions. See `docs/agents/domain.md`.
