package song_guess

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// playTestPack is the *Pack fixture for play.go tests: one language with two
// cells — a 4-block song of single-word blocks (used by the drain/loop
// tests) and a 5-block song of multi-word blocks (rules scenario 7). Named
// to avoid colliding with round.go's roundTestPack / roster.go's
// fakeRosterPack (same package namespace — see the plan's "Discovered
// during implementation" section).
func playTestPack() *Pack {
	return &Pack{
		Name: "Play Test Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 4, Songs: []Song{
						{
							ID: "play4-1", Title: "Four Blocks", Artist: "Artist Four",
							Line: []string{"alpha", "bravo", "charlie", "delta"},
						},
					}},
					{Blocks: 5, Songs: []Song{
						{
							ID: "play5-1", Title: "Five Blocks", Artist: "Artist Five",
							Line: []string{"Is this", "the real", "life", "is this", "just fantasy"},
						},
					}},
				},
			},
		},
		TotalSongs: 2,
	}
}

// playSongPlayState builds a PHASE_SONG_PLAY state for pack/seats/sh, with
// (language, bucket) already selected by TEAM_A's captain (seats[2], per
// the default roster's parity assignment — see engine_round_test.go's
// TestSelectCellRejectsWrongActor comment) — the standard starting point for
// every open_next/guess/steal/pass test in this file.
func playSongPlayState(
	t *testing.T, pack *Pack, seats []engine.SeatInit, sh Shuffler, language string, bucket int32,
) (*Engine, []byte) {
	t.Helper()
	eng, state := roundSelectState(t, pack, seats, sh)
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, language, bucket)
	return eng, state
}

// applyPlayAction applies a against state via eng, requiring success, then
// runs assertInvariants and assertTransition on the result. Returns the new
// state bytes, the unmarshalled after-state, and the raw events.
func applyPlayAction(
	t *testing.T, eng *Engine, pack *Pack, state []byte, a engine.Action,
) ([]byte, *pb.GameState, []engine.Event) {
	t.Helper()

	before := requireRosterState(t, state)
	next, events, err := eng.Apply(state, a)
	if err != nil {
		t.Fatalf("%s: %v", a.Type, err)
	}
	after := requireRosterState(t, next)
	assertInvariants(t, pack, after)
	assertTransition(t, pack, before, after)
	return next, after, events
}

// marshalGuessPayload builds a guess/steal action payload.
func marshalGuessPayload(t *testing.T, guess string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(GuessPayload{Guess: guess})
	if err != nil {
		t.Fatalf("marshal GuessPayload: %v", err)
	}
	return b
}

// openBlockPayload builds an open_block action payload naming index (ruling
// R3.2/C1's free-choice reveal).
func openBlockPayload(t *testing.T, index int32) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(OpenBlockPayload{Index: index})
	if err != nil {
		t.Fatalf("marshal OpenBlockPayload: %v", err)
	}
	return b
}

// findEvent returns a pointer to the first event of the given type in
// events, or nil if none match.
func findEvent(events []engine.Event, eventType string) *engine.Event {
	for i := range events {
		if events[i].Type == eventType {
			return &events[i]
		}
	}
	return nil
}

// TestOpenBlockAtZeroRevealedSkipsStealWindow is architect ruling R3's C2:
// unlike the earlier Q9 behavior (a steal window at revealed==0, asserted by
// this test's predecessor TestOpenNextOpensStealWindowBeforeReveal), the
// active team's very first open_block never offers the opponent a steal
// chance — it reveals the chosen block directly. This is a genuine C2
// behavior change, not a mechanical rename: the old test's assertions
// (AWAITING_STEAL, zero revealed, a lone steal_window_opened event) are now
// exactly wrong and are replaced with their opposite.
func TestOpenBlockAtZeroRevealedSkipsStealWindow(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(1), "English", 4)

	_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID:  seats[2].UserID, // TEAM_A captain: active.
		Type:    ActionOpenBlock,
		Payload: openBlockPayload(t, 1),
	})

	if after.GetSong().GetAwaiting() != pb.Awaiting_AWAITING_ACTIVE {
		t.Fatalf("awaiting = %s, want AWAITING_ACTIVE (C2: no steal window at revealed==0)", after.GetSong().GetAwaiting())
	}
	if revealedCount(after.GetSong()) != 1 {
		t.Fatalf("revealed = %d, want 1 (the chosen block was revealed directly)", revealedCount(after.GetSong()))
	}
	if len(events) != 1 || events[0].Type != "block_revealed" {
		t.Fatalf("events = %v, want exactly one block_revealed (no steal_window_opened at revealed==0)", events)
	}
	if got := events[0].Data["index"]; got != int32(1) {
		t.Fatalf("block_revealed.index = %v, want 1 (the captain's chosen index, C1 free choice)", got)
	}
}

