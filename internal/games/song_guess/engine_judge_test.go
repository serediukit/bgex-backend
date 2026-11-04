package song_guess

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// judgeTestPack is the *Pack fixture for judge.go tests: one language with
// four cells (buckets 4/5/6/7), one song each, so every rules-file scenario
// this step covers has a distinct intersection to draw from. Named to avoid
// colliding with play.go's playTestPack / round.go's roundTestPack /
// roster.go's fakeRosterPack (same package namespace — see the plan's
// "Discovered during implementation" section).
func judgeTestPack() *Pack {
	return &Pack{
		Name: "Judge Test Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 4, Songs: []Song{
						{ID: "j4-1", Title: "Four", Artist: "Artist Four", Line: []string{"a", "b", "c", "d"}},
					}},
					{Blocks: 5, Songs: []Song{
						{ID: "j5-1", Title: "Five", Artist: "Artist Five", Line: []string{"a", "b", "c", "d", "e"}},
					}},
					{Blocks: 6, Songs: []Song{
						{ID: "j6-1", Title: "Six", Artist: "Artist Six", Line: []string{"a", "b", "c", "d", "e", "f"}},
					}},
					{Blocks: 7, Songs: []Song{
						{ID: "j7-1", Title: "Seven", Artist: "Artist Seven", Line: []string{"a", "b", "c", "d", "e", "f", "g"}},
					}},
				},
			},
		},
		TotalSongs: 4,
	}
}

// judgeFullGamePack is a single-cell, four-song pack (one language, one
// bucket) for TestScoreConservationOverFullGame: which specific song is
// drawn each round doesn't matter for a score-conservation check, only that
// four rounds are played end to end.
func judgeFullGamePack() *Pack {
	return &Pack{
		Name: "Judge Full Game Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 4, Songs: []Song{
						{ID: "fg-1", Title: "One", Artist: "Artist One", Line: []string{"a", "b", "c", "d"}},
						{ID: "fg-2", Title: "Two", Artist: "Artist Two", Line: []string{"e", "f", "g", "h"}},
						{ID: "fg-3", Title: "Three", Artist: "Artist Three", Line: []string{"i", "j", "k", "l"}},
						{ID: "fg-4", Title: "Four", Artist: "Artist Four", Line: []string{"m", "n", "o", "p"}},
					}},
				},
			},
		},
		TotalSongs: 4,
	}
}

// marshalJudgeResultPayload builds a judge_result action payload.
func marshalJudgeResultPayload(t *testing.T, correct bool) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(JudgeResultPayload{Correct: correct})
	if err != nil {
		t.Fatalf("marshal JudgeResultPayload: %v", err)
	}
	return b
}

// revealBlocksViaStealDeclineLoop drives eng/state's current song to exactly
// n revealed blocks (indices 0..n-1, in order) by open_block+pass, the
// active captain's seat being activeSeat and the opponent's opponentSeat.
// Under architect ruling R3's C2, the FIRST open_block (revealed==0) always
// reveals directly — there is no steal window to decline yet — so only the
// second and later opens in the loop call pass. This replaces the pre-R3
// "open_next then pass every time" loop, which assumed a steal window
// opened on every single reveal including the first.
func revealBlocksViaStealDeclineLoop(
	t *testing.T, eng *Engine, pack *Pack, state []byte, activeSeat, opponentSeat uuid.UUID, n int,
) []byte {
	t.Helper()
	for i := range n {
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: activeSeat, Type: ActionOpenBlock, Payload: openBlockPayload(t, int32(i)),
		})
		if i > 0 {
			state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{UserID: opponentSeat, Type: ActionPass})
		}
	}
	return state
}

// judgeActiveGuessState drives pack/seats to PHASE_PENDING_JUDGEMENT with
// pending.kind == JUDGE_ACTIVE_GUESS: the active captain (seats[2]) opens
// nReveal blocks via revealBlocksViaStealDeclineLoop, then guesses. Shared
// by every test that needs an active-guess judgement pending at a specific
// revealed count. nReveal may be 0 (a cold guess): unaffected by C2, since
// guessing never opens or declines a steal window.
func judgeActiveGuessState(
	t *testing.T, pack *Pack, seats []engine.SeatInit, sh Shuffler, language string, bucket int32, nReveal int,
) (*Engine, []byte) {
	t.Helper()
	eng, state := playSongPlayState(t, pack, seats, sh, language, bucket)
	state = revealBlocksViaStealDeclineLoop(t, eng, pack, state, seats[2].UserID, seats[1].UserID, nReveal)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "my guess"),
	})
	return eng, state
}

