package song_guess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/serediukit/bgex-backend/internal/games/engine"
)

// TestIntegration_PackRepoAndSession exercises PackRepo, StateRepo and
// Session against a real Postgres instance end to end: create a pack,
// publish a version, create a lobby pinned to it, build the initial state
// (mirroring what lobby.Service.Start does), and drive a full, short game
// through Session.Apply — the exact call sequence realtime.Handler uses in
// production. It then asserts:
//   - action_log has exactly one row per applied action, with contiguous seq;
//   - game_results has exactly one row per participant (players AND judge);
//   - View for a user who is connected but not seated (a spectator) contains
//     no song title anywhere in its marshalled JSON;
//   - the composite FK from game_states to pack_versions genuinely rejects
//     deleting a pack version that a lobby still pins.
//
// Skipped unless SONG_GUESS_DB_DSN is set, so `make test` stays green with no
// database configured — mirrors ttr/integration_test.go's TTR_DB_DSN gate.
func TestIntegration_PackRepoAndSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB integration test in -short mode")
	}
	dsn := os.Getenv("SONG_GUESS_DB_DSN")
	if dsn == "" {
		t.Skip("set SONG_GUESS_DB_DSN to a real Postgres DSN to run this test, e.g. " +
			"SONG_GUESS_DB_DSN=postgres://bgex:bgex@localhost:5432/bgex?sslmode=disable go test ./internal/games/song_guess/ -run Integration -v")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to %s: %v", dsn, err)
	}
	// Registered first so it runs LAST (t.Cleanup is LIFO): every other
	// Cleanup below issues a DELETE against pool and must run before it
	// closes.
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping db: %v", err)
	}

	// --- fixtures: three throwaway users — host/judge, TEAM_B captain,
	// TEAM_A captain — seat the 3-participant minimum game (Req 18: judge +
	// one player per team). action_log.user_id / game_results.user_id /
	// packs.created_by all have real FKs to users(id). ---
	hostJudge := mustCreateUser(t, ctx, pool)
	teamBCaptain := mustCreateUser(t, ctx, pool)
	teamACaptain := mustCreateUser(t, ctx, pool)

	packRepo := NewPackRepo(pool)
	summary := createAndPublishTestPack(t, ctx, packRepo, hostJudge)
	packID, version := summary.ID, *summary.LatestPublishedVersion

	packCache := NewPackCache(packRepo)
	sess := NewSession(pool, NewStateRepo(), NewWithShuffler(packCache, NewSeededShuffler(7)), packCache, packRepo)

	// --- ValidateLobbyConfig: resolves "pack_id" to its pinned version,
	// exactly like lobby.Service.Create would call it. ---
	cfg, err := sess.ValidateLobbyConfig(ctx, map[string]any{"pack_id": packID.String()})
	if err != nil {
		t.Fatalf("ValidateLobbyConfig: %v", err)
	}
	if cfg["pack_id"] != packID.String() || cfg["pack_version"] != version {
		t.Fatalf("ValidateLobbyConfig: got %+v, want pack_id=%s pack_version=%d", cfg, packID, version)
	}

	// ValidateLobbyConfig must reject a missing pack_id — there is
	// deliberately no default pack (Q1, unlike ttr's defaultMapSlug).
	if _, err := sess.ValidateLobbyConfig(ctx, map[string]any{}); err == nil {
		t.Fatalf("ValidateLobbyConfig with no pack_id unexpectedly succeeded — song_guess has no default pack")
	}

	// --- Build the initial state exactly as lobby.Service.Start does:
	// Engine.InitState, then Session.InitGameState pins it inside a tx. ---
	seats := []engine.SeatInit{
		{Seat: 0, UserID: hostJudge},
		{Seat: 1, UserID: teamBCaptain},
		{Seat: 2, UserID: teamACaptain},
	}
	eng := NewWithShuffler(packCache, NewSeededShuffler(7))
	state, _, err := eng.InitState(ctx, cfg, seats)
	if err != nil {
		t.Fatalf("InitState: %v", err)
	}

	lobbyID := mustCreateLobby(t, ctx, pool, hostJudge)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := sess.InitGameState(ctx, tx, lobbyID, state, cfg); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("InitGameState: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit InitGameState tx: %v", err)
	}

	spectator := uuid.New() // never seated in this lobby
	secretsMidSong := []string{"First Secret Title", "First Artist", "itest-song-1", "itest-song-2"}
	seq := driveFullGame(t, ctx, sess, lobbyID, hostJudge, teamACaptain, teamBCaptain, func() {
		checkViewLeaksNone(t, ctx, sess, lobbyID, spectator, secretsMidSong)
	})

	checkActionLogContiguous(t, ctx, pool, lobbyID, seq)
	checkGameResultsOneRowPerParticipant(t, ctx, pool, lobbyID, len(seats))
	// Post-FINISHED, the raw song ids must STILL never appear (view.go: "the
	// pack's remaining song ids ... NEVER emitted to anyone, in any phase") —
	// unlike titles, which Q5 deliberately makes public once a song ends.
	checkViewLeaksNone(t, ctx, sess, lobbyID, spectator, []string{"itest-song-1", "itest-song-2"})
	checkPinnedVersionDeleteRejected(t, ctx, packRepo, packID, version)
}