// TestOpponentCannotStealBeforeFirstReveal is architect ruling R3's required
// negative coverage (R3.5's brief): the opponent has no steal action to even
// attempt before the first block is revealed, because C2 never opens a
// steal window at revealed==0 in the first place — attempting steal in
// AWAITING_ACTIVE (the only sub-state a fresh song ever starts in) is
// rejected as illegal, not merely "not yet offered".
//
// Extended under R3.4 item 4 (rather than duplicated into a new test): the
// rejection must also mutate nothing and leave the song exactly where it
// started — C2's actual guarantee is that no steal window exists yet, not
// merely "the attempt happened to fail".
func TestOpponentCannotStealBeforeFirstReveal(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(51), "English", 4)
	before := append([]byte(nil), state...)

	_, _, err := eng.Apply(state, engine.Action{
		UserID: seats[1].UserID, Type: ActionSteal, Payload: marshalGuessPayload(t, ""),
	})
	if !errors.Is(err, engine.ErrWrongPhase) {
		t.Fatalf("err = %v, want engine.ErrWrongPhase (no steal window exists to attempt before the first reveal)", err)
	}
	if !bytes.Equal(state, before) {
		t.Fatalf("Apply mutated the input state bytes despite returning an error")
	}
	after := requireRosterState(t, state)
	if after.GetSong().GetAwaiting() != pb.Awaiting_AWAITING_ACTIVE {
		t.Fatalf("awaiting = %s, want AWAITING_ACTIVE (unchanged)", after.GetSong().GetAwaiting())
	}
	if revealedCount(after.GetSong()) != 0 {
		t.Fatalf("revealed = %d, want 0 (unchanged)", revealedCount(after.GetSong()))
	}
}

// TestOpenBlockOutOfOrderTracksPotentialAndMask is R3.4 item 1 — C1's core
// property: the active captain may open blocks in ANY order, not "the next
// one". Opening index 3, then 1, then 2 (in that order) on a 4-block song
// must reveal exactly those indices, in that order, each dropping potential
// by exactly one, with every other index staying closed throughout. From
// the second open onward (C2), the opponent must decline the steal window
// before the chosen block actually reveals.
func TestOpenBlockOutOfOrderTracksPotentialAndMask(t *testing.T) {
	pack := playTestPack() // English/4 bucket: blocks [alpha, bravo, charlie, delta]
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(60), "English", 4)

	order := []int32{3, 1, 2}
	wantMask := []bool{false, false, false, false}
	for i, idx := range order {
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, idx),
		})
		if i > 0 {
			state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{UserID: seats[1].UserID, Type: ActionPass})
		}

		wantMask[idx] = true
		after := requireRosterState(t, state)
		if got := int(revealedCount(after.GetSong())); got != i+1 {
			t.Fatalf("after opening index %d: revealed = %d, want %d", idx, got, i+1)
		}
		if got := potentialOf(after.GetSong()); got != int32(4-(i+1)) {
			t.Fatalf("after opening index %d: potential = %d, want %d", idx, got, 4-(i+1))
		}
		if got := after.GetSong().GetRevealedMask(); !slices.Equal(got, wantMask) {
			t.Fatalf("after opening index %d: mask = %v, want %v", idx, got, wantMask)
		}
	}
}