// judgeStealState drives pack/seats to PHASE_PENDING_JUDGEMENT with
// pending.kind == JUDGE_STEAL: nReveal blocks are opened via
// revealBlocksViaStealDeclineLoop, then one more open_block (index nReveal)
// opens the (nReveal+1)th steal window, which the opponent captain
// (seats[1]) steals into. nReveal must be >= 1 under C2: opening the very
// first block (nReveal == 0) never offers a steal window at all — there is
// no earlier reveal for C2's "revealed > 0" condition to see — so a
// JUDGE_STEAL pending genuinely cannot exist at nReveal == 0 any more (see
// TestOpponentCannotStealBeforeFirstReveal, engine_play_test.go).
func judgeStealState(
	t *testing.T, pack *Pack, seats []engine.SeatInit, sh Shuffler, language string, bucket int32, nReveal int,
) (*Engine, []byte) {
	t.Helper()
	if nReveal < 1 {
		t.Fatalf("judgeStealState: nReveal must be >= 1 under C2 (no steal window opens at revealed==0), got %d", nReveal)
	}
	eng, state := playSongPlayState(t, pack, seats, sh, language, bucket)
	state = revealBlocksViaStealDeclineLoop(t, eng, pack, state, seats[2].UserID, seats[1].UserID, nReveal)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, int32(nReveal)),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[1].UserID, Type: ActionSteal, Payload: marshalGuessPayload(t, "steal guess"),
	})
	return eng, state
}

// teamScore reads gs's TeamState.Score for team, defensively (nil-safe via
// proto getters) so a fixture missing an entry reads as zero rather than
// panicking.
func teamScore(gs *pb.GameState, team pb.Team) int32 {
	return teamState(gs, team).GetScore()
}

// TestJudgeActiveGuessCorrectScenario1 is rules scenario 1: 5 blocks, 0
// revealed, the active team's guess is judged correct -> +5 to the active
// team, 0 to the opponent.
func TestJudgeActiveGuessCorrectScenario1(t *testing.T) {
	pack := judgeTestPack()
	seats := rosterTestSeats(4)
	eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(1), "English", 5, 0)

	_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})

	if got := teamScore(after, pb.Team_TEAM_A); got != 5 {
		t.Fatalf("TEAM_A score = %d, want 5", got)
	}
	if got := teamScore(after, pb.Team_TEAM_B); got != 0 {
		t.Fatalf("TEAM_B score = %d, want 0", got)
	}
	if after.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
		t.Fatalf("phase = %s, want PHASE_SONG_RESULT", after.GetPhase())
	}
	wantTypes := []string{"judged", "points_awarded", "song_finished"}
	if len(events) != len(wantTypes) {
		t.Fatalf("events = %v, want %v", events, wantTypes)
	}
	for i, want := range wantTypes {
		if events[i].Type != want {
			t.Fatalf("events[%d].Type = %q, want %q", i, events[i].Type, want)
		}
	}
}

// TestJudgeActiveGuessCorrectScenario2 is rules scenario 2: 7 blocks, 3
// revealed, judged correct -> +4 to the active team.
func TestJudgeActiveGuessCorrectScenario2(t *testing.T) {
	pack := judgeTestPack()
	seats := rosterTestSeats(4)
	eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(2), "English", 7, 3)

	_, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})

	if got := teamScore(after, pb.Team_TEAM_A); got != 4 {
		t.Fatalf("TEAM_A score = %d, want 4 (7 blocks - 3 revealed)", got)
	}
}

// TestJudgeStealCorrectScenario3 is rules scenario 3: 6 blocks, 2 revealed,
// a steal judged correct -> +4 to the OPPONENT, never the active team.
func TestJudgeStealCorrectScenario3(t *testing.T) {
	pack := judgeTestPack()
	seats := rosterTestSeats(4)
	eng, state := judgeStealState(t, pack, seats, NewSeededShuffler(3), "English", 6, 2)

	_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})

	if got := teamScore(after, pb.Team_TEAM_B); got != 4 {
		t.Fatalf("TEAM_B (opponent) score = %d, want 4 (6 blocks - 2 revealed)", got)
	}
	if got := teamScore(after, pb.Team_TEAM_A); got != 0 {
		t.Fatalf("TEAM_A (active) score = %d, want 0", got)
	}
	last := after.GetHistory()[len(after.GetHistory())-1]
	if last.GetActiveTeam() == last.GetAwardedTeam() {
		t.Fatalf("history active_team and awarded_team both = %s, want them to differ (a steal awards the opponent)",
			last.GetActiveTeam())
	}
	wantTypes := []string{"judged", "points_awarded", "song_finished"}
	if len(events) != len(wantTypes) {
		t.Fatalf("events = %v, want %v", events, wantTypes)
	}
}

