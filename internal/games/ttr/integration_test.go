package ttr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	"github.com/serediukit/bgex-backend/internal/games/ttr/mapdata"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// TestIntegration_MapRepoAndStateRepo exercises MapRepo and StateRepo against
// a real Postgres instance end to end: create a map, upsert/publish a
// version, load it back, create a game state pinned to that version, lock
// and update it under a transaction, append an action-log entry and, once
// the (deliberately tiny, 2-seat) game reaches PHASE_FINISHED via a resign,
// insert its final results.
//
// This is the step-14 substitute for the REST curl smoke test: the
// composition root does not register "ttr" until a later step, so there is
// no HTTP path to create a TTR lobby yet. Skipped unless TTR_DB_DSN is set
// (or -short is not passed and the caller opts in), so `make test` stays
// green with no database configured. The bulk of the work is split into
// plain (non-subtest) helper functions purely to keep this function's own
// cyclomatic complexity down; a t.Fatalf in any of them still aborts the
// whole test, since they run in the same goroutine.
func TestIntegration_MapRepoAndStateRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB integration test in -short mode")
	}
	dsn := os.Getenv("TTR_DB_DSN")
	if dsn == "" {
		t.Skip("set TTR_DB_DSN to a real Postgres DSN to run this test, e.g. " +
			"TTR_DB_DSN=postgres://bgex:bgex@localhost:5432/bgex?sslmode=disable go test ./internal/games/ttr/ -run TestIntegration -v")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to %s: %v", dsn, err)
	}
	// Registered first so it runs LAST (t.Cleanup is LIFO): every other
	// Cleanup below issues a DELETE against pool and must run before it
	// closes. A plain `defer pool.Close()` would instead run as soon as this
	// function returns — before any t.Cleanup callback — and every cleanup
	// DELETE would fail against a closed pool.
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping db: %v", err)
	}

	// --- fixtures: two throwaway users seat the two players; the lobby is
	// hosted by the first. action_log.user_id / game_results.user_id /
	// maps.created_by all have real FKs to users(id), so these can't be
	// arbitrary UUIDs. ---
	user1 := mustCreateUser(t, ctx, pool)
	user2 := mustCreateUser(t, ctx, pool)

	mapRepo := NewMapRepo(pool)
	slug := "ttr-itest-" + uuid.NewString()[:8]
	summary, err := mapRepo.CreateMap(ctx, slug, "Integration Test Map", user1)
	if err != nil {
		t.Fatalf("CreateMap: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM ttr.maps WHERE id = $1`, summary.ID); err != nil {
			t.Logf("cleanup: delete map %s: %v", summary.ID, err)
		}
	})

	v1 := checkUpsertDraftAndPublish(t, ctx, mapRepo, summary.ID)
	checkDraftImmutability(t, ctx, mapRepo, summary.ID, v1)
	checkLoaderAndListing(t, ctx, mapRepo, summary.ID, slug, v1)
	checkAssetRoundTrip(t, ctx, pool, mapRepo, user1)

	// --- Engine: build real initial state pinned to (summary.ID, v1) via
	// MapCache, exercising MapRepo.LoadDoc through the same MapLoader path
	// Session uses. ---
	eng := NewWithShuffler(NewMapCache(mapRepo), NewSeededShuffler(42))
	cfg := map[string]any{"map_id": summary.ID.String(), "map_version": v1}
	seats := []engine.SeatInit{{Seat: 0, UserID: user1}, {Seat: 1, UserID: user2}}
	state, _, err := eng.InitState(ctx, cfg, seats)
	if err != nil {
		t.Fatalf("InitState: %v", err)
	}

	lobbyID := mustCreateLobby(t, ctx, pool, user1)
	repo := NewStateRepo()
	checkStateRepoInsert(t, ctx, pool, repo, lobbyID, summary.ID, v1, state)
	checkApplyCommitAndVerify(t, ctx, pool, repo, eng, lobbyID, state, user2)
}