// TestOpenBlockRejectsAlreadyRevealedIndex is R3.4 item 2: opening a block
// that is already revealed is illegal action and mutates nothing.
func TestOpenBlockRejectsAlreadyRevealedIndex(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(61), "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	before := append([]byte(nil), state...)

	_, _, err := eng.Apply(state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
	}
	if !bytes.Equal(state, before) {
		t.Fatalf("Apply mutated the input state bytes despite returning an error")
	}
}

// TestOpenBlockRejectsOutOfRangeIndex is R3.4 item 3: a negative index and
// an index >= len(blocks) are both illegal action and mutate nothing.
func TestOpenBlockRejectsOutOfRangeIndex(t *testing.T) {
	tests := []struct {
		name  string
		index int32
	}{
		{"negative", -1},
		{"at_length", 4}, // English/4 has exactly 4 blocks: valid indices 0-3
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pack := playTestPack()
			seats := rosterTestSeats(4)
			eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(62), "English", 4)
			before := append([]byte(nil), state...)

			_, _, err := eng.Apply(state, engine.Action{
				UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, tt.index),
			})
			if !errors.Is(err, engine.ErrIllegalAction) {
				t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
			}
			if !bytes.Equal(state, before) {
				t.Fatalf("Apply mutated the input state bytes despite returning an error")
			}
		})
	}
}

// TestPassFromStealRevealsExactlyOneBlock checks row 10: declining the
// steal reveals exactly one block — the SAME index open_block chose (C1) —
// and drops potential by exactly one. Under C2 a steal window cannot open
// at revealed==0, so block 0 is opened directly first (no pass involved)
// before block 1's open_block can offer the opponent a steal window at all.
func TestPassFromStealRevealsExactlyOneBlock(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(2), "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 1),
	})

	_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[1].UserID, // TEAM_B captain: opponent.
		Type:   ActionPass,
	})

	if revealedCount(after.GetSong()) != 2 {
		t.Fatalf("revealed = %d, want 2", revealedCount(after.GetSong()))
	}
	if got := potentialOf(after.GetSong()); got != 2 {
		t.Fatalf("potential = %d, want 2 (4 - 2)", got)
	}
	if after.GetSong().GetAwaiting() != pb.Awaiting_AWAITING_ACTIVE {
		t.Fatalf("awaiting = %s, want AWAITING_ACTIVE", after.GetSong().GetAwaiting())
	}
	wantTypes := []string{"steal_passed", "block_revealed"}
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
		t.Fatalf("block_revealed.index = %v, want 1 (the block the steal window put on offer)", got)
	}
}

// TestPassFromStealRevealsCaptainsChosenIndexNotLowestClosed is R3.4 item 7
// (amendment R3.2b's sibling to judge.go's already-falsified
// TestJudgeStealIncorrectRevealsTheOwedIndexNotAnArbitraryOne): with
// free-choice reveals (C1), declining a steal must reveal the SPECIFIC
// block the active captain chose to open — SongState.StealWindowIndex —
// never the lowest still-closed index. Block 0 is revealed first (C2: no
// steal window at revealed==0); the active captain then opens block 2
// specifically (NOT block 1, the lowest closed index), so a fallback to
// "lowest closed" would provably reveal the wrong block.
//
// Written first and falsified by temporarily hardcoding
//
//	return revealAt(gs, 1, events)
//
// in play.go's applyPassFromSteal, in place of
// revealAt(gs, gs.GetSong().GetStealWindowIndex(), events). Observed
// failure (see the worked report for this step for the verbatim output):
// the test failed on the mask assertion below, not on an engine error —
// unlike the judge-incorrect sibling (where a hardcoded 0 collided with an
// already-revealed block and revealAt's own guard rejected it loudly), here
// the hardcoded 1 names a genuinely closed block, so the bug would have
// silently revealed the WRONG block rather than erroring — exactly the
// silent-corruption case this test exists to catch. The hardcoded change
// was reverted immediately after.
func TestPassFromStealRevealsCaptainsChosenIndexNotLowestClosed(t *testing.T) {
	pack := playTestPack() // English/4 bucket: blocks [alpha, bravo, charlie, delta]
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(63), "English", 4)

	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	// The active captain opens block 2 specifically — NOT block 1, the
	// lowest closed index — exactly C1's free choice.
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 2),
	})

	_, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[1].UserID, Type: ActionPass,
	})

	mask := after.GetSong().GetRevealedMask()
	want := []bool{true, false, true, false}
	if !slices.Equal(mask, want) {
		t.Fatalf("revealed_mask = %v, want %v (block 2 — the captain's chosen index — revealed, block 1 untouched)", mask, want)
	}
}

