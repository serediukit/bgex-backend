package song_guess

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// ---------------------------------------------------------------------
// Leak tests (view.go) — this step's core deliverable.
// ---------------------------------------------------------------------

// viewTestPack is the *Pack fixture for view.go's leak tests: one cell
// holding a single 6-block song with media (the "current song" every test
// in this section draws), and a second cell holding a song that is never
// drawn (the "unplayed song in another cell" the leak tests must never see
// any trace of). Named to avoid colliding with every other engine_*_test.go
// pack fixture (same package namespace — see the plan's "Discovered during
// implementation" section).
func viewTestPack() *Pack {
	return &Pack{
		Name: "View Test Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 6, Songs: []Song{
						{
							ID: "vt6-1", Title: "Currently Playing", Artist: "Artist Current",
							Line:  []string{"Wordone", "Wordtwo", "Wordthree", "Wordfour", "Wordfive", "Wordsix"},
							Media: &Media{VideoID: "dQw4w9WgXcQ", StartSec: 5, EndSec: 15},
						},
					}},
					{Blocks: 4, Songs: []Song{
						{ID: "vt4-1", Title: "Unplayed Secret Song", Artist: "Unplayed Artist Name", Line: []string{"w", "x", "y", "z"}},
					}},
				},
			},
		},
		TotalSongs: 2,
	}
}

// viewMidSongFixture drives viewTestPack's 6-block song to PHASE_PENDING_
// JUDGEMENT with exactly 2 of 6 blocks revealed and a pending active_guess
// judgement — the shape Step 11's brief specifies for the leak tests — via
// the real engine (judgeActiveGuessState, engine_judge_test.go), not a
// hand-built fixture: every transition it drives through is already
// invariant/transition-checked by the helper itself.
func viewMidSongFixture(t *testing.T) (*Engine, *Pack, []engine.SeatInit, []byte) {
	t.Helper()
	pack := viewTestPack()
	seats := rosterTestSeats(4)
	eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(99), "English", 6, 2)
	return eng, pack, seats, state
}

// midSongAlwaysHiddenSubstrings must never appear in ANY view's marshalled
// JSON while viewMidSongFixture's song is still mid-play, regardless of
// viewer class: the current song's video id and song id, and the other
// cell's unplayed song's id/title/artist. It deliberately does NOT include
// the current song's unrevealed block texts (Wordthree..Wordsix) — since
// the 2026-08-15 judge-answer-line change those ARE visible to the judge
// (via answer.line) mid-song, even though they stay hidden from a player or
// spectator; see midSongUnrevealedBlockWords below for that half.
func midSongAlwaysHiddenSubstrings() []string {
	return []string{
		"dQw4w9WgXcQ",
		"vt6-1",
		"vt4-1", "Unplayed Secret Song", "Unplayed Artist Name",
	}
}

// midSongUnrevealedBlockWords is viewMidSongFixture's current song's
// still-closed block texts (blocks 2-5 of 6; 0-1 are revealed and MUST
// appear everywhere). Hidden from a player/spectator (no blocks[] entry,
// no answer field at all); visible to the judge via answer.line.
func midSongUnrevealedBlockWords() []string {
	return []string{"Wordthree", "Wordfour", "Wordfive", "Wordsix"}
}

// midSongPlayerOnlyHiddenSubstrings adds the current song's title/artist and
// its still-closed block words — visible to the JUDGE mid-song (title/
// artist always; the closed block words via answer.line, 2026-08-15), but
// not to a player or a spectator.
func midSongPlayerOnlyHiddenSubstrings() []string {
	out := append([]string{"Currently Playing", "Artist Current"}, midSongUnrevealedBlockWords()...)
	return append(out, midSongAlwaysHiddenSubstrings()...)
}

// midSongStillHiddenAfterFinish is what remains hidden even once the song
// has ended: the other cell's unplayed song is never played, so it is never
// disclosed to anyone in any phase.
func midSongStillHiddenAfterFinish() []string {
	return []string{"vt6-1", "vt4-1", "Unplayed Secret Song", "Unplayed Artist Name"}
}

// mustMarshalView marshals view (the `any` ViewFor/View return) to a string
// for substring scanning.
func mustMarshalView(t *testing.T, view any) string {
	t.Helper()
	b, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	return string(b)
}

// assertNoneContain fails t if raw contains any of substrs, naming which
// one leaked and the view's full JSON for debugging.
func assertNoneContain(t *testing.T, raw string, substrs []string, label string) {
	t.Helper()
	for _, s := range substrs {
		if strings.Contains(raw, s) {
			t.Fatalf("%s leaks %q: %s", label, s, raw)
		}
	}
}

// TestPlayerViewLeaksNothingMidSong is Step 11's test 1: the active
// captain's (a player's) view of viewMidSongFixture's mid-song state must
// contain none of the current song's title/artist/video id/song id,
// unrevealed block text, or the other cell's unplayed song data. Asserted
// as a substring scan over the marshalled JSON, not field-by-field — a
// field check cannot catch a field someone adds later (see
// TestFalsifiedAnswerLeakIsCaught below, which proves this test actually
// fails when it should).
func TestPlayerViewLeaksNothingMidSong(t *testing.T) {
	eng, pack, seats, state := viewMidSongFixture(t)
	view, err := eng.ViewFor(pack, state, seats[2].UserID) // TEAM_A captain: the pending guess's claimant.
	if err != nil {
		t.Fatalf("ViewFor: %v", err)
	}
	raw := mustMarshalView(t, view)
	assertNoneContain(t, raw, midSongPlayerOnlyHiddenSubstrings(), "player view")

	// The revealed blocks (Wordone, Wordtwo) ARE expected to be public —
	// confirming their presence guards against a vacuous test that would
	// pass even if blocksView never revealed anything at all.
	for _, want := range []string{"Wordone", "Wordtwo"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("player view missing revealed block %q, want it public: %s", want, raw)
		}
	}
}

// TestSpectatorViewLeaksNothingMidSong is Step 11's test 2: an unseated
// viewer gets the player shape (YourSeat == -1, no "answer" key), with the
// identical leak-freedom as TestPlayerViewLeaksNothingMidSong.
func TestSpectatorViewLeaksNothingMidSong(t *testing.T) {
	eng, pack, _, state := viewMidSongFixture(t)
	view, err := eng.ViewFor(pack, state, uuid.New())
	if err != nil {
		t.Fatalf("ViewFor: %v", err)
	}

	sv, ok := view.(*SongGuessView)
	if !ok {
		t.Fatalf("view is %T, want *SongGuessView", view)
	}
	if sv.YourSeat != -1 {
		t.Fatalf("spectator YourSeat = %d, want -1", sv.YourSeat)
	}

	raw := mustMarshalView(t, view)
	assertNoneContain(t, raw, midSongPlayerOnlyHiddenSubstrings(), "spectator view")
	if strings.Contains(raw, `"answer"`) {
		t.Fatalf(`spectator view leaks an "answer" key: %s`, raw)
	}
}