// checkUpsertDraftAndPublish creates the map's first version, publishes it,
// and confirms publishing it again is rejected (ErrVersionPublished) rather
// than silently re-stamped.
func checkUpsertDraftAndPublish(t *testing.T, ctx context.Context, mapRepo *MapRepo, mapID uuid.UUID) int32 {
	t.Helper()

	// mapdata.EuropeV1 is a real, fully-valid map document (it's what
	// migration 0008 seeds as the official map) — reusing it here means the
	// engine round-trip later runs against genuine board data instead of a
	// hand-rolled fixture.
	v1, err := mapRepo.UpsertDraft(ctx, mapID, mapdata.EuropeV1)
	if err != nil {
		t.Fatalf("UpsertDraft: %v", err)
	}
	if v1 != 1 {
		t.Fatalf("UpsertDraft: got version %d, want 1 (first version for a new map)", v1)
	}

	if err := mapRepo.Publish(ctx, mapID, v1); err != nil {
		t.Fatalf("Publish(%d): %v", v1, err)
	}
	if err := mapRepo.Publish(ctx, mapID, v1); !errors.Is(err, ErrVersionPublished) {
		t.Fatalf("Publish(%d) again: got %v, want ErrVersionPublished", v1, err)
	}

	status, doc, err := mapRepo.GetVersion(ctx, mapID, v1)
	if err != nil {
		t.Fatalf("GetVersion(%d): %v", v1, err)
	}
	if status != "published" {
		t.Fatalf("GetVersion(%d): status = %q, want published", v1, status)
	}
	assertJSONEqual(t, fmt.Sprintf("GetVersion(%d)", v1), doc, mapdata.EuropeV1)
	return v1
}

// checkDraftImmutability confirms UpsertDraft's two-sided contract: a second
// call (with v1 already published) creates a new version rather than
// touching v1, and a third call overwrites that new draft in place rather
// than creating yet another version — while v1's published document stays
// byte-for-byte (JSON-equal) untouched throughout.
func checkDraftImmutability(t *testing.T, ctx context.Context, mapRepo *MapRepo, mapID uuid.UUID, v1 int32) {
	t.Helper()

	v2, err := mapRepo.UpsertDraft(ctx, mapID, mapdata.EuropeV1)
	if err != nil {
		t.Fatalf("UpsertDraft (2nd): %v", err)
	}
	if v2 != v1+1 {
		t.Fatalf("UpsertDraft (2nd): got version %d, want %d (max(version)+1, since v1 is published)", v2, v1+1)
	}

	modifiedDoc := append([]byte(nil), mapdata.EuropeV1...)
	modifiedDoc = append(modifiedDoc, '\n') // trivially different bytes; UpsertDraft does not validate map JSON
	v3, err := mapRepo.UpsertDraft(ctx, mapID, modifiedDoc)
	if err != nil {
		t.Fatalf("UpsertDraft (3rd): %v", err)
	}
	if v3 != v2 {
		t.Fatalf("UpsertDraft (3rd): got version %d, want %d (overwrite the existing draft, not a new version)", v3, v2)
	}

	status2, doc2, err := mapRepo.GetVersion(ctx, mapID, v2)
	if err != nil {
		t.Fatalf("GetVersion(%d) after overwrite: %v", v2, err)
	}
	if status2 != "draft" {
		t.Fatalf("GetVersion(%d): status = %q, want draft", v2, status2)
	}
	assertJSONEqual(t, fmt.Sprintf("GetVersion(%d) after overwrite", v2), doc2, modifiedDoc)

	_, docStillV1, err := mapRepo.GetVersion(ctx, mapID, v1)
	if err != nil {
		t.Fatalf("GetVersion(%d) after drafting v2/v3: %v", v1, err)
	}
	assertJSONEqual(t, fmt.Sprintf("published version %d must be untouched by a later UpsertDraft", v1), docStillV1, mapdata.EuropeV1)
}

// checkLoaderAndListing confirms LoadDoc (the MapLoader interface method)
// and LatestPublished return the same document, and that the map shows up
// in every listing/lookup path (ListPublished, ListAll, GetBySlugOrID by
// both slug and id).
func checkLoaderAndListing(t *testing.T, ctx context.Context, mapRepo *MapRepo, mapID uuid.UUID, slug string, v1 int32) {
	t.Helper()

	loaded, err := mapRepo.LoadDoc(ctx, mapID.String(), v1)
	if err != nil {
		t.Fatalf("LoadDoc: %v", err)
	}
	assertJSONEqual(t, "LoadDoc", loaded, mapdata.EuropeV1)

	latestVersion, latestDoc, err := mapRepo.LatestPublished(ctx, mapID)
	if err != nil {
		t.Fatalf("LatestPublished: %v", err)
	}
	if latestVersion != v1 {
		t.Fatalf("LatestPublished: version = %d, want %d (the newer draft doesn't count)", latestVersion, v1)
	}
	assertJSONEqual(t, "LatestPublished", latestDoc, mapdata.EuropeV1)

	published, err := mapRepo.ListPublished(ctx)
	if err != nil {
		t.Fatalf("ListPublished: %v", err)
	}
	if !containsMapID(published, mapID) {
		t.Fatalf("ListPublished: does not contain %s", mapID)
	}
	all, err := mapRepo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if !containsMapID(all, mapID) {
		t.Fatalf("ListAll: does not contain %s", mapID)
	}

	bySlug, err := mapRepo.GetBySlugOrID(ctx, slug)
	if err != nil {
		t.Fatalf("GetBySlugOrID(slug): %v", err)
	}
	if bySlug.ID != mapID {
		t.Fatalf("GetBySlugOrID(slug): got %s, want %s", bySlug.ID, mapID)
	}
	byID, err := mapRepo.GetBySlugOrID(ctx, mapID.String())
	if err != nil {
		t.Fatalf("GetBySlugOrID(id): %v", err)
	}
	if byID.ID != mapID {
		t.Fatalf("GetBySlugOrID(id): got %s, want %s", byID.ID, mapID)
	}
}

