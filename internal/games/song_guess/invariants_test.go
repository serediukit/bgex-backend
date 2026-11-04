package song_guess

import (
	"testing"

	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// assertInvariants checks all twelve conservation laws from the plan's
// "assertInvariants conservation laws" section against gs (for pack) and
// fails t on any violation, with a distinct, greppable message per check so
// a failure immediately identifies which invariant broke. Every song_guess
// engine test calls this after every transition, mirroring
// ttr/invariants_test.go's assertInvariants.
//
// This step (6) only exercises laws 1, 2, 5, 6 and 10 (roster/pack-shape
// invariants; no song is ever in flight during PHASE_ROSTER). The remaining
// laws are still written here, in full, so Steps 7-10 (which put a song in
// flight, open a steal window, and reach PHASE_FINISHED) only ever add call
// sites against this same function — never a new law.
func assertInvariants(t *testing.T, pack *Pack, gs *pb.GameState) {
	t.Helper()

	assertSongConservation(t, pack, gs)
	assertPerCellConservation(t, pack, gs)
	assertScoreConservation(t, gs)
	assertPointsBound(t, gs)
	assertCaptainRoster(t, gs)
	assertJudgeRoster(t, gs)
	assertRevealBounds(t, gs)
	assertPotentialIdentity(t, gs)
	assertPendingCoherence(t, gs)
	assertActorExistence(t, gs)
	assertStealAllowedLifecycle(t, gs)
	assertAlternation(t, pack, gs)
}

// assertTransition checks the eight cross-state properties that
// assertInvariants (a single-snapshot check) cannot express: the seven from
// ruling R1.5, plus NEW law 13 (ruling R3.4) — reveals are monotone within
// one song. It compares the state on either side of one successful Apply
// call. Every song_guess engine test from Step 7 onward calls this alongside
// assertInvariants(t, pack, after) after every transition. pack is accepted
// for signature symmetry with assertInvariants and future use; none of the
// eight properties currently need it.
func assertTransition(t *testing.T, pack *Pack, before, after *pb.GameState) {
	t.Helper()

	assertStealAllowedMonotonic(t, before, after)
	assertScoreMonotonic(t, before, after)
	assertHistoryAppendOnly(t, before, after)
	assertCellsOnlyShrink(t, before, after)
	assertCountersMonotonic(t, before, after)
	assertNoReactivation(t, before, after)
	assertRevealsMonotonic(t, before, after)
}

// assertStealAllowedMonotonic checks R1.5 items 1-2: within the same song,
// steal_allowed never goes false -> true; a fresh song (before had none, or
// a different song id) always starts steal_allowed=true with a full-length
// revealed_mask of all false (ruling R3.4's restatement of the old
// "revealed=0" check, now that revealed is derived from the mask rather than
// stored directly — see assertFreshSongMask).
func assertStealAllowedMonotonic(t *testing.T, before, after *pb.GameState) {
	t.Helper()

	if after.GetSong() == nil {
		return
	}
	sameSong := before.GetSong() != nil && before.GetSong().GetSongId() == after.GetSong().GetSongId()
	if sameSong {
		if !before.GetSong().GetStealAllowed() && after.GetSong().GetStealAllowed() {
			t.Errorf("transition [steal_allowed monotonicity]: song %q went steal_allowed false -> true",
				after.GetSong().GetSongId())
		}
		return
	}
	if !after.GetSong().GetStealAllowed() {
		t.Errorf("transition [fresh song]: song %q started with steal_allowed=false, want true", after.GetSong().GetSongId())
	}
	assertFreshSongMask(t, after.GetSong())
}

// assertFreshSongMask is assertStealAllowedMonotonic's "fresh song" check,
// restated under ruling R3.4/R3.1: a freshly-drawn song must start with a
// revealed_mask sized to exactly len(blocks), with every entry false. This
// replaces the pre-R3.1 "revealed == 0" check now that revealed is derived
// from the mask (revealedCount) rather than stored as its own field.
func assertFreshSongMask(t *testing.T, s *pb.SongState) {
	t.Helper()

	mask := s.GetRevealedMask()
	if len(mask) != len(s.GetBlocks()) {
		t.Errorf("transition [fresh song]: song %q started with mask length %d, want %d (len(blocks))",
			s.GetSongId(), len(mask), len(s.GetBlocks()))
		return
	}
	for i, r := range mask {
		if r {
			t.Errorf("transition [fresh song]: song %q started with mask[%d]=true, want all false", s.GetSongId(), i)
		}
	}
}

// assertRevealsMonotonic checks NEW law 13 (ruling R3.4): within one song
// (before.Song and after.Song both set, and the same song_id), no
// revealed_mask entry ever flips true -> false, and the mask gains AT MOST
// ONE newly-true entry per Apply call. The second half is what pins "one
// open_block reveals exactly one block" and would catch an owed reveal
// (judge.go's applyJudgeStealIncorrect, or play.go's applyPassFromSteal)
// firing twice in a single transition, or a hypothetical bug that revealed
// two blocks at once. Split out into its own helper (rather than inlined in
// assertTransition, which is already large) to keep the lint gate's
// cyclomatic-complexity cap satisfied.
func assertRevealsMonotonic(t *testing.T, before, after *pb.GameState) {
	t.Helper()

	bs, as := before.GetSong(), after.GetSong()
	if bs == nil || as == nil || bs.GetSongId() != as.GetSongId() {
		return
	}

	bm, am := bs.GetRevealedMask(), as.GetRevealedMask()
	newlyRevealed := 0
	for i, wasRevealed := range bm {
		if i >= len(am) {
			break
		}
		if wasRevealed && !am[i] {
			t.Errorf("transition [reveals monotone]: song %q block %d went revealed true -> false", as.GetSongId(), i)
		}
		if !wasRevealed && am[i] {
			newlyRevealed++
		}
	}
	if newlyRevealed > 1 {
		t.Errorf("transition [reveals monotone]: song %q gained %d newly-revealed blocks in one Apply, want at most 1",
			as.GetSongId(), newlyRevealed)
	}
}

// assertScoreMonotonic checks R1.5 item 3: no TeamState.Score ever
// decreases across one transition.
func assertScoreMonotonic(t *testing.T, before, after *pb.GameState) {
	t.Helper()

	prev := make(map[pb.Team]int32, len(before.GetTeams()))
	for _, ts := range before.GetTeams() {
		prev[ts.GetTeam()] = ts.GetScore()
	}
	for _, ts := range after.GetTeams() {
		if ts.GetScore() < prev[ts.GetTeam()] {
			t.Errorf("transition [score monotonicity]: team %s score %d -> %d decreased",
				teamWire(ts.GetTeam()), prev[ts.GetTeam()], ts.GetScore())
		}
	}
}

// assertHistoryAppendOnly checks R1.5 item 4: after.History has
// before.History as a prefix and grew by 0 or 1 entries.
func assertHistoryAppendOnly(t *testing.T, before, after *pb.GameState) {
	t.Helper()

	bh, ah := before.GetHistory(), after.GetHistory()
	if len(ah) < len(bh) {
		t.Errorf("transition [history append-only]: history shrank from %d to %d entries", len(bh), len(ah))
		return
	}
	if len(ah)-len(bh) > 1 {
		t.Errorf("transition [history append-only]: history grew by %d entries in one transition, want 0 or 1",
			len(ah)-len(bh))
	}
	for i, h := range bh {
		if h.GetSongId() != ah[i].GetSongId() {
			t.Errorf("transition [history append-only]: history[%d] changed from song %q to %q",
				i, h.GetSongId(), ah[i].GetSongId())
		}
	}
}

// assertCellsOnlyShrink checks R1.5 item 5: every cell's unplayed count is
// non-increasing and played_count non-decreasing across one transition.
func assertCellsOnlyShrink(t *testing.T, before, after *pb.GameState) {
	t.Helper()

	prev := make(map[cellKey]*pb.Cell, len(before.GetCells()))
	for _, c := range before.GetCells() {
		prev[cellKey{c.GetLanguageName(), c.GetBucket()}] = c
	}
	for _, c := range after.GetCells() {
		p, ok := prev[cellKey{c.GetLanguageName(), c.GetBucket()}]
		if !ok {
			continue
		}
		if len(c.GetUnplayedSongIds()) > len(p.GetUnplayedSongIds()) {
			t.Errorf("transition [cells only shrink]: cell (%s,%d) unplayed count grew %d -> %d",
				c.GetLanguageName(), c.GetBucket(), len(p.GetUnplayedSongIds()), len(c.GetUnplayedSongIds()))
		}
		if c.GetPlayedCount() < p.GetPlayedCount() {
			t.Errorf("transition [cells only shrink]: cell (%s,%d) played_count shrank %d -> %d",
				c.GetLanguageName(), c.GetBucket(), p.GetPlayedCount(), c.GetPlayedCount())
		}
	}
}

// assertCountersMonotonic checks R1.5 item 6: round_no never decreases, and
// seq increases by exactly 1 per successful Apply.
func assertCountersMonotonic(t *testing.T, before, after *pb.GameState) {
	t.Helper()

	if after.GetRoundNo() < before.GetRoundNo() {
		t.Errorf("transition [counters]: round_no decreased %d -> %d", before.GetRoundNo(), after.GetRoundNo())
	}
	if after.GetSeq() != before.GetSeq()+1 {
		t.Errorf("transition [counters]: seq went %d -> %d, want exactly +1", before.GetSeq(), after.GetSeq())
	}
}

// assertNoReactivation checks R1.5 item 7: a participant's active flag
// never flips false -> true across one transition.
func assertNoReactivation(t *testing.T, before, after *pb.GameState) {
	t.Helper()

	prev := make(map[int32]bool, len(before.GetParticipants()))
	for _, p := range before.GetParticipants() {
		prev[p.GetSeatIndex()] = p.GetActive()
	}
	for _, p := range after.GetParticipants() {
		if !prev[p.GetSeatIndex()] && p.GetActive() {
			t.Errorf("transition [no reactivation]: seat %d went active false -> true", p.GetSeatIndex())
		}
	}
}

// isSongInFlight reports whether a song has been drawn from its cell but its
// outcome has not yet been appended to history — the state a song passes
// through between select_cell (which removes it from unplayed_song_ids
// immediately) and finishSong (which appends its outcome). This is
// deliberately a phase-based predicate, not `gs.Song != nil`: at
// PHASE_SONG_RESULT the outcome has already been appended to history while
// gs.Song is still populated (kept around for the result screen's display),
// so testing Song != nil there would double-count that song against both
// history and in-flight. See R1.2 in the plan.
//
// Evaluated against effectivePhase, not gs.Phase directly (Step 10 fix): a
// judge resigning mid-song (transition table row 25) parks the game in
// PHASE_JUDGE_VACANT with the real interrupted phase stashed in
// gs.ResumePhase — the drawn song is still genuinely in flight behind that
// parking, exactly as law 12b/12d already account for via effectivePhase.
// Without this substitution, law 1 false-fails the instant a judge leaves
// mid-song: the song is real, drawn, and unaccounted for in either its cell
// or history, yet gs.Phase alone reads as "not in flight".
func isSongInFlight(gs *pb.GameState) bool {
	switch effectivePhase(gs) {
	case pb.Phase_PHASE_SONG_PLAY, pb.Phase_PHASE_PENDING_JUDGEMENT:
		return true
	case pb.Phase_PHASE_UNSPECIFIED, pb.Phase_PHASE_ROSTER, pb.Phase_PHASE_ROUND_SELECT,
		pb.Phase_PHASE_SONG_RESULT, pb.Phase_PHASE_JUDGE_VACANT, pb.Phase_PHASE_FINISHED:
		return false
	}
	return false
}

// assertSongConservation checks law 1: every song is sitting unplayed in
// exactly one cell, accounted for exactly once in history, or in flight
// (drawn from its cell, outcome not yet appended) — never more than one of
// those, never none. See isSongInFlight and R1.2 in the plan for why the
// in-flight term is required: select_cell removes a song from its cell
// immediately, but the outcome is only appended once the song finishes, so
// a mid-song snapshot has one song that belongs to neither container.
func assertSongConservation(t *testing.T, pack *Pack, gs *pb.GameState) {
	t.Helper()

	inFlight := 0
	if isSongInFlight(gs) {
		inFlight = 1
	}

	unplayed := 0
	seenUnplayed := make(map[string]bool)
	playedCount := 0
	for _, c := range gs.GetCells() {
		for _, id := range c.GetUnplayedSongIds() {
			unplayed++
			seenUnplayed[id] = true
		}
		playedCount += int(c.GetPlayedCount())
	}

	seenHistory := make(map[string]bool, len(gs.GetHistory()))
	for _, h := range gs.GetHistory() {
		id := h.GetSongId()
		if seenHistory[id] {
			t.Errorf("invariant [song conservation]: song %q appears twice in history", id)
		}
		seenHistory[id] = true
		if seenUnplayed[id] {
			t.Errorf("invariant [song conservation]: song %q appears in both a cell and history", id)
		}
	}

	total := unplayed + len(gs.GetHistory()) + inFlight
	if total != pack.TotalSongs {
		t.Errorf("invariant [song conservation]: unplayed(%d)+history(%d)+inFlight(%d) = %d, want pack total %d",
			unplayed, len(gs.GetHistory()), inFlight, total, pack.TotalSongs)
	}

	wantPlayed := len(gs.GetHistory()) + inFlight
	if playedCount != wantPlayed {
		t.Errorf("invariant [song conservation]: sum of cell played_count = %d, want history(%d)+inFlight(%d) = %d",
			playedCount, len(gs.GetHistory()), inFlight, wantPlayed)
	}
}

// cellKey identifies a selection-table cell by its (language, bucket)
// intersection, ignoring language_index — used only to compare cell sets.
type cellKey struct {
	language string
	bucket   int32
}

// assertPerCellConservation checks law 2: for every cell,
// len(unplayed_song_ids) + played_count == pack.CountAt(language, bucket),
// and the set of cells is exactly the pack's, unchanged in size for the
// whole game (a cell is never removed once its unplayed_song_ids empties).
func assertPerCellConservation(t *testing.T, pack *Pack, gs *pb.GameState) {
	t.Helper()

	want := buildCells(pack)
	wantSet := make(map[cellKey]bool, len(want))
	for _, c := range want {
		wantSet[cellKey{c.GetLanguageName(), c.GetBucket()}] = true
	}

	if len(gs.GetCells()) != len(want) {
		t.Errorf("invariant [per-cell conservation]: %d cells, want %d (the pack's cell shape must never change)",
			len(gs.GetCells()), len(want))
	}

	for _, c := range gs.GetCells() {
		key := cellKey{c.GetLanguageName(), c.GetBucket()}
		if !wantSet[key] {
			t.Errorf("invariant [per-cell conservation]: cell (%s,%d) is not part of the pinned pack",
				c.GetLanguageName(), c.GetBucket())
			continue
		}
		wantCount := pack.CountAt(c.GetLanguageName(), int(c.GetBucket()))
		gotCount := len(c.GetUnplayedSongIds()) + int(c.GetPlayedCount())
		if gotCount != wantCount {
			t.Errorf("invariant [per-cell conservation]: cell (%s,%d) unplayed(%d)+played(%d) = %d, want %d",
				c.GetLanguageName(), c.GetBucket(), len(c.GetUnplayedSongIds()), c.GetPlayedCount(), gotCount, wantCount)
		}
	}
}

// assertScoreConservation checks law 3: each team's score equals the sum of
// history points awarded to it, the grand total of both teams' scores
// equals the grand total of history points (no points minted or lost), and
// no score or history point is ever negative.
func assertScoreConservation(t *testing.T, gs *pb.GameState) {
	t.Helper()

	sums := map[pb.Team]int32{}
	totalHistoryPoints := int32(0)
	for _, h := range gs.GetHistory() {
		if h.GetPoints() < 0 {
			t.Errorf("invariant [score conservation]: history entry %q has negative points %d", h.GetSongId(), h.GetPoints())
		}
		sums[h.GetAwardedTeam()] += h.GetPoints()
		totalHistoryPoints += h.GetPoints()
	}

	totalTeamScore := int32(0)
	for _, ts := range gs.GetTeams() {
		if ts.GetScore() < 0 {
			t.Errorf("invariant [score conservation]: team %s has negative score %d", teamWire(ts.GetTeam()), ts.GetScore())
		}
		if want := sums[ts.GetTeam()]; ts.GetScore() != want {
			t.Errorf("invariant [score conservation]: team %s score=%d, want sum of its history points %d",
				teamWire(ts.GetTeam()), ts.GetScore(), want)
		}
		totalTeamScore += ts.GetScore()
	}

	if totalTeamScore != totalHistoryPoints {
		t.Errorf("invariant [score conservation]: sum of team scores %d != sum of history points %d",
			totalTeamScore, totalHistoryPoints)
	}
}

// assertPointsBound checks law 4: every history entry's points fall within
// [0, bucket], and whenever a team was awarded (awarded_team != TEAM_NONE),
// points == bucket - revealed_at_end exactly.
func assertPointsBound(t *testing.T, gs *pb.GameState) {
	t.Helper()

	for _, h := range gs.GetHistory() {
		if h.GetPoints() < 0 || h.GetPoints() > h.GetBucket() {
			t.Errorf("invariant [points bound]: song %q points=%d out of [0,%d]", h.GetSongId(), h.GetPoints(), h.GetBucket())
		}
		if h.GetAwardedTeam() != pb.Team_TEAM_NONE {
			want := h.GetBucket() - h.GetRevealedAtEnd()
			if h.GetPoints() != want {
				t.Errorf("invariant [points bound]: song %q points=%d, want bucket(%d)-revealed(%d)=%d",
					h.GetSongId(), h.GetPoints(), h.GetBucket(), h.GetRevealedAtEnd(), want)
			}
		}
	}
}

// assertCaptainRoster checks law 5: for each of TEAM_A and TEAM_B, exactly
// one active captain whenever phase is outside {ROSTER, FINISHED}; at most
// one during ROSTER (FINISHED is grouped with ROSTER's laxer bound since no
// further roster mutation can happen there either).
func assertCaptainRoster(t *testing.T, gs *pb.GameState) {
	t.Helper()

	for _, team := range []pb.Team{pb.Team_TEAM_A, pb.Team_TEAM_B} {
		count := 0
		for _, p := range gs.GetParticipants() {
			if p.GetActive() && p.GetTeam() == team && p.GetIsCaptain() {
				count++
			}
		}
		switch gs.GetPhase() {
		case pb.Phase_PHASE_ROSTER, pb.Phase_PHASE_FINISHED:
			if count > 1 {
				t.Errorf("invariant [captain roster]: team %s has %d captains, want <= 1 during %s",
					teamWire(team), count, gs.GetPhase())
			}
		case pb.Phase_PHASE_UNSPECIFIED, pb.Phase_PHASE_ROUND_SELECT, pb.Phase_PHASE_SONG_PLAY,
			pb.Phase_PHASE_PENDING_JUDGEMENT, pb.Phase_PHASE_SONG_RESULT, pb.Phase_PHASE_JUDGE_VACANT:
			if count != 1 {
				t.Errorf("invariant [captain roster]: team %s has %d captains, want exactly 1 during %s",
					teamWire(team), count, gs.GetPhase())
			}
		}
	}
}

// assertJudgeRoster checks law 6: at most one participant is_judge overall;
// exactly one whenever phase is outside {ROSTER, JUDGE_VACANT, FINISHED};
// the judge's team is always TEAM_NONE; the judge is never is_captain.
func assertJudgeRoster(t *testing.T, gs *pb.GameState) {
	t.Helper()

	judges := 0
	for _, p := range gs.GetParticipants() {
		if !p.GetIsJudge() {
			continue
		}
		judges++
		if p.GetTeam() != pb.Team_TEAM_NONE {
			t.Errorf("invariant [judge roster]: judge seat %d has team %s, want none", p.GetSeatIndex(), teamWire(p.GetTeam()))
		}
		if p.GetIsCaptain() {
			t.Errorf("invariant [judge roster]: judge seat %d is also marked captain", p.GetSeatIndex())
		}
	}
	if judges > 1 {
		t.Errorf("invariant [judge roster]: %d participants marked judge, want <= 1", judges)
	}

	switch gs.GetPhase() {
	case pb.Phase_PHASE_ROSTER, pb.Phase_PHASE_JUDGE_VACANT, pb.Phase_PHASE_FINISHED:
	case pb.Phase_PHASE_UNSPECIFIED, pb.Phase_PHASE_ROUND_SELECT, pb.Phase_PHASE_SONG_PLAY,
		pb.Phase_PHASE_PENDING_JUDGEMENT, pb.Phase_PHASE_SONG_RESULT:
		if judges != 1 {
			t.Errorf("invariant [judge roster]: %d judges during %s, want exactly 1", judges, gs.GetPhase())
		}
	}
}

// assertRevealBounds checks law 7: 0 <= revealed <= len(blocks), and
// len(blocks) == bucket. A no-op when no song is in flight.
func assertRevealBounds(t *testing.T, gs *pb.GameState) {
	t.Helper()

	s := gs.GetSong()
	if s == nil {
		return
	}
	if rc := revealedCount(s); rc < 0 || int(rc) > len(s.GetBlocks()) {
		t.Errorf("invariant [reveal bounds]: revealed=%d out of [0,%d]", rc, len(s.GetBlocks()))
	}
	if len(s.GetBlocks()) != int(s.GetBucket()) {
		t.Errorf("invariant [reveal bounds]: len(blocks)=%d != bucket=%d", len(s.GetBlocks()), s.GetBucket())
	}
}

// assertPotentialIdentity checks law 8, restated under ruling R3.4:
// potential == len(blocks) - revealedCount(song); 0 <= revealedCount(song)
// <= len(blocks); and — R3.4's new clause — len(revealed_mask) ==
// len(blocks) whenever gs.Song != nil. The potential/bounds arithmetic holds
// by construction (potentialOf(song) IS exactly that expression — Step 11's
// view_test.go is where a second, independently-computed "potential" field
// first exists to compare against), but the mask-length clause is the one
// that actually catches something: a mask that was never sized at
// select_cell (left nil, or sized to the wrong bucket) would otherwise
// silently compute a wrong potential rather than fail loudly. Verified to
// genuinely fire (see the worked report for this step) by temporarily
// shrinking round.go's `RevealedMask: make([]bool, len(song.Line))` to one
// entry short and re-running an existing assertInvariants call site; the
// change was reverted immediately after — this is deliberately NOT a
// permanent test, since a fixture that must always fail this check would
// itself always fail `make test`. The runtime-meaningful half, checked last, is that
// the engine never leaves phase == SONG_PLAY with potential == 0: row 11 of
// the transition table auto-ends the song at that exact instant (Q9).
func assertPotentialIdentity(t *testing.T, gs *pb.GameState) {
	t.Helper()

	s := gs.GetSong()
	if s == nil {
		return
	}

	rc := revealedCount(s)
	if rc < 0 || int(rc) > len(s.GetBlocks()) {
		t.Errorf("invariant [potential identity]: revealedCount=%d out of [0,%d]", rc, len(s.GetBlocks()))
	}
	if len(s.GetRevealedMask()) != len(s.GetBlocks()) {
		t.Errorf("invariant [potential identity]: len(revealed_mask)=%d, want len(blocks)=%d (mask was never sized to the line)",
			len(s.GetRevealedMask()), len(s.GetBlocks()))
	}

	if potential := potentialOf(s); gs.GetPhase() == pb.Phase_PHASE_SONG_PLAY && potential == 0 {
		t.Errorf("invariant [potential identity]: phase=SONG_PLAY with potential=0, should have auto-ended into SONG_RESULT")
	}
}

// assertPendingCoherence checks law 9: pending is set iff (effective) phase
// == PENDING_JUDGEMENT; a set pending always has a resolved resume_awaiting;
// potential_at_claim matches len(blocks)-revealed at the moment checked
// (both fields are read from the same gs, so this also catches a stale
// potential_at_claim left over from a reveal that happened without updating
// it — which should never happen since pending is only set once per claim);
// and — ruling R3.2's addition — reveal_after_incorrect is true exactly for
// JUDGE_STEAL (never any other kind), and owed_reveal_index is a real,
// still-closed block index whenever reveal_after_incorrect is true, or
// exactly -1 (the sentinel every Pending constructor uses) when it is
// false.
//
// Evaluated against effectivePhase, not gs.Phase directly (Step 10 fix, the
// same class of gap as isSongInFlight above): the judge resigning while a
// judgement is pending (row 25) parks the game in PHASE_JUDGE_VACANT
// without touching gs.Pending — it is deliberately left exactly as-is for
// row 27's assign_judge to resume against — so a genuinely coherent pending
// judgement parked behind PHASE_JUDGE_VACANT must not read as incoherent.
func assertPendingCoherence(t *testing.T, gs *pb.GameState) {
	t.Helper()

	p := gs.GetPending()
	isPending := effectivePhase(gs) == pb.Phase_PHASE_PENDING_JUDGEMENT
	if isPending && p == nil {
		t.Errorf("invariant [pending coherence]: phase=PENDING_JUDGEMENT but pending is nil")
		return
	}
	if !isPending && p != nil {
		t.Errorf("invariant [pending coherence]: pending is set but phase=%s, want PENDING_JUDGEMENT", gs.GetPhase())
	}
	if p == nil {
		return
	}
	if p.GetResumeAwaiting() == pb.Awaiting_AWAITING_UNSPECIFIED {
		t.Errorf("invariant [pending coherence]: pending.resume_awaiting is unspecified")
	}
	if s := gs.GetSong(); s != nil {
		want := int32(len(s.GetBlocks())) - revealedCount(s)
		if p.GetPotentialAtClaim() != want {
			t.Errorf("invariant [pending coherence]: potential_at_claim=%d, want %d", p.GetPotentialAtClaim(), want)
		}
	}
	assertOwedRevealCoherence(t, gs, p)
}

// assertOwedRevealCoherence is law 9's ruling R3.2 addition, split out of
// assertPendingCoherence to keep that function's cyclomatic complexity
// under the lint gate: reveal_after_incorrect is true exactly for
// JUDGE_STEAL (never any other kind), and owed_reveal_index is a real,
// still-closed block index whenever reveal_after_incorrect is true, or
// exactly -1 (the sentinel every Pending constructor uses) when it is
// false.
func assertOwedRevealCoherence(t *testing.T, gs *pb.GameState, p *pb.Pending) {
	t.Helper()

	wantRevealAfterIncorrect := p.GetKind() == pb.JudgeKind_JUDGE_STEAL
	if p.GetRevealAfterIncorrect() != wantRevealAfterIncorrect {
		t.Errorf("invariant [pending coherence]: reveal_after_incorrect=%v, want %v (kind=%s)",
			p.GetRevealAfterIncorrect(), wantRevealAfterIncorrect, judgeKindWire(p.GetKind()))
	}

	idx := p.GetOwedRevealIndex()
	if !p.GetRevealAfterIncorrect() {
		if idx != -1 {
			t.Errorf("invariant [pending coherence]: owed_reveal_index=%d, want -1 (reveal_after_incorrect=false)", idx)
		}
		return
	}
	s := gs.GetSong()
	if s == nil {
		return
	}
	mask := s.GetRevealedMask()
	if idx < 0 || int(idx) >= len(mask) || mask[idx] {
		t.Errorf("invariant [pending coherence]: owed_reveal_index=%d invalid or already revealed (mask len %d)", idx, len(mask))
	}
}

// assertActorExistence checks law 10, the direct guard against the
// turn-order-deadlock class: for every non-FINISHED phase, at least one
// legal actor for the current sub-state exists and is active.
func assertActorExistence(t *testing.T, gs *pb.GameState) {
	t.Helper()

	if gs.GetPhase() == pb.Phase_PHASE_FINISHED {
		return
	}
	if !hasLegalActor(gs) {
		t.Errorf("invariant [actor existence]: phase %s has no legal actor available", gs.GetPhase())
	}
}

// hasLegalActor derives, per phase/sub-state, whether the actor the plan's
// transition table names for it is present and active. See the "Deadlock
// audit" paragraph in the plan's "Chosen Approach" section for why each
// phase's actor set is never empty by design.
func hasLegalActor(gs *pb.GameState) bool {
	switch gs.GetPhase() {
	case pb.Phase_PHASE_ROSTER, pb.Phase_PHASE_JUDGE_VACANT:
		h := hostParticipant(gs)
		return h != nil && h.GetActive()
	case pb.Phase_PHASE_ROUND_SELECT:
		return activeCaptainExists(gs, gs.GetActiveTeam())
	case pb.Phase_PHASE_SONG_PLAY:
		return hasLegalSongPlayActor(gs)
	case pb.Phase_PHASE_PENDING_JUDGEMENT:
		j := judgeOf(gs)
		return j != nil && j.GetActive()
	case pb.Phase_PHASE_SONG_RESULT:
		// Row 21: the *next* active team's captain, or the host.
		if activeCaptainExists(gs, opponentOf(gs.GetActiveTeam())) {
			return true
		}
		h := hostParticipant(gs)
		return h != nil && h.GetActive()
	case pb.Phase_PHASE_UNSPECIFIED, pb.Phase_PHASE_FINISHED:
		return false
	}
	return false
}

// hasLegalSongPlayActor is PHASE_SONG_PLAY's share of hasLegalActor, split
// out to keep hasLegalActor's switch flat (gocyclo).
func hasLegalSongPlayActor(gs *pb.GameState) bool {
	s := gs.GetSong()
	if s == nil {
		return false
	}
	switch s.GetAwaiting() {
	case pb.Awaiting_AWAITING_ACTIVE:
		return activeCaptainExists(gs, gs.GetActiveTeam())
	case pb.Awaiting_AWAITING_STEAL, pb.Awaiting_AWAITING_LAST_CHANCE:
		return activeCaptainExists(gs, opponentOf(gs.GetActiveTeam()))
	case pb.Awaiting_AWAITING_UNSPECIFIED:
		return false
	}
	return false
}

// activeCaptainExists reports whether team currently has an active captain.
func activeCaptainExists(gs *pb.GameState, team pb.Team) bool {
	c := captainOf(gs, team)
	return c != nil && c.GetActive()
}

// assertStealAllowedLifecycle checks law 11, restated under ruling R3.4:
// steal_allowed == false ⇒ revealedCount(song) > 0 — a lockout can only
// exist after at least one reveal.
//
// The pre-R3.4 phrasing (revealed == 0 ⇒ steal_allowed == true) is the exact
// same boolean statement by contraposition, but it had gone vacuous in
// spirit under C2: no steal window can even open at revealed == 0 any more
// (TestOpponentCannotStealBeforeFirstReveal), so the one scenario the old
// phrasing was written to guard against — a lockout recorded against the
// very first window — is now structurally unreachable, even though the
// truth table never changed. Restating it this way documents the actually-
// meaningful C2 corollary instead of a scenario that no longer arises.
//
// This form is NOT vacuous: it is evaluated, and can genuinely fail, at
// every snapshot taken after judge.go's applyJudgeStealIncorrect sets
// steal_allowed=false — a real, reachable state this check actively
// constrains. It would catch, for example, a regression that decoupled
// "set steal_allowed=false" from "perform the owed reveal" into two separate
// Apply calls (so a lockout briefly existed with revealedCount still at its
// pre-window value of 0), or one that let requireSongPlayAwaiting's guard
// slip and allowed a steal attempt at revealed==0 in the first place.
func assertStealAllowedLifecycle(t *testing.T, gs *pb.GameState) {
	t.Helper()

	s := gs.GetSong()
	if s == nil {
		return
	}
	if !s.GetStealAllowed() && revealedCount(s) == 0 {
		t.Errorf("invariant [steal_allowed lifecycle]: steal_allowed=false but revealedCount=0; a lockout can only exist after at least one reveal (C2 guarantees the first window never opens at revealed==0)")
	}
}

// assertAlternation checks law 12 in full (R1.4), now that SongOutcome
// carries active_team (Step 2 retrofit, R1.1): history entries alternate
// active team pairwise (12a), the round in progress continues that
// alternation (12b), the two teams' active-round counts are equal at
// natural exhaustion (12c), and round_no tracks history length plus
// whichever round is currently in flight (12d). 12d subsumes the earlier
// round_no-monotonicity proxy — that proxy is gone, not kept alongside
// this.
func assertAlternation(t *testing.T, pack *Pack, gs *pb.GameState) {
	t.Helper()

	assertPairwiseAlternation(t, gs)
	assertCurrentRoundCoherence(t, gs)
	assertRoundCountParity(t, pack, gs)
	assertRoundCounterIdentity(t, gs)
}

// effectivePhase now lives in state.go (Step 11 moved it out of this test
// file): view.go's redaction seam needs the exact same "what is really
// going on" resolution this package's invariant laws 12b/12d already relied
// on, and production code cannot import a _test.go file.

// assertPairwiseAlternation checks law 12a: every history entry's
// active_team is TEAM_A or TEAM_B (never TEAM_NONE), and no two consecutive
// entries share the same active team.
func assertPairwiseAlternation(t *testing.T, gs *pb.GameState) {
	t.Helper()

	history := gs.GetHistory()
	for i, h := range history {
		team := h.GetActiveTeam()
		if team != pb.Team_TEAM_A && team != pb.Team_TEAM_B {
			t.Errorf("invariant [alternation 12a]: history[%d].active_team=%s, want TEAM_A or TEAM_B", i, teamWire(team))
			continue
		}
		if i > 0 && team == history[i-1].GetActiveTeam() {
			t.Errorf("invariant [alternation 12a]: history[%d].active_team=%s repeats history[%d]'s active_team",
				i, teamWire(team), i-1)
		}
	}
}

// assertCurrentRoundCoherence checks law 12b: the round in progress (or
// about to be selected) continues the alternation implied by the last
// history entry. Constrains nothing when history is empty (any starting
// team is legal per begin_game{starting_team}).
//
// Evaluated against gs.ResumePhase, not gs.Phase, when the judge seat is
// currently vacant: PHASE_JUDGE_VACANT parks the game without touching
// active_team or history, so the parked phase — not the parking phase — is
// the one this rule actually governs.
func assertCurrentRoundCoherence(t *testing.T, gs *pb.GameState) {
	t.Helper()

	history := gs.GetHistory()
	if len(history) == 0 {
		return
	}
	last := history[len(history)-1].GetActiveTeam()
	phase := effectivePhase(gs)

	switch phase {
	case pb.Phase_PHASE_SONG_PLAY, pb.Phase_PHASE_PENDING_JUDGEMENT, pb.Phase_PHASE_ROUND_SELECT:
		// A song in flight, or a captain about to pick one, continues the
		// alternation: next_round already flipped active_team to the
		// opponent of whoever was active last.
		if want := opponentOf(last); gs.GetActiveTeam() != want {
			t.Errorf("invariant [alternation 12b]: phase=%s active_team=%s, want opponent of last history active_team %s (%s)",
				phase, teamWire(gs.GetActiveTeam()), teamWire(last), teamWire(want))
		}
	case pb.Phase_PHASE_SONG_RESULT:
		// The outcome is already appended; the flip to the opponent only
		// happens at next_round, so active_team is still last's.
		if gs.GetActiveTeam() != last {
			t.Errorf("invariant [alternation 12b]: phase=%s active_team=%s, want last history active_team %s",
				phase, teamWire(gs.GetActiveTeam()), teamWire(last))
		}
	case pb.Phase_PHASE_UNSPECIFIED, pb.Phase_PHASE_ROSTER, pb.Phase_PHASE_JUDGE_VACANT, pb.Phase_PHASE_FINISHED:
		// No constraint.
	}
}

// assertRoundCounterIdentity checks law 12d: gs.RoundNo == len(history) +
// roundInFlight for every phase EXCEPT PHASE_FINISHED, which needs its own,
// looser bound — see assertRoundCounterAtFinished below (Step 10 fix).
//
// roundInFlight is deliberately its own predicate, not law 1's inFlight
// (assertSongConservation / isSongInFlight): the plan's GameState field
// table documents round_no as "1-based; increments on each round_started",
// and round_started fires the moment a round enters PHASE_ROUND_SELECT —
// applyBeginGame sets round_no = 1 there directly, one full round-cycle
// before any song is drawn, and next_round's row 21 increments it on the
// same SONG_RESULT -> ROUND_SELECT transition. So round_no's own in-flight
// window is wider than law 1's by exactly PHASE_ROUND_SELECT: reusing law
// 1's inFlight here would make this law false-fail on every
// PHASE_ROUND_SELECT snapshot, including immediately after begin_game
// (round_no=1, history empty) — see the worked report for this fix.
func assertRoundCounterIdentity(t *testing.T, gs *pb.GameState) {
	t.Helper()

	if effectivePhase(gs) == pb.Phase_PHASE_FINISHED {
		assertRoundCounterAtFinished(t, gs)
		return
	}

	roundInFlight := 0
	switch effectivePhase(gs) {
	case pb.Phase_PHASE_ROUND_SELECT, pb.Phase_PHASE_SONG_PLAY, pb.Phase_PHASE_PENDING_JUDGEMENT:
		roundInFlight = 1
	case pb.Phase_PHASE_UNSPECIFIED, pb.Phase_PHASE_ROSTER, pb.Phase_PHASE_SONG_RESULT,
		pb.Phase_PHASE_JUDGE_VACANT, pb.Phase_PHASE_FINISHED:
		roundInFlight = 0
	}

	if want := int32(len(gs.GetHistory()) + roundInFlight); gs.GetRoundNo() != want {
		t.Errorf("invariant [alternation 12d]: round_no=%d, want len(history)(%d)+round_in_flight(%d)=%d",
			gs.GetRoundNo(), len(gs.GetHistory()), roundInFlight, want)
	}
}

// assertRoundCounterAtFinished is law 12d's PHASE_FINISHED case (Step 10
// fix; found while implementing row 26's resign path).
//
// PHASE_FINISHED is NOT reachable by a single fixed round_no formula the
// way every other phase is. Two of its three arrival paths keep the
// original exact identity (round_no == len(history)):
//   - natural exhaustion (row 21): applyNextRound only increments round_no
//     in the branch that does NOT reach FINISHED, so the round_no of the
//     just-completed final round is never advanced past it.
//   - row-26 abandonment while a song was already drawn (PHASE_SONG_PLAY or
//     PHASE_PENDING_JUDGEMENT, including behind a PHASE_JUDGE_VACANT
//     parking): finalize's R1.3 backfill appends that in-flight song to
//     history, so history catches back up to round_no exactly.
//
// But row-26 abandonment can also fire from PHASE_ROUND_SELECT — a round
// has been announced (round_started already fired, round_no already
// incremented) but no captain has drawn a song for it yet. R1.3 only
// backfills an in-flight SONG; there is no song to backfill here, so that
// announced round never gets a history entry at all, and round_no is
// permanently one ahead of len(history) — legitimately, not as a bug in
// production code. (This is exactly the class of gap the plan's "invariant
// law that false-fails" callout was watching for on this path.) So the
// FINISHED bound widens to round_no in {len(history), len(history)+1}.
func assertRoundCounterAtFinished(t *testing.T, gs *pb.GameState) {
	t.Helper()

	h := int32(len(gs.GetHistory()))
	if rn := gs.GetRoundNo(); rn != h && rn != h+1 {
		t.Errorf("invariant [alternation 12d]: round_no=%d at PHASE_FINISHED, want len(history)(%d) or len(history)+1(%d)",
			rn, h, h+1)
	}
}

// assertRoundCountParity checks law 12c: at natural exhaustion of the pack
// (every song played, not a row-26 mid-song abandonment), each team was
// active for exactly half of the rounds. Guarded on len(history) ==
// pack.TotalSongs so the abandonment path — which can finish with an odd,
// non-exhaustive history — cannot false-fail this.
func assertRoundCountParity(t *testing.T, pack *Pack, gs *pb.GameState) {
	t.Helper()

	if gs.GetPhase() != pb.Phase_PHASE_FINISHED || len(gs.GetHistory()) != pack.TotalSongs {
		return
	}

	counts := map[pb.Team]int{}
	for _, h := range gs.GetHistory() {
		counts[h.GetActiveTeam()]++
	}

	want := pack.TotalSongs / 2
	for _, team := range []pb.Team{pb.Team_TEAM_A, pb.Team_TEAM_B} {
		if counts[team] != want {
			t.Errorf("invariant [alternation 12c]: team %s was active %d times, want %d (pack total %d)",
				teamWire(team), counts[team], want, pack.TotalSongs)
		}
	}
}

// midSongFixture builds a minimal (*Pack, *pb.GameState) pair for a mid-song
// snapshot: one song already drawn from its cell (removed from
// unplayed_song_ids, that cell's played_count incremented) whose outcome
// has not yet been appended to history. This is exactly the shape the
// pre-R1.2 law 1 (unplayed + history, no in-flight term) undercounted by
// one song — see TestSongConservationMidSong below.
func midSongFixture() (*Pack, *pb.GameState) {
	pack := &Pack{TotalSongs: 2}
	gs := &pb.GameState{
		Phase: pb.Phase_PHASE_SONG_PLAY,
		Cells: []*pb.Cell{
			{LanguageName: "english", Bucket: 4, UnplayedSongIds: []string{"s2"}, PlayedCount: 1},
		},
	}
	return pack, gs
}

// TestSongConservationMidSong is a permanent regression test for R1.2: law 1
// must hold at a mid-song snapshot (phase SONG_PLAY, one song drawn from its
// cell, its outcome not yet in history) once the in-flight term is counted.
// The fixture is hand-built rather than reached through Apply, so this
// cannot silently stop exercising the fix if a later step changes how songs
// are drawn.
func TestSongConservationMidSong(t *testing.T) {
	pack, gs := midSongFixture()
	assertSongConservation(t, pack, gs)
}