// TestJudgeViewShowsAnswerAndFullLineButNotOtherSongsMidSong is Step 11's
// test 3, updated for the 2026-08-15 judge-answer-line change: the judge
// sees the title/artist early (Req 26) AND, now, the full line via
// answer.line — including the still-closed blocks (Wordthree..Wordsix) —
// but still nothing about the OTHER cell's unplayed song, no video id
// (media stays finished-only, even for the judge), and blocks[] ITSELF is
// unchanged: the closed indices still carry no "text" key there.
func TestJudgeViewShowsAnswerAndFullLineButNotOtherSongsMidSong(t *testing.T) {
	eng, pack, seats, state := viewMidSongFixture(t)
	view, err := eng.ViewFor(pack, state, seats[0].UserID) // host+judge in the default 4-seat roster.
	if err != nil {
		t.Fatalf("ViewFor: %v", err)
	}
	sv, ok := view.(*SongGuessView)
	if !ok {
		t.Fatalf("view is %T, want *SongGuessView", view)
	}
	raw := mustMarshalView(t, view)

	for _, want := range append([]string{"Currently Playing", "Artist Current"}, midSongUnrevealedBlockWords()...) {
		if !strings.Contains(raw, want) {
			t.Fatalf("judge view missing %q, want the answer/line visible early: %s", want, raw)
		}
	}
	// blocks[] itself must still hide the closed indices (2-5): the line's
	// visibility must not have flipped blocks[] to all-revealed.
	if sv.Song == nil {
		t.Fatalf("judge song panel missing")
	}
	for i := 2; i <= 5; i++ {
		assertBlockExactlyClosed(t, sv, i, "judge blocks[] mid-song")
	}
	assertNoneContain(t, raw, midSongAlwaysHiddenSubstrings(), "judge view")
}

// TestAllClassesSeeEverythingAtSongResult is Step 11's test 4: once the
// pending judgement resolves (correct=true), every class — player,
// opponent, judge, spectator — sees the full block line, the answer and
// the media; the OTHER cell's unplayed song remains hidden forever.
func TestAllClassesSeeEverythingAtSongResult(t *testing.T) {
	eng, pack, seats, state := viewMidSongFixture(t)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})
	if gs := requireRosterState(t, state); gs.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
		t.Fatalf("phase = %s, want PHASE_SONG_RESULT", gs.GetPhase())
	}

	viewers := map[string]uuid.UUID{
		"active_captain":   seats[2].UserID,
		"opponent_captain": seats[1].UserID,
		"judge":            seats[0].UserID,
		"spectator":        uuid.New(),
	}
	wantPresent := []string{
		"Currently Playing", "Artist Current", "dQw4w9WgXcQ",
		"Wordone", "Wordtwo", "Wordthree", "Wordfour", "Wordfive", "Wordsix",
	}

	for name, uid := range viewers {
		t.Run(name, func(t *testing.T) {
			view, err := eng.ViewFor(pack, state, uid)
			if err != nil {
				t.Fatalf("ViewFor: %v", err)
			}
			raw := mustMarshalView(t, view)
			for _, want := range wantPresent {
				if !strings.Contains(raw, want) {
					t.Fatalf("%s view missing %q at SONG_RESULT: %s", name, want, raw)
				}
			}
			assertNoneContain(t, raw, midSongStillHiddenAfterFinish(), name+" view at SONG_RESULT")
		})
	}
}

