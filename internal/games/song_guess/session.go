package song_guess

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// Session ties the song_guess Engine to its Postgres-backed state. It
// implements realtime.GameService, lobby.GameInitializer,
// lobby.ConfigValidator and lobby.ResignHandler. It deliberately does NOT
// implement realtime.HandBasedGameService: a song_guess lobby plays exactly
// one pack to a final score (Q4), so realtime.Handler finishes the lobby
// instead of redealing — the ttr.Session precedent, not poker's.
type Session struct {
	pool     *pgxpool.Pool
	repo     *StateRepo
	eng      *Engine
	packs    *PackCache
	packRepo *PackRepo
}

// NewSession creates a song_guess Session.
func NewSession(pool *pgxpool.Pool, repo *StateRepo, eng *Engine, packs *PackCache, packRepo *PackRepo) *Session {
	return &Session{pool: pool, repo: repo, eng: eng, packs: packs, packRepo: packRepo}
}

// GameKey identifies song_guess lobbies.
func (s *Session) GameKey() string { return GameKey }

// ValidateLobbyConfig resolves cfg's "pack_id" (a slug or a UUID string) to
// its latest published version, confirms the document actually parses via
// the pack cache, and returns the normalized config InitState/InitGameState
// expect: {"pack_id": "<uuid>", "pack_version": N, "pack_name": "<name>"}.
//
// Unlike ttr.Session.ValidateLobbyConfig there is deliberately NO default
// pack (Q1): a missing or empty "pack_id" is an error. The seeded
// "example-hits" pack (migration 0012) guarantees the pack list is
// non-empty, but it is never an implicit default — the user's explicit
// decision, since (unlike TTR's single official board) song packs are
// user-authored content with no canonical choice.
func (s *Session) ValidateLobbyConfig(ctx context.Context, cfg map[string]any) (map[string]any, error) {
	raw, ok := cfg["pack_id"]
	if !ok {
		return nil, fmt.Errorf("%w: missing %q", ErrInvalidConfig, "pack_id")
	}
	ref, ok := raw.(string)
	if !ok || ref == "" {
		return nil, fmt.Errorf("%w: %q must be a non-empty string", ErrInvalidConfig, "pack_id")
	}

	summary, err := s.packRepo.GetBySlugOrID(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve pack %q: %w", ref, err)
	}
	if summary.LatestPublishedVersion == nil {
		return nil, fmt.Errorf("%w: pack %q has no published version", ErrPackVersionNotFound, ref)
	}
	version := *summary.LatestPublishedVersion

	if _, err := s.packs.Get(ctx, summary.ID.String(), version); err != nil {
		return nil, fmt.Errorf("load pack %q@%d: %w", ref, version, err)
	}

	return map[string]any{
		"pack_id":      summary.ID.String(),
		"pack_version": version,
		"pack_name":    summary.Name,
	}, nil
}

// InitGameState persists the freshly-built initial state (lobby.GameInitializer).
// state was already produced by Engine.InitState(ctx, config, seats) in
// lobby.Service.Start; this just pins (pack_id, pack_version) from config
// alongside it. The composite FK on song_guess.game_states guarantees the
// pinned version actually exists and is published.
func (s *Session) InitGameState(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID, state []byte, config map[string]any) error {
	packIDStr, version, err := packConfigFrom(config)
	if err != nil {
		return err
	}
	packID, err := uuid.Parse(packIDStr)
	if err != nil {
		return fmt.Errorf("%w: pack_id %q is not a uuid", ErrInvalidConfig, packIDStr)
	}
	return s.repo.Insert(ctx, tx, lobbyID, packID, version, state)
}

// View returns the redacted song_guess view for a viewer. It resolves the
// pinned pack itself via s.packs.Get(ctx, ...) — using the real request ctx —
// and calls Engine.ViewFor rather than Engine.View, so the engine never
// falls back to its own context.Background() pack lookup on this path.
func (s *Session) View(ctx context.Context, lobbyID, userID uuid.UUID) (any, error) {
	state, packID, packVersion, err := s.repo.Get(ctx, s.pool, lobbyID)
	if err != nil {
		return nil, err
	}
	pack, err := s.packs.Get(ctx, packID, packVersion)
	if err != nil {
		return nil, err
	}
	return s.eng.ViewFor(pack, state, userID)
}

