package song_guess

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// leaveTestPack is the *Pack fixture for most of this file's scenarios: one
// language, one bucket, three 4-block songs — enough draws for a couple of
// rounds without needing the pack exhausted. Named to avoid colliding with
// the other engine_*_test.go fixtures (same package namespace — see the
// plan's "Discovered during implementation" section).
func leaveTestPack() *Pack {
	return &Pack{
		Name: "Leave Test Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 4, Songs: []Song{
						{ID: "lv4-1", Title: "One", Artist: "Artist One", Line: []string{"a", "b", "c", "d"}},
						{ID: "lv4-2", Title: "Two", Artist: "Artist Two", Line: []string{"e", "f", "g", "h"}},
						{ID: "lv4-3", Title: "Three", Artist: "Artist Three", Line: []string{"i", "j", "k", "l"}},
					}},
				},
			},
		},
		TotalSongs: 3,
	}
}

// leaveFullGamePack is a minimal even pack (one cell, two songs) for
// TestNaturalExhaustionEmitsGameOverOnceWithTiedResults: each song is
// awarded to a different team so the game ends in an exact tie.
func leaveFullGamePack() *Pack {
	return &Pack{
		Name: "Leave Full Game Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 4, Songs: []Song{
						{ID: "lvfg-1", Title: "One", Artist: "Artist One", Line: []string{"a", "b", "c", "d"}},
						{ID: "lvfg-2", Title: "Two", Artist: "Artist Two", Line: []string{"e", "f", "g", "h"}},
					}},
				},
			},
		},
		TotalSongs: 2,
	}
}

// allSongGuessActions enumerates every action constant declared in
// actions.go. TestJudgeVacancyGuardPermitsOnlyAssignJudgeAndResign iterates
// this list to prove row 28's guard blocks everything except assign_judge
// and resign, rather than trusting a hand-picked subset.
var allSongGuessActions = []string{
	ActionAssignTeam, ActionAssignCaptain, ActionAssignJudge, ActionSetTeamName,
	ActionBeginGame, ActionSelectCell, ActionOpenBlock, ActionGuess, ActionSteal,
	ActionPass, ActionJudgeResult, ActionVoid, ActionSkipSong, ActionNextRound,
	ActionResign,
}

// applyResignAndCheck applies resign as actor against state via eng,
// requiring success, then runs assertInvariants and assertTransition on the
// result. Returns the new state bytes, the unmarshalled after-state, and the
// raw events. Mirrors selectCellAndCheck/nextRoundAndCheck/applyPlayAction's
// shared shape.
func applyResignAndCheck(
	t *testing.T, eng *Engine, pack *Pack, state []byte, actor uuid.UUID,
) ([]byte, *pb.GameState, []engine.Event) {
	t.Helper()

	before := requireRosterState(t, state)
	next, events, err := eng.Apply(state, engine.Action{UserID: actor, Type: ActionResign})
	if err != nil {
		t.Fatalf("resign: %v", err)
	}
	after := requireRosterState(t, next)
	assertInvariants(t, pack, after)
	assertTransition(t, pack, before, after)
	return next, after, events
}