// TestRevealingFinalBlockAutoEndsSongAtZeroZero is rules scenario 6: driving
// a full open_block/pass loop to the last block auto-ends the song at 0/0
// without ever offering the active captain another guess at potential==0
// (Q9, invariant law 8). Under C2 the first open_block (revealed==0) reveals
// directly with no steal window to pass from — only the second and later
// opens (revealed>0) offer the opponent a decision.
func TestRevealingFinalBlockAutoEndsSongAtZeroZero(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(3), "English", 4)

	for i := range 4 {
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, int32(i)),
		})
		if i > 0 {
			state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{UserID: seats[1].UserID, Type: ActionPass})
		}
	}

	after := requireRosterState(t, state)
	if after.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
		t.Fatalf("phase = %s, want PHASE_SONG_RESULT", after.GetPhase())
	}
	if revealedCount(after.GetSong()) != 4 {
		t.Fatalf("revealed = %d, want 4 (all blocks)", revealedCount(after.GetSong()))
	}
	if after.GetSong().GetAwardedTeam() != pb.Team_TEAM_NONE || after.GetSong().GetAwardedPoints() != 0 {
		t.Fatalf("song awarded = (%s, %d), want (TEAM_NONE, 0)",
			after.GetSong().GetAwardedTeam(), after.GetSong().GetAwardedPoints())
	}
	last := after.GetHistory()[len(after.GetHistory())-1]
	if last.GetAwardedTeam() != pb.Team_TEAM_NONE || last.GetPoints() != 0 {
		t.Fatalf("history entry = (%s, %d), want (TEAM_NONE, 0)", last.GetAwardedTeam(), last.GetPoints())
	}
}

// TestMultiWordBlockCountsAsOneBlock is rules scenario 7: a multi-word block
// ("Is this", block index 0) counts as one block and drops potential by
// exactly one, and its full text (words included) is exactly what
// block_revealed carries. Block 1 is opened directly first (C2: no steal
// window at revealed==0) so that opening block 0 second can actually offer
// the opponent the pass this test exercises — the pass's reveal is the one
// asserted against, not the first (direct) one.
func TestMultiWordBlockCountsAsOneBlock(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(4), "English", 5)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 1),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})

	_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{UserID: seats[1].UserID, Type: ActionPass})

	if revealedCount(after.GetSong()) != 2 {
		t.Fatalf("revealed = %d, want 2", revealedCount(after.GetSong()))
	}
	if got := potentialOf(after.GetSong()); got != 3 {
		t.Fatalf("potential = %d, want 3 (5 - 2 blocks, not 5 - 3 words)", got)
	}
	revealed := findEvent(events, "block_revealed")
	if revealed == nil {
		t.Fatalf("events = %v, want a block_revealed event", events)
	}
	if got := revealed.Data["text"]; got != "Is this" {
		t.Fatalf("block_revealed.text = %v, want %q (the whole multi-word block)", got, "Is this")
	}
}

// TestOpenBlockRejectsOpposingCaptain is rules scenario 14: the opponent
// captain calling open_block (the active captain's decision) is rejected.
// No payload is needed: requireActiveCaptain runs before decodePayload, so
// the wrong actor is rejected regardless of payload content.
func TestOpenBlockRejectsOpposingCaptain(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(5), "English", 4)

	_, _, err := eng.Apply(state, engine.Action{UserID: seats[1].UserID, Type: ActionOpenBlock})
	if !errors.Is(err, engine.ErrNotYourTurn) {
		t.Fatalf("err = %v, want engine.ErrNotYourTurn", err)
	}
}

