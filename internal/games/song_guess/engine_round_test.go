package song_guess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// roundTestPack is the *Pack fixture for round.go tests: two languages,
// three cells, with English/4 holding exactly two songs (en4-1, en4-2) so
// the cell-draining tests have a natural intersection to exhaust. Named to
// avoid colliding with engine_roster_test.go's fakeRosterPack and
// invariants_test.go's midSongFixture (same package namespace — see the
// plan's "Discovered during implementation" section).
func roundTestPack() *Pack {
	return &Pack{
		Name: "Round Test Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 4, Songs: []Song{
						{ID: "en4-1", Title: "One", Artist: "Artist One", Line: []string{"a", "b", "c", "d"}},
						{
							ID: "en4-2", Title: "Two", Artist: "Artist Two", Line: []string{"e", "f", "g", "h"},
							Media: &Media{VideoID: "dQw4w9WgXcQ", StartSec: 10, EndSec: 20},
						},
					}},
					{Blocks: 5, Songs: []Song{
						{ID: "en5-1", Title: "Three", Artist: "Artist Three", Line: []string{"i", "j", "k", "l", "m"}},
					}},
				},
			},
			{
				Name: "French",
				Buckets: []Bucket{
					{Blocks: 4, Songs: []Song{
						{ID: "fr4-1", Title: "Quatre", Artist: "Artiste Quatre", Line: []string{"n", "o", "p", "q"}},
					}},
				},
			},
		},
		TotalSongs: 4,
	}
}

// roundTestPackTwoSongs is a minimal even pack (one cell, two songs) for the
// "next_round reaches FINISHED" test, which needs the whole pack drained.
func roundTestPackTwoSongs() *Pack {
	return &Pack{
		Name: "Two Song Round Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 4, Songs: []Song{
						{ID: "ts-1", Title: "First", Artist: "Artist A", Line: []string{"a", "b", "c", "d"}},
						{ID: "ts-2", Title: "Second", Artist: "Artist B", Line: []string{"e", "f", "g", "h"}},
					}},
				},
			},
		},
		TotalSongs: 2,
	}
}

// roundSelectState builds an engine (backed by sh) and a PHASE_ROUND_SELECT
// state for pack and seats via InitState + begin_game. The default roster
// is already begin_game-valid (Req 17; TestDefaultRosterSatisfiesBeginGame),
// so no roster editing is needed first.
func roundSelectState(t *testing.T, pack *Pack, seats []engine.SeatInit, sh Shuffler) (*Engine, []byte) {
	t.Helper()

	eng := NewWithShuffler(&fakeRosterPackProvider{pack: pack}, sh)
	state, _, err := eng.InitState(context.Background(), rosterTestCfg(), seats)
	if err != nil {
		t.Fatalf("InitState: %v", err)
	}
	state, _, err = eng.Apply(state, engine.Action{
		UserID:  seats[0].UserID,
		Type:    ActionBeginGame,
		Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("begin_game: %v", err)
	}
	return eng, state
}

// marshalSelectCell builds a select_cell action payload.
func marshalSelectCell(t *testing.T, language string, bucket int32) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(SelectCellPayload{Language: language, Bucket: bucket})
	if err != nil {
		t.Fatalf("marshal SelectCellPayload: %v", err)
	}
	return b
}

// selectCellAndCheck applies select_cell(language, bucket) as actor against
// state, requiring success and exactly one song_selected event, then runs
// assertInvariants and assertTransition on the result. It returns the new
// state bytes, the unmarshalled after-state, and the raw events (for tests
// that need to inspect payload contents beyond the type check already done
// here). Shared by every round.go test that needs a successful draw.
func selectCellAndCheck(
	t *testing.T, eng *Engine, pack *Pack, state []byte, actor uuid.UUID, language string, bucket int32,
) ([]byte, *pb.GameState, []engine.Event) {
	t.Helper()

	before := requireRosterState(t, state)
	next, events, err := eng.Apply(state, engine.Action{
		UserID:  actor,
		Type:    ActionSelectCell,
		Payload: marshalSelectCell(t, language, bucket),
	})
	if err != nil {
		t.Fatalf("select_cell(%s,%d): %v", language, bucket, err)
	}
	after := requireRosterState(t, next)
	assertInvariants(t, pack, after)
	assertTransition(t, pack, before, after)
	if len(events) != 1 || events[0].Type != "song_selected" {
		t.Fatalf("select_cell(%s,%d) events = %v, want exactly one song_selected", language, bucket, events)
	}
	return next, after, events
}