// TestUnrevealedBlockSerializesWithoutTextKey is Step 11's test 5: an
// unrevealed SongGuessBlockView serializes to EXACTLY {"revealed":false} —
// asserted on the raw JSON, both in isolation and as it actually appears
// inside a real mid-song view.
func TestUnrevealedBlockSerializesWithoutTextKey(t *testing.T) {
	b, err := json.Marshal(SongGuessBlockView{Revealed: false})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"revealed":false}` {
		t.Fatalf(`unrevealed block JSON = %s, want exactly {"revealed":false}`, b)
	}

	eng, pack, seats, state := viewMidSongFixture(t)
	view, err := eng.ViewFor(pack, state, seats[2].UserID)
	if err != nil {
		t.Fatalf("ViewFor: %v", err)
	}
	raw := mustMarshalView(t, view)
	if !strings.Contains(raw, `{"revealed":false}`) {
		t.Fatalf(`player view has no literal unrevealed block {"revealed":false}: %s`, raw)
	}
}

// TestJudgeVacantMidSongKeepsSongPanelButHidesAnswer is Step 11's test 6,
// covering the PHASE_JUDGE_VACANT trap head-on: the judge (seat0, also the
// host in the default 4-seat roster) resigns mid-PENDING_JUDGEMENT. The
// wire phase must show the LITERAL "judge_vacant" (for the frontend's
// blocking banner), the song panel must still be present (buildView checks
// gs.Song != nil, not any phase), and — the redaction half of the trap — no
// player must see the answer, because effectivePhase resolves to
// PENDING_JUDGEMENT (still in play), not SONG_RESULT/FINISHED.
func TestJudgeVacantMidSongKeepsSongPanelButHidesAnswer(t *testing.T) {
	eng, pack, seats, state := viewMidSongFixture(t)
	state, after, _ := applyResignAndCheck(t, eng, pack, state, seats[0].UserID)
	if after.GetPhase() != pb.Phase_PHASE_JUDGE_VACANT {
		t.Fatalf("phase = %s, want PHASE_JUDGE_VACANT", after.GetPhase())
	}
	if after.GetResumePhase() != pb.Phase_PHASE_PENDING_JUDGEMENT {
		t.Fatalf("resume_phase = %s, want PHASE_PENDING_JUDGEMENT", after.GetResumePhase())
	}

	view, err := eng.ViewFor(pack, state, seats[2].UserID)
	if err != nil {
		t.Fatalf("ViewFor: %v", err)
	}
	sv, ok := view.(*SongGuessView)
	if !ok {
		t.Fatalf("view is %T, want *SongGuessView", view)
	}
	if sv.Phase != "judge_vacant" {
		t.Fatalf(`phase = %q, want "judge_vacant" (the literal parked phase, for the blocking banner)`, sv.Phase)
	}
	if sv.Song == nil {
		t.Fatalf("song panel is nil while parked mid-song; want it to remain present")
	}

	raw := mustMarshalView(t, view)
	assertNoneContain(t, raw, midSongPlayerOnlyHiddenSubstrings(), "player view while judge-vacant mid-song")
}

// TestFalsifiedAnswerLeakIsCaught is Step 11's test 7: a negative control
// proving TestPlayerViewLeaksNothingMidSong actually fails when the
// production code regresses, not just when the test happens to pass. This
// test does NOT modify songView — see the worked report for the actual
// failure text observed when Answer was temporarily assigned
// unconditionally in songView (for every viewer class, every phase) and
// TestPlayerViewLeaksNothingMidSong was re-run; the change was reverted
// immediately afterward, and go vet/test/lint were re-confirmed clean.
// This test instead re-derives the same leak check inline against a
// deliberately over-disclosing view built by hand, so the regression proof
// stays in the suite permanently without requiring anyone to re-break
// production code to reproduce it.
func TestFalsifiedAnswerLeakIsCaught(t *testing.T) {
	eng, pack, seats, state := viewMidSongFixture(t)
	view, err := eng.ViewFor(pack, state, seats[2].UserID)
	if err != nil {
		t.Fatalf("ViewFor: %v", err)
	}
	sv, ok := view.(*SongGuessView)
	if !ok {
		t.Fatalf("view is %T, want *SongGuessView", view)
	}

	// Simulate the falsification: unconditionally assign Answer, exactly
	// the change described in the step report.
	broken := *sv
	brokenSong := *sv.Song
	brokenSong.Answer = &SongGuessAnswerView{Title: "Currently Playing", Artist: "Artist Current"}
	broken.Song = &brokenSong

	raw := mustMarshalView(t, &broken)
	leaked := false
	for _, s := range midSongPlayerOnlyHiddenSubstrings() {
		if strings.Contains(raw, s) {
			leaked = true
			break
		}
	}
	if !leaked {
		t.Fatalf("the leak-detection scan did not fire against a deliberately over-disclosing view; the check itself is broken")
	}
}

// ---------------------------------------------------------------------
// Architect ruling R3.5 — the "hole" leak surface: free-choice reveals
// (C1) can leave a closed block sitting BETWEEN two revealed ones, which a
// naive i < revealedCount(song) port (the pre-R3 logic) would
// over-disclose because it treats every index below the COUNT as revealed,
// not just the indices the mask actually marks true (view.go).
// ---------------------------------------------------------------------

// holeLeakBlockTexts is holeLeakPack's four block texts, each unique in this
// package so a substring leak can never be masked by a coincidental match
// against another fixture's text.
var holeLeakBlockTexts = [4]string{"HoleZero", "HoleOne", "HoleTwo", "HoleThree"}

// holeLeakPack is the *Pack fixture for the block-hole leak tests: a single
// 4-block song, one cell, no sibling cell — the hole itself is the entire
// point of this fixture, so there is no need to duplicate viewTestPack's
// separate "unplayed song in another cell" coverage here. Named to avoid
// colliding with viewTestPack/judgeTestPack/playTestPack/legalCheckPack
// (same package namespace — see the plan's "Discovered during
// implementation" section).
func holeLeakPack() *Pack {
	return &Pack{
		Name: "Hole Leak Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 4, Songs: []Song{
						{
							ID: "hole4-1", Title: "Hole Song Title", Artist: "Hole Song Artist",
							Line: []string{holeLeakBlockTexts[0], holeLeakBlockTexts[1], holeLeakBlockTexts[2], holeLeakBlockTexts[3]},
						},
					}},
				},
			},
		},
		TotalSongs: 1,
	}
}

// holeOpenedFixture drives holeLeakPack's 4-block song to a genuine "hole"
// mid-song state: blocks 0 and 2 revealed, blocks 1 and 3 still closed, with
// block 1 sitting closed BETWEEN two revealed blocks — exactly the shape
// ruling R3.5 flags as the new leak surface non-contiguous free-choice
// reveals (C1) introduce. Built via two real open_block Applies (index 0
// reveals directly under C2, since revealed==0 has no steal window yet;
// index 2 opens a steal window that the opponent captain declines via pass,
// which reveals the SAME index the active captain chose —
// song.StealWindowIndex, ruling R3.2b — never an arbitrary lowest-closed
// fallback) rather than engine_judge_test.go's
// revealBlocksViaStealDeclineLoop, which only ever produces the contiguous
// 0..n-1 shape and could never exercise a hole at all.
func holeOpenedFixture(t *testing.T) (*Engine, *Pack, []engine.SeatInit, []byte) {
	t.Helper()
	pack := holeLeakPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(211), "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 2),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[1].UserID, Type: ActionPass,
	})
	return eng, pack, seats, state
}

// assertBlockExactlyRevealed asserts sv.Song.Blocks[index] marshals to
// EXACTLY {"revealed":true,"text":"<want>"} (Req 25's revealed shape).
func assertBlockExactlyRevealed(t *testing.T, sv *SongGuessView, index int, want, label string) {
	t.Helper()
	if index >= len(sv.Song.Blocks) {
		t.Fatalf("%s: block %d out of range (len=%d)", label, index, len(sv.Song.Blocks))
	}
	b, err := json.Marshal(sv.Song.Blocks[index])
	if err != nil {
		t.Fatalf("%s: marshal block %d: %v", label, index, err)
	}
	wantJSON := `{"revealed":true,"text":"` + want + `"}`
	if string(b) != wantJSON {
		t.Fatalf("%s: block %d JSON = %s, want %s", label, index, b, wantJSON)
	}
}

// assertBlockExactlyClosed asserts sv.Song.Blocks[index] marshals to EXACTLY
// {"revealed":false} — no "text" key at all, not an empty string (Req 25).
// A field-by-field check (Blocks[index].Text == nil) cannot substitute for
// this: it would not catch a field someone adds to SongGuessBlockView later
// (the same reasoning TestFalsifiedAnswerLeakIsCaught documents above).
func assertBlockExactlyClosed(t *testing.T, sv *SongGuessView, index int, label string) {
	t.Helper()
	if index >= len(sv.Song.Blocks) {
		t.Fatalf("%s: block %d out of range (len=%d)", label, index, len(sv.Song.Blocks))
	}
	b, err := json.Marshal(sv.Song.Blocks[index])
	if err != nil {
		t.Fatalf("%s: marshal block %d: %v", label, index, err)
	}
	if string(b) != `{"revealed":false}` {
		t.Fatalf(`%s: block %d (hole) JSON = %s, want exactly {"revealed":false}`, label, index, b)
	}
}

// TestBlockHoleNotLeakedMidSong is Architect ruling R3.5's required
// regression test for the new leak surface free-choice reveals (C1)
// introduce. With blocks 0 and 2 revealed, block 1 is a genuine hole — a
// closed block sitting BETWEEN two revealed ones. A naive i <
// revealedCount(song) port (the pre-R3 logic, which this test's sibling
// TestFalsifyingHoleProjectionDisclosesBlockOne exists to falsify against)
// would treat blocks 0 AND 1 as revealed, because revealedCount == 2 either
// way — disclosing block 1's text even though it was never actually opened.
//
// blocks[] itself must hide the hole for EVERY class, judge included — that
// part is unconditional. The raw-JSON "hole text never appears at all"
// check, however, is scoped to player/spectator only: since the 2026-08-15
// judge-answer-line change, the judge legitimately sees every block's text,
// holes included, via answer.line (asserted directly below instead).
func TestBlockHoleNotLeakedMidSong(t *testing.T) {
	eng, pack, seats, state := holeOpenedFixture(t)

	viewers := map[string]uuid.UUID{
		"player":    seats[2].UserID, // active captain
		"spectator": uuid.New(),
		"judge":     seats[0].UserID, // host+judge in the default 4-seat roster.
	}

	for label, uid := range viewers {
		t.Run(label, func(t *testing.T) {
			view, err := eng.ViewFor(pack, state, uid)
			if err != nil {
				t.Fatalf("ViewFor: %v", err)
			}
			sv, ok := view.(*SongGuessView)
			if !ok {
				t.Fatalf("view is %T, want *SongGuessView", view)
			}
			if sv.Song == nil || len(sv.Song.Blocks) != 4 {
				t.Fatalf("%s: song panel missing or wrong length: %+v", label, sv.Song)
			}

			// blocks[] itself: unconditional, every class, judge included.
			assertBlockExactlyRevealed(t, sv, 0, holeLeakBlockTexts[0], label)
			assertBlockExactlyClosed(t, sv, 1, label)
			assertBlockExactlyRevealed(t, sv, 2, holeLeakBlockTexts[2], label)
			assertBlockExactlyClosed(t, sv, 3, label)

			raw := mustMarshalView(t, view)

			if label == "judge" {
				if sv.Song.Answer == nil || sv.Song.Answer.Title != "Hole Song Title" || sv.Song.Answer.Artist != "Hole Song Artist" {
					t.Fatalf("judge: answer = %+v, want the song's title/artist visible early (R2)", sv.Song.Answer)
				}
				// The judge's answer.line legitimately contains every block,
				// holes included — asserted here rather than excluded from
				// the raw-JSON scan below.
				for i, want := range holeLeakBlockTexts {
					if sv.Song.Answer.Line[i] != want {
						t.Fatalf("judge: answer.line[%d] = %q, want %q", i, sv.Song.Answer.Line[i], want)
					}
				}
				return
			}
			if strings.Contains(raw, `"answer"`) {
				t.Fatalf(`%s: leaks an "answer" key`, label)
			}
			assertNoneContain(t, raw, []string{holeLeakBlockTexts[1], holeLeakBlockTexts[3]}, label+" view (hole leak)")
		})
	}
}

// TestFalsifyingHoleProjectionDisclosesBlockOne is
// TestBlockHoleNotLeakedMidSong's negative control: it does NOT modify
// blocksView (view.go) — see the worked report for the actual failure text
// observed when blocksView's `i < len(mask) && mask[i]` condition was
// temporarily replaced with the pre-R3 `int32(i) < revealedCount(song)` and
// TestBlockHoleNotLeakedMidSong was re-run; the change was reverted
// immediately afterward, and go vet/test/lint were re-confirmed clean. This
// test instead re-derives the same over-disclosing projection inline
// against a deliberately broken *copy* of the real view, so the regression
// proof stays in the suite permanently without requiring anyone to re-break
// production code to reproduce it.
func TestFalsifyingHoleProjectionDisclosesBlockOne(t *testing.T) {
	eng, pack, seats, state := holeOpenedFixture(t)
	view, err := eng.ViewFor(pack, state, seats[2].UserID)
	if err != nil {
		t.Fatalf("ViewFor: %v", err)
	}
	sv, ok := view.(*SongGuessView)
	if !ok {
		t.Fatalf("view is %T, want *SongGuessView", view)
	}

	// Simulate the falsification: an "i < revealedCount" projection over
	// blocks 0/2-revealed would show index 1 (i=1 < revealedCount=2) as
	// revealed, with the real (still-secret) block 1 text attached.
	broken := *sv
	brokenSong := *sv.Song
	brokenBlocks := slices.Clone(sv.Song.Blocks)
	holeText := holeLeakBlockTexts[1]
	brokenBlocks[1] = SongGuessBlockView{Revealed: true, Text: &holeText}
	brokenSong.Blocks = brokenBlocks
	broken.Song = &brokenSong

	raw := mustMarshalView(t, &broken)
	if !strings.Contains(raw, holeLeakBlockTexts[1]) {
		t.Fatalf("the leak-detection scan did not fire against a deliberately over-disclosing hole projection; the check itself is broken")
	}
}

// TestSongResultRevealsAllBlocksIncludingHoles is ruling R3.5's second
// required test: a song that ends WITH a hole still present (blocks 0/2
// revealed, blocks 1/3 never opened) must show every block's text, to every
// viewer class, once it reaches SONG_RESULT — isSongFinished's "reveal
// everything" branch must ignore the mask entirely (blocksView's `finished
// ||` short-circuit), not iterate it, so an unlucky ending never
// permanently hides a block nobody got around to opening.
func TestSongResultRevealsAllBlocksIncludingHoles(t *testing.T) {
	eng, pack, seats, state := holeOpenedFixture(t)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "guess"),
	})
	state, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})
	if after.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
		t.Fatalf("phase = %s, want PHASE_SONG_RESULT", after.GetPhase())
	}

	viewers := map[string]uuid.UUID{
		"active_captain":   seats[2].UserID,
		"opponent_captain": seats[1].UserID,
		"judge":            seats[0].UserID,
		"spectator":        uuid.New(),
	}
	for label, uid := range viewers {
		t.Run(label, func(t *testing.T) {
			view, err := eng.ViewFor(pack, state, uid)
			if err != nil {
				t.Fatalf("ViewFor: %v", err)
			}
			sv, ok := view.(*SongGuessView)
			if !ok {
				t.Fatalf("view is %T, want *SongGuessView", view)
			}
			if sv.Song == nil || len(sv.Song.Blocks) != 4 {
				t.Fatalf("%s: song panel missing or wrong length: %+v", label, sv.Song)
			}
			for i, want := range holeLeakBlockTexts {
				assertBlockExactlyRevealed(t, sv, i, want, label)
			}
		})
	}
}