// TestJudgeStealCorrectAtOneRevealedScoresBucketMinusOne is R3.4 item 5: a
// steal at exactly one block revealed — the earliest a steal window can
// ever open under C2 (TestOpponentCannotStealBeforeFirstReveal forbids
// nReveal==0) — judged correct awards the opponent exactly bucket-1,
// confirming C2 didn't shift the steal window's first reachable potential
// away from "one block already open".
func TestJudgeStealCorrectAtOneRevealedScoresBucketMinusOne(t *testing.T) {
	pack := judgeTestPack() // English/4 bucket: 4 blocks
	seats := rosterTestSeats(4)
	eng, state := judgeStealState(t, pack, seats, NewSeededShuffler(70), "English", 4, 1)

	_, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})

	if got := teamScore(after, pb.Team_TEAM_B); got != 3 {
		t.Fatalf("TEAM_B (opponent) score = %d, want 3 (4 blocks - 1 revealed)", got)
	}
	if got := teamScore(after, pb.Team_TEAM_A); got != 0 {
		t.Fatalf("TEAM_A (active) score = %d, want 0", got)
	}
}

// TestSongFinishesWithHoleScoresCorrectly is R3.4 item 6: opening blocks 0
// and 3 (a non-contiguous "hole" at indices 1 and 2) on a 5-block song,
// then the active team guessing correctly, must score exactly 5-2=3 and
// record SongOutcome.revealed_at_end=2 — C1's free choice must never let a
// non-contiguous reveal set perturb scoring away from a simple count.
func TestSongFinishesWithHoleScoresCorrectly(t *testing.T) {
	pack := judgeTestPack() // English/5 bucket: 5 blocks
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(71), "English", 5)

	// Block 0 opens directly (C2: no steal window at revealed==0).
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	// Block 3 (leaving 1 and 2 as a hole) opens a steal window; the
	// opponent declines, so it reveals via the pass path.
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 3),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[1].UserID, Type: ActionPass,
	})

	after := requireRosterState(t, state)
	wantMask := []bool{true, false, false, true, false}
	if got := after.GetSong().GetRevealedMask(); !slices.Equal(got, wantMask) {
		t.Fatalf("revealed_mask = %v, want %v (a hole at indices 1,2)", got, wantMask)
	}

	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "correct guess"),
	})
	_, final, _ := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})

	if got := teamScore(final, pb.Team_TEAM_A); got != 3 {
		t.Fatalf("TEAM_A score = %d, want 3 (5 blocks - 2 revealed)", got)
	}
	last := final.GetHistory()[len(final.GetHistory())-1]
	if last.GetRevealedAtEnd() != 2 {
		t.Fatalf("history revealed_at_end = %d, want 2", last.GetRevealedAtEnd())
	}
	if last.GetAwardedTeam() != pb.Team_TEAM_A || last.GetPoints() != 3 {
		t.Fatalf("history awarded = (%s, %d), want (TEAM_A, 3)", last.GetAwardedTeam(), last.GetPoints())
	}
}

// TestJudgeStealIncorrectScenario4 is rules scenario 4: a failed steal locks
// out further steals for the rest of this song, and — the part naive code
// gets wrong — the reveal the open_block that opened the window still owed
// happens in the SAME Apply call as the incorrect verdict, not a follow-up
// action. A subsequent open_block then opens no steal window at all: the
// active team continues alone.
//
// nReveal=1 (not 0, as this test used before ruling R3): C2 forbids a steal
// window opening at revealed==0, so judgeStealState requires at least one
// block already revealed before the window this test exercises can exist.
// Block 0 is that pre-reveal; block 1 is the one placed on offer and then
// owed back after the failed steal.
func TestJudgeStealIncorrectScenario4(t *testing.T) {
	pack := judgeTestPack()
	seats := rosterTestSeats(4)
	eng, state := judgeStealState(t, pack, seats, NewSeededShuffler(4), "English", 4, 1)

	_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
	})

	if after.GetSong().GetStealAllowed() {
		t.Fatalf("steal_allowed = true, want false after a failed steal")
	}
	if revealedCount(after.GetSong()) != 2 {
		t.Fatalf("revealed = %d, want 2 (block 0 from setup, block 1 the owed reveal in this same Apply call)", revealedCount(after.GetSong()))
	}
	if after.GetPhase() != pb.Phase_PHASE_SONG_PLAY {
		t.Fatalf("phase = %s, want PHASE_SONG_PLAY (potential=2, song continues)", after.GetPhase())
	}
	if after.GetSong().GetAwaiting() != pb.Awaiting_AWAITING_ACTIVE {
		t.Fatalf("awaiting = %s, want AWAITING_ACTIVE (the active team continues alone)", after.GetSong().GetAwaiting())
	}
	wantTypes := []string{"judged", "steal_locked_out", "block_revealed"}
	if len(events) != len(wantTypes) {
		t.Fatalf("events = %v, want %v", events, wantTypes)
	}
	for i, want := range wantTypes {
		if events[i].Type != want {
			t.Fatalf("events[%d].Type = %q, want %q", i, events[i].Type, want)
		}
	}
	revealed := findEvent(events, "block_revealed")
	if got := revealed.Data["index"]; got != int32(1) {
		t.Fatalf("block_revealed.index = %v, want 1 (the block the failed steal owed, not an arbitrary one)", got)
	}

	// A subsequent open_block must NOT reopen a steal window: steal_allowed
	// is now false, so it reveals immediately.
	state = mustMarshalState(t, after)
	_, after2, events2 := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 2),
	})
	if len(events2) != 1 || events2[0].Type != "block_revealed" {
		t.Fatalf("open_block events after lockout = %v, want exactly one block_revealed (no steal window)", events2)
	}
	if revealedCount(after2.GetSong()) != 3 {
		t.Fatalf("revealed = %d, want 3", revealedCount(after2.GetSong()))
	}
}