// checkAssetRoundTrip confirms InsertAsset/GetAsset round-trip a background
// image's mime type and bytes exactly (map_assets.bytes is BYTEA, not
// JSONB, so an exact byte comparison is correct here, unlike the doc
// checks above).
func checkAssetRoundTrip(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mapRepo *MapRepo, createdBy uuid.UUID) {
	t.Helper()

	assetBytes := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	sum := sha256.Sum256(assetBytes)
	assetID, err := mapRepo.InsertAsset(ctx, "image/png", assetBytes, hex.EncodeToString(sum[:]), createdBy)
	if err != nil {
		t.Fatalf("InsertAsset: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM ttr.map_assets WHERE id = $1`, assetID); err != nil {
			t.Logf("cleanup: delete asset %s: %v", assetID, err)
		}
	})

	mime, data, err := mapRepo.GetAsset(ctx, assetID)
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if mime != "image/png" || string(data) != string(assetBytes) {
		t.Fatalf("GetAsset: round-trip mismatch")
	}
}

// checkStateRepoInsert confirms the ttr.game_states -> ttr.map_versions
// composite FK rejects a state row pinned to a nonexistent map version, and
// that the real insert (pinned to the actually-published version) succeeds.
func checkStateRepoInsert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *StateRepo, lobbyID, mapID uuid.UUID, v1 int32, state []byte) {
	t.Helper()

	if err := repo.Insert(ctx, pool, lobbyID, mapID, 999, state); err == nil {
		t.Fatalf("Insert with a nonexistent map version unexpectedly succeeded — the composite FK should have rejected it")
	}
	if err := repo.Insert(ctx, pool, lobbyID, mapID, v1, state); err != nil {
		t.Fatalf("Insert: %v", err)
	}
}

// checkApplyCommitAndVerify runs one full Apply cycle — exactly the shape
// ttr.Session.Apply uses — then confirms the commit landed and that a
// duplicate seq is rejected by the (lobby_id, seq) primary key. Split into
// lockAndApplyResign/persistAndCommit purely to keep each function's own
// cyclomatic complexity down.
func checkApplyCommitAndVerify(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *StateRepo, eng *Engine, lobbyID uuid.UUID, state []byte, resigningUser uuid.UUID) {
	t.Helper()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	next, gs, action := lockAndApplyResign(t, ctx, tx, repo, eng, lobbyID, state, resigningUser)
	actionJSON := persistAndCommit(t, ctx, tx, repo, lobbyID, next, gs, action)
	committed = true

	verifyCommittedRows(t, ctx, pool, repo, lobbyID, next)
	checkDuplicateSeqRejected(t, ctx, pool, repo, lobbyID, action.UserID, gs.Seq, actionJSON)
}

// lockAndApplyResign locks lobbyID's state row (FOR UPDATE, via tx) and
// applies a resign action for resigningUser through the engine. With only 2
// seats, this resigns the game down to 1 active player, which ends it
// immediately (plan Q14) — asserted here via eng.IsOver.
func lockAndApplyResign(t *testing.T, ctx context.Context, tx pgx.Tx, repo *StateRepo, eng *Engine, lobbyID uuid.UUID, state []byte, resigningUser uuid.UUID) (next []byte, gs *pb.GameState, action engine.Action) {
	t.Helper()

	gotState, _, _, gotVersion, err := repo.GetForUpdate(ctx, tx, lobbyID)
	if err != nil {
		t.Fatalf("GetForUpdate: %v", err)
	}
	if string(gotState) != string(state) {
		t.Fatalf("GetForUpdate: state does not match what was inserted")
	}
	if gotVersion != 0 {
		t.Fatalf("GetForUpdate: version = %d, want 0 (freshly inserted)", gotVersion)
	}

	action = engine.Action{UserID: resigningUser, Type: ActionResign}
	next, _, err = eng.Apply(gotState, action)
	if err != nil {
		t.Fatalf("engine.Apply(resign): %v", err)
	}
	if !eng.IsOver(next) {
		t.Fatalf("engine.Apply(resign): expected the game to be over (2 seats, 1 resigned)")
	}

	gs, err = unmarshal(next)
	if err != nil {
		t.Fatalf("unmarshal next state: %v", err)
	}
	return next, gs, action
}