// TestGuessRejectsNonCaptainTeammate checks that only the active captain
// (never a teammate) may guess. Five seats are used so TEAM_A has a
// non-captain member: seat2 is TEAM_A's default captain (lowest active
// seat), seat4 (also TEAM_A by parity) is not.
func TestGuessRejectsNonCaptainTeammate(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(5)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(6), "English", 4)

	_, _, err := eng.Apply(state, engine.Action{
		UserID:  seats[4].UserID,
		Type:    ActionGuess,
		Payload: marshalGuessPayload(t, ""),
	})
	if !errors.Is(err, engine.ErrNotYourTurn) {
		t.Fatalf("err = %v, want engine.ErrNotYourTurn", err)
	}
}

// TestStealRejectedWhenStealAllowedFalse checks row 9's guard: steal is
// rejected once steal_allowed is false, mutating nothing. steal_allowed can
// only go false via a failed steal (judge_result, Step 9), so this hand-sets
// it on an otherwise-legitimate AWAITING_STEAL state — the same scaffolding
// pattern engine_round_test.go's empty-cell test uses. Block 0 is opened
// directly first (C2: no steal window at revealed==0) so that opening block
// 1 second can actually reach AWAITING_STEAL for the hand-set to matter.
func TestStealRejectedWhenStealAllowedFalse(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(7), "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 1),
	})

	gs := requireRosterState(t, state)
	gs.GetSong().StealAllowed = false
	state = mustMarshalState(t, gs)
	before := append([]byte(nil), state...)

	_, _, err := eng.Apply(state, engine.Action{
		UserID:  seats[1].UserID,
		Type:    ActionSteal,
		Payload: marshalGuessPayload(t, ""),
	})
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
	}
	if !bytes.Equal(state, before) {
		t.Fatalf("Apply mutated the input state bytes despite returning an error")
	}
}

// TestSkipSongRejectsNonHost checks row 22's actor guard.
func TestSkipSongRejectsNonHost(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(8), "English", 4)

	_, _, err := eng.Apply(state, engine.Action{UserID: seats[2].UserID, Type: ActionSkipSong})
	if !errors.Is(err, engine.ErrNotYourTurn) {
		t.Fatalf("err = %v, want engine.ErrNotYourTurn", err)
	}
}

// assertSkippedSongResult is the shared assertion body for
// TestSkipSongFromSongPlayReachesResult and
// TestSkipSongFromPendingJudgementReachesResult: after a successful
// skip_song, phase is SONG_RESULT, the song/history entry are 0 points and
// skipped=true, pending is cleared, and — the point of Req 14/the step
// brief — the cell's counts are untouched (the song was already marked
// played at select_cell; skip_song is not a second bookkeeping site).
func assertSkippedSongResult(t *testing.T, before, after *pb.GameState, language string, bucket int32) {
	t.Helper()

	if after.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
		t.Fatalf("phase = %s, want PHASE_SONG_RESULT", after.GetPhase())
	}
	if after.GetPending() != nil {
		t.Fatalf("pending = %v, want nil (skip_song clears it)", after.GetPending())
	}
	if !after.GetSong().GetSkipped() || after.GetSong().GetAwardedPoints() != 0 {
		t.Fatalf("song = (skipped=%v, points=%d), want (true, 0)",
			after.GetSong().GetSkipped(), after.GetSong().GetAwardedPoints())
	}
	last := after.GetHistory()[len(after.GetHistory())-1]
	if !last.GetSkipped() || last.GetPoints() != 0 {
		t.Fatalf("history entry = (skipped=%v, points=%d), want (true, 0)", last.GetSkipped(), last.GetPoints())
	}

	cellBefore, cellAfter := cellFor(before, language, bucket), cellFor(after, language, bucket)
	if len(cellAfter.GetUnplayedSongIds()) != len(cellBefore.GetUnplayedSongIds()) || cellAfter.GetPlayedCount() != cellBefore.GetPlayedCount() {
		t.Fatalf("cell (%s,%d) changed by skip_song: unplayed %d->%d, played %d->%d", language, bucket,
			len(cellBefore.GetUnplayedSongIds()), len(cellAfter.GetUnplayedSongIds()), cellBefore.GetPlayedCount(), cellAfter.GetPlayedCount())
	}
}

