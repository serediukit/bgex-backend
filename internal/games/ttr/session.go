package ttr

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// defaultMapSlug is the map a lobby gets when its config omits "map_id"
// entirely — the only official board at launch (plan Q5).
const defaultMapSlug = "europe"

// Session ties the TTR Engine to its Postgres-backed state. It implements
// realtime.GameService, lobby.GameInitializer, lobby.ConfigValidator and
// lobby.ResignHandler. It deliberately does NOT implement
// realtime.HandBasedGameService: a TTR lobby plays exactly one game.
type Session struct {
	pool    *pgxpool.Pool
	repo    *StateRepo
	eng     *Engine
	maps    *MapCache
	mapRepo *MapRepo
}

// NewSession creates a TTR Session.
func NewSession(pool *pgxpool.Pool, repo *StateRepo, eng *Engine, maps *MapCache, mapRepo *MapRepo) *Session {
	return &Session{pool: pool, repo: repo, eng: eng, maps: maps, mapRepo: mapRepo}
}

// GameKey identifies TTR lobbies.
func (s *Session) GameKey() string { return GameKey }

// ValidateLobbyConfig resolves cfg's "map_id" (a slug or a UUID string;
// defaults to "europe" when absent) to its latest published version,
// confirms the document actually parses via the map cache, and returns the
// normalized config InitState/InitGameState expect:
// {"map_id": "<uuid>", "map_version": N, "map_name": "<name>"}. Any error
// here (unknown map, no published version, a malformed document) is mapped
// by lobby.Service.Create to lobby.ErrInvalidConfig.
func (s *Session) ValidateLobbyConfig(ctx context.Context, cfg map[string]any) (map[string]any, error) {
	ref := defaultMapSlug
	if raw, ok := cfg["map_id"]; ok {
		str, ok := raw.(string)
		if !ok || str == "" {
			return nil, fmt.Errorf("%w: %q must be a non-empty string", ErrInvalidConfig, "map_id")
		}
		ref = str
	}

	summary, err := s.mapRepo.GetBySlugOrID(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve map %q: %w", ref, err)
	}
	if summary.LatestPublishedVersion == nil {
		return nil, fmt.Errorf("%w: map %q has no published version", ErrMapVersionNotFound, ref)
	}
	version := *summary.LatestPublishedVersion

	if _, err := s.maps.Get(ctx, summary.ID.String(), version); err != nil {
		return nil, fmt.Errorf("load map %q@%d: %w", ref, version, err)
	}

	return map[string]any{
		"map_id":      summary.ID.String(),
		"map_version": version,
		"map_name":    summary.Name,
	}, nil
}

// InitGameState persists the freshly-built initial state (lobby.GameInitializer).
// state was already produced by Engine.InitState(ctx, config, seats) in
// lobby.Service.Start; this just pins (map_id, map_version) from config
// alongside it.
func (s *Session) InitGameState(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID, state []byte, config map[string]any) error {
	mapIDStr, version, err := mapConfigFrom(config)
	if err != nil {
		return err
	}
	mapID, err := uuid.Parse(mapIDStr)
	if err != nil {
		return fmt.Errorf("%w: map_id %q is not a uuid", ErrInvalidConfig, mapIDStr)
	}
	return s.repo.Insert(ctx, tx, lobbyID, mapID, version, state)
}

// View returns the redacted TTR view for a viewer. It resolves the pinned
// map itself via s.maps.Get(ctx, ...) — using the real request ctx — and
// calls Engine.ViewFor rather than Engine.View, so the engine never falls
// back to its own context.Background() map lookup on this path.
func (s *Session) View(ctx context.Context, lobbyID, userID uuid.UUID) (any, error) {
	state, mapID, mapVersion, err := s.repo.Get(ctx, s.pool, lobbyID)
	if err != nil {
		return nil, err
	}
	m, err := s.maps.Get(ctx, mapID, mapVersion)
	if err != nil {
		return nil, err
	}
	return s.eng.ViewFor(m, state, userID)
}

