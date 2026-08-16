# small-light

A fast, cache-backed URL shortener with JWT auth and async click analytics.

`small-light` is a minimal URL shortener written in Go. It mints short, base62
codes, serves redirects from a Redis read-cache, and buffers click counts in
Redis before flushing them to Postgres in the background — so the redirect path
never touches the database.

## Features

- **JWT auth** — register/login, BCrypt password hashing, per-user ownership of links
- **Short links** — base62 codes, default 7-day TTL, updateable URLs and expiry
- **Redis read-path cache** — cached links serve redirects without touching Postgres; cache TTL mirrors link expiry, and updates/soft-deletes invalidate it
- **Async click analytics** — clicks are buffered in Redis (`GetDel` drain) and flushed to Postgres by a background worker; every flush is idempotent
- **Background cleanup** — expired links are soft-deleted and purged hourly
- **Middleware stack** — request ID, structured request logging, rate limiting, panic recovery
- **Graceful shutdown** — drains workers and closes connections on SIGTERM/SIGINT
- **Dockerized** — multi-stage, non-root image; `docker compose` for Postgres + Redis

## Architecture

```
                ┌──────────────────────────────────────────────┐
                │                 HTTP (chi router)            │
                │  middleware: request-id → logging → auth →   │
                │              rate-limit → recover            │
                └────────────────────┬─────────────────────────┘
                                     │
                ┌────────────────────▼─────────────────────────┐
                │                 handler layer                │
                └────────────────────┬─────────────────────────┘
                                     │
                ┌────────────────────▼─────────────────────────┐
                │                 service layer                │
                │  auth (JWT/BCrypt) · links · click counter   │
                └──────────────┬───────────────┬───────────────┘
                               │               │
                  ┌────────────▼───┐   ┌───────▼───────────────┐
                  │   Redis        │   │   PostgreSQL (pgx)    │
                  │  · link cache  │   │  links · users ·      │
                  │  · click buffer│   │  click_events         │
                  └────────┬───────┘   └───────────────────────┘
                           │
                  ┌────────▼──────────┐
                  │  background       │
                  │  workers          │
                  │  · click flush    │
                  │  · link cleanup   │
                  └───────────────────┘
```

The redirect path (`GET /{code}`) is served entirely from Redis; the click
worker (`CLICK_FLUSH_INTERVAL`, default 30s) persists counted clicks, and the
cleanup worker (`LINK_CLEANUP_INTERVAL`, default 1h) removes expired links.

## Quick start

Requires Go 1.26+ and Docker.

Run the whole stack in Docker (server + Postgres + Redis):

```sh
make up          # docker compose up -d --build
```

Or run the server locally against containerized dependencies:

```sh
# 1. Start Postgres and Redis
make dc-up

# 2. Run the server (applies migrations on startup)
make run
```

In both cases the server listens on `http://localhost:8080`. Try a full flow:

```sh
# Health check
curl -i http://localhost:8080/health

# Register (note: keys are "Email"/"Password" — the decoder is strict)
curl -i -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"Email":"alice@example.com","Password":"password123"}'

# Login (requires jq for token extraction)
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"Email":"alice@example.com","Password":"password123"}' \
  | jq -r .access_token)

# Create a link (default TTL 7 days)
curl -i -X POST http://localhost:8080/api/v1/links \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com"}'
# → 201 { "id": "...", "short_code": "4fK2a", "original_url": "https://example.com", "click_count": 0, ... }

# Redirect (302 to https://example.com)
curl -i http://localhost:8080/4fK2a

# Analytics — reflects clicks after the worker flush (default 30s)
curl http://localhost:8080/api/v1/links/<id>/analytics \
  -H "Authorization: Bearer $TOKEN"
# → { "link_id": "...", "short_code": "4fK2a", "click_count": 1 }
```

## Configuration

All settings are environment variables with sensible development defaults
(`internal/config`). Duration fields accept Go duration strings (`30s`, `1h`).

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port |
| `APP_ENV` | `development` | `production` enforces a non-default `JWT_SECRET` |
| `LOG_LEVEL` | `info` | Log level |
| `DATABASE_URL` | `postgres://myuser:mysecretpassword@localhost:5432/smalllight` | Postgres DSN |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `REDIS_PASSWORD` | `myredispassword` | Redis password |
| `REDIS_DB` | `0` | Redis logical DB |
| `SHORT_URL_BASE` | `http://localhost:8080` | Base used to build absolute short URLs |
| `DEFAULT_LINK_TTL` | `168h` | Default link expiry (7 days) |
| `CLICK_FLUSH_INTERVAL` | `30s` | How often buffered clicks flush to Postgres |
| `LINK_CLEANUP_INTERVAL` | `1h` | How often expired links are purged |
| `JWT_SECRET` | `dev-jwt-secret-change-me` | HMAC secret; **must be set in production** |
| `JWT_TTL` | `24h` | Token lifetime |

## API

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| `GET` | `/health` | — | Health check |
| `POST` | `/api/v1/auth/register` | — | Create an account |
| `POST` | `/api/v1/auth/login` | — | Exchange credentials for a JWT |
| `POST` | `/api/v1/links` | Bearer | Create a short link |
| `GET` | `/api/v1/links` | Bearer | List your links |
| `GET` | `/api/v1/links/{id}` | Bearer | Get one link |
| `PATCH` | `/api/v1/links/{id}` | Bearer | Update URL / expiry (evicts cache) |
| `DELETE` | `/api/v1/links/{id}` | Bearer | Soft-delete a link |
| `GET` | `/api/v1/links/{id}/analytics` | Bearer | Click count |
| `GET` | `/{code}` | — | 302 redirect (rate-limited: 20/min) |

Links are owner-scoped: a link is only visible to the user who created it.

See [docs/API.md](docs/API.md) for full request/response examples and error codes.

## Testing

Unit tests are hermetic (no external services):

```sh
make test        # go test ./...
make vet         # go vet ./...
```

Integration tests run against real Postgres and Redis (via `docker compose up -d`).
They're opt-in so `go test ./...` stays fast and docker-free:

```sh
TEST_INTEGRATION=1 go test ./...
```

## Project layout

```
cmd/server/          entrypoint: config, wiring, graceful shutdown
internal/
  config/            env-based configuration
  cache/             Redis link cache + click counter buffer
  database/          pgx pool + SQL migrations
  handler/           HTTP handlers
  middleware/        auth, request-id, logging, rate limit, recover
  model/             User / Link / ClickEvent types
  repository/        Postgres data access
  server/            chi router + middleware wiring
  service/           auth (JWT/BCrypt), links, click counter
  shortcode/         base62 short-code generation
  worker/            background click flush + link cleanup
  integration/       end-to-end tests (TEST_INTEGRATION=1)
migrations/          SQL schema
```

## Docker

The `Dockerfile` is multi-stage: a static, `-trimpath -s -w` binary built with
cached module/build mounts, running as a non-root user on `alpine:3.20` with a
`/health` healthcheck.

`docker-compose.yml` runs the entire stack — Postgres, Redis, and the app
(waits on the DBs being healthy, exposes `8080`). One command:

```sh
make up          # docker compose up -d --build
```

Use `make dc-up` (DBs only) when you want to run the server via `make run`
instead. For production, override the environment (at minimum a real
`JWT_SECRET` and `APP_ENV=production`, which the server enforces at startup).

```sh
docker build -t small-light .
```

## Docs

- [docs/API.md](docs/API.md) — full API reference
- [PRD.md](PRD.md) — product requirements and roadmap