// TestJudgeStealIncorrectRevealsTheOwedIndexNotAnArbitraryOne is ruling
// R3.3's required test, written first and confirmed to fail before the fix:
// with free-choice reveals (C1), a failed steal must reveal the SPECIFIC
// block the active captain chose to open — pending.owed_reveal_index — never
// the lowest closed index and never a hardcoded block. The active captain
// opens block 2 of a 4-block song (after block 0 is already revealed, so
// C2's steal window can open at all); the opponent steals; the judge rules
// incorrect; block 2 must be the one that comes back revealed, and blocks 1
// and 3 must stay untouched.
//
// Falsified by temporarily hardcoding
//
//	return revealAt(gs, 0, events)
//
// in judge.go's applyJudgeStealIncorrect in place of
// revealAt(gs, pending.GetOwedRevealIndex(), events): the test failed with
//
//	engine_judge_test.go:334: judge_result: illegal action: block 0 is
//	already revealed
//
// (block 0 was already revealed by this test's own setup, so the
// hardcoded-0 bug didn't even silently reveal the wrong block — revealAt's
// own already-revealed guard rejected it outright, and because R3.3
// requires that error to be PROPAGATED rather than swallowed,
// applyJudgeResult surfaced it as a failed Apply call. This is an even
// stronger confirmation than a wrong-mask assertion would have been: it is
// exactly R3.2's note that "index is user-supplied... must propagate it"
// paying off — a wrong hardcoded index doesn't just corrupt state
// quietly, it fails loudly). The hardcoded change was reverted immediately
// after.
func TestJudgeStealIncorrectRevealsTheOwedIndexNotAnArbitraryOne(t *testing.T) {
	pack := judgeTestPack() // English/4 bucket: blocks [a,b,c,d]
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(50), "English", 4)

	// Reveal block 0 directly first (C2: no steal window at revealed==0).
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	// The active captain opens block 2 specifically — NOT the lowest closed
	// index (which is 1) — exactly C1's free choice.
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 2),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[1].UserID, Type: ActionSteal, Payload: marshalGuessPayload(t, "steal guess"),
	})

	_, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
	})

	mask := after.GetSong().GetRevealedMask()
	want := []bool{true, false, true, false}
	for i, w := range want {
		if mask[i] != w {
			t.Fatalf("revealed_mask = %v, want %v (block 2 — the captain's chosen index — revealed, block 1 untouched)", mask, want)
		}
	}
}

// TestJudgeLastChanceScenario5 is rules scenario 5's both branches: a wrong
// active guess opens a last chance for the opponent; if the opponent then
// guesses correctly they get the current potential, and if they guess wrong
// the song ends at 0/0.
func TestJudgeLastChanceScenario5(t *testing.T) {
	t.Run("opponent_correct", func(t *testing.T) {
		pack := judgeTestPack()
		seats := rosterTestSeats(4)
		eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(5), "English", 5, 0)

		state, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
		})
		if after.GetSong().GetAwaiting() != pb.Awaiting_AWAITING_LAST_CHANCE {
			t.Fatalf("awaiting = %s, want AWAITING_LAST_CHANCE", after.GetSong().GetAwaiting())
		}
		lastChance := findEvent(events, "last_chance_opened")
		if lastChance == nil || lastChance.Data["team"] != "b" {
			t.Fatalf("last_chance_opened = %v, want team=b (the opponent of active TEAM_A)", events)
		}

		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[1].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "opponent guess"),
		})
		_, after, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
		})

		if got := teamScore(after, pb.Team_TEAM_B); got != 5 {
			t.Fatalf("TEAM_B (opponent, last chance) score = %d, want 5 (the full potential)", got)
		}
		if got := teamScore(after, pb.Team_TEAM_A); got != 0 {
			t.Fatalf("TEAM_A score = %d, want 0", got)
		}
	})

	t.Run("opponent_wrong", func(t *testing.T) {
		pack := judgeTestPack()
		seats := rosterTestSeats(4)
		eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(6), "English", 5, 0)

		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
		})
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[1].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "opponent guess"),
		})
		_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
		})

		if got := teamScore(after, pb.Team_TEAM_A); got != 0 {
			t.Fatalf("TEAM_A score = %d, want 0", got)
		}
		if got := teamScore(after, pb.Team_TEAM_B); got != 0 {
			t.Fatalf("TEAM_B score = %d, want 0", got)
		}
		last := after.GetHistory()[len(after.GetHistory())-1]
		if last.GetAwardedTeam() != pb.Team_TEAM_NONE || last.GetPoints() != 0 {
			t.Fatalf("history entry = (%s, %d), want (TEAM_NONE, 0)", last.GetAwardedTeam(), last.GetPoints())
		}
		wantTypes := []string{"judged", "song_finished"}
		if len(events) != len(wantTypes) {
			t.Fatalf("events = %v, want %v", events, wantTypes)
		}
	})
}