// TestSongViewPotentialMatchesBlocksMinusRevealed closes the view-level half
// of law 8 (invariants_test.go's assertPotentialIdentity doc comment defers
// this exact check to view_test.go): SongGuessView.Song.Potential must equal
// total_blocks - revealed. wantPotential is computed here directly from the
// view's OWN TotalBlocks/Revealed fields, independently of songView's
// internal call to potentialOf, so this catches either a regression in
// potentialOf itself or a regression in how songView wires Potential onto
// the view.
func TestSongViewPotentialMatchesBlocksMinusRevealed(t *testing.T) {
	eng, pack, seats, state := holeOpenedFixture(t)
	view, err := eng.ViewFor(pack, state, seats[2].UserID)
	if err != nil {
		t.Fatalf("ViewFor: %v", err)
	}
	sv, ok := view.(*SongGuessView)
	if !ok {
		t.Fatalf("view is %T, want *SongGuessView", view)
	}
	if sv.Song == nil {
		t.Fatalf("song panel is nil")
	}

	if sv.Song.Revealed != 2 {
		t.Fatalf("song.revealed = %d, want 2 (blocks 0 and 2 opened)", sv.Song.Revealed)
	}
	if sv.Song.TotalBlocks != 4 {
		t.Fatalf("song.total_blocks = %d, want 4", sv.Song.TotalBlocks)
	}
	wantPotential := int32(sv.Song.TotalBlocks) - sv.Song.Revealed
	if sv.Song.Potential != wantPotential {
		t.Fatalf("song.potential = %d, want %d (= total_blocks - revealed)", sv.Song.Potential, wantPotential)
	}
}

// ---------------------------------------------------------------------
// Architect ruling R2 — pending.guess_text is team-scoped, not public
// (view.go).
// ---------------------------------------------------------------------

