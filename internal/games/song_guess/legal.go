package song_guess

import (
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// legalActions enumerates the action names participant may legally issue
// against gs right now. The UI's action bar is driven by this list, not by
// re-deriving roster.go/round.go/play.go/judge.go/scoring.go's guards in
// TypeScript (plan "Redaction seam" / Step 11) — so it must agree with
// those handlers exactly, including the "right phase, wrong sub-state"
// distinctions the transition table draws inside a single phase (e.g.
// guess is illegal during AWAITING_STEAL even though the phase is still
// PHASE_SONG_PLAY). See TestLegalActionsAgreeWithApply (view_test.go) for
// the cross-check that keeps this honest as the state machine evolves.
//
// participant is nil for a spectator (viewerOf's classification), who may
// never act; an inactive (e.g. resigned) participant is defensively
// treated the same way, though buildView never actually calls this with
// one — it only ever passes viewerOf's ACTIVE match.
//
// The returned slice is always non-nil (an empty slice, never nil), so it
// always marshals to a JSON array rather than null.
func legalActions(gs *pb.GameState, participant *pb.Participant) []string {
	if participant == nil || !participant.GetActive() {
		return []string{}
	}

	// Row 28's guard: while parked in PHASE_JUDGE_VACANT, this is read from
	// the LITERAL gs.Phase, never effectivePhase. dispatch's own row-28
	// guard (engine.go) gates on the literal parked phase itself,
	// restricting EVERY actor — regardless of which phase was interrupted —
	// to assign_judge (host only) and resign. Resolving through
	// effectivePhase here would incorrectly resurrect the interrupted
	// phase's full action set, exactly the class of bug the plan's
	// "Discovered during implementation" PHASE_JUDGE_VACANT note warns
	// about (which is about view.go/legal.go specifically).
	if gs.GetPhase() == pb.Phase_PHASE_JUDGE_VACANT {
		return judgeVacantActions(gs, participant)
	}

	actions := phaseActions(gs, participant)
	if gs.GetPhase() != pb.Phase_PHASE_FINISHED {
		// applyResign (scoring.go) accepts any active seated participant
		// from any non-FINISHED phase — engine.Apply already rejects
		// PHASE_FINISHED outright with engine.ErrGameOver before dispatch
		// ever runs, so resign is deliberately excluded there instead.
		actions = append(actions, ActionResign)
	}
	if actions == nil {
		actions = []string{}
	}
	return actions
}

// judgeVacantActions implements row 28's exact permitted set: only the host
// may assign_judge, and resign remains available to every active
// participant (mirrors engine.go's dispatch guard and
// TestJudgeVacancyGuardPermitsOnlyAssignJudgeAndResign).
func judgeVacantActions(gs *pb.GameState, p *pb.Participant) []string {
	actions := make([]string, 0, 2)
	if p.GetSeatIndex() == gs.GetHostSeat() {
		actions = append(actions, ActionAssignJudge)
	}
	return append(actions, ActionResign)
}

// phaseActions dispatches to the per-phase legal-action set, mirroring
// engine.go's dispatch switch. PHASE_JUDGE_VACANT is handled by
// legalActions itself before this is ever reached; PHASE_UNSPECIFIED and
// PHASE_FINISHED have no actor-specific legal actions of their own (resign
// is added, or withheld, by the caller).
func phaseActions(gs *pb.GameState, p *pb.Participant) []string {
	switch gs.GetPhase() {
	case pb.Phase_PHASE_ROSTER:
		return rosterActions(gs, p)
	case pb.Phase_PHASE_ROUND_SELECT:
		return roundSelectActions(gs, p)
	case pb.Phase_PHASE_SONG_PLAY:
		return songPlayActions(gs, p)
	case pb.Phase_PHASE_PENDING_JUDGEMENT:
		return pendingJudgementActions(gs, p)
	case pb.Phase_PHASE_SONG_RESULT:
		return songResultActions(gs, p)
	case pb.Phase_PHASE_UNSPECIFIED, pb.Phase_PHASE_JUDGE_VACANT, pb.Phase_PHASE_FINISHED:
		return nil
	}
	return nil
}

// rosterActions implements transition table rows 1-5: host-only
// (rosterHostGuard, roster.go). begin_game is offered unconditionally to
// the host here — legalActions reflects WHO may attempt an action and in
// WHICH phase/sub-state, not whether the roster is currently complete
// enough for it to succeed (that finer-grained validation stays server-
// side, exactly like select_cell being offered without checking any one
// specific cell's remaining count).
func rosterActions(gs *pb.GameState, p *pb.Participant) []string {
	if p.GetSeatIndex() != gs.GetHostSeat() {
		return nil
	}
	return []string{ActionAssignTeam, ActionAssignCaptain, ActionAssignJudge, ActionSetTeamName, ActionBeginGame}
}

// roundSelectActions implements row 6: the active team's captain only
// (requireActiveCaptain, round.go).
func roundSelectActions(gs *pb.GameState, p *pb.Participant) []string {
	if !isActiveCaptainSeat(gs, p) {
		return nil
	}
	return []string{ActionSelectCell}
}

// songPlayActions implements rows 7-13 and 22 (play.go): the sub-state
// (gs.Song.Awaiting) gates open_block/guess/steal/pass exactly per
// requireSongPlayAwaiting/applyGuess/applyPass's own dispatch — guess is
// never legal during AWAITING_STEAL, pass is never legal during
// AWAITING_ACTIVE. skip_song is the host's own action, independent of the
// sub-state (isSkippablePhase already covers both SONG_PLAY and
// PENDING_JUDGEMENT).
func songPlayActions(gs *pb.GameState, p *pb.Participant) []string {
	var actions []string
	switch gs.GetSong().GetAwaiting() {
	case pb.Awaiting_AWAITING_ACTIVE:
		if isActiveCaptainSeat(gs, p) {
			// guess is always legal here — cold (potential == total_blocks)
			// or otherwise. open_block (ruling R3.5) is offered only while
			// blocksRemainClosed reports at least one still-closed block,
			// checked directly against song.RevealedMask rather than a
			// "revealed < total" count comparison, so this can never diverge
			// from what blocksView (view.go) actually renders as open. In
			// practice AWAITING_ACTIVE is never reached with zero closed
			// blocks — law 8 auto-ends the song the instant potential hits 0,
			// in the same Apply call that drove it there (play.go's
			// revealAt) — so this is a defensive guard, exactly like
			// applyOpenBlock's own potentialOf(song) <= 0 check. Which
			// indices are actually open is NOT reflected here: the client
			// derives that from song.blocks[]'s own per-entry `revealed`
			// (Req 25), so no new view field or legal-action-set encoding of
			// it is needed.
			actions = append(actions, ActionGuess)
			if blocksRemainClosed(gs.GetSong()) {
				actions = append(actions, ActionOpenBlock)
			}
		}
	case pb.Awaiting_AWAITING_STEAL:
		if isOpponentCaptainSeat(gs, p) {
			actions = append(actions, ActionSteal, ActionPass)
		}
	case pb.Awaiting_AWAITING_LAST_CHANCE:
		if isOpponentCaptainSeat(gs, p) {
			actions = append(actions, ActionGuess, ActionPass)
		}
	case pb.Awaiting_AWAITING_UNSPECIFIED:
		// Not a reachable sub-state in practice (select_cell always sets
		// AWAITING_ACTIVE immediately); no actor-specific action either way.
	}
	if p.GetSeatIndex() == gs.GetHostSeat() {
		actions = append(actions, ActionSkipSong)
	}
	return actions
}

// pendingJudgementActions implements rows 14-20 (judge_result/void,
// judge-only, judge.go) and row 22's PENDING_JUDGEMENT half of skip_song
// (host-only, play.go's isSkippablePhase).
func pendingJudgementActions(gs *pb.GameState, p *pb.Participant) []string {
	var actions []string
	if p.GetIsJudge() {
		actions = append(actions, ActionJudgeResult, ActionVoid)
	}
	if p.GetSeatIndex() == gs.GetHostSeat() {
		actions = append(actions, ActionSkipSong)
	}
	return actions
}

// songResultActions implements row 21: the captain about to become active,
// OR the host (round.go's applyNextRound deliberately widens the actor set
// so a silent captain can never strand the game).
func songResultActions(gs *pb.GameState, p *pb.Participant) []string {
	if isOpponentCaptainSeat(gs, p) || p.GetSeatIndex() == gs.GetHostSeat() {
		return []string{ActionNextRound}
	}
	return nil
}

// blocksRemainClosed reports whether song has at least one block whose
// RevealedMask entry is still false (ruling R3.5). Reading the mask
// directly, rather than comparing revealedCount(song) against
// len(song.Blocks), keeps this in the same "the mask is the only source of
// truth for per-index openness" discipline ruling R3.1 established for
// view.go's blocksView — a count comparison would give the identical
// boolean today, but only because nothing yet lets the two diverge; reading
// the mask means they never can.
func blocksRemainClosed(song *pb.SongState) bool {
	for _, revealed := range song.GetRevealedMask() {
		if !revealed {
			return true
		}
	}
	return false
}

// isActiveCaptainSeat reports whether p occupies the current active team's
// captaincy (rows 6-8's actor guard, requireActiveCaptain).
func isActiveCaptainSeat(gs *pb.GameState, p *pb.Participant) bool {
	c := captainOf(gs, gs.GetActiveTeam())
	return c != nil && c.GetSeatIndex() == p.GetSeatIndex()
}

// isOpponentCaptainSeat reports whether p occupies the captaincy of
// opponentOf(gs.ActiveTeam) — rows 9-13's actor guard
// (requireOpponentCaptain) during PHASE_SONG_PLAY, and the SAME team
// (the one about to become active) for row 21's next_round during
// PHASE_SONG_RESULT: opponentOf(active_team) already is "the team about to
// become active" once a song has ended (the flip happens at next_round
// itself), so this one helper serves both call sites.
func isOpponentCaptainSeat(gs *pb.GameState, p *pb.Participant) bool {
	c := captainOf(gs, opponentOf(gs.GetActiveTeam()))
	return c != nil && c.GetSeatIndex() == p.GetSeatIndex()
}