// TestWrongGuessAfterFailedStealStillGrantsLastChance is the Req 10 branch a
// naive reading of rules §7.1 gets wrong: a wrong active-team guess grants
// the opponent's last chance even when that same opponent's own steal
// already failed on this song (steal_allowed already false). The last
// chance is a separate mechanic from the steal.
//
// nReveal=1 (not 0, as this test used before ruling R3): C2 forbids a steal
// window opening at revealed==0, so judgeStealState requires at least one
// block already revealed first.
func TestWrongGuessAfterFailedStealStillGrantsLastChance(t *testing.T) {
	pack := judgeTestPack()
	seats := rosterTestSeats(4)
	eng, state := judgeStealState(t, pack, seats, NewSeededShuffler(7), "English", 4, 1)

	// Fail the steal: steal_allowed -> false, owed reveal happens (revealed=2).
	state, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
	})
	if after.GetSong().GetStealAllowed() {
		t.Fatalf("steal_allowed = true, want false after the failed steal (test setup)")
	}

	// The active captain now guesses wrong.
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "active guess"),
	})
	_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
	})

	if after.GetSong().GetAwaiting() != pb.Awaiting_AWAITING_LAST_CHANCE {
		t.Fatalf("awaiting = %s, want AWAITING_LAST_CHANCE (Req 10: granted despite steal_allowed=false)",
			after.GetSong().GetAwaiting())
	}
	if after.GetSong().GetStealAllowed() {
		t.Fatalf("steal_allowed = true, want false (unchanged by the last-chance grant)")
	}
	lastChance := findEvent(events, "last_chance_opened")
	if lastChance == nil {
		t.Fatalf("events = %v, want a last_chance_opened event", events)
	}
}

// assertVoidRestoresPreClaimState is the shared assertion body for
// TestVoidRestoresPreClaimState's three kind subtests: void must restore
// exactly the awaiting recorded at claim time, leave revealed/steal_allowed
// and both scores untouched, and clear pending — all while emitting only
// decision_voided.
func assertVoidRestoresPreClaimState(t *testing.T, before, after *pb.GameState, wantAwaiting pb.Awaiting, wantKind pb.JudgeKind) {
	t.Helper()

	if after.GetPhase() != pb.Phase_PHASE_SONG_PLAY {
		t.Fatalf("phase = %s, want PHASE_SONG_PLAY", after.GetPhase())
	}
	if after.GetPending() != nil {
		t.Fatalf("pending = %v, want nil", after.GetPending())
	}
	if after.GetSong().GetAwaiting() != wantAwaiting {
		t.Fatalf("awaiting = %s, want %s (the pre-claim resume_awaiting)", after.GetSong().GetAwaiting(), wantAwaiting)
	}
	if revealedCount(after.GetSong()) != revealedCount(before.GetSong()) {
		t.Fatalf("revealed = %d, want unchanged at %d", revealedCount(after.GetSong()), revealedCount(before.GetSong()))
	}
	if after.GetSong().GetStealAllowed() != before.GetSong().GetStealAllowed() {
		t.Fatalf("steal_allowed = %v, want unchanged at %v", after.GetSong().GetStealAllowed(), before.GetSong().GetStealAllowed())
	}
	for _, team := range []pb.Team{pb.Team_TEAM_A, pb.Team_TEAM_B} {
		if teamScore(after, team) != teamScore(before, team) {
			t.Fatalf("team %s score changed by void: %d -> %d", teamWire(team), teamScore(before, team), teamScore(after, team))
		}
	}
	_ = wantKind
}