// pendingGuessScopeTitle is the current song's title in
// pendingGuessScopeFixture, and also the exact guess text TEAM_A's captain
// submits. The equality is deliberate (R2's required test): it is what
// makes a team-scope leak detectable by a plain substring scan, exactly
// like TestFalsifiedAnswerLeakIsCaught's answer-leak check.
const pendingGuessScopeTitle = "Currently Playing"

// pendingGuessScopeFixture drives viewTestPack's 6-block song to
// PHASE_PENDING_JUDGEMENT via TEAM_A's captain (seats[2]) submitting a guess
// text exactly equal to the song's title. Uses 5 seats, not
// viewMidSongFixture's 4, specifically so TEAM_A has a non-captain member
// (seat4) in addition to its captain (seat2) — the default parity roster
// (engine.go's defaultParticipants/applyHostJudgeDefault) gives TEAM_A only
// one member at 4 seats, which cannot exercise both a captain and a
// non-captain seeing it.
func pendingGuessScopeFixture(t *testing.T) (*Engine, *Pack, []engine.SeatInit, []byte) {
	t.Helper()
	pack := viewTestPack()
	seats := rosterTestSeats(5)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(199), "English", 6)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, pendingGuessScopeTitle),
	})
	return eng, pack, seats, state
}

// pendingViewFor is TestPendingGuessTextIsTeamScopedAndJudgeVisible's
// shared ViewFor-and-unwrap step: every assertion helper below needs the
// typed *SongGuessView (for Pending.HasGuess/GuessText) AND the marshalled
// JSON (for the leak-scan substring checks), so this builds both once.
func pendingViewFor(t *testing.T, eng *Engine, pack *Pack, state []byte, uid uuid.UUID, label string) (*SongGuessView, string) {
	t.Helper()
	view, err := eng.ViewFor(pack, state, uid)
	if err != nil {
		t.Fatalf("%s: ViewFor: %v", label, err)
	}
	sv, ok := view.(*SongGuessView)
	if !ok {
		t.Fatalf("%s: view is %T, want *SongGuessView", label, view)
	}
	return sv, mustMarshalView(t, view)
}

// assertPendingGuessVisible asserts uid's view of state's pending judgement
// carries has_guess=true AND the exact claimant guess text — the judge and
// the claimant's own team (R2, Option C).
func assertPendingGuessVisible(t *testing.T, eng *Engine, pack *Pack, state []byte, uid uuid.UUID, label string) {
	t.Helper()
	sv, _ := pendingViewFor(t, eng, pack, state, uid, label)
	if sv.Pending == nil {
		t.Fatalf("%s: pending is nil, want the in-flight judgement", label)
	}
	if !sv.Pending.HasGuess {
		t.Fatalf("%s: has_guess = false, want true", label)
	}
	if sv.Pending.GuessText == nil || *sv.Pending.GuessText != pendingGuessScopeTitle {
		t.Fatalf("%s: guess_text = %v, want %q", label, sv.Pending.GuessText, pendingGuessScopeTitle)
	}
}

// assertPendingGuessHidden asserts uid's view of state's pending judgement
// carries has_guess=true but the "guess_text" key is omitted entirely (not
// emptied) and the raw JSON contains no trace of the title substring — the
// opposing team and spectators (R2, Option C).
func assertPendingGuessHidden(t *testing.T, eng *Engine, pack *Pack, state []byte, uid uuid.UUID, label string) {
	t.Helper()
	sv, raw := pendingViewFor(t, eng, pack, state, uid, label)
	if sv.Pending == nil {
		t.Fatalf("%s: pending is nil, want the in-flight judgement", label)
	}
	if !sv.Pending.HasGuess {
		t.Fatalf("%s: has_guess = false, want true", label)
	}
	if sv.Pending.GuessText != nil {
		t.Fatalf("%s: guess_text = %q, want omitted entirely", label, *sv.Pending.GuessText)
	}
	assertNoneContain(t, raw, []string{`"guess_text"`, pendingGuessScopeTitle}, label)
}

// assertTitleNeverLeaked asserts uid's raw view JSON contains no trace of
// the title substring at all — used post-void, when there is no longer a
// pending judgement to check has_guess/guess_text against, only the leak
// itself.
func assertTitleNeverLeaked(t *testing.T, eng *Engine, pack *Pack, state []byte, uid uuid.UUID, label string) {
	t.Helper()
	_, raw := pendingViewFor(t, eng, pack, state, uid, label)
	assertNoneContain(t, raw, []string{pendingGuessScopeTitle}, label)
}

// assertLastGuessTextPublic asserts uid's view shows song.last_guess_text ==
// the title once the song has genuinely ended (SONG_RESULT) — public to
// every class per SongGuessSongView's doc comment, with no team scoping at
// all at this point.
func assertLastGuessTextPublic(t *testing.T, eng *Engine, pack *Pack, state []byte, uid uuid.UUID, label string) {
	t.Helper()
	sv, _ := pendingViewFor(t, eng, pack, state, uid, label)
	if sv.Song == nil {
		t.Fatalf("%s: song panel is nil at SONG_RESULT", label)
	}
	if sv.Song.LastGuessText != pendingGuessScopeTitle {
		t.Fatalf("%s: last_guess_text = %q, want %q", label, sv.Song.LastGuessText, pendingGuessScopeTitle)
	}
}

// TestPendingGuessTextIsTeamScopedAndJudgeVisible is Architect ruling R2's
// required test. Team A's captain submits a guess exactly equal to the
// song's title; the judge and Team A (captain and non-captain both) must
// see it, Team B (both roles) and a spectator must not — has_guess=true but
// guess_text omitted, and the title substring absent from the raw JSON
// entirely. void-ing the decision back to SONG_PLAY must not have let Team B
// see the title in the interim (the whole reasoning behind the ruling: void
// makes a decision "as if it never happened," but knowledge already shown
// cannot be un-shown).
//
// R2's 6th assertion — "at SONG_RESULT, last_guess_text is present for all
// four classes" — is its own subtest below. It was originally skipped
// (song.LastGuessText had no writer at all: finishSong/awardPoints never
// copied pending.GuessText into it before gs.Pending was cleared, so the
// field was permanently empty). Fixed by R2 follow-up F2:
// judge.go's awardPoints and applyJudgeLastChanceIncorrect now call
// play.go's finishSongWithGuess, a thin wrapper that stamps
// gs.GetSong().LastGuessText before delegating to finishSong (which clears
// gs.Pending). view.go's `sv.LastGuessText = song.GetLastGuessText()` was
// already correct — it just had nothing to read.
func TestPendingGuessTextIsTeamScopedAndJudgeVisible(t *testing.T) {
	eng, pack, seats, state := pendingGuessScopeFixture(t)

	assertPendingGuessVisible(t, eng, pack, state, seats[2].UserID, "team A captain")
	assertPendingGuessVisible(t, eng, pack, state, seats[4].UserID, "team A non-captain")
	assertPendingGuessHidden(t, eng, pack, state, seats[1].UserID, "team B captain")
	assertPendingGuessHidden(t, eng, pack, state, seats[3].UserID, "team B non-captain")
	assertPendingGuessVisible(t, eng, pack, state, seats[0].UserID, "judge")
	assertPendingGuessHidden(t, eng, pack, state, uuid.New(), "spectator")

	// void discards the decision "as if it never happened" and returns the
	// song to play. Team B must never have seen the title, even now.
	state, after, _ := applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionVoid,
	})
	if after.GetPhase() != pb.Phase_PHASE_SONG_PLAY {
		t.Fatalf("phase after void = %s, want PHASE_SONG_PLAY", after.GetPhase())
	}
	assertTitleNeverLeaked(t, eng, pack, state, seats[1].UserID, "team B captain after void")
	assertTitleNeverLeaked(t, eng, pack, state, seats[3].UserID, "team B non-captain after void")

	// Re-submit the same guess and let the judge rule it correct this time,
	// driving the song to SONG_RESULT.
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, pendingGuessScopeTitle),
	})
	state, after, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})
	if after.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
		t.Fatalf("phase = %s, want PHASE_SONG_RESULT", after.GetPhase())
	}

	t.Run("last_guess_text_public_at_song_result", func(t *testing.T) {
		assertLastGuessTextPublic(t, eng, pack, state, seats[2].UserID, "team A captain at result")
		assertLastGuessTextPublic(t, eng, pack, state, seats[1].UserID, "team B captain at result")
		assertLastGuessTextPublic(t, eng, pack, state, seats[0].UserID, "judge at result")
		assertLastGuessTextPublic(t, eng, pack, state, uuid.New(), "spectator at result")
	})
}