// nextRoundAndCheck applies next_round as actor against state, requiring
// success, then runs assertInvariants and assertTransition on the result.
// It returns the new state bytes, the unmarshalled after-state, and the raw
// events. Shared by every round.go test that needs a successful advance.
func nextRoundAndCheck(t *testing.T, eng *Engine, pack *Pack, state []byte, actor uuid.UUID) ([]byte, *pb.GameState, []engine.Event) {
	t.Helper()

	before := requireRosterState(t, state)
	next, events, err := eng.Apply(state, engine.Action{UserID: actor, Type: ActionNextRound})
	if err != nil {
		t.Fatalf("next_round: %v", err)
	}
	after := requireRosterState(t, next)
	assertInvariants(t, pack, after)
	assertTransition(t, pack, before, after)
	return next, after, events
}

// appendHistoryOutcome appends outcome to gs.History and, if it awarded a
// team, bumps that team's score to match — the by-hand equivalent of Step
// 8's finishSong (not implemented yet; see ruling R1.1/R1.7: "Step 7 appends
// nothing; its next_round tests build history fixtures by hand"). Test
// scaffolding only: production code's single append site is finishSong.
func appendHistoryOutcome(gs *pb.GameState, outcome *pb.SongOutcome) {
	gs.History = append(gs.History, outcome)
	if ts := teamState(gs, outcome.GetAwardedTeam()); ts != nil {
		ts.Score += outcome.GetPoints()
	}
}

// finishCurrentSongByHand simulates Step 8's finishSong for gs's currently
// drawn song: it appends the outcome (0/0, fully revealed, nobody awarded)
// to history and transitions to PHASE_SONG_RESULT, leaving gs.Song populated
// for display exactly as the plan specifies. Returns the finished song's id.
func finishCurrentSongByHand(gs *pb.GameState) string {
	s := gs.GetSong()
	outcome := &pb.SongOutcome{
		SongId:        s.GetSongId(),
		LanguageName:  s.GetLanguageName(),
		Bucket:        s.GetBucket(),
		RevealedAtEnd: int32(len(s.GetBlocks())), // #nosec G115 -- bounded by maxBlocksPerLine (64)
		AwardedTeam:   pb.Team_TEAM_NONE,
		Points:        0,
		Title:         s.GetTitle(),
		Artist:        s.GetArtist(),
		ActiveTeam:    gs.GetActiveTeam(),
	}
	appendHistoryOutcome(gs, outcome)
	gs.Phase = pb.Phase_PHASE_SONG_RESULT
	return s.GetSongId()
}

// TestSelectCellRejectsWrongActor checks row 6's actor guard: a non-captain
// and the opposing team's captain are both rejected with ErrNotYourTurn.
// Seats 0-3 default to: seat0 host/judge, seat1 B-captain, seat2 A-captain,
// seat3 B-member (see engine_roster_test.go's TestAssignCaptainMovesCaptaincy
// comment) — active_team defaults to TEAM_A, so seat2 is the only legal
// actor.
func TestSelectCellRejectsWrongActor(t *testing.T) {
	cases := []struct {
		name string
		seat int
	}{
		{"non_captain", 3},
		{"opposing_captain", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pack := roundTestPack()
			seats := rosterTestSeats(4)
			eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(1))

			_, _, err := eng.Apply(state, engine.Action{
				UserID:  seats[tc.seat].UserID,
				Type:    ActionSelectCell,
				Payload: marshalSelectCell(t, "English", 4),
			})
			if !errors.Is(err, engine.ErrNotYourTurn) {
				t.Fatalf("err = %v, want engine.ErrNotYourTurn", err)
			}
		})
	}
}