// TestSkipSongFromSongPlayReachesResult checks row 22 from PHASE_SONG_PLAY.
func TestSkipSongFromSongPlayReachesResult(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(9), "English", 4)
	before := requireRosterState(t, state)

	_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{UserID: seats[0].UserID, Type: ActionSkipSong})

	assertSkippedSongResult(t, before, after, "English", 4)
	wantTypes := []string{"song_skipped", "song_finished"}
	if len(events) != len(wantTypes) {
		t.Fatalf("events = %v, want %v", events, wantTypes)
	}
	for i, want := range wantTypes {
		if events[i].Type != want {
			t.Fatalf("events[%d].Type = %q, want %q", i, events[i].Type, want)
		}
	}
}

// TestSkipSongFromPendingJudgementReachesResult checks row 22 from
// PHASE_PENDING_JUDGEMENT (reached here via row 8's guess, entirely within
// this step's scope — no judge_result involved).
func TestSkipSongFromPendingJudgementReachesResult(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(10), "English", 4)
	before := requireRosterState(t, state)

	state, afterGuess, _ := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID:  seats[2].UserID,
		Type:    ActionGuess,
		Payload: marshalGuessPayload(t, "some guess"),
	})
	if afterGuess.GetPhase() != pb.Phase_PHASE_PENDING_JUDGEMENT {
		t.Fatalf("phase after guess = %s, want PHASE_PENDING_JUDGEMENT", afterGuess.GetPhase())
	}

	_, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{UserID: seats[0].UserID, Type: ActionSkipSong})

	assertSkippedSongResult(t, before, after, "English", 4)
}

// TestGuessRejectsOversizedGuessText checks the ~200-char cap (Req 12) is
// enforced on the real guess dispatch path, not just trimmedGuessText in
// isolation (already covered by engine_roster_test.go's
// TestTrimmedGuessTextRejectsTooLong).
func TestGuessRejectsOversizedGuessText(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(11), "English", 4)
	before := append([]byte(nil), state...)

	oversized := strings.Repeat("x", maxGuessTextLen+1)
	_, _, err := eng.Apply(state, engine.Action{
		UserID:  seats[2].UserID,
		Type:    ActionGuess,
		Payload: marshalGuessPayload(t, oversized),
	})
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
	}
	if !bytes.Equal(state, before) {
		t.Fatalf("Apply mutated the input state bytes despite returning an error")
	}
}

