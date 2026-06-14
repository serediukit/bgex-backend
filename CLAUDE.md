# bgex-backend — Claude context

## What this project is

**bgex** is a web app for playing board games together online. Planned game catalog: Monopoly, Poker, Alias, Hadrian's Wall, The Castles of Burgundy, Who Am I, Codenames (and more).

This repo is the Go backend. The frontend is a separate repo.

## Tech stack (locked in — do not change without discussion)

| Concern | Choice |
|---------|--------|
| HTTP | `gin-gonic/gin` |
| Database | PostgreSQL 16 via `jackc/pgx/v5` |
| Query building | `Masterminds/squirrel` (no ORM, no sqlc) |
| Migrations | `golang-migrate` CLI against `./migrations` |
| Auth tokens | HS256 JWT (access, 15 min) + rotating opaque refresh tokens (SHA-256 hashed, stored in DB) |
| Password hashing | argon2id (RFC 9106 params) |
| OAuth | Google OAuth 2.0 (`golang.org/x/oauth2`) |
| Config | `caarlos0/env/v11` + `joho/godotenv` |
| Logging | `sirupsen/logrus` (text in dev, JSON in prod) |
| IDs | `google/uuid` (UUID v4) |

## Local dev

```bash
make db-up        # start pgconn:16 on :5432 (docker compose)
cp .env.example .env
# set JWT_SECRET >= 32 bytes: openssl rand -base64 48
make migrate-up   # apply ./migrations
make run          # go run ./cmd/bgex-server — listens on :8080
make db-down      # stop pgconn
```

Key env vars:

| Var | Purpose |
|-----|---------|
| `DATABASE_URL` | postgres DSN |
| `JWT_SECRET` | HMAC signing key (min 32 bytes) |
| `JWT_ACCESS_TTL` | access token lifetime (default `15m`) |
| `REFRESH_TOKEN_TTL` | refresh token lifetime (default `720h`) |
| `GOOGLE_OAUTH_CLIENT_ID/SECRET/REDIRECT_URL` | optional — enables Google OAuth routes |
| `CORS_ALLOWED_ORIGINS` | comma-separated list |

Google OAuth routes (`/auth/google/login`, `/auth/google/callback`) only mount when all three `GOOGLE_OAUTH_*` vars are set.

## Project layout

```
cmd/app/               — entry point (main.go)
internal/
  app/app.go           — composition root: config → db → router → http.Server + graceful shutdown
  config/              — env-based Config struct (Load())
  postgres/            — pgxpool.New wrapper + ping
  httpx/
    router.go          — NewRouter(RouterOptions) — mounts middleware + /healthz + /readyz + /api/v1/*
    response/          — JSON success/error envelopes; stable ErrorCode constants
    middleware/
      auth.go          — RequireAuth(AccessTokenVerifier) + UserIDFrom(ctx)
      logger.go        — slog access log
      recovery.go      — panic → 500
      request_id.go    — X-Request-ID propagation
      cors.go          — CORS via gin-contrib/cors
  domain/
    user/              — model (User, PublicProfile, UpdateParams), repository (pgx+squirrel), service (profile validation), handler
    auth/              — password, jwt, refresh, oauth_google, repository, service, handler
migrations/            — golang-migrate SQL files (sequential numbered)
```

## Domain pattern

Every new domain follows this shape:

```
internal/domain/<name>/
  model.go       — structs (no DB tags)
  repository.go  — pgxpool + squirrel queries; ErrNotFound sentinel
  service.go     — business logic (depends on repo, other services)
  handler.go     — gin handlers; exposes Register(authMiddleware) func(*gin.RouterGroup)
```

Wire the domain into `internal/app/app.go` (the composition root).

Game engines live under `internal/games/<game>/` — separate concern from REST domains.

## API routes (current)

| Method | Path | Auth |
|--------|------|------|
| GET | `/healthz` | — |
| GET | `/readyz` | — |
| POST | `/api/v1/auth/register` | — |
| POST | `/api/v1/auth/login` | — |
| POST | `/api/v1/auth/refresh` | — |
| POST | `/api/v1/auth/logout` | bearer |
| GET | `/api/v1/auth/google/login` | — |
| GET | `/api/v1/auth/google/callback` | — |
| GET | `/api/v1/users/me` | bearer |
| PATCH | `/api/v1/users/me` | bearer |
| GET | `/api/v1/users/:id` | — |

`:id` accepts a UUID or a username — the handler tries `uuid.Parse` first, falls back to username lookup.

New routes go under `/api/v1/<domain>` and are registered via the `RouteRegistrar` func type in `internal/httpx/router.go`.

**Route registration:** pass `authMiddleware` inline per route (`users.GET("/me", authMiddleware, h.me)`) rather than `group.Use(authMiddleware)` — two groups at the same prefix with `Use` on one silently drops routes from Gin's radix tree.

## Auth model

- **Access token**: short-lived (15 min) HS256 JWT, `sub` = user UUID. Passed as `Authorization: Bearer <token>`.
- **Refresh token**: 32 random bytes, base64url-encoded. Server stores `sha256(token)` only — raw token never persisted. Rotated on every `/auth/refresh` call (old token revoked, new pair issued). Reuse of an already-revoked token returns 401.
- **Google OAuth state**: HMAC-SHA256 signed nonce stored in a short-lived httpOnly cookie; validated on callback.

## Database conventions

- UUID PKs (`gen_random_uuid()`), `created_at`/`updated_at` TIMESTAMPTZ defaulting to `now()`.
- Email stored as `citext` (case-insensitive).
- Nullable text columns in Go are `*string` in scan targets, then unpacked into plain strings on the model.
- Migrations: `make migrate-new name=<slug>` creates the pair; always write a `.down.sql`.
- Unique constraint violations for known columns are mapped to domain sentinel errors (e.g. `user.ErrEmailTaken`).

## Error response shape

```json
{
  "error": {
    "code": "invalid_request",
    "message": "human-readable detail"
  }
}
```

Stable codes live in `internal/httpx/response/response.go` as `ErrorCode` constants.

## Testing conventions

- Unit tests live next to the code they test (`*_test.go` same package or `_test` external package).
- Use `net/http/httptest` for handler tests — no live server needed.
- Integration tests (DB) require a real Postgres instance; do not mock the DB.
- Run: `make test` (`go test ./...`), `make vet` (`go vet ./...`).

## Planned future domains (not yet implemented)

- `internal/domain/profile/` — richer user profile (bio, stats, etc.)
- `internal/domain/friends/` — friend requests / graph
- `internal/domain/chat/` — message threads; will likely need WebSocket support
- `internal/games/<game>/` — per-game engine logic