// TestSelectCellRejectsWrongPhase checks that select_cell is rejected
// outside PHASE_ROUND_SELECT, both before the game starts (PHASE_ROSTER)
// and after a song is already in play (PHASE_SONG_PLAY).
func TestSelectCellRejectsWrongPhase(t *testing.T) {
	pack := roundTestPack()

	t.Run("roster", func(t *testing.T) {
		seats := rosterTestSeats(4)
		eng := NewWithShuffler(&fakeRosterPackProvider{pack: pack}, NewSeededShuffler(1))
		state, _, err := eng.InitState(context.Background(), rosterTestCfg(), seats)
		if err != nil {
			t.Fatalf("InitState: %v", err)
		}
		_, _, err = eng.Apply(state, engine.Action{
			UserID:  seats[2].UserID,
			Type:    ActionSelectCell,
			Payload: marshalSelectCell(t, "English", 4),
		})
		if !errors.Is(err, engine.ErrWrongPhase) {
			t.Fatalf("err = %v, want engine.ErrWrongPhase", err)
		}
	})

	t.Run("song_play", func(t *testing.T) {
		seats := rosterTestSeats(4)
		eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(1))
		state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)

		_, _, err := eng.Apply(state, engine.Action{
			UserID:  seats[2].UserID,
			Type:    ActionSelectCell,
			Payload: marshalSelectCell(t, "English", 5),
		})
		if !errors.Is(err, engine.ErrWrongPhase) {
			t.Fatalf("err = %v, want engine.ErrWrongPhase", err)
		}
	})
}

// TestSelectCellOnEmptyCellRejectedMutatesNothing checks that a cell which
// exists but has no unplayed songs left is rejected with ErrIllegalAction
// and that the state bytes Apply was given are left untouched.
func TestSelectCellOnEmptyCellRejectedMutatesNothing(t *testing.T) {
	pack := roundTestPack()
	seats := rosterTestSeats(4)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(1))

	gs := requireRosterState(t, state)
	cellFor(gs, "English", 5).UnplayedSongIds = nil
	state = mustMarshalState(t, gs)
	before := append([]byte(nil), state...)

	_, _, err := eng.Apply(state, engine.Action{
		UserID:  seats[2].UserID,
		Type:    ActionSelectCell,
		Payload: marshalSelectCell(t, "English", 5),
	})
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
	}
	if !bytes.Equal(state, before) {
		t.Fatalf("Apply mutated the input state bytes despite returning an error")
	}
}

// TestSelectCellDrainsCellAndRejectsThirdAttempt drives the (English,4)
// cell's two songs out via two real select_cell + next_round round-trips,
// then asserts a third attempt against that now-empty cell is rejected
// while the cell remains present in gs.Cells with played_count == 2 (the
// selection table's shape must never change, per Req 4 / test 8-9).
//
// This is also where law 1's in-flight term (R1.2) is first genuinely
// exercised: selectCellAndCheck's assertInvariants call runs immediately
// after the first successful select_cell, while phase == PHASE_SONG_PLAY
// and the drawn song sits in neither a cell nor history.
func TestSelectCellDrainsCellAndRejectsThirdAttempt(t *testing.T) {
	pack := roundTestPack()
	seats := rosterTestSeats(4)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(7))

	drawn := make(map[string]bool)

	// Round 1: TEAM_A's captain (seat2) draws.
	_, after, _ := selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
	drawn[after.GetSong().GetSongId()] = true
	finishCurrentSongByHand(after)
	state = mustMarshalState(t, after)

	// next_round: TEAM_B's captain (seat1) becomes active.
	state, _, _ = nextRoundAndCheck(t, eng, pack, state, seats[1].UserID)

	// Round 2: TEAM_B's captain (seat1) draws the cell's last song.
	_, after, _ = selectCellAndCheck(t, eng, pack, state, seats[1].UserID, "English", 4)
	drawn[after.GetSong().GetSongId()] = true

	if len(drawn) != 2 || !drawn["en4-1"] || !drawn["en4-2"] {
		t.Fatalf("drawn = %v, want both en4-1 and en4-2 drawn exactly once", drawn)
	}
	cell := cellFor(after, "English", 4)
	if len(cell.GetUnplayedSongIds()) != 0 || cell.GetPlayedCount() != 2 {
		t.Fatalf("cell (English,4) = unplayed=%v played=%d, want unplayed=[] played=2",
			cell.GetUnplayedSongIds(), cell.GetPlayedCount())
	}

	// Finish round 2's song and advance back to PHASE_ROUND_SELECT (active
	// team flips back to TEAM_A) so the third attempt below genuinely
	// exercises the empty-cell guard, rather than short-circuiting on a
	// wrong-phase rejection.
	finishCurrentSongByHand(after)
	state = mustMarshalState(t, after)
	state, _, _ = nextRoundAndCheck(t, eng, pack, state, seats[2].UserID)

	// Third attempt against the now-empty cell: rejected, cell still present.
	_, _, err := eng.Apply(state, engine.Action{
		UserID:  seats[2].UserID,
		Type:    ActionSelectCell,
		Payload: marshalSelectCell(t, "English", 4),
	})
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("third select_cell: err = %v, want engine.ErrIllegalAction", err)
	}
	stillThere := cellFor(requireRosterState(t, state), "English", 4)
	if stillThere == nil {
		t.Fatalf("cell (English,4) was removed from gs.Cells; it must remain present even when fully played")
	}
	if len(stillThere.GetUnplayedSongIds()) != 0 || stillThere.GetPlayedCount() != 2 {
		t.Fatalf("cell (English,4) changed shape after the rejected third attempt: unplayed=%v played=%d",
			stillThere.GetUnplayedSongIds(), stillThere.GetPlayedCount())
	}
}