// leaveRosterWithSeparateJudge builds a 5-seat roster where the judge and
// the host are different seats — seat0 stays host but ends up on no team
// and not the judge (roleless), seat4 becomes judge — then begins the game.
// This isolates "the judge leaves" and "the host leaves" from each other,
// since the default roster always makes the host the judge. Mirrors
// engine_judge_test.go's TestJudgeResultRejectsNonJudgeActors/host_not_judge
// setup.
func leaveRosterWithSeparateJudge(t *testing.T, pack *Pack, seats []engine.SeatInit, sh Shuffler) (*Engine, []byte) {
	t.Helper()

	eng := NewWithShuffler(&fakeRosterPackProvider{pack: pack}, sh)
	state, _, err := eng.InitState(context.Background(), rosterTestCfg(), seats)
	if err != nil {
		t.Fatalf("InitState: %v", err)
	}
	judgePayload, err := json.Marshal(AssignJudgePayload{Seat: 4})
	if err != nil {
		t.Fatalf("marshal AssignJudgePayload: %v", err)
	}
	state, _, err = eng.Apply(state, engine.Action{UserID: seats[0].UserID, Type: ActionAssignJudge, Payload: judgePayload})
	if err != nil {
		t.Fatalf("assign_judge: %v", err)
	}
	state, _, err = eng.Apply(state, engine.Action{UserID: seats[0].UserID, Type: ActionBeginGame, Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("begin_game: %v", err)
	}
	return eng, state
}

// assertCaptainPromotedFromResign checks the roster half of
// TestCaptainLeaveMidSongPromotesReplacement: the resigning captain (seat2)
// is inactive and no longer captain, and the promoted teammate (seat4) is
// active, captain, and still on TEAM_A.
func assertCaptainPromotedFromResign(t *testing.T, after *pb.GameState) {
	t.Helper()

	seat2 := participantBySeat(after, 2)
	if seat2.GetActive() || seat2.GetIsCaptain() {
		t.Fatalf("seat 2 = (active=%v, is_captain=%v), want (false, false)", seat2.GetActive(), seat2.GetIsCaptain())
	}
	seat4 := participantBySeat(after, 4)
	if !seat4.GetActive() || !seat4.GetIsCaptain() || seat4.GetTeam() != pb.Team_TEAM_A {
		t.Fatalf("seat 4 not promoted correctly: active=%v is_captain=%v team=%s",
			seat4.GetActive(), seat4.GetIsCaptain(), seat4.GetTeam())
	}
}

// assertCaptainLeaveEvents checks the event half: exactly player_left then
// captain_changed{team:a, seat:4}.
func assertCaptainLeaveEvents(t *testing.T, events []engine.Event) {
	t.Helper()

	wantTypes := []string{"player_left", "captain_changed"}
	if len(events) != len(wantTypes) {
		t.Fatalf("events = %v, want %v", events, wantTypes)
	}
	for i, want := range wantTypes {
		if events[i].Type != want {
			t.Fatalf("events[%d].Type = %q, want %q", i, events[i].Type, want)
		}
	}
	if events[1].Data["team"] != "a" || events[1].Data["seat"] != int32(4) {
		t.Fatalf("captain_changed data = %v, want team=a seat=4", events[1].Data)
	}
}

// TestCaptainLeaveMidSongPromotesReplacement checks rows 23-24: a resigning
// captain is marked inactive, the lowest-seat remaining active teammate is
// promoted with captain_changed emitted, the phase is unchanged (still
// PHASE_SONG_PLAY), and the promoted captain can act immediately.
func TestCaptainLeaveMidSongPromotesReplacement(t *testing.T) {
	pack := leaveTestPack()
	// 5 seats: seat0 host/judge, TeamA={seat2 capt, seat4}, TeamB={seat1
	// capt, seat3} (see engine_roster_test.go's
	// TestAssignJudgePromotesReplacementCaptain comment for this shape).
	seats := rosterTestSeats(5)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(40))
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)

	_, after, events := applyResignAndCheck(t, eng, pack, state, seats[2].UserID)

	if after.GetPhase() != pb.Phase_PHASE_SONG_PLAY {
		t.Fatalf("phase = %s, want PHASE_SONG_PLAY (unchanged by the resign)", after.GetPhase())
	}
	assertCaptainPromotedFromResign(t, after)
	assertCaptainLeaveEvents(t, events)

	// The promoted captain (seat4) can act immediately.
	stateBytes, err := marshal(after)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, _, err := eng.Apply(stateBytes, engine.Action{
		UserID: seats[4].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	}); err != nil {
		t.Fatalf("open_block by promoted captain: %v", err)
	}
}