// createAndPublishTestPack creates a pack with two songs in one
// (language, bucket) cell (an even total, so it satisfies the parity
// validator with the minimum content needed to drive one full round each
// way) and publishes it.
func createAndPublishTestPack(t *testing.T, ctx context.Context, packRepo *PackRepo, createdBy uuid.UUID) *PackSummary {
	t.Helper()

	slug := "sg-itest-" + uuid.NewString()[:8]
	created, err := packRepo.CreatePack(ctx, createdBy, slug, "Integration Test Pack", "Tester")
	if err != nil {
		t.Fatalf("CreatePack: %v", err)
	}
	t.Cleanup(func() {
		if err := packRepo.DeletePack(context.Background(), created.ID); err != nil {
			t.Logf("cleanup: delete pack %s: %v", created.ID, err)
		}
	})

	doc := map[string]any{
		"packName":   "Integration Test Pack",
		"packAuthor": "Tester",
		"version":    2,
		"languages": []any{
			map[string]any{
				"name": "english",
				"buckets": map[string]any{
					"4": []any{
						map[string]any{
							"id": "itest-song-1", "title": "First Secret Title", "artist": "First Artist",
							"line": []any{"one", "two", "three", "four"},
						},
						map[string]any{
							"id": "itest-song-2", "title": "Second Secret Title", "artist": "Second Artist",
							"line": []any{"five", "six", "seven", "eight"},
						},
					},
				},
			},
		},
	}
	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal pack doc: %v", err)
	}
	sum := sha256.Sum256(docBytes)
	sha := hex.EncodeToString(sum[:])

	version, err := packRepo.UpsertDraft(ctx, created.ID, docBytes, sha, true, 2)
	if err != nil {
		t.Fatalf("UpsertDraft: %v", err)
	}
	if err := packRepo.Publish(ctx, created.ID, version); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	summary, err := packRepo.GetBySlugOrID(ctx, created.ID.String())
	if err != nil {
		t.Fatalf("GetBySlugOrID after publish: %v", err)
	}
	if summary.LatestPublishedVersion == nil || *summary.LatestPublishedVersion != version {
		t.Fatalf("GetBySlugOrID: LatestPublishedVersion = %v, want %d", summary.LatestPublishedVersion, version)
	}
	return summary
}

