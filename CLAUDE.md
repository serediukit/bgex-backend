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

`internal/games/engine.Engine` and `internal/games/realtime.GameService` are the two seams every game implements. Both are now "slim" — no hand/redeal concept baked in — with `HandBased` / `HandBasedGameService` as **optional** interfaces layered on top: `poker.Engine`/`poker.Session` implement them (a poker table deals a new hand after each one finishes); `ttr.Engine`/`ttr.Session` and `song_guess.Engine`/`song_guess.Session` deliberately do not (a TTR lobby plays exactly one game to a final score, and a Song Guess lobby plays one pack to a final score, so `realtime.Handler` finishes the lobby instead of redealing when a game doesn't implement `HandBasedGameService`). A new game only needs `HandBased` if it, too, is a series of independent hands.

**Per-viewer redaction is mandatory, and `View` is not the only surface.** `View(state, forUser)` is the sole path secret state may leave the server by — but **engine events broadcast to every subscriber including spectators and cannot be per-viewer redacted**, so event payloads need the same scrutiny as views. Song Guess learned this the hard way: `guess_submitted` originally carried the raw guess text and defeated an entire redaction decision one layer below a correct `View`. Events must carry a boolean (`has_guess`) and let the redacted `state` broadcast deliver the text.

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
| POST | `/api/v1/games/lobbies` | bearer |
| GET | `/api/v1/games/lobbies` | bearer |
| GET | `/api/v1/games/lobbies/current` | bearer |
| GET | `/api/v1/games/lobbies/:id` | bearer |
| POST | `/api/v1/games/lobbies/:id/seats` | bearer |
| DELETE | `/api/v1/games/lobbies/:id/seats` | bearer |
| POST | `/api/v1/games/lobbies/:id/start` | bearer |
| GET | `/api/v1/games/lobbies/:id/ws` | bearer (`?token=`) |
| GET | `/api/v1/games/ttr/maps` | bearer |
| GET | `/api/v1/games/ttr/maps/:ref` | bearer |
| GET | `/api/v1/games/ttr/assets/:id` | — |
| GET | `/api/v1/admin/ttr/maps` | bearer + admin |
| POST | `/api/v1/admin/ttr/maps` | bearer + admin |
| GET | `/api/v1/admin/ttr/maps/:id` | bearer + admin |
| GET | `/api/v1/admin/ttr/maps/:id/versions` | bearer + admin |
| GET | `/api/v1/admin/ttr/maps/:id/versions/:version` | bearer + admin |
| PUT | `/api/v1/admin/ttr/maps/:id/draft` | bearer + admin |
| POST | `/api/v1/admin/ttr/maps/:id/versions/:version/publish` | bearer + admin |
| POST | `/api/v1/admin/ttr/assets` | bearer + admin |
| GET | `/api/v1/games/song-guess/packs` | bearer |
| GET | `/api/v1/games/song-guess/packs/:ref` | bearer |
| POST | `/api/v1/games/song-guess/packs` | bearer |
| GET | `/api/v1/games/song-guess/packs/:ref/doc` | bearer + owner-or-admin |
| GET | `/api/v1/games/song-guess/packs/:ref/versions` | bearer + owner-or-admin |
| PUT | `/api/v1/games/song-guess/packs/:ref/draft` | bearer + owner-or-admin |
| POST | `/api/v1/games/song-guess/packs/:ref/versions/:version/publish` | bearer + owner-or-admin |
| DELETE | `/api/v1/games/song-guess/packs/:ref` | bearer + owner-or-admin |

**Song Guess pack routes are auth-only — there is no `RequireAdmin` on this game.** Unlike the TTR map routes (which are admin-gated under `/api/v1/admin/ttr/...`), pack authoring is open to every user, with **per-row authorization in the handler**: writes and the full-document read require `created_by == caller OR caller.role == 'admin'`. A non-owner receives **404, not 403**, so the existence of someone else's draft is never confirmed. Both handlers (`packhandler.go`, `authorhandler.go`) mount the **same** `/games/song-guess` prefix with `authMiddleware` inline per route and **no `group.Use`** — see the route-registration note below for why that matters.

The read routes return **summaries only** (`total_songs`, `is_even`, and per-`(language × bucket)` counts) and never a song, title, artist, block or video id: a pack document is the answer key, so any client able to fetch it has won the game. `PUT …/draft` accepts **`?validate=false`** with the same semantics as the TTR map draft route, and enforces a 512 KiB body cap either way.

`:id` accepts a UUID or a username — the handler tries `uuid.Parse` first, falls back to username lookup.

`admin/ttr/maps/:id/versions` lists every version of a map, newest first, each with `status`, `validated` and timestamps — the editor's version picker reads it.

`PUT admin/ttr/maps/:id/draft` accepts **`?validate=false`**, which stores a work-in-progress document *without* running `ParseMap` and records `validated = false` on the row. The body must still be a JSON object within the size cap. This exists because a half-authored map (a handful of cities, no tickets yet) can never satisfy full validation, so without it "build a map from scratch" would mean finishing in one unbroken browser session. `publish` re-validates unconditionally, so an unvalidated draft can never be published or played. Any other value of `validate` — including absent — validates.

`games/ttr/maps/:ref` accepts a slug or a UUID; `?version=N` pins a specific **published** version (default: latest published). `games/ttr/assets/:id` is deliberately unauthenticated — its content is content-addressed and immutable (served with `Cache-Control: public, max-age=31536000, immutable` and an `ETag`, honoring `If-None-Match` with 304), so a bare `<image>` tag can load it without a token.

New routes go under `/api/v1/<domain>` and are registered via the `RouteRegistrar` func type in `internal/httpx/router.go`.

**Route registration:** pass `authMiddleware` inline per route (`users.GET("/me", authMiddleware, h.me)`) rather than `group.Use(authMiddleware)` — two groups at the same prefix with `Use` on one silently drops routes from Gin's radix tree. Admin routes carry **both** `authMiddleware` and `adminMiddleware` inline, in that order.

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
- Migration numbering is sequential 4-digit (`0001`, `0002`, …). Current head is `0012_seed_song_guess_example_pack`; the next migration is **`0013`**. (Song Guess added two: `0011` creates the schema, `0012` seeds the official example pack.)

### `ttr` schema (Ticket to Ride)

Migration `0006_add_ttr_schema` creates a dedicated `ttr` Postgres schema, mirroring the `poker` schema pattern (per-game hot state lives in its own schema; down migration is `DROP SCHEMA ttr CASCADE`):

| Table | Purpose |
|-------|---------|
| `ttr.maps` | A board definition (slug, name, official flag). |
| `ttr.map_versions` | Immutable-once-published `(map_id, version)` rows; `doc JSONB` holds the full map document (`rules` + `layout`); `status` is `draft` or `published`. `validated BOOLEAN NOT NULL DEFAULT TRUE` (migration `0009`) is `FALSE` for a work-in-progress draft saved via `?validate=false`; `publish` always re-runs `ParseMap` and sets it back to `TRUE`, so an unvalidated draft can never be published or played. A lobby pins `(map_id, version)` at Start so later edits can't affect a running game. |
| `ttr.map_assets` | Content-addressed background images (`bytes BYTEA`, `sha256`), max 4 MB, `image/png`/`image/jpeg`/`image/webp` only. |
| `ttr.game_states` | Hot state: `state BYTEA` (protobuf), `version` bumped on every mutation under `SELECT ... FOR UPDATE` — same contract as `poker.game_states`. FK's `(map_id, map_version)` to `ttr.map_versions`. |
| `ttr.action_log` | `(lobby_id, seq)` append-only JSONB action log for replay/debug. |
| `ttr.game_results` | Final per-seat scoring breakdown (JSONB), one row per player per finished lobby. |

### Europe ships two published versions

Migration `0008` seeds **Europe v1**; migration `0010` seeds **Europe v2**. `GET /games/ttr/maps/:ref` resolves the **latest published** version by default, so new lobbies pin **v2**.

**v2** carries: 15 route-colour corrections read off the physical board, a **9th double route** (Budapest–Wien — `45` White ↔ `99` Red, mutually paired, so 99 routes total), and slot angles regenerated in **pixel space**.

**v1 is immutable and stays exactly as seeded.** Its `mapdata/europe_test.go` drift-guard must keep passing, and lobbies started before v2 remain pinned to v1 forever.

⚠️ **`Slot.angle` means different things across versions.** It is **pixel-space degrees** in v2 and anything authored later, but **normalized-space degrees** in `europe.v1.json` (the original convention, which the renderer applied in pixel space — that was the bug v2 fixes). `mapdata/europe_v2_test.go` asserts the pixel-space property permanently; asserting it over v1 would correctly fail. **Do not "fix" v1.**

### `song_guess` schema (Guess the Song by Words)

Migration `0011_add_song_guess_schema` creates a dedicated `song_guess` Postgres schema, following the `poker`/`ttr` pattern (down migration is `DROP SCHEMA song_guess CASCADE`):

| Table | Purpose |
|-------|---------|
| `song_guess.packs` | A content pack (slug, name, `author_label` free-text credit, `created_by` → `users(id)`, `is_official`, `is_public`). |
| `song_guess.pack_versions` | Immutable-once-published `(pack_id, version)` rows; `doc JSONB` holds the full pack v2 document; `status` is `draft` or `published`; `doc_sha256` and `total_songs` are denormalized for cheap listing. `validated BOOLEAN NOT NULL DEFAULT TRUE` is `FALSE` for a work-in-progress draft saved via `?validate=false`; `publish` always re-runs `ParsePack` and sets it back to `TRUE`, so an unvalidated draft can never be published or played. **Saving over a published version forks a new draft** rather than mutating it. |
| `song_guess.game_states` | Hot state: `state BYTEA` (protobuf), `version` bumped on every mutation under `SELECT … FOR UPDATE` — same contract as `poker`/`ttr`. Carries a **composite FK `(pack_id, pack_version)` → `song_guess.pack_versions`**, which is what makes a lobby's pin undanglable: editing or deleting a pack can never affect a running or finished game. |
| `song_guess.action_log` | `(lobby_id, seq)` append-only JSONB action log for replay/debug; `seq` is the engine's post-`Apply` value. |
| `song_guess.game_results` | Final per-participant rows, PK `(lobby_id, seat_index)` — one row per participant **including the judge** (`team = 'none'`, `is_judge = true`, empty breakdown). |

**No default pack.** Unlike TTR's `europe`, `ValidateLobbyConfig` for `song_guess` **rejects** a missing or empty `pack_id` — there is deliberately no fallback. Migration `0012` seeds an official `example-hits` pack (8 songs, 2 languages × buckets 4 and 5) purely so the list is never empty on a fresh install; it is never an implicit default.

**Not `HandBased`.** `song_guess.Engine`/`song_guess.Session` deliberately do **not** implement `HandBased`/`HandBasedGameService` — a lobby plays one pack to a final score, so `realtime.Handler` finishes the lobby. The engine emits its **own** `game_over` event, which is what suppresses `realtime.Handler`'s synthesized duplicate. It is the second non-`HandBased` precedent after `ttr`.

**Teams/captains/judge live in game state, not the lobby.** `game_seats` has no team concept and `lobby.Config` is write-once at create time, so the roster is a `PHASE_ROSTER` phase inside the protobuf state, mutated by host-only engine actions. `MinSeats() == 3` (two teams plus a judge), `MaxSeats() == 10` (the platform ceiling from `game_lobbies.max_seats CHECK (max_seats BETWEEN 2 AND 10)`).

### Admin role

Migration `0007_add_user_role` adds `users.role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin'))` plus a partial index on admins. `middleware.RequireAdmin` (chained **after** `middleware.RequireAuth`) gates admin-only routes.

There is no self-service admin promotion. Promote the first admin manually via SQL:

```sql
UPDATE users SET role = 'admin' WHERE email = 'you@example.com';
```

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