// persistAndCommit writes next, appends the action-log entry (using the
// engine's own monotonic gs.Seq), inserts gs.Results (populated because the
// game just ended) and commits tx. Returns the marshaled action JSON so the
// caller can reuse it for the duplicate-seq check.
func persistAndCommit(t *testing.T, ctx context.Context, tx pgx.Tx, repo *StateRepo, lobbyID uuid.UUID, next []byte, gs *pb.GameState, action engine.Action) []byte {
	t.Helper()

	if err := repo.Update(ctx, tx, lobbyID, next); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if gs.Seq != 1 {
		t.Fatalf("gs.Seq = %d, want 1 after exactly one successful Apply", gs.Seq)
	}
	actionJSON, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("marshal action: %v", err)
	}
	if err := repo.AppendLog(ctx, tx, lobbyID, action.UserID, gs.Seq, actionJSON); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	rows, err := resultRows(gs)
	if err != nil {
		t.Fatalf("resultRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("resultRows: got %d rows, want 2 (one per seat)", len(rows))
	}
	if err := repo.InsertResults(ctx, tx, lobbyID, rows); err != nil {
		t.Fatalf("InsertResults: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	return actionJSON
}

// checkDuplicateSeqRejected confirms a second action-log entry at the same
// seq collides on the (lobby_id, seq) primary key — i.e. that gs.Seq really
// is treated as a hard uniqueness requirement, not just a convention. Run in
// its own throwaway transaction: a duplicate-key error poisons the
// surrounding transaction in Postgres, so this must not share a tx with
// anything the caller still needs to commit.
func checkDuplicateSeqRejected(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *StateRepo, lobbyID, userID uuid.UUID, seq int64, actionJSON []byte) {
	t.Helper()

	probeTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin probe tx: %v", err)
	}
	defer probeTx.Rollback(ctx) //nolint:errcheck
	if err := repo.AppendLog(ctx, probeTx, lobbyID, userID, seq, actionJSON); err == nil {
		t.Fatalf("AppendLog with a duplicate seq unexpectedly succeeded")
	}
}

// verifyCommittedRows confirms, from outside the transaction, that the
// state, action-log and game-results writes actually landed.
func verifyCommittedRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *StateRepo, lobbyID uuid.UUID, wantState []byte) {
	t.Helper()

	finalState, _, _, err := repo.Get(ctx, pool, lobbyID)
	if err != nil {
		t.Fatalf("Get after commit: %v", err)
	}
	if string(finalState) != string(wantState) {
		t.Fatalf("Get after commit: state does not match what Update wrote")
	}

	var resultCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ttr.game_results WHERE lobby_id = $1`, lobbyID).Scan(&resultCount); err != nil {
		t.Fatalf("count game_results: %v", err)
	}
	if resultCount != 2 {
		t.Fatalf("ttr.game_results row count = %d, want 2", resultCount)
	}

	var logCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ttr.action_log WHERE lobby_id = $1`, lobbyID).Scan(&logCount); err != nil {
		t.Fatalf("count action_log: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("ttr.action_log row count = %d, want 1", logCount)
	}
}

// assertJSONEqual compares got and want as JSON values rather than as raw
// bytes: doc columns are JSONB, which does not preserve whitespace or object
// key order, so a byte-for-byte comparison against what was originally
// written would spuriously fail even when the document is unchanged.
func assertJSONEqual(t *testing.T, context string, got, want []byte) {
	t.Helper()
	var gotVal, wantVal any
	if err := json.Unmarshal(got, &gotVal); err != nil {
		t.Fatalf("%s: got is not valid JSON: %v", context, err)
	}
	if err := json.Unmarshal(want, &wantVal); err != nil {
		t.Fatalf("%s: want is not valid JSON: %v", context, err)
	}
	if !reflect.DeepEqual(gotVal, wantVal) {
		t.Fatalf("%s: JSON documents differ", context)
	}
}

// containsMapID reports whether summaries includes id.
func containsMapID(summaries []MapSummary, id uuid.UUID) bool {
	for _, s := range summaries {
		if s.ID == id {
			return true
		}
	}
	return false
}

// mustCreateUser inserts a throwaway user row (email is the only required
// column) and registers its cleanup.
func mustCreateUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	email := fmt.Sprintf("ttr-itest-%s@example.test", uuid.NewString())
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
// shared with poker) hosted by hostID and registers its cleanup. Deleting it
// cascades to ttr.game_states/action_log/game_results.
func mustCreateLobby(t *testing.T, ctx context.Context, pool *pgxpool.Pool, hostID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO game_lobbies (game_key, name, host_id, max_seats) VALUES ($1, $2, $3, $4) RETURNING id`,
		GameKey, "ttr integration test lobby", hostID, 2,
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