// Apply runs one action atomically against the locked state: warm the pack
// cache with the real request ctx, lock the row, apply, persist, append the
// action log entry and — on game over — insert final results, all in one
// transaction.
//
// The pack cache is warmed BEFORE the transaction even begins, via an
// unlocked read of the lobby's pinned (pack_id, pack_version) — immutable
// once a lobby starts, so reading it outside the tx is safe. This ordering
// matters: if warming instead happened after Begin+GetForUpdate, a cold
// cache would call PackRepo.LoadDoc (a second pool.QueryRow) while this
// call's own tx already held the FOR UPDATE lock on song_guess.game_states —
// i.e. a second pool connection acquired while the first sat idle inside an
// open transaction. Under a small pool and many simultaneously-acting cold
// lobbies that self-deadlocks the pool (the ttr platform review's M1
// finding, and the reason root CLAUDE.md lists this as a hard-won gotcha).
// Warming here instead means Apply only ever opens its tx once the pack is
// already resolved, so Engine.Apply's own internal resolvePack (which has no
// ctx of its own and falls back to context.Background()) can only ever hit a
// warm in-process cache, never perform real, uncancellable I/O inside the
// transaction — it is this ordering, and not the comment, that upholds that
// invariant.
func (s *Session) Apply(ctx context.Context, lobbyID uuid.UUID, action engine.Action) (events []engine.Event, over bool, err error) {
	if err := s.warmPackCache(ctx, lobbyID); err != nil {
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
		return nil, false, fmt.Errorf("marshal song_guess action log entry: %w", err)
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

// warmPackCache reads lobbyID's pinned (pack_id, pack_version) via an
// unlocked query — safe because that pin never changes once a lobby starts —
// and primes the pack cache for it. Apply calls this before opening its
// transaction; see Apply's doc comment for why the ordering matters (the ttr
// platform review's M1 finding). Extracted into its own method so Apply
// itself reads as "warm, then the transaction" rather than inlining the
// two-step lookup.
func (s *Session) warmPackCache(ctx context.Context, lobbyID uuid.UUID) error {
	packID, packVersion, err := s.repo.GetPackPin(ctx, s.pool, lobbyID)
	if err != nil {
		return err
	}
	_, err = s.packs.Get(ctx, packID, packVersion)
	return err
}

// Resign applies an ActionResign on userID's behalf, reusing Apply's tx
// shape (lock, warm pack cache, apply, persist, log, finalize-if-over,
// commit) unchanged. Its events (e.g. "player_left", and "game_over" if the
// resignation ends the game) are discarded here: lobby.ResignHandler, the
// interface this method serves, has no channel back to the realtime
// broadcaster — resignation via REST "leave" was never wired for event
// broadcast even before this step (see ttr.Session.Resign), and widening
// ResignHandler is out of this step's scope.
//
// lobby.Service.resignFromInProgressGame already tolerates
// engine.ErrGameOver / ErrNotSeated / ErrIllegalAction as no-ops (Req 21),
// so a leaving player is never locked out of their platform-wide single
// seat — no change to lobby/service.go is needed for this.
func (s *Session) Resign(ctx context.Context, lobbyID, userID uuid.UUID) (gameOver bool, err error) {
	_, over, err := s.Apply(ctx, lobbyID, engine.Action{UserID: userID, Type: ActionResign})
	return over, err
}

// resultRows converts gs.Results (populated by the engine once the game
// reaches PHASE_FINISHED) into the ResultRow rows StateRepo.InsertResults
// persists — one row per participant, judge included (Req 36).
//
// The judge's Team is TEAM_NONE, and breakdownFor(gs, TEAM_NONE) must NOT be
// called for them: TEAM_NONE also marks every unawarded (0/0) history entry
// (TestBreakdownForFiltersToAwardedTeam asserts such an entry appears in
// neither team's breakdown), so calling breakdownFor with it would
// mislabel every unclaimed song as one of "the judge's awards". The judge
// has no awards by construction (Req 17: the judge is on no team), so their
// breakdown is always the empty JSON array.
func resultRows(gs *pb.GameState) ([]ResultRow, error) {
	rows := make([]ResultRow, 0, len(gs.Results))
	for _, re := range gs.Results {
		b := []byte("[]")
		if re.GetTeam() != pb.Team_TEAM_NONE {
			var err error
			b, err = breakdownFor(gs, re.GetTeam())
			if err != nil {
				return nil, fmt.Errorf("marshal breakdown seat %d: %w", re.GetSeatIndex(), err)
			}
		}
		row := ResultRow{
			SeatIndex: re.GetSeatIndex(),
			Team:      teamWire(re.GetTeam()),
			Total:     re.GetTeamTotal(),
			Rank:      re.GetRank(),
			IsJudge:   re.GetIsJudge(),
			Breakdown: b,
		}
		if uid, err := uuid.Parse(re.GetUserId()); err == nil {
			row.UserID = &uid
		}
		rows = append(rows, row)
	}
	return rows, nil
}