// ---------------------------------------------------------------------
// Judge-visible full answer line (2026-08-15 user request): the judge must
// see the WHOLE line — every block's text, including still-closed ones —
// for the entire time a song is on screen, not only while a team is being
// judged. blocks[] itself must stay exactly as it was: this is additive
// (SongGuessAnswerView.Line), never a flip of blocks[] to all-revealed.
// holeOpenedFixture (R3.5, above) already produces the exact "hole" shape
// this needs (blocks 0 and 2 open, 1 and 3 closed), so it is reused rather
// than duplicated.
// ---------------------------------------------------------------------

// TestJudgeAnswerLineIncludesClosedBlocksButBlocksViewIsUnchanged is the
// required leak/shape test for the judge-visible full line: the judge's
// answer.line must contain EVERY block's text, including the still-closed
// holes (indices 1 and 3), while song.blocks[] — the redacted, per-viewer
// projection players themselves see — must be byte-for-byte unchanged: the
// closed indices still carry no "text" key at all. This is exactly the
// assertion that would fail if someone "simplified" the change into
// flipping blocks[] to all-revealed for the judge instead of adding a
// separate answer.line.
func TestJudgeAnswerLineIncludesClosedBlocksButBlocksViewIsUnchanged(t *testing.T) {
	eng, pack, seats, state := holeOpenedFixture(t)
	view, err := eng.ViewFor(pack, state, seats[0].UserID) // host+judge in the default 4-seat roster.
	if err != nil {
		t.Fatalf("ViewFor: %v", err)
	}
	sv, ok := view.(*SongGuessView)
	if !ok {
		t.Fatalf("view is %T, want *SongGuessView", view)
	}
	if sv.Song == nil || sv.Song.Answer == nil {
		t.Fatalf("judge song/answer missing: %+v", sv.Song)
	}

	if len(sv.Song.Answer.Line) != len(holeLeakBlockTexts) {
		t.Fatalf("answer.line = %v, want %d entries (%v)", sv.Song.Answer.Line, len(holeLeakBlockTexts), holeLeakBlockTexts)
	}
	for i, want := range holeLeakBlockTexts {
		if sv.Song.Answer.Line[i] != want {
			t.Fatalf("answer.line[%d] = %q, want %q — the judge must see closed blocks too", i, sv.Song.Answer.Line[i], want)
		}
	}

	// blocks[] is the OTHER field on the same song panel: it must be
	// completely untouched by this change. The hole is still a hole.
	assertBlockExactlyRevealed(t, sv, 0, holeLeakBlockTexts[0], "judge blocks[]")
	assertBlockExactlyClosed(t, sv, 1, "judge blocks[]")
	assertBlockExactlyRevealed(t, sv, 2, holeLeakBlockTexts[2], "judge blocks[]")
	assertBlockExactlyClosed(t, sv, 3, "judge blocks[]")
}

// TestAnswerLineNotLeakedToPlayerSpectatorOrOpposingTeam is
// TestJudgeAnswerLineIncludesClosedBlocksButBlocksViewIsUnchanged's negative
// counterpart: the active captain (player), a spectator, and the OPPOSING
// team's captain must all get no "answer" key at all mid-song — and their
// marshalled JSON must contain neither closed block's text (1 and 3) as a
// substring, proving answer.line cannot leak through any other path either.
func TestAnswerLineNotLeakedToPlayerSpectatorOrOpposingTeam(t *testing.T) {
	eng, pack, seats, state := holeOpenedFixture(t)

	viewers := map[string]uuid.UUID{
		"player_active_captain": seats[2].UserID, // TEAM_A captain, opened both blocks.
		"opposing_team_captain": seats[1].UserID, // TEAM_B captain.
		"spectator":             uuid.New(),
	}
	for label, uid := range viewers {
		t.Run(label, func(t *testing.T) {
			view, err := eng.ViewFor(pack, state, uid)
			if err != nil {
				t.Fatalf("ViewFor: %v", err)
			}
			raw := mustMarshalView(t, view)
			if strings.Contains(raw, `"answer"`) {
				t.Fatalf("%s: view leaks an \"answer\" key: %s", label, raw)
			}
			assertNoneContain(t, raw, []string{holeLeakBlockTexts[1], holeLeakBlockTexts[3]}, label)
		})
	}
}

// ---------------------------------------------------------------------
// legalActions cross-check (legal.go).
// ---------------------------------------------------------------------

// legalCheckPack is the *Pack fixture for the legalActions cross-check: one
// cell, two 4-block songs — enough for every representative state below to
// draw its own song from a fresh engine/state instance.
func legalCheckPack() *Pack {
	return &Pack{
		Name: "Legal Check Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 4, Songs: []Song{
						{ID: "lc4-1", Title: "One", Artist: "Artist One", Line: []string{"a", "b", "c", "d"}},
						{ID: "lc4-2", Title: "Two", Artist: "Artist Two", Line: []string{"e", "f", "g", "h"}},
					}},
				},
			},
		},
		TotalSongs: 2,
	}
}

// legalCheckState is one representative (phase, sub-state) snapshot for
// TestLegalActionsAgreeWithApply: eng/pack/state must all correspond to the
// same live engine so payload-bearing actions can genuinely be applied
// against it, and want maps each ACTIVE participant's seat index to the
// exact action-name set legalActions must return for them.
type legalCheckState struct {
	name  string
	eng   *Engine
	pack  *Pack
	state []byte
	want  map[int32][]string
}