// TestJudgeVacancyGuardPermitsOnlyAssignJudgeAndResign checks row 28: while
// parked in PHASE_JUDGE_VACANT, every action constant except assign_judge
// and resign is rejected with engine.ErrWrongPhase, and the two permitted
// actions genuinely reach their real handlers and succeed.
func TestJudgeVacancyGuardPermitsOnlyAssignJudgeAndResign(t *testing.T) {
	pack := leaveTestPack()
	seats := rosterTestSeats(5)
	eng, state := leaveRosterWithSeparateJudge(t, pack, seats, NewSeededShuffler(41))
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
	state, after, _ := applyResignAndCheck(t, eng, pack, state, seats[4].UserID) // judge resigns.
	if after.GetPhase() != pb.Phase_PHASE_JUDGE_VACANT {
		t.Fatalf("phase = %s, want PHASE_JUDGE_VACANT", after.GetPhase())
	}

	blocked := make([]string, 0, len(allSongGuessActions))
	for _, actionType := range allSongGuessActions {
		if actionType == ActionAssignJudge || actionType == ActionResign {
			continue
		}
		blocked = append(blocked, actionType)
	}
	if len(blocked)+2 != len(allSongGuessActions) {
		t.Fatalf("blocked(%d)+permitted(2) != total(%d)", len(blocked), len(allSongGuessActions))
	}

	for _, actionType := range blocked {
		t.Run(actionType, func(t *testing.T) {
			_, _, err := eng.Apply(state, engine.Action{
				UserID: seats[1].UserID, Type: actionType, Payload: json.RawMessage(`{}`),
			})
			if !errors.Is(err, engine.ErrWrongPhase) {
				t.Fatalf("%s: err = %v, want engine.ErrWrongPhase", actionType, err)
			}
		})
	}

	t.Run("resign_permitted", func(t *testing.T) {
		if _, _, err := eng.Apply(state, engine.Action{UserID: seats[3].UserID, Type: ActionResign}); err != nil {
			t.Fatalf("resign: %v, want success (row 28 permits it)", err)
		}
	})
	t.Run("assign_judge_permitted", func(t *testing.T) {
		payload, err := json.Marshal(AssignJudgePayload{Seat: 3})
		if err != nil {
			t.Fatalf("marshal AssignJudgePayload: %v", err)
		}
		if _, _, err := eng.Apply(state, engine.Action{
			UserID: seats[0].UserID, Type: ActionAssignJudge, Payload: payload,
		}); err != nil {
			t.Fatalf("assign_judge: %v, want success (row 28 permits it)", err)
		}
	})
}

// assertJudgeVacancyRestoresPhase is the shared assertion body for
// TestJudgeVacancyAssignJudgeRestoresExactPhase's two subtests: after the
// host repairs the vacancy, phase is restored to exactly wantPhase,
// resume_phase is cleared, and the new judge (seat3) is installed correctly.
func assertJudgeVacancyRestoresPhase(t *testing.T, after *pb.GameState, wantPhase pb.Phase) {
	t.Helper()

	if after.GetPhase() != wantPhase {
		t.Fatalf("phase = %s, want %s (the exact phase interrupted by the vacancy)", after.GetPhase(), wantPhase)
	}
	if after.GetResumePhase() != pb.Phase_PHASE_UNSPECIFIED {
		t.Fatalf("resume_phase = %s, want PHASE_UNSPECIFIED (cleared once restored)", after.GetResumePhase())
	}
	newJudge := judgeOf(after)
	if newJudge == nil || newJudge.GetSeatIndex() != 3 {
		t.Fatalf("judge = %v, want seat 3", newJudge)
	}
}