// TestVoidRestoresPreClaimState covers row 20 for each of the three
// pending.kind values.
func TestVoidRestoresPreClaimState(t *testing.T) {
	t.Run("active_guess", func(t *testing.T) {
		pack := judgeTestPack()
		seats := rosterTestSeats(4)
		eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(8), "English", 4, 1)
		before := requireRosterState(t, state)

		_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionVoid,
		})
		assertVoidRestoresPreClaimState(t, before, after, pb.Awaiting_AWAITING_ACTIVE, pb.JudgeKind_JUDGE_ACTIVE_GUESS)
		if len(events) != 1 || events[0].Type != "decision_voided" {
			t.Fatalf("events = %v, want exactly one decision_voided", events)
		}
		if events[0].Data["kind"] != "active_guess" || events[0].Data["team"] != "a" {
			t.Fatalf("decision_voided data = %v, want kind=active_guess team=a", events[0].Data)
		}

		// The same captain may act again (block 0 was already revealed by
		// setup, so block 1 is the next still-closed index).
		state = mustMarshalState(t, after)
		if _, _, err := eng.Apply(state, engine.Action{
			UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 1),
		}); err != nil {
			t.Fatalf("open_block after void: %v", err)
		}
	})

	t.Run("steal", func(t *testing.T) {
		pack := judgeTestPack()
		seats := rosterTestSeats(4)
		eng, state := judgeStealState(t, pack, seats, NewSeededShuffler(9), "English", 6, 1)
		before := requireRosterState(t, state)

		_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionVoid,
		})
		assertVoidRestoresPreClaimState(t, before, after, pb.Awaiting_AWAITING_STEAL, pb.JudgeKind_JUDGE_STEAL)
		if events[0].Data["kind"] != "steal" || events[0].Data["team"] != "b" {
			t.Fatalf("decision_voided data = %v, want kind=steal team=b", events[0].Data)
		}

		// The same opponent captain may act again (steal once more).
		state = mustMarshalState(t, after)
		if _, _, err := eng.Apply(state, engine.Action{
			UserID: seats[1].UserID, Type: ActionSteal, Payload: marshalGuessPayload(t, "second attempt"),
		}); err != nil {
			t.Fatalf("steal after void: %v", err)
		}
	})

	t.Run("last_chance", func(t *testing.T) {
		pack := judgeTestPack()
		seats := rosterTestSeats(4)
		eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(10), "English", 7, 0)
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
		})
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[1].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "last chance guess"),
		})
		before := requireRosterState(t, state)

		_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionVoid,
		})
		assertVoidRestoresPreClaimState(t, before, after, pb.Awaiting_AWAITING_LAST_CHANCE, pb.JudgeKind_JUDGE_LAST_CHANCE)
		if events[0].Data["kind"] != "last_chance" || events[0].Data["team"] != "b" {
			t.Fatalf("decision_voided data = %v, want kind=last_chance team=b", events[0].Data)
		}

		// The same opponent (holding the last chance) may act again.
		state = mustMarshalState(t, after)
		if _, _, err := eng.Apply(state, engine.Action{
			UserID: seats[1].UserID, Type: ActionPass,
		}); err != nil {
			t.Fatalf("pass after void: %v", err)
		}
	})
}

// TestLastGuessTextEmptyAfterFailedStealExhaustsReveal is R2 follow-up F2's
// negative coverage for the trickiest "no guess" path: a failed steal DOES
// carry an adjudicated guess (the steal itself, ruled incorrect), but the
// song only ends because the reveal the failed steal owed happens to
// exhaust the last block in the SAME Apply call (play.go's revealAt, shared
// with judge.go's applyJudgeStealIncorrect). That reveal — not the failed
// guess — is what ends the song, so song.LastGuessText must stay empty:
// exactly the "reveal exhausting the blocks" bucket, not the failed steal's
// text.
//
// A two-block song (not one, as this test used before ruling R3): C2
// forbids a steal window opening at revealed==0, so a one-block song could
// never reach a steal window at all (its only block would always be
// revealed directly, ending the song before any steal is possible). Block 0
// is revealed directly first; the steal window then opens on block 1, and
// the failed steal's owed reveal of block 1 is what exhausts the song.
func TestLastGuessTextEmptyAfterFailedStealExhaustsReveal(t *testing.T) {
	pack := &Pack{
		Name: "Two Block Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 2, Songs: []Song{
						{ID: "two-1", Title: "Two Block Song", Artist: "Artist One", Line: []string{"first", "second"}},
						{ID: "two-2", Title: "Other Two Block Song", Artist: "Artist Two", Line: []string{"alpha", "beta"}},
					}},
				},
			},
		},
		TotalSongs: 2,
	}
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(45), "English", 2)

	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 1),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[1].UserID, Type: ActionSteal, Payload: marshalGuessPayload(t, "failed steal guess"),
	})

	_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
	})

	if after.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
		t.Fatalf("phase = %s, want PHASE_SONG_RESULT (the owed reveal exhausted the only block)", after.GetPhase())
	}
	if got := after.GetSong().GetLastGuessText(); got != "" {
		t.Fatalf("last_guess_text = %q, want empty (the song ended by exhausting blocks, not by an adjudicated guess)", got)
	}
	if findEvent(events, "song_finished") == nil {
		t.Fatalf("events = %v, want a song_finished event in the same Apply call", events)
	}
}