// Apply runs one action atomically against the locked state: warm the map
// cache with the real request ctx, lock the row, apply, persist, append the
// action log entry and — on game over — insert final results, all in one
// transaction.
//
// The map cache is warmed BEFORE the transaction even begins, via an
// unlocked read of the lobby's pinned (map_id, map_version) — immutable
// once a lobby starts, so reading it outside the tx is safe. This ordering
// matters: warming used to happen after Begin+GetForUpdate, which meant a
// cold cache called MapRepo.LoadDoc (a second pool.QueryRow) while this
// call's own tx already held the FOR UPDATE lock on ttr.game_states — i.e.
// a second pool connection acquired while the first sat idle inside an open
// transaction. Under a small pool and many simultaneously-acting cold
// lobbies that self-deadlocks the pool (see the ttr platform review's M1
// finding). Warming here instead means Apply only ever opens its tx once
// the map is already resolved, so Engine.Apply's own internal resolveMap
// (which has no ctx of its own and falls back to context.Background()) can
// only ever hit a warm in-process cache, never perform real, uncancellable
// I/O inside the transaction — the invariant this code has always
// documented, now actually upheld by the ordering rather than undermined by
// it.
func (s *Session) Apply(ctx context.Context, lobbyID uuid.UUID, action engine.Action) (events []engine.Event, over bool, err error) {
	if err := s.warmMapCache(ctx, lobbyID); err != nil {
		return nil, false, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	state, _, _, _, err := s.repo.GetForUpdate(ctx, tx, lobbyID)
	if err != nil {
		return nil, false, err
	}

	next, events, err := s.eng.Apply(state, action)
	if err != nil {
		return nil, false, err // validation error surfaces to the client unwrapped
	}
	if err := s.repo.Update(ctx, tx, lobbyID, next); err != nil {
		return nil, false, err
	}

	gs, err := unmarshal(next)
	if err != nil {
		return nil, false, err
	}
	actionJSON, err := json.Marshal(action)
	if err != nil {
		return nil, false, fmt.Errorf("marshal ttr action log entry: %w", err)
	}
	if err := s.repo.AppendLog(ctx, tx, lobbyID, action.UserID, gs.Seq, actionJSON); err != nil {
		return nil, false, err
	}

	// gs is already unmarshaled above; read Phase directly rather than
	// having s.eng.IsOver(next) unmarshal the same protobuf blob a third
	// time (state, then next, then next again) just to check one field.
	over = gs.Phase == pb.Phase_PHASE_FINISHED
	if over {
		rows, err := resultRows(gs)
		if err != nil {
			return nil, false, err
		}
		if err := s.repo.InsertResults(ctx, tx, lobbyID, rows); err != nil {
			return nil, false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit tx: %w", err)
	}
	return events, over, nil
}

// warmMapCache reads lobbyID's pinned (map_id, map_version) via an unlocked
// query — safe because that pin never changes once a lobby starts — and
// primes the map cache for it. Apply calls this before opening its
// transaction; see Apply's doc comment for why the ordering matters (the
// ttr platform review's M1 finding). Extracted into its own method so Apply
// itself reads as "warm, then the transaction" rather than inlining the
// two-step lookup.
func (s *Session) warmMapCache(ctx context.Context, lobbyID uuid.UUID) error {
	mapID, mapVersion, err := s.repo.GetMapPin(ctx, s.pool, lobbyID)
	if err != nil {
		return err
	}
	_, err = s.maps.Get(ctx, mapID, mapVersion)
	return err
}

// Resign applies an ActionResign on userID's behalf, reusing Apply's tx
// shape (lock, warm map cache, apply, persist, log, finalize-if-over,
// commit) unchanged. Its events (e.g. "player_resigned", and "game_over" if
// the resignation ends the game) are discarded here: lobby.ResignHandler,
// the interface this method serves, has no channel back to the realtime
// broadcaster — resignation via REST "leave" was never wired for event
// broadcast even before this step, and widening ResignHandler is out of
// this step's scope.
func (s *Session) Resign(ctx context.Context, lobbyID, userID uuid.UUID) (gameOver bool, err error) {
	_, over, err := s.Apply(ctx, lobbyID, engine.Action{UserID: userID, Type: ActionResign})
	return over, err
}

// resultRows converts gs.Results (populated by the engine once the game
// reaches PHASE_FINISHED) into the ResultRow rows StateRepo.InsertResults
// persists.
func resultRows(gs *pb.GameState) ([]ResultRow, error) {
	rows := make([]ResultRow, 0, len(gs.Results))
	for _, sb := range gs.Results {
		b, err := json.Marshal(sb)
		if err != nil {
			return nil, fmt.Errorf("marshal score breakdown seat %d: %w", sb.SeatIndex, err)
		}
		row := ResultRow{SeatIndex: sb.SeatIndex, Total: sb.Total, Rank: sb.Rank, Breakdown: b}
		if p := playerBySeat(gs, sb.SeatIndex); p != nil {
			if uid, err := uuid.Parse(p.UserId); err == nil {
				row.UserID = &uid
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