// TestJudgeVacancyAssignJudgeRestoresExactPhase checks row 27: the host's
// assign_judge restores gs.Phase to exactly the phase that was interrupted,
// from both PHASE_SONG_PLAY and PHASE_PENDING_JUDGEMENT.
func TestJudgeVacancyAssignJudgeRestoresExactPhase(t *testing.T) {
	t.Run("from_song_play", func(t *testing.T) {
		pack := leaveTestPack()
		seats := rosterTestSeats(5)
		eng, state := leaveRosterWithSeparateJudge(t, pack, seats, NewSeededShuffler(42))
		state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
		state, vacant, _ := applyResignAndCheck(t, eng, pack, state, seats[4].UserID)
		if vacant.GetResumePhase() != pb.Phase_PHASE_SONG_PLAY {
			t.Fatalf("resume_phase = %s, want PHASE_SONG_PLAY (test setup)", vacant.GetResumePhase())
		}

		payload, err := json.Marshal(AssignJudgePayload{Seat: 3})
		if err != nil {
			t.Fatalf("marshal AssignJudgePayload: %v", err)
		}
		next, events, err := eng.Apply(state, engine.Action{UserID: seats[0].UserID, Type: ActionAssignJudge, Payload: payload})
		if err != nil {
			t.Fatalf("assign_judge: %v", err)
		}
		after := requireRosterState(t, next)
		assertInvariants(t, pack, after)
		assertJudgeVacancyRestoresPhase(t, after, pb.Phase_PHASE_SONG_PLAY)

		if findEvent(events, "judge_restored") == nil {
			t.Fatalf("events = %v, want a judge_restored event", events)
		}
		// The active captain (seat2, TEAM_A) can still act.
		if _, _, err := eng.Apply(next, engine.Action{
			UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
		}); err != nil {
			t.Fatalf("open_block after restore: %v", err)
		}
	})

	t.Run("from_pending_judgement", func(t *testing.T) {
		pack := leaveTestPack()
		seats := rosterTestSeats(5)
		eng, state := leaveRosterWithSeparateJudge(t, pack, seats, NewSeededShuffler(43))
		state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "guess"),
		})
		state, vacant, _ := applyResignAndCheck(t, eng, pack, state, seats[4].UserID)
		if vacant.GetResumePhase() != pb.Phase_PHASE_PENDING_JUDGEMENT {
			t.Fatalf("resume_phase = %s, want PHASE_PENDING_JUDGEMENT (test setup)", vacant.GetResumePhase())
		}

		payload, err := json.Marshal(AssignJudgePayload{Seat: 3})
		if err != nil {
			t.Fatalf("marshal AssignJudgePayload: %v", err)
		}
		next, _, err := eng.Apply(state, engine.Action{UserID: seats[0].UserID, Type: ActionAssignJudge, Payload: payload})
		if err != nil {
			t.Fatalf("assign_judge: %v", err)
		}
		after := requireRosterState(t, next)
		assertInvariants(t, pack, after)
		assertJudgeVacancyRestoresPhase(t, after, pb.Phase_PHASE_PENDING_JUDGEMENT)

		// The new judge (seat3) can resolve the still-pending judgement.
		if _, _, err := eng.Apply(next, engine.Action{
			UserID: seats[3].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
		}); err != nil {
			t.Fatalf("judge_result by new judge: %v", err)
		}
	})
}

// TestHostLeaveMovesHostSeatAndNewHostCanSkip checks row 23's host-cache
// refresh: when the host (isolated from the judge, per
// leaveRosterWithSeparateJudge) resigns, gs.HostSeat moves to the new
// lowest active seat, and that new host can immediately exercise a
// host-only action (skip_song).
func TestHostLeaveMovesHostSeatAndNewHostCanSkip(t *testing.T) {
	pack := leaveTestPack()
	seats := rosterTestSeats(5)
	eng, state := leaveRosterWithSeparateJudge(t, pack, seats, NewSeededShuffler(44))
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)

	_, after, events := applyResignAndCheck(t, eng, pack, state, seats[0].UserID) // host (roleless, not judge) resigns.

	if after.GetHostSeat() != 1 {
		t.Fatalf("host_seat = %d, want 1 (the new lowest active seat)", after.GetHostSeat())
	}
	if findEvent(events, "judge_vacant") != nil || findEvent(events, "captain_changed") != nil {
		t.Fatalf("events = %v, want no judge_vacant/captain_changed (host is neither judge nor captain here)", events)
	}

	stateBytes, err := marshal(after)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, _, err := eng.Apply(stateBytes, engine.Action{UserID: seats[1].UserID, Type: ActionSkipSong}); err != nil {
		t.Fatalf("skip_song by new host: %v", err)
	}
}