// buildRosterLegalCheck: PHASE_ROSTER, default 4-seat roster (seat0 host/
// judge, seat1 TEAM_B captain, seat2 TEAM_A captain, seat3 TEAM_B member —
// see engine_round_test.go's TestSelectCellRejectsWrongActor comment).
func buildRosterLegalCheck(t *testing.T) legalCheckState {
	t.Helper()
	pack := legalCheckPack()
	seats := rosterTestSeats(4)
	eng := New(&fakeRosterPackProvider{pack: pack})
	state, _, err := eng.InitState(context.Background(), rosterTestCfg(), seats)
	if err != nil {
		t.Fatalf("InitState: %v", err)
	}
	return legalCheckState{
		name: "roster", eng: eng, pack: pack, state: state,
		want: map[int32][]string{
			0: {ActionAssignTeam, ActionAssignCaptain, ActionAssignJudge, ActionSetTeamName, ActionBeginGame, ActionResign},
			1: {ActionResign},
			2: {ActionResign},
			3: {ActionResign},
		},
	}
}

// buildRoundSelectLegalCheck: PHASE_ROUND_SELECT, active_team == TEAM_A.
func buildRoundSelectLegalCheck(t *testing.T) legalCheckState {
	t.Helper()
	pack := legalCheckPack()
	seats := rosterTestSeats(4)
	eng, state := roundSelectState(t, pack, seats, NewSeededShuffler(101))
	return legalCheckState{
		name: "round_select", eng: eng, pack: pack, state: state,
		want: map[int32][]string{
			0: {ActionResign},
			1: {ActionResign},
			2: {ActionSelectCell, ActionResign},
			3: {ActionResign},
		},
	}
}

// buildSongPlayActiveLegalCheck: PHASE_SONG_PLAY, AWAITING_ACTIVE.
func buildSongPlayActiveLegalCheck(t *testing.T) legalCheckState {
	t.Helper()
	pack := legalCheckPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(102), "English", 4)
	return legalCheckState{
		name: "song_play_active", eng: eng, pack: pack, state: state,
		want: map[int32][]string{
			0: {ActionSkipSong, ActionResign},
			1: {ActionResign},
			2: {ActionOpenBlock, ActionGuess, ActionResign},
			3: {ActionResign},
		},
	}
}

// buildSongPlayStealLegalCheck: PHASE_SONG_PLAY, AWAITING_STEAL (opened by
// the active captain's open_block). C2 forbids a steal window at
// revealed==0, so block 0 is opened directly first (revealed count 0->1)
// before opening block 1 can actually reach AWAITING_STEAL.
func buildSongPlayStealLegalCheck(t *testing.T) legalCheckState {
	t.Helper()
	pack := legalCheckPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(103), "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 0),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionOpenBlock, Payload: openBlockPayload(t, 1),
	})
	return legalCheckState{
		name: "song_play_steal", eng: eng, pack: pack, state: state,
		want: map[int32][]string{
			0: {ActionSkipSong, ActionResign},
			1: {ActionSteal, ActionPass, ActionResign},
			2: {ActionResign},
			3: {ActionResign},
		},
	}
}

// buildSongPlayLastChanceLegalCheck: PHASE_SONG_PLAY, AWAITING_LAST_CHANCE
// (opened by the active captain's wrong guess).
func buildSongPlayLastChanceLegalCheck(t *testing.T) legalCheckState {
	t.Helper()
	pack := legalCheckPack()
	seats := rosterTestSeats(4)
	eng, state := playSongPlayState(t, pack, seats, NewSeededShuffler(104), "English", 4)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[2].UserID, Type: ActionGuess, Payload: marshalGuessPayload(t, "wrong guess"),
	})
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, false),
	})
	return legalCheckState{
		name: "song_play_last_chance", eng: eng, pack: pack, state: state,
		want: map[int32][]string{
			0: {ActionSkipSong, ActionResign},
			1: {ActionGuess, ActionPass, ActionResign},
			2: {ActionResign},
			3: {ActionResign},
		},
	}
}

// buildPendingJudgementLegalCheck: PHASE_PENDING_JUDGEMENT, kind ==
// active_guess.
func buildPendingJudgementLegalCheck(t *testing.T) legalCheckState {
	t.Helper()
	pack := legalCheckPack()
	seats := rosterTestSeats(4)
	eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(105), "English", 4, 0)
	return legalCheckState{
		name: "pending_judgement", eng: eng, pack: pack, state: state,
		want: map[int32][]string{
			0: {ActionJudgeResult, ActionVoid, ActionSkipSong, ActionResign},
			1: {ActionResign},
			2: {ActionResign},
			3: {ActionResign},
		},
	}
}

// buildSongResultLegalCheck: PHASE_SONG_RESULT, active_team still TEAM_A
// (the flip happens at next_round, not here).
func buildSongResultLegalCheck(t *testing.T) legalCheckState {
	t.Helper()
	pack := legalCheckPack()
	seats := rosterTestSeats(4)
	eng, state := judgeActiveGuessState(t, pack, seats, NewSeededShuffler(106), "English", 4, 0)
	state, _, _ = applyPlayAction(t, eng, pack, state, engine.Action{
		UserID: seats[0].UserID, Type: ActionJudgeResult, Payload: marshalJudgeResultPayload(t, true),
	})
	return legalCheckState{
		name: "song_result", eng: eng, pack: pack, state: state,
		want: map[int32][]string{
			0: {ActionNextRound, ActionResign}, // host
			1: {ActionNextRound, ActionResign}, // TEAM_B captain, about to become active
			2: {ActionResign},
			3: {ActionResign},
		},
	}
}

// buildJudgeVacantLegalCheck: PHASE_JUDGE_VACANT, parked mid-SONG_PLAY, host
// and judge are DIFFERENT seats (leaveRosterWithSeparateJudge,
// engine_leave_test.go) so resigning the judge does not also touch the
// host seat.
func buildJudgeVacantLegalCheck(t *testing.T) legalCheckState {
	t.Helper()
	pack := legalCheckPack()
	seats := rosterTestSeats(5)
	eng, state := leaveRosterWithSeparateJudge(t, pack, seats, NewSeededShuffler(107))
	state, _, _ = selectCellAndCheck(t, eng, pack, state, seats[2].UserID, "English", 4)
	state, _, _ = applyResignAndCheck(t, eng, pack, state, seats[4].UserID) // judge resigns.
	return legalCheckState{
		name: "judge_vacant", eng: eng, pack: pack, state: state,
		want: map[int32][]string{
			0: {ActionAssignJudge, ActionResign}, // host (roleless after losing the judgeship earlier).
			1: {ActionResign},
			2: {ActionResign},
			3: {ActionResign},
			// seat4 (the resigned judge) is inactive and excluded from the
			// participants loop automatically.
		},
	}
}

