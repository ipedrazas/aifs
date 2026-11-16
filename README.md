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

Vite proxies `/api` and `/healthz` to the Go server. Development data goes to `./tmp/data`. In development mode the password is `dev` unless `AIFS_PASSWORD` is set.

After changing `openapi.yaml`, run `task generate` and commit the regenerated types; CI fails on drift.

## Checks

```sh
task ci         # everything CI runs: lint, tests, vulncheck, type drift, build, image e2e
task --list     # individual tasks
```

## Running with Docker

```sh
echo 'AIFS_PASSWORD=choose-a-long-password' > .env
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
| `AIFS_PASSWORD` | none (`dev` in development) | Shared password. Required in production, at least 12 characters. Changing it logs everyone out. |
| `AIFS_PASSWORD_FILE` | | Read the password from a file instead (Docker/Compose secrets) |
| `AIFS_SESSION_TTL` | `720h` | How long a login lasts |
| `AIFS_MAX_UPLOAD_MB` | `30` | Largest accepted photograph (also limited to 64 megapixels) |
| `AIFS_MAX_VARIATIONS` | `4` | Most variations one generation may request (1–20) |
| `AIFS_WORKERS` | `4` | Generations processed at once |
| `AIFS_STEP_TIMEOUT` | `10m` | Hard limit for one provider call, including retries |
| `AIFS_FAKE_PROVIDER` | `true` in development, else `false` | Enable the offline fake provider (deterministic placeholder images, no cost) |
| `AIFS_FAKE_DELAY` | `2s` | How long each fake provider job takes |
| `AIFS_FASHN_API_KEY` | | Enables the FASHN virtual try-on provider (or `AIFS_FASHN_API_KEY_FILE`). Get a key at https://app.fashn.ai/api |
| `AIFS_FASHN_MODEL` | `tryon-max` | `tryon-max` (FASHN's recommended model) or `tryon-v1.6` (stable, cheaper) |
| `AIFS_FASHN_RESOLUTION` | `2k` | `tryon-max` output: `1k`, `2k` or `4k` |
| `AIFS_FASHN_MODE` | `balanced` | `fast`, `balanced` or `quality` |
| `AIFS_FASHN_CREDIT_PRICE_USD` | `0.075` | Price of one FASHN credit, for cost estimates and records |
| `AIFS_COOKIE_SECURE` | `true` | Mark the session cookie `Secure`. Set `false` only when serving over plain HTTP on a non-localhost address |

## Data

Everything lives under `AIFS_DATA_DIR`: the SQLite database (`aifs.db`) and image bytes under `blobs/`. Images are content-addressed by SHA-256. Uploaded originals (`blobs/original/`) are stored exactly as uploaded, as read-only files, and nothing in the application can modify them. Thumbnails are rendered on demand (with EXIF orientation applied) into `blobs/derived/`, which is safe to delete.

## Generation

A photoshoot pairs a product with a model. Generating from it queues a job and returns at once; the page shows images as they arrive. Every generation stores a complete snapshot (recipe, provider, provider model versions, prompt, settings and the hashes of every reference photograph sent), and its records are never deleted. The queue lives in the database: after a restart, unfinished generations resume from their stored provider job ids, and at most one image per variation can be recorded.

Recipes combine provider calls: `reference` sends garment photographs, model references and the scene prompt in one call; `tryon` first generates the model in the scene and then dresses them with the garment photograph. Providers:

- **FASHN** (virtual try-on) runs the `tryon-photo` recipe: it dresses the model's own full-body photograph with one garment photograph (chosen per generation, front by default). The cost FASHN reports per job is recorded. `tryon-max` at 2k/balanced costs 3 credits ($0.225) per image.
- **Fake** (offline, no cost) runs every recipe; enable it with `AIFS_FAKE_PROVIDER=true`.

Provider tests replay recorded responses; CI never calls a real provider. To check FASHN against the live API (spends credits): `AIFS_FASHN_API_KEY=... task test:live:fashn`.

## Review

Every generated image starts as pending and only a human decision in the Review screen changes that: approve, reject with a reason (product fidelity, photographic quality, other) or mark as needing refinement. The database enforces it: a review status can only change together with a recorded review event, and the history of decisions is append-only. The review screen compares the image with any reference photograph side by side, as a swipe or as an overlay, with linked zoom and pan; press `?` there for keyboard shortcuts.

## Security

Everything under `/api/v1` except `POST /api/v1/session` requires a session. Sessions are random tokens kept server-side (only their SHA-256 is stored), sent as an `HttpOnly`, `SameSite=Strict` cookie. Login attempts are rate limited per client IP and in total. State-changing requests from another origin are rejected. Run it behind an HTTPS reverse proxy when it is reachable from other machines; behind a proxy all clients share one rate-limit bucket.

The container runs as a non-root user on a distroless base, stores everything under the `/data` volume, and reports health through `aifs healthcheck` (`GET /healthz`).