// TestTeamEmptyingLeaveEndsGameWithCorrectScores checks row 26's
// team-empties branch: TEAM_A's sole member resigns after a scored round,
// ending the game immediately at the score already earned — the departure
// never changes points already awarded.
func TestTeamEmptyingLeaveEndsGameWithCorrectScores(t *testing.T) {
	pack := leaveTestPack()
	seats := rosterTestSeats(4)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(45))
	// Default 4-seat roster: seat0 host/judge, TeamB={seat1 capt, seat3},
	// TeamA={seat2 capt} only (see engine_round_test.go's
	// TestSelectCellRejectsWrongActor comment) — TEAM_A has exactly one
	// member, so seat2 resigning empties it outright.

	// Round 1 (TEAM_A active): correct guess -> +4 TEAM_A.
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "r1"),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})
	state, _, _ = nextRoundAndCheck(t, eng, pack, state, seats[1].UserID) // TEAM_B now active, round 2.

	_, after, events := applyResignAndCheck(t, eng, pack, state, seats[2].UserID) // TEAM_A's sole member leaves.

	if after.GetPhase() != pb.Phase_PHASE_FINISHED {
		t.Fatalf("phase = %s, want PHASE_FINISHED", after.GetPhase())
	}
	if got := teamScore(after, pb.Team_TEAM_A); got != 4 {
		t.Fatalf("TEAM_A score = %d, want 4 (unchanged by the departure)", got)
	}
	if got := teamScore(after, pb.Team_TEAM_B); got != 0 {
		t.Fatalf("TEAM_B score = %d, want 0", got)
	}
	gameOver := findEvent(events, "game_over")
	if gameOver == nil {
		t.Fatalf("events = %v, want a game_over event", events)
	}
	if len(after.GetResults()) != len(seats) {
		t.Fatalf("results has %d entries, want %d (one per participant)", len(after.GetResults()), len(seats))
	}
}

// TestThreePlayerGameEndsWhenAnyoneLeaves checks row 26's fewer-than-3
// branch at the platform minimum (Q2/Req 18: player+player+judge). At
// exactly 3 active participants, ANY departure ends the game immediately —
// tested for each of the three roles (judge/host, and each team's sole
// captain).
func TestThreePlayerGameEndsWhenAnyoneLeaves(t *testing.T) {
	for _, leaverSeat := range []int{0, 1, 2} {
		t.Run(seatRoleName(leaverSeat), func(t *testing.T) {
			pack := leaveTestPack()
			seats := rosterTestSeats(3)
			eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(46))

			_, after, events := applyResignAndCheck(t, eng, pack, state, seats[leaverSeat].UserID)

			if after.GetPhase() != pb.Phase_PHASE_FINISHED {
				t.Fatalf("phase = %s, want PHASE_FINISHED (fewer than 3 active remain)", after.GetPhase())
			}
			if findEvent(events, "game_over") == nil {
				t.Fatalf("events = %v, want a game_over event", events)
			}
		})
	}
}

// seatRoleName names TestThreePlayerGameEndsWhenAnyoneLeaves's subtests by
// the default 3-seat roster's role at that seat (seat0 host/judge, seat1
// TEAM_B captain, seat2 TEAM_A captain).
func seatRoleName(seat int) string {
	switch seat {
	case 0:
		return "judge_host_leaves"
	case 1:
		return "team_b_captain_leaves"
	case 2:
		return "team_a_captain_leaves"
	default:
		return "unknown_seat"
	}
}