// legalCheckPayload builds a valid, gs-appropriate payload for actionType,
// or nil for actions that need none (pass, void, skip_song, next_round,
// resign never call decodePayload). The payload's CONTENT only has to be
// valid for whichever seat genuinely IS the legal actor in gs — every
// requireX guard in roster.go/round.go/play.go/judge.go runs BEFORE
// decodePayload, so a wrong-actor/wrong-phase Apply call fails on the
// guard regardless of payload content.
func legalCheckPayload(t *testing.T, actionType string, gs *pb.GameState) json.RawMessage {
	t.Helper()
	switch actionType {
	case ActionAssignTeam:
		return mustMarshalJSON(t, AssignTeamPayload{Seat: firstNonJudgeActiveSeat(gs), Team: "b"})
	case ActionAssignCaptain:
		return mustMarshalJSON(t, AssignCaptainPayload{Seat: firstNonJudgeActiveSeat(gs)})
	case ActionAssignJudge:
		return mustMarshalJSON(t, AssignJudgePayload{Seat: firstNonHostActiveSeat(gs)})
	case ActionSetTeamName:
		return mustMarshalJSON(t, SetTeamNamePayload{Team: "a", Name: "Cross Check"})
	case ActionBeginGame:
		return json.RawMessage(`{}`)
	case ActionSelectCell:
		if cell := firstOpenCell(gs); cell != nil {
			return marshalSelectCell(t, cell.GetLanguageName(), cell.GetBucket())
		}
		return marshalSelectCell(t, "English", 4)
	case ActionOpenBlock:
		// Index 0 only needs to be VALID for the one fixture where
		// open_block is genuinely in the legal set (buildSongPlayActiveLegalCheck's
		// fresh, zero-revealed song) — every other fixture rejects it earlier,
		// on phase/sub-state/actor, before the index is ever inspected.
		return openBlockPayload(t, 0)
	case ActionGuess, ActionSteal:
		return marshalGuessPayload(t, "cross check guess")
	case ActionJudgeResult:
		return marshalJudgeResultPayload(t, true)
	default:
		return nil
	}
}

// firstNonJudgeActiveSeat returns the lowest-seat active, non-judge
// participant — a valid assign_team/assign_captain target in every
// representative state's default roster.
func firstNonJudgeActiveSeat(gs *pb.GameState) int32 {
	for _, p := range gs.GetParticipants() {
		if p.GetActive() && !p.GetIsJudge() {
			return p.GetSeatIndex()
		}
	}
	return 0
}

// firstNonHostActiveSeat returns the lowest-seat active participant who is
// NOT the host — a valid assign_judge target.
func firstNonHostActiveSeat(gs *pb.GameState) int32 {
	for _, p := range gs.GetParticipants() {
		if p.GetActive() && p.GetSeatIndex() != gs.GetHostSeat() {
			return p.GetSeatIndex()
		}
	}
	return gs.GetHostSeat()
}

// firstOpenCell returns the first cell with at least one unplayed song, or
// nil if none remain.
func firstOpenCell(gs *pb.GameState) *pb.Cell {
	for _, c := range gs.GetCells() {
		if len(c.GetUnplayedSongIds()) > 0 {
			return c
		}
	}
	return nil
}

// mustMarshalJSON marshals v, failing t on error.
func mustMarshalJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	return b
}

// sameActionSet reports whether want and got contain the same action names,
// order-independent.
func sameActionSet(want, got []string) bool {
	if len(want) != len(got) {
		return false
	}
	for _, a := range want {
		if !slices.Contains(got, a) {
			return false
		}
	}
	return true
}

// isTrackedLegalityError reports whether err is one of the three sentinels
// the plan's cross-check requires (Step 11 brief): engine.ErrNotYourTurn,
// engine.ErrWrongPhase, engine.ErrIllegalAction. Deliberately excludes
// engine.ErrNotSeated/ErrGameOver — see TestLegalActionsAgreeWithApply's
// doc comment for why this cross-check is scoped to ACTIVE seated
// participants and non-FINISHED states.
func isTrackedLegalityError(err error) bool {
	return errors.Is(err, engine.ErrNotYourTurn) || errors.Is(err, engine.ErrWrongPhase) || errors.Is(err, engine.ErrIllegalAction)
}

// assertLegalActionsCrossCheck is TestLegalActionsAgreeWithApply's per-seat
// body: legalActions(gs, p) must equal want (as a set), and for EVERY
// action constant, whether it is genuinely applicable via eng.Apply must
// agree with whether it is in that set.
func assertLegalActionsCrossCheck(t *testing.T, tc legalCheckState, p *pb.Participant, want []string) {
	t.Helper()

	gs := requireRosterState(t, tc.state)
	got := legalActions(gs, p)
	if !sameActionSet(want, got) {
		t.Fatalf("seat %d: legalActions = %v, want %v", p.GetSeatIndex(), got, want)
	}

	userID, err := uuid.Parse(p.GetUserId())
	if err != nil {
		t.Fatalf("parse seat %d user id %q: %v", p.GetSeatIndex(), p.GetUserId(), err)
	}

	for _, actionType := range allSongGuessActions {
		inSet := slices.Contains(got, actionType)
		payload := legalCheckPayload(t, actionType, gs)
		_, _, applyErr := tc.eng.Apply(tc.state, engine.Action{UserID: userID, Type: actionType, Payload: payload})

		switch {
		case inSet && isTrackedLegalityError(applyErr):
			t.Errorf("seat %d: %q is in the legal set but Apply failed with %v", p.GetSeatIndex(), actionType, applyErr)
		case !inSet && !isTrackedLegalityError(applyErr):
			t.Errorf("seat %d: %q is NOT in the legal set but Apply's error (%v) is not one of ErrNotYourTurn/ErrWrongPhase/ErrIllegalAction",
				p.GetSeatIndex(), actionType, applyErr)
		}
	}
}

// TestLegalActionsAgreeWithApply is the cross-check the Step 11 brief
// demands: for a representative set of (phase, sub-state) snapshots and
// every ACTIVE seated participant, legalActions' returned set must agree
// exactly with what eng.Apply actually accepts or rejects.
//
// Scoped to ACTIVE participants (never a spectator or a resigned seat) and
// to non-FINISHED phases: engine.Apply rejects PHASE_FINISHED outright with
// engine.ErrGameOver before dispatch ever runs, and a genuinely unseated
// caller gets engine.ErrNotSeated — neither is one of the three sentinels
// the brief's cross-check names (ErrNotYourTurn/ErrWrongPhase/
// ErrIllegalAction), so both are out of scope by construction rather than
// papered over.
func TestLegalActionsAgreeWithApply(t *testing.T) {
	cases := []legalCheckState{
		buildRosterLegalCheck(t),
		buildRoundSelectLegalCheck(t),
		buildSongPlayActiveLegalCheck(t),
		buildSongPlayStealLegalCheck(t),
		buildSongPlayLastChanceLegalCheck(t),
		buildPendingJudgementLegalCheck(t),
		buildSongResultLegalCheck(t),
		buildJudgeVacantLegalCheck(t),
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := requireRosterState(t, tc.state)
			for _, p := range gs.GetParticipants() {
				if !p.GetActive() {
					continue
				}
				want, ok := tc.want[p.GetSeatIndex()]
				if !ok {
					t.Fatalf("no expectation registered for active seat %d in case %q", p.GetSeatIndex(), tc.name)
				}
				assertLegalActionsCrossCheck(t, tc, p, want)
			}
		})
	}
}