// TestFinishSongCanAwardOpponentOfActiveTeam is ruling R1.7's required test:
// finishSong must be able to produce a SongOutcome whose awarded_team
// differs from its active_team — exactly the case (a successful steal or
// last-chance guess, transition table rows 16/18) that motivated adding
// SongOutcome.active_team in the first place (R1.1). The full
// steal -> judge_result -> award flow needs Step 9's judge; calling
// finishSong directly is legitimate here because finishSong — not
// judge_result — is the single history-append site this ruling concerns.
func TestFinishSongCanAwardOpponentOfActiveTeam(t *testing.T) {
	gs := &pb.GameState{
		Phase:      pb.Phase_PHASE_SONG_PLAY,
		ActiveTeam: pb.Team_TEAM_A,
		Teams: []*pb.TeamState{
			{Team: pb.Team_TEAM_A, Name: "Team A"},
			{Team: pb.Team_TEAM_B, Name: "Team B"},
		},
		Song: &pb.SongState{
			SongId:       "steal-song",
			LanguageName: "English",
			Bucket:       6,
			Blocks:       []string{"a", "b", "c", "d", "e", "f"},
			RevealedMask: []bool{true, true, false, false, false, false},
		},
	}

	events := finishSong(gs, pb.Team_TEAM_B, 4, false)

	last := gs.GetHistory()[len(gs.GetHistory())-1]
	if last.GetActiveTeam() != pb.Team_TEAM_A {
		t.Fatalf("history active_team = %s, want TEAM_A (the round's active team)", last.GetActiveTeam())
	}
	if last.GetAwardedTeam() != pb.Team_TEAM_B {
		t.Fatalf("history awarded_team = %s, want TEAM_B (the opponent, per a successful steal)", last.GetAwardedTeam())
	}
	if last.GetActiveTeam() == last.GetAwardedTeam() {
		t.Fatalf("active_team and awarded_team must differ for this scenario, both = %s", last.GetActiveTeam())
	}
	if got := teamState(gs, pb.Team_TEAM_B).GetScore(); got != 4 {
		t.Fatalf("TEAM_B score = %d, want 4", got)
	}
	if len(events) != 1 || events[0].Type != "song_finished" {
		t.Fatalf("events = %v, want exactly one song_finished", events)
	}
}

// TestBlockRevealedEventLeaksNothing is this step's redaction check:
// block_revealed's Data map must contain exactly {index, text}, and text
// must equal the block that was just revealed — never the whole line. See
// the worked report for what was observed when the payload was temporarily
// widened, and confirmation the widened version was reverted. Block 0 is
// opened directly first (C2: no steal window at revealed==0) so that
// opening block 1 second can reach the pass this test exercises.
func TestBlockRevealedEventLeaksNothing(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(12), "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 1),
	})

	_, after, events := applyPlayAction(t, eng, pack, state, engine.Action{UserID: seats[1].UserID, Type: ActionPass})

	revealed := findEvent(events, "block_revealed")
	if revealed == nil {
		t.Fatalf("events = %v, want a block_revealed event", events)
	}

	wantKeys := []string{"index", "text"}
	if len(revealed.Data) != len(wantKeys) {
		t.Fatalf("block_revealed has %d keys %v, want exactly %v", len(revealed.Data), revealed.Data, wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := revealed.Data[k]; !ok {
			t.Fatalf("block_revealed missing key %q: %v", k, revealed.Data)
		}
	}
	idx, ok := revealed.Data["index"].(int32)
	if !ok {
		t.Fatalf("block_revealed.index = %v (%T), want int32", revealed.Data["index"], revealed.Data["index"])
	}
	if got, want := revealed.Data["text"], after.GetSong().GetBlocks()[idx]; got != want {
		t.Fatalf("block_revealed.text = %q, want %q (blocks[%d])", got, want, idx)
	}
}

// TestGuessSubmittedEventLeaksNothing is R2 follow-up F1's required leak
// test. guess_submitted broadcasts to every subscriber including the
// opposing team and spectators, and an engine event cannot be per-viewer
// redacted — so its Data map must contain exactly {team, seat, kind,
// has_guess}, never the raw guess text under any key, and the raw guess
// text must not appear anywhere in the event once marshalled to JSON.
//
// This was falsified by temporarily restoring the old
// `if p.GetGuessText() != "" { data["guess"] = p.GetGuessText() }` line in
// guessSubmittedEvent: the test failed with
//
//	guess_submitted has 5 keys map[guess:a very identifying guess kind:active_guess seat:2 team:a], want exactly [team seat kind has_guess]
//
// confirming the test catches the exact regression F1 describes. The
// temporary change was reverted immediately after.
func TestGuessSubmittedEventLeaksNothing(t *testing.T) {
	pack := playTestPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(41), "English", 4)

	guessText := "a very identifying guess"
	_, _, events := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, guessText),
	})

	submitted := findEvent(events, "guess_submitted")
	if submitted == nil {
		t.Fatalf("events = %v, want a guess_submitted event", events)
	}

	wantKeys := []string{"team", "seat", "kind", "has_guess"}
	if len(submitted.Data) != len(wantKeys) {
		t.Fatalf("guess_submitted has %d keys %v, want exactly %v", len(submitted.Data), submitted.Data, wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := submitted.Data[k]; !ok {
			t.Fatalf("guess_submitted missing key %q: %v", k, submitted.Data)
		}
	}
	if _, ok := submitted.Data["guess"]; ok {
		t.Fatalf("guess_submitted carries a %q key: %v, want it dropped entirely", "guess", submitted.Data)
	}
	if hasGuess, ok := submitted.Data["has_guess"].(bool); !ok || !hasGuess {
		t.Fatalf("guess_submitted.has_guess = %v (%T), want true", submitted.Data["has_guess"], submitted.Data["has_guess"])
	}

	b, err := json.Marshal(submitted.Data)
	if err != nil {
		t.Fatalf("marshal guess_submitted.Data: %v", err)
	}
	if strings.Contains(string(b), guessText) {
		t.Fatalf("guess_submitted JSON contains the raw guess text: %s", b)
	}
}