// assertAbandonedSongRecorded is the shared assertion body for
// TestAbandonMidSongRecordsInFlightSongAndFinishes's two subtests (R1.3):
// law 1 (checked inside applyResignAndCheck's assertInvariants call) must
// hold at PHASE_FINISHED, and history's last entry must be the abandoned
// song, unawarded, with the right active_team.
func assertAbandonedSongRecorded(t *testing.T, after *pb.GameState, wantSongID string, wantActiveTeam pb.Team) {
	t.Helper()

	if after.GetPhase() != pb.Phase_PHASE_FINISHED {
		t.Fatalf("phase = %s, want PHASE_FINISHED", after.GetPhase())
	}
	if len(after.GetHistory()) == 0 {
		t.Fatalf("history is empty, want the abandoned song appended")
	}
	last := after.GetHistory()[len(after.GetHistory())-1]
	if last.GetSongId() != wantSongID {
		t.Fatalf("history's last entry = song %q, want the abandoned song %q", last.GetSongId(), wantSongID)
	}
	if last.GetAwardedTeam() != pb.Team_TEAM_NONE || last.GetPoints() != 0 || !last.GetSkipped() {
		t.Fatalf("abandoned entry = (awarded=%s, points=%d, skipped=%v), want (TEAM_NONE, 0, true)",
			last.GetAwardedTeam(), last.GetPoints(), last.GetSkipped())
	}
	if last.GetActiveTeam() != wantActiveTeam {
		t.Fatalf("abandoned entry active_team = %s, want %s", last.GetActiveTeam(), wantActiveTeam)
	}
}

// TestAbandonMidSongRecordsInFlightSongAndFinishes is R1.3's dedicated
// regression: a team emptying while a song is in flight must append that
// song to history as an abandoned 0/0 outcome before PHASE_FINISHED is
// reached, or law 1 (song conservation) would fail — the drawn song would
// sit in neither a cell nor history. Covers both isSongInFlight phases:
// PHASE_SONG_PLAY directly, and PHASE_PENDING_JUDGEMENT via the
// PHASE_JUDGE_VACANT ResumePhase substitution (finalize's doc comment).
func TestAbandonMidSongRecordsInFlightSongAndFinishes(t *testing.T) {
	t.Run("song_play", func(t *testing.T) {
		pack := leaveTestPack()
		// 5 seats: TeamA={seat2 capt, seat4}; both leave, so the second
		// resign (seat2, the active captain) empties TEAM_A mid-song.
		seats := rosterTestSeats(5)
		eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(47))
		state, drawn, _ := selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
		songID := drawn.GetSong().GetSongId()

		state, _, _ = applyResignAndCheck(t, eng, pack, state, seats[4].UserID)  // TEAM_A's other member.
		_, after, _ := applyResignAndCheck(t, eng, pack, state, seats[2].UserID) // the active captain: TEAM_A empties.

		assertAbandonedSongRecorded(t, after, songID, pb.Team_TEAM_A)
	})

	t.Run("pending_judgement_via_judge_vacant", func(t *testing.T) {
		// A 3-player game (the platform minimum) where the judge resigns
		// while a judgement is pending: applyResign tentatively parks the
		// game in PHASE_JUDGE_VACANT (ResumePhase=PENDING_JUDGEMENT), but the
		// very same resign also drops active participants below 3, so the
		// row-26 terminal check must win over the parking (per the step
		// brief) — and finalize must still find the in-flight song behind
		// ResumePhase, not gs.Phase (which is JUDGE_VACANT at that instant).
		pack := leaveTestPack()
		seats := rosterTestSeats(3)
		eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(48))
		state, drawn, _ := selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
		songID := drawn.GetSong().GetSongId()
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "guess"),
		})

		_, after, _ := applyResignAndCheck(t, eng, pack, state, seats[0].UserID) // judge/host resigns.

		if after.GetPhase() == pb.Phase_PHASE_JUDGE_VACANT {
			t.Fatalf("phase = PHASE_JUDGE_VACANT, want PHASE_FINISHED (the terminal check must win)")
		}
		assertAbandonedSongRecorded(t, after, songID, pb.Team_TEAM_A)
	})
}