// driveFullGame plays the 2-song pack to PHASE_FINISHED through
// Session.Apply exclusively — the same seam realtime.Handler drives in
// production — and returns the final gs.Seq (== the number of actions
// applied, since Engine.Apply bumps Seq by exactly one per successful
// call). Roster defaults (Req 17) seat hostJudge as judge, teamBCaptain as
// TEAM_B's captain (seat 1, odd), and teamACaptain as TEAM_A's captain
// (seat 2, promoted after the host vacates TEAM_A's default captaincy) —
// see engine.go's defaultParticipants/applyHostJudgeDefault.
//
// midSong is called once, right after round 1's select_cell, while the song
// is still genuinely in play (PHASE_SONG_PLAY, nobody has guessed yet) — the
// point at which a spectator view leaking the title would be a real bug
// (Req 25), as opposed to after SONG_RESULT/FINISHED, where the title is
// deliberately public to everyone (Q5) and asserting its absence there would
// be asserting the wrong thing.
func driveFullGame(
	t *testing.T, ctx context.Context, sess *Session, lobbyID uuid.UUID,
	hostJudge, teamACaptain, teamBCaptain uuid.UUID, midSong func(),
) int64 {
	t.Helper()

	var lastSeq int64
	apply := func(actor uuid.UUID, actionType string, payload any) []engine.Event {
		t.Helper()
		var raw json.RawMessage
		if payload != nil {
			b, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal %s payload: %v", actionType, err)
			}
			raw = b
		}
		events, over, err := sess.Apply(ctx, lobbyID, engine.Action{UserID: actor, Type: actionType, Payload: raw})
		if err != nil {
			t.Fatalf("Apply(%s): %v", actionType, err)
		}
		lastSeq++
		if over {
			// Every apply() call in this helper happens strictly before the
			// pack is exhausted — only the trailing next_round (applied
			// directly by the caller, not through this closure) is expected
			// to finalize the game.
			t.Fatalf("Apply(%s): game ended unexpectedly early", actionType)
		}
		return events
	}

	apply(hostJudge, ActionBeginGame, BeginGamePayload{})

	// Round 1: TEAM_A is active by default (begin_game with no
	// starting_team). Draw a song, guess immediately, judge confirms
	// correct.
	apply(teamACaptain, ActionSelectCell, SelectCellPayload{Language: "english", Bucket: 4})
	midSong()
	apply(teamACaptain, ActionGuess, GuessPayload{Guess: "first guess"})
	apply(hostJudge, ActionJudgeResult, JudgeResultPayload{Correct: true})
	apply(hostJudge, ActionNextRound, nil)

	// Round 2: active_team flipped to TEAM_B. Same shape; this exhausts the
	// pack's only remaining song, so the round's judge_result leaves the
	// game at PHASE_SONG_RESULT (over=false — judge_result never finalizes
	// by itself, only next_round does, per transition table row 21). The
	// TRAILING next_round is what actually finds unplayedRemaining()==0 and
	// finalizes, so it's the one call expected to report over=true.
	apply(teamBCaptain, ActionSelectCell, SelectCellPayload{Language: "english", Bucket: 4})
	apply(teamBCaptain, ActionGuess, GuessPayload{Guess: "second guess"})
	apply(hostJudge, ActionJudgeResult, JudgeResultPayload{Correct: true})

	events, over, err := sess.Apply(ctx, lobbyID, engine.Action{UserID: hostJudge, Type: ActionNextRound})
	if err != nil {
		t.Fatalf("Apply(final next_round): %v", err)
	}
	lastSeq++
	if !over {
		t.Fatalf("Apply(final next_round): expected the game to be over (pack exhausted), got over=false")
	}
	if !hasEventType(events, "game_over") {
		t.Fatalf("Apply(final next_round): expected a game_over event among %+v", events)
	}

	return lastSeq
}

func hasEventType(events []engine.Event, typ string) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// checkActionLogContiguous confirms song_guess.action_log has exactly one
// row per applied action, with seq running 1..wantCount contiguously (the
// (lobby_id, seq) primary key would already reject a duplicate; this checks
// there are no gaps either, i.e. every Apply call actually appended).
func checkActionLogContiguous(t *testing.T, ctx context.Context, pool *pgxpool.Pool, lobbyID uuid.UUID, wantCount int64) {
	t.Helper()

	rows, err := pool.Query(ctx, `SELECT seq FROM song_guess.action_log WHERE lobby_id = $1 ORDER BY seq`, lobbyID)
	if err != nil {
		t.Fatalf("query action_log: %v", err)
	}
	defer rows.Close()

	var seqs []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatalf("scan action_log seq: %v", err)
		}
		seqs = append(seqs, seq)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate action_log: %v", err)
	}

	if int64(len(seqs)) != wantCount {
		t.Fatalf("action_log row count = %d, want %d (one per applied action)", len(seqs), wantCount)
	}
	for i, seq := range seqs {
		want := int64(i + 1)
		if seq != want {
			t.Fatalf("action_log.seq[%d] = %d, want %d (contiguous starting at 1)", i, seq, want)
		}
	}
}

