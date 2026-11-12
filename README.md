# AI Fashion Studio

A lightweight AI-powered fashion photography studio that allows users to create professional e-commerce and lifestyle photographs of their garments without organising traditional photoshoots.

## Layout

| Path | What |
|---|---|
| `api/` | Go server (standard library HTTP, `slog`, SQLite via pure-Go driver). Serves the API under `/api/v1` and the embedded frontend at `/`. |
| `web/` | React + TypeScript + Vite frontend (React Router, TanStack Query, Tailwind). |
| `openapi.yaml` | The API contract. TypeScript types in `web/src/api/schema.d.ts` are generated from it. |
| `Taskfile.yml` | Every development, build and CI command. |
| `Dockerfile`, `compose.yaml` | Single-image, self-hosted deployment. |

## Requirements

Go 1.27, Node 24+, [Task](https://taskfile.dev), Docker, and `golangci-lint` v2 for linting.

```sh
task setup      # npm ci + Playwright Chromium (needed for `task e2e`)
```

## Development

```sh
task dev        # API on :8080 with live reload + Vite on http://localhost:5173
```

Vite proxies `/api` and `/healthz` to the Go server. Development data goes to `./tmp/data`.

After changing `openapi.yaml`, run `task generate` and commit the regenerated types; CI fails on drift.

## Checks

```sh
task ci         # everything CI runs: lint, tests, vulncheck, type drift, build, image e2e
task --list     # individual tasks
```

## Running with Docker

```sh
task docker:up    # build and start on http://localhost:8080 (AIFS_PORT to change)
task docker:down  # stop; the data volume is kept
```

Release images are published to `ghcr.io/ipedrazas/aifs` for `linux/amd64` and `linux/arm64` when a `vX.Y.Z` tag is pushed.

## Configuration

All configuration comes from environment variables.

| Variable | Default | Meaning |
|---|---|---|
| `AIFS_ADDR` | `:8080` | Listen address |
| `AIFS_DATA_DIR` | `./data` (`/data` in the image) | SQLite database and image storage |
| `AIFS_ENV` | `production` | `production` or `development` |
| `AIFS_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

The container runs as a non-root user on a distroless base, stores everything under the `/data` volume, and reports health through `aifs healthcheck` (`GET /healthz`).
