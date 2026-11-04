package song_guess

import (
	"fmt"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// This file implements plan "Chosen Approach" transition table rows 14-20:
// judge_result resolves the pending judgement (gs.Pending) by its kind, and
// void discards it as if it never happened. Both actions are legal only in
// PHASE_PENDING_JUDGEMENT and only for the participant with is_judge.
//
// Every "correct" branch (rows 14, 16, 18) does the same thing regardless of
// kind: award pending.potential_at_claim to pending.claimant_team and finish
// the song. That symmetry is why applyJudgeResult branches on kind only for
// the "incorrect" outcome — the three kinds genuinely differ there (last
// chance opens, steal locks out, or the song simply ends at 0/0).
//
// awardPoints and the three "incorrect" helpers all funnel song-ending
// mutations through play.go's finishSong (the package's single history-
// append and score-mutation site, ruling R1.1) rather than touching
// TeamState.Score or gs.Pending/gs.Phase directly, so score conservation
// (invariant law 3) and pending coherence (law 9) each still have exactly
// one place to be right.

// requireJudge resolves the acting participant and confirms they are the
// current judge (is_judge == true). Both judge_result and void are
// judge-only (rows 14-20): a genuinely unseated caller gets
// engine.ErrNotSeated, matching every sibling guard in this package
// (requireHost, requireActiveCaptain, requireOpponentCaptain); a seated
// caller who is not the judge — a captain, or the host when they are not
// also the judge — gets engine.ErrNotYourTurn. (Step 9's brief originally
// demanded ErrNotYourTurn uniformly, including for an unseated caller; that
// was an orchestrator briefing error, corrected in Step 10 — see the plan's
// "Discovered during implementation" section.)
func requireJudge(gs *pb.GameState, a engine.Action) (*pb.Participant, error) {
	p := participantByUser(gs, a.UserID.String())
	if p == nil {
		return nil, engine.ErrNotSeated
	}
	if !p.GetIsJudge() {
		return nil, engine.ErrNotYourTurn
	}
	return p, nil
}

// requirePendingJudgement rejects unless gs is in PHASE_PENDING_JUDGEMENT.
func requirePendingJudgement(gs *pb.GameState) error {
	if gs.GetPhase() != pb.Phase_PHASE_PENDING_JUDGEMENT {
		return engine.ErrWrongPhase
	}
	return nil
}

// judgedEvent builds the judged{correct, kind, team} event common to every
// judge_result branch (rows 14-19): a verdict, never a title or the answer.
func judgedEvent(correct bool, kind pb.JudgeKind, team pb.Team) engine.Event {
	return engine.Event{
		Type: "judged",
		Data: map[string]any{
			"correct": correct,
			"kind":    judgeKindWire(kind),
			"team":    teamWire(team),
		},
	}
}

// awardPoints implements the "correct" half of rows 14, 16 and 18: credit
// points to team by finishing the current song through finishSongWithGuess
// (which also stamps guessText onto song.LastGuessText, R2 follow-up F2, so
// the result screen can show what was guessed), and report the award via
// points_awarded{team, points, total}. finishSong is the only site that
// ever mutates TeamState.Score in this package, so awardPoints deliberately
// never touches ts.Score itself — it reads the already-updated total back
// out afterward for the event payload. Events: judged, points_awarded,
// song_finished (in that order, per the plan's "Engine events" table).
func awardPoints(gs *pb.GameState, kind pb.JudgeKind, team pb.Team, points int32, guessText string) []engine.Event {
	events := []engine.Event{judgedEvent(true, kind, team)}
	finished := finishSongWithGuess(gs, team, points, false, guessText)
	total := teamState(gs, team).GetScore()
	events = append(events, engine.Event{
		Type: "points_awarded",
		Data: map[string]any{"team": teamWire(team), "points": points, "total": total},
	})
	return append(events, finished...)
}

// applyJudgeResult implements transition table rows 14-19: judge_result
// resolves gs.Pending. Every "correct" outcome is identical regardless of
// kind (award the claimant, finish the song), so only the "incorrect" path
// branches by kind.
func applyJudgeResult(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if err := requirePendingJudgement(gs); err != nil {
		return nil, err
	}
	if _, err := requireJudge(gs, a); err != nil {
		return nil, err
	}
	payload, err := decodePayload[JudgeResultPayload](a.Payload)
	if err != nil {
		return nil, err
	}

	pending := gs.GetPending()
	if payload.Correct {
		return awardPoints(gs, pending.GetKind(), pending.GetClaimantTeam(), pending.GetPotentialAtClaim(), pending.GetGuessText()), nil
	}

	switch pending.GetKind() {
	case pb.JudgeKind_JUDGE_ACTIVE_GUESS:
		return applyJudgeActiveGuessIncorrect(gs, pending), nil
	case pb.JudgeKind_JUDGE_STEAL:
		return applyJudgeStealIncorrect(gs, pending)
	case pb.JudgeKind_JUDGE_LAST_CHANCE:
		return applyJudgeLastChanceIncorrect(gs, pending), nil
	case pb.JudgeKind_JUDGE_UNSPECIFIED:
		return nil, fmt.Errorf("%w: pending judgement has no kind", engine.ErrIllegalAction)
	}
	return nil, fmt.Errorf("%w: unknown judge kind %v", engine.ErrIllegalAction, pending.GetKind())
}

// applyJudgeActiveGuessIncorrect implements row 15: the active team's wrong
// guess opens a last chance for the opponent, regardless of steal_allowed
// (Req 10 — the last chance is a separate mechanic from the steal). Events:
// judged, last_chance_opened{team, potential}.
func applyJudgeActiveGuessIncorrect(gs *pb.GameState, pending *pb.Pending) []engine.Event {
	claimant := pending.GetClaimantTeam()
	events := []engine.Event{judgedEvent(false, pending.GetKind(), claimant)}

	gs.Phase = pb.Phase_PHASE_SONG_PLAY
	gs.Pending = nil
	gs.Song.Awaiting = pb.Awaiting_AWAITING_LAST_CHANCE

	opponent := opponentOf(claimant)
	return append(events, engine.Event{
		Type: "last_chance_opened",
		Data: map[string]any{"team": teamWire(opponent), "potential": potentialOf(gs.GetSong())},
	})
}

// applyJudgeStealIncorrect implements row 17: a failed steal locks the
// opponent out of stealing for the rest of this song, then consumes
// pending.reveal_after_incorrect by performing the reveal the open_block
// that opened this window still owed — pending.owed_reveal_index (ruling
// R3.2/C1), via revealAt, never the lowest closed index and never a
// hardcoded block. The index is user-supplied (it traces back to the
// active captain's own open_block choice), so — unlike the old revealNext,
// which could treat revealAt's error as unreachable — that error must be
// propagated here rather than swallowed. Events: judged, steal_locked_out,
// block_revealed (+ song_finished if that reveal drives potential to 0).
func applyJudgeStealIncorrect(gs *pb.GameState, pending *pb.Pending) ([]engine.Event, error) {
	claimant := pending.GetClaimantTeam()
	events := []engine.Event{judgedEvent(false, pending.GetKind(), claimant)}

	gs.Song.StealAllowed = false
	events = append(events, engine.Event{
		Type: "steal_locked_out",
		Data: map[string]any{"team": teamWire(claimant)},
	})

	gs.Phase = pb.Phase_PHASE_SONG_PLAY
	gs.Pending = nil
	if pending.GetRevealAfterIncorrect() {
		return revealAt(gs, pending.GetOwedRevealIndex(), events)
	}
	// Defensive fallback: every real JUDGE_STEAL pending sets
	// reveal_after_incorrect=true (play.go's applySteal), so this branch is
	// unreached in practice. If it were ever false, restore the sub-state
	// void() would restore rather than leaving awaiting unset.
	gs.Song.Awaiting = pending.GetResumeAwaiting()
	return events, nil
}

// applyJudgeLastChanceIncorrect implements row 19: the opponent's last
// chance also failed, so the song ends at 0/0. The opponent's guess WAS
// adjudicated (just ruled incorrect), so it is still what the result screen
// should show as "the last thing that was guessed" — captured via
// finishSongWithGuess (R2 follow-up F2). Events: judged, song_finished.
func applyJudgeLastChanceIncorrect(gs *pb.GameState, pending *pb.Pending) []engine.Event {
	events := []engine.Event{judgedEvent(false, pending.GetKind(), pending.GetClaimantTeam())}
	return append(events, finishSongWithGuess(gs, pb.Team_TEAM_NONE, 0, false, pending.GetGuessText())...)
}

// applyVoid implements transition table row 20: the judge discards the
// pending decision as if it never happened, restoring exactly the pre-claim
// sub-state recorded in pending.resume_awaiting (never re-derived from
// kind — that would be a second source of truth). No points, no
// steal_allowed change, no reveal; the same actor may act again. Event:
// decision_voided{kind, team}.
func applyVoid(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if err := requirePendingJudgement(gs); err != nil {
		return nil, err
	}
	if _, err := requireJudge(gs, a); err != nil {
		return nil, err
	}

	pending := gs.GetPending()
	kind := pending.GetKind()
	team := pending.GetClaimantTeam()

	gs.Phase = pb.Phase_PHASE_SONG_PLAY
	gs.Song.Awaiting = pending.GetResumeAwaiting()
	gs.Pending = nil

	return []engine.Event{{
		Type: "decision_voided",
		Data: map[string]any{"kind": judgeKindWire(kind), "team": teamWire(team)},
	}}, nil
}