// TestSongSelectedEventLeaksNothing is this step's redaction check: the
// song_selected event's Data map must contain exactly {language, bucket,
// total_blocks} — no song id, no title. See the worked report for what was
// observed when song_id was temporarily added to this payload and the test
// was confirmed to fail.
func TestSongSelectedEventLeaksNothing(t *testing.T) {
	pack := roundTestPack()
	seats := rosterTestSeats(4)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(3))

	_, _, events := selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)

	data := events[0].Data
	wantKeys := []string{"language", "bucket", "total_blocks"}
	if len(data) != len(wantKeys) {
		t.Fatalf("song_selected event has %d keys %v, want exactly %v", len(data), data, wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := data[k]; !ok {
			t.Fatalf("song_selected event missing key %q: %v", k, data)
		}
	}
}

// TestNextRoundAlternatesActiveTeamAndIncrementsRoundNo drives a full
// select_cell -> (hand-finished) -> next_round cycle and checks row 21's
// core claim: active_team flips to the opponent and round_no increments.
func TestNextRoundAlternatesActiveTeamAndIncrementsRoundNo(t *testing.T) {
	pack := roundTestPack()
	seats := rosterTestSeats(4)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(2))

	_, after, _ := selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4) // TEAM_A captain.
	finishCurrentSongByHand(after)
	state = mustMarshalState(t, after)
	roundNoBefore := after.GetRoundNo()

	_, after, events := nextRoundAndCheck(t, eng, pack, state, seats[1].UserID) // TEAM_B captain.

	if after.GetActiveTeam() != pb.Team_TEAM_B {
		t.Fatalf("active_team = %s, want TEAM_B", after.GetActiveTeam())
	}
	if after.GetRoundNo() != roundNoBefore+1 {
		t.Fatalf("round_no = %d, want %d", after.GetRoundNo(), roundNoBefore+1)
	}
	if after.GetPhase() != pb.Phase_PHASE_ROUND_SELECT {
		t.Fatalf("phase = %s, want PHASE_ROUND_SELECT", after.GetPhase())
	}
	if after.GetSong() != nil {
		t.Fatalf("song = %v, want nil after next_round clears it", after.GetSong())
	}
	if len(events) != 1 || events[0].Type != "round_started" {
		t.Fatalf("events = %v, want exactly one round_started", events)
	}
	want := map[string]any{"round_no": after.GetRoundNo(), "active_team": "b"}
	for k, v := range want {
		if events[0].Data[k] != v {
			t.Fatalf("round_started.Data[%q] = %v, want %v", k, events[0].Data[k], v)
		}
	}
}