// TestLastGuessTextEmptyOnNoGuessPaths is R2 follow-up F2's required
// negative coverage: song.LastGuessText must stay empty on every path where
// no guess was actually adjudicated — even when a guess's text genuinely
// existed somewhere earlier in the same song (a declined last chance, or a
// still-pending, never-judged guess abandoned by skip_song) — never a stale
// value from an earlier pending.
func TestLastGuessTextEmptyOnNoGuessPaths(t *testing.T) {
	t.Run("reveal_exhausts_the_blocks", func(t *testing.T) {
		pack := playTestPack()
		seats := rosterTestSeats(4)
		eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(42), "English", 4)

		// C2: the first open_block (revealed==0) reveals directly with no
		// steal window to pass from.
		for i := range 4 {
			state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
				UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, int32(i)),
			})
			if i > 0 {
				state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{UserID: seats[1].UserID, Type: ActionPass})
			}
		}

		after := requireRosterState(t, state)
		if after.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
			t.Fatalf("phase = %s, want PHASE_SONG_RESULT (test setup: no guess was ever submitted)", after.GetPhase())
		}
		if got := after.GetSong().GetLastGuessText(); got != "" {
			t.Fatalf("last_guess_text = %q, want empty (no guess was ever submitted)", got)
		}
	})

	t.Run("pass_from_last_chance_after_an_earlier_wrong_guess", func(t *testing.T) {
		pack := playTestPack()
		seats := rosterTestSeats(4)
		eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(43), "English", 4)

		// The active captain guesses wrong (real text, judged incorrect) —
		// this opens the opponent's last chance but does not end the song.
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "wrong active guess"),
		})
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
		})

		// The opponent declines the last chance instead of guessing: the
		// song ends 0/0 with no guess of its own on this decision.
		_, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{UserID: seats[1].UserID, Type: ActionPass})

		if after.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
			t.Fatalf("phase = %s, want PHASE_SONG_RESULT", after.GetPhase())
		}
		if got := after.GetSong().GetLastGuessText(); got != "" {
			t.Fatalf("last_guess_text = %q, want empty (the last decision was a pass, not a guess)", got)
		}
	})

	t.Run("skip_song_abandons_a_live_unadjudicated_guess", func(t *testing.T) {
		pack := playTestPack()
		seats := rosterTestSeats(4)
		eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(44), "English", 4)

		// A guess is submitted but never judged: the host skips instead.
		state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
			UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "never adjudicated"),
		})
		_, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{UserID: seats[0].UserID, Type: ActionSkipSong})

		if after.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
			t.Fatalf("phase = %s, want PHASE_SONG_RESULT", after.GetPhase())
		}
		if got := after.GetSong().GetLastGuessText(); got != "" {
			t.Fatalf("last_guess_text = %q, want empty (skip_song abandons the pending guess, it is never adjudicated)", got)
		}
	})
}