// checkGameResultsOneRowPerParticipant confirms song_guess.game_results has
// exactly one row per seated participant, judge included (Req 36).
func checkGameResultsOneRowPerParticipant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, lobbyID uuid.UUID, wantCount int) {
	t.Helper()

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM song_guess.game_results WHERE lobby_id = $1`, lobbyID).Scan(&count); err != nil {
		t.Fatalf("count game_results: %v", err)
	}
	if count != wantCount {
		t.Fatalf("game_results row count = %d, want %d (one per participant)", count, wantCount)
	}

	var judgeCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM song_guess.game_results WHERE lobby_id = $1 AND is_judge`, lobbyID).Scan(&judgeCount); err != nil {
		t.Fatalf("count game_results (judge): %v", err)
	}
	if judgeCount != 1 {
		t.Fatalf("game_results judge row count = %d, want exactly 1", judgeCount)
	}
}

// checkViewLeaksNone confirms Session.View for forUser (here always a
// spectator: connected but never seated in this lobby, per Req 24) contains
// none of the given secret substrings anywhere in its marshalled JSON.
// Session.View must go through Engine.ViewFor with the real ctx, never
// Engine.View's context.Background() fallback (Req 40) — this call is
// exactly that path.
//
// Called twice with deliberately different secret sets (see the two call
// sites): mid-song, a spectator must see neither title/artist NOR the raw
// song id (Req 25); once the game reaches FINISHED, titles become public to
// everyone by design (Q5) so only the raw song id remains a permanent leak
// to check for (view.go: "the pack's remaining song ids ... NEVER emitted to
// anyone, in any phase").
func checkViewLeaksNone(t *testing.T, ctx context.Context, sess *Session, lobbyID, forUser uuid.UUID, secrets []string) {
	t.Helper()

	view, err := sess.View(ctx, lobbyID, forUser)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	b, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	body := string(b)

	for _, leaked := range secrets {
		if strings.Contains(body, leaked) {
			t.Fatalf("view leaks %q: %s", leaked, body)
		}
	}
}

// checkPinnedVersionDeleteRejected confirms the composite FK from
// song_guess.game_states to song_guess.pack_versions genuinely rejects
// deleting a pack version a lobby still pins — the pin can never dangle
// (Req 32). PackRepo has no direct "delete one version" method, so this
// exercises the constraint at the SQL level directly, exactly as the FK
// would be exercised by any future handler that tried to add one.
func checkPinnedVersionDeleteRejected(t *testing.T, ctx context.Context, packRepo *PackRepo, packID uuid.UUID, version int32) {
	t.Helper()

	// Same package as PackRepo, so packRepo.pool is reachable directly —
	// PackRepo exposes no "delete one version" method (versions are meant
	// to be immutable once published), so this exercises the FK at the SQL
	// level exactly as any future caller attempting it would hit it.
	_, err := packRepo.pool.Exec(ctx, `DELETE FROM song_guess.pack_versions WHERE pack_id = $1 AND version = $2`, packID, version)
	if err == nil {
		t.Fatalf("deleting a pack version still pinned by a game_states row unexpectedly succeeded")
	}
}

// mustCreateUser inserts a throwaway user row (email is the only required
// column) and registers its cleanup.
func mustCreateUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	email := fmt.Sprintf("song-guess-itest-%s@example.test", uuid.NewString())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&id); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id); err != nil {
			t.Logf("cleanup: delete user %s: %v", id, err)
		}
	})
	return id
}

// mustCreateLobby inserts a throwaway game_lobbies row (game-agnostic table,
// shared with poker/ttr) hosted by hostID and registers its cleanup.
// Deleting it cascades to song_guess.game_states/action_log/game_results.
func mustCreateLobby(t *testing.T, ctx context.Context, pool *pgxpool.Pool, hostID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO game_lobbies (game_key, name, host_id, max_seats) VALUES ($1, $2, $3, $4) RETURNING id`,
		GameKey, "song_guess integration test lobby", hostID, 10,
	).Scan(&id)
	if err != nil {
		t.Fatalf("create test lobby: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM game_lobbies WHERE id = $1`, id); err != nil {
			t.Logf("cleanup: delete lobby %s: %v", id, err)
		}
	})
	return id
}