// TestNextRoundReachesFinishedWhenNoSongsRemain drains a two-song pack via
// two select_cell/next_round cycles and checks that the second next_round
// (whose captain, per row 21, may be the host) reaches PHASE_FINISHED and
// emits exactly Step 10's finalize-owned game_over event, while active_team
// still flips and round_no does not change. Both songs finish 0/0 by hand
// (finishCurrentSongByHand), so both teams tie at score 0 — winner_teams
// names both.
func TestNextRoundReachesFinishedWhenNoSongsRemain(t *testing.T) {
	pack := roundTestPackTwoSongs()
	seats := rosterTestSeats(4)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(4))

	// Song 1: TEAM_A active.
	_, after, _ := selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
	finishCurrentSongByHand(after)
	state = mustMarshalState(t, after)
	state, after, _ = nextRoundAndCheck(t, eng, pack, state, seats[1].UserID)
	if after.GetPhase() != pb.Phase_PHASE_ROUND_SELECT {
		t.Fatalf("phase after next_round 1 = %s, want PHASE_ROUND_SELECT", after.GetPhase())
	}

	// Song 2 (the pack's last unplayed song): TEAM_B active.
	_, after, _ = selectCellAndCheck(t, eng, pack, state, seats[1].UserID, "English", 4)
	finishCurrentSongByHand(after)
	state = mustMarshalState(t, after)
	roundNoBefore := after.GetRoundNo()

	// The host (seat0) exercises row 21's "or the host" branch.
	_, after, events := nextRoundAndCheck(t, eng, pack, state, seats[0].UserID)

	if after.GetPhase() != pb.Phase_PHASE_FINISHED {
		t.Fatalf("phase = %s, want PHASE_FINISHED", after.GetPhase())
	}
	if after.GetActiveTeam() != pb.Team_TEAM_A {
		t.Fatalf("active_team = %s, want TEAM_A (flipped from TEAM_B)", after.GetActiveTeam())
	}
	if after.GetRoundNo() != roundNoBefore {
		t.Fatalf("round_no = %d, want unchanged at %d", after.GetRoundNo(), roundNoBefore)
	}
	if len(events) != 1 || events[0].Type != "game_over" {
		t.Fatalf("events = %v, want exactly one game_over (Step 10's finalize)", events)
	}
	if len(after.GetResults()) != len(seats) {
		t.Fatalf("results has %d entries, want %d (one per participant)", len(after.GetResults()), len(seats))
	}
	if len(after.GetHistory()) != pack.TotalSongs {
		t.Fatalf("history has %d entries, want %d (pack fully played)", len(after.GetHistory()), pack.TotalSongs)
	}
}

// TestNextRoundRejectsWrongActor checks row 21's actor guard: the CURRENT
// active team's captain (not the one about to become active) and a
// non-captain, non-host participant are both rejected.
func TestNextRoundRejectsWrongActor(t *testing.T) {
	pack := roundTestPack()
	seats := rosterTestSeats(4)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(5))

	_, after, _ := selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
	finishCurrentSongByHand(after) // active_team is still TEAM_A at PHASE_SONG_RESULT.
	state = mustMarshalState(t, after)

	cases := []struct {
		name string
		seat int
	}{
		{"current_active_captain", 2}, // TEAM_A captain: not the next captain, not the host.
		{"non_captain_non_host", 3},   // TEAM_B member.
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := eng.Apply(state, engine.Action{UserID: seats[tc.seat].UserID, Type: ActionNextRound})
			if !errors.Is(err, engine.ErrNotYourTurn) {
				t.Fatalf("err = %v, want engine.ErrNotYourTurn", err)
			}
		})
	}
}

// TestNextRoundRejectsWrongPhase checks that next_round is rejected outside
// PHASE_SONG_RESULT, both before any song is picked (PHASE_ROUND_SELECT) and
// while a song is mid-play (PHASE_SONG_PLAY).
func TestNextRoundRejectsWrongPhase(t *testing.T) {
	pack := roundTestPack()

	t.Run("round_select", func(t *testing.T) {
		seats := rosterTestSeats(4)
		eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(6))
		_, _, err := eng.Apply(state, engine.Action{UserID: seats[2].UserID, Type: ActionNextRound})
		if !errors.Is(err, engine.ErrWrongPhase) {
			t.Fatalf("err = %v, want engine.ErrWrongPhase", err)
		}
	})

	t.Run("song_play", func(t *testing.T) {
		seats := rosterTestSeats(4)
		eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(6))
		state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)

		_, _, err := eng.Apply(state, engine.Action{UserID: seats[2].UserID, Type: ActionNextRound})
		if !errors.Is(err, engine.ErrWrongPhase) {
			t.Fatalf("err = %v, want engine.ErrWrongPhase", err)
		}
	})
}
