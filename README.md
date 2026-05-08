# bgex-backend

Backend for **bgex** — a web app for playing board games (Monopoly, Poker, Alias, Hadrian's Wall, The Castles of Burgundy, Who Am I, Codenames, …) together online.

## Stack

- Go 1.26 + [Gin](https://github.com/gin-gonic/gin)
- PostgreSQL 16 via [pgx/v5](https://github.com/jackc/pgx) + [squirrel](https://github.com/Masterminds/squirrel) query builder
- Migrations via [golang-migrate](https://github.com/golang-migrate/migrate)
- Auth: email/password (argon2id) + Google OAuth; JWT access tokens + rotating refresh tokens
- Structured logging via stdlib `log/slog`

## Getting started

```bash
# 1. Start Postgres
make db-up

# 2. Configure
cp .env.example .env
# Edit JWT_SECRET (use `openssl rand -base64 48`) and Google OAuth creds.

# 3. Migrate
make migrate-up

# 4. Run
make run
```

Server listens on `http://localhost:8080` by default.

## Project layout

```
cmd/app/               — entry point
internal/
  app/                 — bootstrap (config → db → router → server)
  config/              — env-based config
  postgres/            — pgx pool
  httpx/               — Gin router, middleware, response helpers
  domain/
    user/              — user model/repo/service/handler
    auth/              — email/password + OAuth, JWT, refresh tokens
migrations/            — golang-migrate SQL files
```

## API

| Method | Path                              | Auth    |
|--------|-----------------------------------|---------|
| GET    | `/healthz`                        | —       |
| GET    | `/readyz`                         | —       |
| POST   | `/api/v1/auth/register`           | —       |
| POST   | `/api/v1/auth/login`              | —       |
| POST   | `/api/v1/auth/refresh`            | —       |
| POST   | `/api/v1/auth/logout`             | bearer  |
| GET    | `/api/v1/auth/google/login`       | —       |
| GET    | `/api/v1/auth/google/callback`    | —       |
| GET    | `/api/v1/users/me`                | bearer  |

## Development

```bash
make test        # go test ./...
make vet         # go vet ./...
make build       # ./bin/bgex
make tidy        # go mod tidy
```