// TestVoidedGuessTextDoesNotPersist is R2 follow-up F2's required negative
// coverage for void: a decision the judge discards is "as if it never
// happened" (applyVoid's own doc comment), so its guess text must never
// resurface in song.LastGuessText even after the song later finishes by a
// completely different route.
func TestVoidedGuessTextDoesNotPersist(t *testing.T) {
	t.Run("song_later_finishes_with_no_guess_at_all", func(t *testing.T) {
		pack := judgeTestPack()
		seats := rosterTestSeats(4)
		eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(46), "English", 4, 0)

		state, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionVoid,
		})
		if after.GetSong().GetLastGuessText() != "" {
			t.Fatalf("last_guess_text = %q immediately after void, want empty (song is not finished yet)",
				after.GetSong().GetLastGuessText())
		}

		// The host skips instead of anyone guessing again.
		_, after, _ = applyPlayAction(t, eng, pack, state, engine.Action{UserID: seats[0].UserID, Type: ActionSkipSong})

		if after.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
			t.Fatalf("phase = %s, want PHASE_SONG_RESULT", after.GetPhase())
		}
		if got := after.GetSong().GetLastGuessText(); got != "" {
			t.Fatalf("last_guess_text = %q, want empty (the voided guess must not resurface via skip_song)", got)
		}
	})

	t.Run("song_later_finishes_with_a_different_guess", func(t *testing.T) {
		pack := judgeTestPack()
		seats := rosterTestSeats(4)
		eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(47), "English", 4, 0)

		// The first guess ("my guess", set by judgeActiveGuessState) is
		// voided as if it never happened.
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionVoid,
		})

		// The active captain submits a genuinely different guess, judged
		// correct this time.
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "second and final guess"),
		})
		_, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
		})

		if after.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
			t.Fatalf("phase = %s, want PHASE_SONG_RESULT", after.GetPhase())
		}
		if got := after.GetSong().GetLastGuessText(); got != "second and final guess" {
			t.Fatalf("last_guess_text = %q, want %q (the voided guess must never surface)", got, "second and final guess")
		}
	})
}

// TestJudgeResultRejectsNonJudgeActors checks that judge_result is rejected
// with engine.ErrNotYourTurn for a seated-but-wrong-role actor (a captain,
// or the host when they are not — or no longer — the judge), and with
// engine.ErrNotSeated for a caller holding no seat at all, matching every
// sibling guard in this package (Step 10's requireJudge fix).
func TestJudgeResultRejectsNonJudgeActors(t *testing.T) {
	pack := judgeTestPack()

	t.Run("captain", func(t *testing.T) {
		seats := rosterTestSeats(4)
		eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(20), "English", 4, 0)

		_, _, err := eng.Apply(state, engine.Action{
			UserID: seats[2].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
		})
		if !errors.Is(err, engine.ErrNotYourTurn) {
			t.Fatalf("err = %v, want engine.ErrNotYourTurn", err)
		}
	})

	t.Run("host_not_judge", func(t *testing.T) {
		seats := rosterTestSeats(5)
		eng := NewWithShuffler(&fakeRosterPackProvider{pack: pack}, NewSeededShuffler(21))
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
		state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "guess"),
		})

		_, _, err = eng.Apply(state, engine.Action{
			UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
		})
		if !errors.Is(err, engine.ErrNotYourTurn) {
			t.Fatalf("err (former host, no longer judge) = %v, want engine.ErrNotYourTurn", err)
		}
	})

	t.Run("unseated", func(t *testing.T) {
		seats := rosterTestSeats(4)
		eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(22), "English", 4, 0)

		_, _, err := eng.Apply(state, engine.Action{
			UserID: uuid.New(), Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
		})
		if !errors.Is(err, engine.ErrNotSeated) {
			t.Fatalf("err = %v, want engine.ErrNotSeated", err)
		}
	})
}

// TestJudgeResultRejectsWrongPhase checks that judge_result outside
// PHASE_PENDING_JUDGEMENT is rejected with engine.ErrWrongPhase.
func TestJudgeResultRejectsWrongPhase(t *testing.T) {
	pack := judgeTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(23), "English", 4)

	_, _, err := eng.Apply(state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})
	if !errors.Is(err, engine.ErrWrongPhase) {
		t.Fatalf("err = %v, want engine.ErrWrongPhase", err)
	}
}

// TestGuessDuringStealWindowIsIllegalActionNotWrongPhase and
// TestPassDuringActiveAwaitingIsIllegalActionNotWrongPhase are the missing
// coverage flagged in the plan's "Discovered during implementation" section:
// Step 8 already returns engine.ErrIllegalAction (never ErrWrongPhase) for
// an action that is legal in PHASE_SONG_PLAY generally but wrong for the
// current gs.Song.Awaiting sub-state — "right phase, wrong sub-state". No
// prior step tested this distinction.
func TestGuessDuringStealWindowIsIllegalActionNotWrongPhase(t *testing.T) {
	pack := judgeTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(24), "English", 4)
	// C2: block 0 must be revealed directly first (no steal window exists
	// at revealed==0); block 1 then opens the AWAITING_STEAL window this
	// test needs.
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 1),
	})

	_, _, err := eng.Apply(state, engine.Action{
		UserID: seats[1].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, ""),
	})
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
	}
	if errors.Is(err, engine.ErrWrongPhase) {
		t.Fatalf("err = %v, must NOT also satisfy engine.ErrWrongPhase (right phase, wrong sub-state)", err)
	}
}