// TestNaturalExhaustionEmitsGameOverOnceWithTiedResults drives a two-song
// pack to natural exhaustion with each song awarded to a different team,
// then checks: PHASE_FINISHED is reached, game_over is emitted exactly
// once, gs.Results has one entry per participant (including the judge, at
// team=NONE/team_total=0), and — since the two awards tie the score — both
// teams rank 1 (a shared victory, Req 16).
func TestNaturalExhaustionEmitsGameOverOnceWithTiedResults(t *testing.T) {
	pack := leaveFullGamePack()
	seats := rosterTestSeats(4)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(49))

	// Round 1 (TEAM_A active): correct guess -> +4 TEAM_A.
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "r1"),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})
	state, _, _ = nextRoundAndCheck(t, eng, pack, state, seats[1].UserID)

	// Round 2 (TEAM_B active, the pack's last song): correct guess -> +4 TEAM_B.
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[1].UserID, "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[1].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "r2"),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})
	_, after, events := nextRoundAndCheck(t, eng, pack, state, seats[2].UserID)

	if after.GetPhase() != pb.Phase_PHASE_FINISHED {
		t.Fatalf("phase = %s, want PHASE_FINISHED", after.GetPhase())
	}
	gameOverCount := 0
	for _, ev := range events {
		if ev.Type == "game_over" {
			gameOverCount++
		}
	}
	if gameOverCount != 1 {
		t.Fatalf("game_over emitted %d times, want exactly 1: %v", gameOverCount, events)
	}
	if len(after.GetResults()) != len(seats) {
		t.Fatalf("results has %d entries, want %d (one per participant)", len(after.GetResults()), len(seats))
	}

	assertTiedResults(t, after)
}

// assertTiedResults checks that gs.Results reflects the 4-4 tie built by
// TestNaturalExhaustionEmitsGameOverOnceWithTiedResults: both teams rank 1,
// and the judge's entry carries team=NONE/team_total=0.
func assertTiedResults(t *testing.T, gs *pb.GameState) {
	t.Helper()

	for _, r := range gs.GetResults() {
		switch r.GetTeam() {
		case pb.Team_TEAM_A, pb.Team_TEAM_B:
			if r.GetTeamTotal() != 4 {
				t.Errorf("seat %d team %s team_total = %d, want 4", r.GetSeatIndex(), r.GetTeam(), r.GetTeamTotal())
			}
			if r.GetRank() != 1 {
				t.Errorf("seat %d team %s rank = %d, want 1 (tie: both teams rank 1)", r.GetSeatIndex(), r.GetTeam(), r.GetRank())
			}
		case pb.Team_TEAM_NONE:
			if !r.GetIsJudge() {
				t.Errorf("seat %d has team=NONE but is_judge=false; every TEAM_NONE participant must be the judge", r.GetSeatIndex())
			}
			if r.GetTeamTotal() != 0 {
				t.Errorf("judge seat %d team_total = %d, want 0", r.GetSeatIndex(), r.GetTeamTotal())
			}
		}
	}
}

