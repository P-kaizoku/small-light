# API Reference

Base URL: `http://localhost:8080` (configurable via `SHORT_URL_BASE`).

- All API routes live under `/api/v1`.
- Auth routes use a JSON body with `"Email"` / `"Password"` keys (the request
  decoder is strict: unknown fields and extra keys are rejected).
- Protected routes require an `Authorization: Bearer <token>` header.
- Errors are always `{"error": "<message>"}`.

## Error codes

| Status | Meaning |
| --- | --- |
| `400 Bad Request` | Invalid JSON, invalid ID, invalid URL, or an unknown field |
| `401 Unauthorized` | Missing/invalid token, or wrong credentials |
| `404 Not Found` | No such resource (or you don't own it) |
| `409 Conflict` | Email already registered, or a conflicting insert |
| `410 Gone` | Link has expired |
| `429 Too Many Requests` | Rate limit exceeded |
| `500 Internal Server Error` | Server fault (client only sees a generic message) |

## Rate limits

- `/api/v1/*`: 100 requests/min per client IP
- `GET /{code}`: 20 requests/min per client IP

## Health

### `GET /health`

Liveness probe. Returns `200` with no body while the process is up.

## Authentication

### `POST /api/v1/auth/register`

Create an account. Passwords must be at least 8 characters.

Request:

```json
{ "Email": "alice@example.com", "Password": "password123" }
```

Response — `201 Created`:

```json
{
  "id": "8f14e45f-ceea-11d2-9d1c-0019723c4e45",
  "email": "alice@example.com",
  "created_at": "2026-08-16T12:00:00Z",
  "updated_at": "2026-08-16T12:00:00Z"
}
```

Errors: `400` (invalid body/password), `409` (email already registered).

### `POST /api/v1/auth/login`

Exchange credentials for a JWT (default TTL 24h).

Request:

```json
{ "Email": "alice@example.com", "Password": "password123" }
```

Response — `200 OK`:

```json
{ "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." }
```

Use the token as `Authorization: Bearer <access_token>` on all other routes.

Errors: `401` (wrong email or password — same message either way, no user
enumeration).

## Links

All link routes are owner-scoped: you can only see, update, or delete links you
created. A missing or foreign resource returns `404`.

### `POST /api/v1/links`

Create a short link. TTL defaults to `DEFAULT_LINK_TTL` (7 days).

Request:

```json
{ "url": "https://example.com/some/long/path" }
```

Response — `201 Created`:

```json
{
  "id": "5b1f2b3a-0000-4000-8000-000000000001",
  "owner_id": "8f14e45f-ceea-11d2-9d1c-0019723c4e45",
  "original_url": "https://example.com/some/long/path",
  "short_code": "4fK2a",
  "click_count": 0,
  "expires_at": "2026-08-23T12:00:00Z",
  "created_at": "2026-08-16T12:00:00Z",
  "updated_at": "2026-08-16T12:00:00Z"
}
```

The short URL is `{SHORT_URL_BASE}/{short_code}`.

Errors: `400` (invalid URL), `401`.

### `GET /api/v1/links`

List your links.

Response — `200 OK`:

```json
[
  {
    "id": "5b1f2b3a-0000-4000-8000-000000000001",
    "owner_id": "8f14e45f-ceea-11d2-9d1c-0019723c4e45",
    "original_url": "https://example.com/some/long/path",
    "short_code": "4fK2a",
    "click_count": 0,
    "expires_at": "2026-08-23T12:00:00Z",
    "created_at": "2026-08-16T12:00:00Z",
    "updated_at": "2026-08-16T12:00:00Z"
  }
]
```

Errors: `401`.

### `GET /api/v1/links/{id}`

Get one link by its UUID.

Response — `200 OK`: a single link object (shape as above).

Errors: `400` (invalid ID), `401`, `404`.

### `PATCH /api/v1/links/{id}`

Update a link. This is a full replace: `url` is required. `expires_at` is
optional and defaults to `now + DEFAULT_LINK_TTL`; supply it as RFC 3339 to
override. The change invalidates the cached copy, so the very next redirect
serves the new URL.

Request:

```json
{
  "url": "https://example.org/updated",
  "expires_at": "2026-09-01T00:00:00Z"
}
```

Response — `200 OK`: the updated link object.

Errors: `400` (invalid ID, invalid JSON, malformed `expires_at`), `401`, `404`.

### `DELETE /api/v1/links/{id}`

Soft-delete a link. The redirect cache is invalidated; the row is purged later
by the cleanup worker.

Response — `204 No Content`.

Errors: `400` (invalid ID), `401`, `404`.

### `GET /api/v1/links/{id}/analytics`

Get the accumulated click count. Counts are flushed from Redis to Postgres in
batches by the click worker (`CLICK_FLUSH_INTERVAL`, default 30s), so the value
may briefly lag live traffic.

Response — `200 OK`:

```json
{
  "link_id": "5b1f2b3a-0000-4000-8000-000000000001",
  "short_code": "4fK2a",
  "click_count": 42
}
```

Errors: `400` (invalid ID), `401`, `404`.

## Redirect

### `GET /{code}`

Resolve a short code. Redirects are served from the Redis cache — no database
hit on the hot path.

- `302 Found` — `Location` header points at the original URL.
- `404 Not Found` — unknown code.
- `410 Gone` — the link has expired.
- `429 Too Many Requests` — over 20 redirects/min per IP.