func TestPassDuringActiveAwaitingIsIllegalActionNotWrongPhase(t *testing.T) {
	pack := judgeTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(25), "English", 4)

	_, _, err := eng.Apply(state, engine.Action{UserID: seats[2].UserID, Type: ActionPass})
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
	}
	if errors.Is(err, engine.ErrWrongPhase) {
		t.Fatalf("err = %v, must NOT also satisfy engine.ErrWrongPhase (right phase, wrong sub-state)", err)
	}
}

// TestJudgedEventLeaksNothing is this step's redaction check: the judged
// event's Data map must contain exactly {correct, kind, team} — a verdict,
// never a title or the answer. See the worked report for what was observed
// when the payload was temporarily widened with "title", and confirmation
// the widened version was reverted.
func TestJudgedEventLeaksNothing(t *testing.T) {
	pack := judgeTestPack()
	seats := rosterTestSeats(4)
	eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(26), "English", 4, 0)

	_, _, events := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})

	judged := findEvent(events, "judged")
	if judged == nil {
		t.Fatalf("events = %v, want a judged event", events)
	}
	wantKeys := []string{"correct", "kind", "team"}
	if len(judged.Data) != len(wantKeys) {
		t.Fatalf("judged has %d keys %v, want exactly %v", len(judged.Data), judged.Data, wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := judged.Data[k]; !ok {
			t.Fatalf("judged missing key %q: %v", k, judged.Data)
		}
	}
}

// TestScoreConservationOverFullGame drives a full four-song game to
// PHASE_FINISHED, exercising an active-team correct guess, a wrong guess
// resolved by a correct last chance, a successful steal, and an auto-ended
// 0/0 song, then asserts both teams' scores sum to the total history points
// and every awarded entry's points equal bucket - revealed_at_end.
func TestScoreConservationOverFullGame(t *testing.T) {
	pack := judgeFullGamePack()
	seats := rosterTestSeats(4)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(30))

	// Round 1 (TEAM_A active): active captain guesses immediately, correct.
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "round1"),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})
	state, _, _ = nextRoundAndCheck(t, eng, pack, state, seats[1].UserID)

	// Round 2 (TEAM_B active): active captain guesses wrong; opponent
	// (TEAM_A) takes the last chance and gets it right.
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[1].UserID, "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[1].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "round2 wrong"),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "round2 last chance"),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})
	state, _, _ = nextRoundAndCheck(t, eng, pack, state, seats[2].UserID)

	// Round 3 (TEAM_A active): C2 forbids a steal window at revealed==0, so
	// block 0 is revealed directly first; opening block 1 then offers the
	// opponent (TEAM_B) the steal window, which it steals correctly (for
	// potential 4-1=3, not the full 4 as before ruling R3).
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 1),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[1].UserID, Type: ActionSteal, Payload: marshalGuessPayload(t, "round3 steal"),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})
	state, _, _ = nextRoundAndCheck(t, eng, pack, state, seats[1].UserID)

	// Round 4 (TEAM_B active, the pack's last song): reveal every block via
	// revealBlocksViaStealDeclineLoop without ever guessing, auto-ending at
	// 0/0. The first open (revealed==0) reveals directly; the rest go
	// through the steal-decline loop.
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[1].UserID, "English", 4)
	state = revealBlocksViaStealDeclineLoop(t, eng, pack, state, seats[1].UserID, seats[2].UserID, 4)
	_, after, _ := nextRoundAndCheck(t, eng, pack, state, seats[0].UserID) // host: no next captain is "current", game ends.

	if after.GetPhase() != pb.Phase_PHASE_FINISHED {
		t.Fatalf("phase = %s, want PHASE_FINISHED", after.GetPhase())
	}
	if len(after.GetHistory()) != pack.TotalSongs {
		t.Fatalf("history has %d entries, want %d", len(after.GetHistory()), pack.TotalSongs)
	}

	var totalHistoryPoints int32
	for _, h := range after.GetHistory() {
		totalHistoryPoints += h.GetPoints()
		if h.GetAwardedTeam() != pb.Team_TEAM_NONE {
			if want := h.GetBucket() - h.GetRevealedAtEnd(); h.GetPoints() != want {
				t.Fatalf("song %q points = %d, want bucket(%d)-revealed(%d) = %d",
					h.GetSongId(), h.GetPoints(), h.GetBucket(), h.GetRevealedAtEnd(), want)
			}
		}
	}
	totalTeamScore := teamScore(after, pb.Team_TEAM_A) + teamScore(after, pb.Team_TEAM_B)
	if totalTeamScore != totalHistoryPoints {
		t.Fatalf("TEAM_A+TEAM_B score = %d, want sum of history points %d", totalTeamScore, totalHistoryPoints)
	}
	if totalHistoryPoints != 11 {
		t.Fatalf("total history points = %d, want 11 (4+4+3+0 across the four rounds — round 3's steal happens at"+
			" 1 revealed under C2, not 0, so it scores 3 rather than the full 4)", totalHistoryPoints)
	}
}