// TestGameOverEventLeaksNothing is this step's redaction check: game_over's
// payload must contain only {teams, winner_teams}, and each team entry only
// {team, name, score} — no participant hands, no remaining pack content, no
// unplayed song titles. Confirmed to fail when an unplayed song's title was
// temporarily added to the payload (see the worked report), then reverted.
func TestGameOverEventLeaksNothing(t *testing.T) {
	pack := leaveTestPack()
	seats := rosterTestSeats(3)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(51))

	_, _, events := applyResignAndCheck(t, eng, pack, state, seats[0].UserID) // judge/host leaves: fewer than 3 remain.

	gameOver := findEvent(events, "game_over")
	if gameOver == nil {
		t.Fatalf("events = %v, want a game_over event", events)
	}
	if len(gameOver.Data) != 2 {
		t.Fatalf("game_over.Data has %d keys %v, want exactly 2 (teams, winner_teams)", len(gameOver.Data), gameOver.Data)
	}
	teams, ok := gameOver.Data["teams"].([]map[string]any)
	if !ok || len(teams) != 2 {
		t.Fatalf("game_over.Data[\"teams\"] = %v, want a 2-element []map[string]any", gameOver.Data["teams"])
	}
	for _, team := range teams {
		if len(team) != 3 {
			t.Fatalf("team entry %v has %d keys, want exactly 3 (team, name, score)", team, len(team))
		}
		for _, k := range []string{"team", "name", "score"} {
			if _, ok := team[k]; !ok {
				t.Fatalf("team entry %v missing key %q", team, k)
			}
		}
	}
	if _, ok := gameOver.Data["winner_teams"].([]string); !ok {
		t.Fatalf("game_over.Data[\"winner_teams\"] = %v, want []string", gameOver.Data["winner_teams"])
	}
}

// TestBreakdownForFiltersToAwardedTeam checks breakdownFor's JSON shape
// (Req 36, for song_guess.game_results.breakdown, wired up by Step 12): only
// history entries actually AWARDED to the requested team are included, each
// carrying active_team so the breakdown still shows whether that team
// picked or stole each of its awarded songs (R1.1/R1.7).
func TestBreakdownForFiltersToAwardedTeam(t *testing.T) {
	gs := &pb.GameState{
		History: []*pb.SongOutcome{
			{
				SongId: "s1", LanguageName: "English", Bucket: 4,
				RevealedAtEnd: 0, AwardedTeam: pb.Team_TEAM_A, Points: 4, ActiveTeam: pb.Team_TEAM_A,
			},
			{
				// A steal: TEAM_B is awarded a song TEAM_A was active for.
				SongId: "s2", LanguageName: "English", Bucket: 5,
				RevealedAtEnd: 2, AwardedTeam: pb.Team_TEAM_B, Points: 3, ActiveTeam: pb.Team_TEAM_A,
			},
			{
				// Nobody awarded: must not appear in either team's breakdown.
				SongId: "s3", LanguageName: "English", Bucket: 6,
				RevealedAtEnd: 6, AwardedTeam: pb.Team_TEAM_NONE, Points: 0, ActiveTeam: pb.Team_TEAM_B,
			},
		},
	}

	b, err := breakdownFor(gs, pb.Team_TEAM_A)
	if err != nil {
		t.Fatalf("breakdownFor(TEAM_A): %v", err)
	}
	var awardsA []songAward
	if err := json.Unmarshal(b, &awardsA); err != nil {
		t.Fatalf("unmarshal TEAM_A breakdown: %v", err)
	}
	if len(awardsA) != 1 || awardsA[0].SongID != "s1" || awardsA[0].Points != 4 {
		t.Fatalf("TEAM_A breakdown = %+v, want exactly [s1, points=4]", awardsA)
	}

	b, err = breakdownFor(gs, pb.Team_TEAM_B)
	if err != nil {
		t.Fatalf("breakdownFor(TEAM_B): %v", err)
	}
	var awardsB []songAward
	if err := json.Unmarshal(b, &awardsB); err != nil {
		t.Fatalf("unmarshal TEAM_B breakdown: %v", err)
	}
	if len(awardsB) != 1 || awardsB[0].SongID != "s2" || awardsB[0].ActiveTeam != "a" {
		t.Fatalf("TEAM_B breakdown = %+v, want exactly [s2, active_team=a] (a steal from TEAM_A)", awardsB)
	}
}
