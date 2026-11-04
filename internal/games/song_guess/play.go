package song_guess

import (
	"fmt"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// This file implements plan "Chosen Approach" transition table rows 7-13 and
// 22: the reveal/steal loop, the active team's guess, the opponent's last
// chance, and the host's skip_song escape hatch. Sub-state inside
// PHASE_SONG_PLAY lives entirely in gs.Song.Awaiting.
//
// finishSong is the single history-append site in the whole package (ruling
// R1.1): all four "song ends" paths (auto-end at potential==0, the
// opponent's failed last chance, a passed last chance, and skip_song) funnel
// through it so they cannot drift apart. judge_result's award paths (Step 9)
// call it too.

// revealedCount counts the true entries of song.RevealedMask (ruling R3.1):
// the number of blocks opened so far, replacing the retired int32 Revealed
// field. A nil song has zero revealed blocks.
func revealedCount(song *pb.SongState) int32 {
	n := int32(0)
	for _, r := range song.GetRevealedMask() {
		if r {
			n++
		}
	}
	return n
}

// potentialOf returns the current potential point value of song:
// total_blocks - revealedCount. A nil song has zero potential.
func potentialOf(song *pb.SongState) int32 {
	return int32(len(song.GetBlocks())) - revealedCount(song) // #nosec G115 -- bounded by maxBlocksPerLine (64)
}

// requireSongPlayAwaiting rejects unless gs is in PHASE_SONG_PLAY with
// gs.Song.Awaiting == want. Shared by the two actions (open_block, steal)
// whose legality is pinned to one specific sub-state.
func requireSongPlayAwaiting(gs *pb.GameState, want pb.Awaiting) error {
	if gs.GetPhase() != pb.Phase_PHASE_SONG_PLAY {
		return engine.ErrWrongPhase
	}
	if gs.GetSong().GetAwaiting() != want {
		return engine.ErrWrongPhase
	}
	return nil
}

// requireOpponentCaptain resolves the acting participant and confirms they
// are the current captain of the opponent of gs.ActiveTeam — the actor
// row 9/10/12/13 name for the steal window and the last chance.
func requireOpponentCaptain(gs *pb.GameState, a engine.Action) (*pb.Participant, error) {
	p := participantByUser(gs, a.UserID.String())
	if p == nil {
		return nil, engine.ErrNotSeated
	}
	captain := captainOf(gs, opponentOf(gs.GetActiveTeam()))
	if captain == nil || captain.GetSeatIndex() != p.GetSeatIndex() {
		return nil, engine.ErrNotYourTurn
	}
	return p, nil
}

// judgeKindWire is the wire form of a pb.JudgeKind, used by guess_submitted
// (this step) and judge_result/judged (Step 9).
func judgeKindWire(k pb.JudgeKind) string {
	switch k {
	case pb.JudgeKind_JUDGE_ACTIVE_GUESS:
		return "active_guess"
	case pb.JudgeKind_JUDGE_STEAL:
		return "steal"
	case pb.JudgeKind_JUDGE_LAST_CHANCE:
		return "last_chance"
	case pb.JudgeKind_JUDGE_UNSPECIFIED:
		return "unspecified"
	}
	return "unspecified"
}

// guessSubmittedEvent builds the guess_submitted event for a freshly-set
// Pending: {team, seat, kind, has_guess} (plan "Engine events" table, as
// corrected by R2 follow-up F1). This event broadcasts to every subscriber
// including the opposing team and spectators, and an engine event cannot be
// per-viewer redacted — so it MUST NOT carry the raw guess text, only
// whether one was submitted. has_guess mirrors SongGuessPendingView.HasGuess
// (view.go): true when the claimant submitted non-empty guess text. The
// claimant's own team (and the judge) still get the actual text through the
// per-viewer state broadcast, which IS redacted (Architect ruling R2,
// pendingGuessTextVisible).
func guessSubmittedEvent(p *pb.Pending) engine.Event {
	return engine.Event{
		Type: "guess_submitted",
		Data: map[string]any{
			"team":      teamWire(p.GetClaimantTeam()),
			"seat":      p.GetClaimantSeat(),
			"kind":      judgeKindWire(p.GetKind()),
			"has_guess": p.GetGuessText() != "",
		},
	}
}

// applyOpenBlock implements transition table row 7 under ruling R3 (C1 free
// choice + C2 no zero-reveal steal window): the active captain names any
// still-closed block index, and either that block is revealed immediately
// or — from the second reveal onward — the opponent gets a steal chance at
// it first.
//
// Guard order matches every sibling handler in this package (phase, actor,
// sub-state before payload-derived checks): requireSongPlayAwaiting,
// requireActiveCaptain, then the index bounds/already-revealed/potential
// checks below. The bounds/already-revealed checks are duplicated by
// revealAt itself, but are checked here first too so a malformed index is
// rejected before the C2 branch has to reason about it.
func applyOpenBlock(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if err := requireSongPlayAwaiting(gs, pb.Awaiting_AWAITING_ACTIVE); err != nil {
		return nil, err
	}
	if _, err := requireActiveCaptain(gs, a); err != nil {
		return nil, err
	}
	payload, err := decodePayload[OpenBlockPayload](a.Payload)
	if err != nil {
		return nil, err
	}

	song := gs.GetSong()
	mask := song.GetRevealedMask()
	if payload.Index < 0 || int(payload.Index) >= len(mask) {
		return nil, fmt.Errorf("%w: block index %d out of range [0,%d)", engine.ErrIllegalAction, payload.Index, len(mask))
	}
	if mask[payload.Index] {
		return nil, fmt.Errorf("%w: block %d is already revealed", engine.ErrIllegalAction, payload.Index)
	}
	// Unreachable given law 8's auto-end (potential==0 always finishes the
	// song in the same Apply call that drives it there) — kept as a
	// defensive guard, per ruling R3.2.
	if potentialOf(song) <= 0 {
		return nil, fmt.Errorf("%w: no potential remains to open", engine.ErrIllegalAction)
	}

	// C2 (architect ruling R3, overriding the earlier Q9 first-steal-window
	// behavior): no steal window at revealed==0 — the active team always
	// gets the first opportunity uncontested, guessing cold or opening one
	// block of choice. From the second reveal onward, a steal_allowed song
	// still offers the opponent a steal chance at this specific chosen
	// block before it opens. song.StealWindowIndex records which block is
	// on offer so applySteal (a later, separate Apply call by the
	// opponent) and applyPassFromSteal both resolve to the SAME index the
	// active captain actually chose — never an arbitrary lowest-closed
	// fallback.
	if song.GetStealAllowed() && revealedCount(song) > 0 {
		song.Awaiting = pb.Awaiting_AWAITING_STEAL
		song.StealWindowIndex = payload.Index
		return []engine.Event{{
			Type: "steal_window_opened",
			Data: map[string]any{"potential": potentialOf(song)},
		}}, nil
	}
	return revealAt(gs, payload.Index, nil)
}

// revealAt implements the free-choice reveal step (ruling R3.1/C1): it opens
// exactly block index of the current song, emits block_revealed{index,
// text}, and applies the existing end-of-song logic (potential==0 auto-ends
// the song at 0/0 per Q9/invariant law 8; otherwise the active captain gets
// another decision). events is the caller's events-so-far (e.g. a
// steal_passed event); block_revealed (and, if the song ends, song_finished)
// are appended to it.
//
// index must name a real, still-closed block: out of [0,len(blocks)) or
// already-revealed is rejected with engine.ErrIllegalAction so a wrong-block
// reveal fails loudly rather than silently opening block 0. Every caller now
// passes a real, user-supplied index (applyOpenBlock's own choice, the
// steal-window's stashed SongState.StealWindowIndex on a decline, or
// Pending.OwedRevealIndex on a failed steal), so — unlike the old
// revealNext, which could treat this error as unreachable because
// lowestClosedIndex only ever named an in-range closed index —
// EVERY caller here must propagate this error rather than swallow it.
func revealAt(gs *pb.GameState, index int32, events []engine.Event) ([]engine.Event, error) {
	song := gs.GetSong()
	mask := song.GetRevealedMask()
	if index < 0 || int(index) >= len(mask) {
		return nil, fmt.Errorf("%w: block index %d out of range [0,%d)", engine.ErrIllegalAction, index, len(mask))
	}
	if mask[index] {
		return nil, fmt.Errorf("%w: block %d is already revealed", engine.ErrIllegalAction, index)
	}
	mask[index] = true

	events = append(events, engine.Event{
		Type: "block_revealed",
		Data: map[string]any{"index": index, "text": song.GetBlocks()[index]},
	})

	if potentialOf(song) == 0 {
		return append(events, finishSong(gs, pb.Team_TEAM_NONE, 0, false)...), nil
	}
	song.Awaiting = pb.Awaiting_AWAITING_ACTIVE
	return events, nil
}

// finishSong is the single append site for gs.History in this package
// (ruling R1.1). It records the song's outcome, credits awarded's
// TeamState, transitions to PHASE_SONG_RESULT, clears any pending
// judgement, and emits song_finished. active_team is read from
// gs.ActiveTeam at append time — still the round's active team, since the
// flip to the opponent only happens in next_round (round.go).
func finishSong(gs *pb.GameState, awarded pb.Team, points int32, skipped bool) []engine.Event {
	song := gs.GetSong()
	song.AwardedTeam = awarded
	song.AwardedPoints = points
	song.Skipped = skipped

	gs.History = append(gs.History, &pb.SongOutcome{
		SongId:        song.GetSongId(),
		LanguageName:  song.GetLanguageName(),
		Bucket:        song.GetBucket(),
		RevealedAtEnd: revealedCount(song),
		AwardedTeam:   awarded,
		Points:        points,
		Title:         song.GetTitle(),
		Artist:        song.GetArtist(),
		Skipped:       skipped,
		ActiveTeam:    gs.GetActiveTeam(),
	})
	if ts := teamState(gs, awarded); ts != nil {
		ts.Score += points
	}

	gs.Phase = pb.Phase_PHASE_SONG_RESULT
	gs.Pending = nil

	return []engine.Event{{
		Type: "song_finished",
		Data: map[string]any{
			"language":     song.GetLanguageName(),
			"bucket":       song.GetBucket(),
			"revealed":     revealedCount(song),
			"awarded_team": teamWire(awarded),
			"points":       points,
			"title":        song.GetTitle(),
			"artist":       song.GetArtist(),
			"skipped":      skipped,
		},
	}}
}

// finishSongWithGuess is a thin wrapper around finishSong for the two
// judge.go paths where a guess was actually adjudicated before the song
// ended — a correct verdict (awardPoints) or a failed last chance
// (applyJudgeLastChanceIncorrect) — so the result screen's
// song.LastGuessText can show what was guessed (R2 follow-up F2).
//
// guessText is stamped onto gs.GetSong().LastGuessText before delegating to
// finishSong, which is exactly "before gs.Pending is cleared" since
// finishSong clears gs.Pending itself. finishSong remains the single
// history-append and score-mutating site (ruling R1.1) — this wrapper adds
// no second append, it only sets one extra field on the live SongState
// ahead of the call.
//
// This mechanism (an explicit caller-supplied value) was chosen over
// reading gs.Pending.GetGuessText() directly inside finishSong because
// gs.Pending is not always safe to read there: applySkipSong (play.go) can
// legally fire from PHASE_PENDING_JUDGEMENT with a genuine, still-live,
// UN-adjudicated pending.GuessText sitting on gs.Pending, and
// applyJudgeStealIncorrect (judge.go) already nils gs.Pending before its
// owed reveal can cascade into finishSong. Reading gs.Pending directly
// inside finishSong would either leak skip_song's abandoned guess or, in
// the steal case, luck into emptiness for the wrong reason. Every "no
// guess" path (revealNext's auto-finish, applyPassFromLastChance,
// applySkipSong, and scoring.go's finalize on the abandonment path) keeps
// calling plain finishSong, so LastGuessText is left at its zero value —
// exactly empty, never a stale value from an earlier pending.
func finishSongWithGuess(gs *pb.GameState, awarded pb.Team, points int32, skipped bool, guessText string) []engine.Event {
	gs.GetSong().LastGuessText = guessText
	return finishSong(gs, awarded, points, skipped)
}

// applyGuess dispatches ActionGuess by gs.Song.Awaiting: AWAITING_ACTIVE is
// row 8 (the active captain's guess), AWAITING_LAST_CHANCE is row 12 (the
// opponent's single post-wrong-guess chance). guess is never legal during
// AWAITING_STEAL (that window's actions are steal/pass, rows 9-10).
func applyGuess(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if gs.GetPhase() != pb.Phase_PHASE_SONG_PLAY {
		return nil, engine.ErrWrongPhase
	}
	switch gs.GetSong().GetAwaiting() {
	case pb.Awaiting_AWAITING_ACTIVE:
		return applyGuessFromActive(gs, a)
	case pb.Awaiting_AWAITING_LAST_CHANCE:
		return applyGuessFromLastChance(gs, a)
	case pb.Awaiting_AWAITING_STEAL:
		return nil, fmt.Errorf("%w: guess is not legal during the steal window; use steal or pass", engine.ErrIllegalAction)
	case pb.Awaiting_AWAITING_UNSPECIFIED:
		return nil, engine.ErrWrongPhase
	}
	return nil, engine.ErrWrongPhase
}

// applyGuessFromActive implements transition table row 8.
func applyGuessFromActive(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	actor, err := requireActiveCaptain(gs, a)
	if err != nil {
		return nil, err
	}
	guessText, err := decodeGuessText(a)
	if err != nil {
		return nil, err
	}

	song := gs.GetSong()
	gs.Pending = &pb.Pending{
		Kind:                 pb.JudgeKind_JUDGE_ACTIVE_GUESS,
		ClaimantTeam:         gs.GetActiveTeam(),
		ClaimantSeat:         actor.GetSeatIndex(),
		GuessText:            guessText,
		PotentialAtClaim:     potentialOf(song),
		ResumeAwaiting:       pb.Awaiting_AWAITING_ACTIVE,
		RevealAfterIncorrect: false,
		OwedRevealIndex:      -1, // no owed reveal on an active-guess claim (R3.2 default)
	}
	gs.Phase = pb.Phase_PHASE_PENDING_JUDGEMENT
	return []engine.Event{guessSubmittedEvent(gs.Pending)}, nil
}

// applyGuessFromLastChance implements transition table row 12.
func applyGuessFromLastChance(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	actor, err := requireOpponentCaptain(gs, a)
	if err != nil {
		return nil, err
	}
	guessText, err := decodeGuessText(a)
	if err != nil {
		return nil, err
	}

	song := gs.GetSong()
	gs.Pending = &pb.Pending{
		Kind:                 pb.JudgeKind_JUDGE_LAST_CHANCE,
		ClaimantTeam:         opponentOf(gs.GetActiveTeam()),
		ClaimantSeat:         actor.GetSeatIndex(),
		GuessText:            guessText,
		PotentialAtClaim:     potentialOf(song),
		ResumeAwaiting:       pb.Awaiting_AWAITING_LAST_CHANCE,
		RevealAfterIncorrect: false,
		OwedRevealIndex:      -1, // no owed reveal on a last-chance claim (R3.2 default)
	}
	gs.Phase = pb.Phase_PHASE_PENDING_JUDGEMENT
	return []engine.Event{guessSubmittedEvent(gs.Pending)}, nil
}

// applySteal implements transition table row 9: the opponent captain
// attempts a steal during AWAITING_STEAL, guarded on steal_allowed.
func applySteal(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if err := requireSongPlayAwaiting(gs, pb.Awaiting_AWAITING_STEAL); err != nil {
		return nil, err
	}
	actor, err := requireOpponentCaptain(gs, a)
	if err != nil {
		return nil, err
	}
	song := gs.GetSong()
	if !song.GetStealAllowed() {
		return nil, fmt.Errorf("%w: stealing is no longer allowed for this song", engine.ErrIllegalAction)
	}
	guessText, err := decodeGuessText(a)
	if err != nil {
		return nil, err
	}

	gs.Pending = &pb.Pending{
		Kind:                 pb.JudgeKind_JUDGE_STEAL,
		ClaimantTeam:         opponentOf(gs.GetActiveTeam()),
		ClaimantSeat:         actor.GetSeatIndex(),
		GuessText:            guessText,
		PotentialAtClaim:     potentialOf(song),
		ResumeAwaiting:       pb.Awaiting_AWAITING_STEAL,
		RevealAfterIncorrect: true,
		// OwedRevealIndex is the block index open_block opened when it
		// opened this steal window (ruling R3.2), stashed on
		// SongState.StealWindowIndex since Pending does not exist yet at
		// that instant — copied here so applyJudgeStealIncorrect
		// (judge.go) can consume it via revealAt instead of an arbitrary
		// lowest-closed-index fallback.
		OwedRevealIndex: song.GetStealWindowIndex(),
	}
	gs.Phase = pb.Phase_PHASE_PENDING_JUDGEMENT
	return []engine.Event{guessSubmittedEvent(gs.Pending)}, nil
}

// applyPass dispatches ActionPass by gs.Song.Awaiting: AWAITING_STEAL is row
// 10 (decline the steal, reveal the next block), AWAITING_LAST_CHANCE is
// row 13 (decline the last chance, the song ends 0/0). pass is never legal
// during AWAITING_ACTIVE (that sub-state's actions are guess/open_next,
// rows 7-8).
func applyPass(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if gs.GetPhase() != pb.Phase_PHASE_SONG_PLAY {
		return nil, engine.ErrWrongPhase
	}
	switch gs.GetSong().GetAwaiting() {
	case pb.Awaiting_AWAITING_STEAL:
		return applyPassFromSteal(gs, a)
	case pb.Awaiting_AWAITING_LAST_CHANCE:
		return applyPassFromLastChance(gs, a)
	case pb.Awaiting_AWAITING_ACTIVE:
		return nil, fmt.Errorf("%w: pass is not legal while the active captain is deciding", engine.ErrIllegalAction)
	case pb.Awaiting_AWAITING_UNSPECIFIED:
		return nil, engine.ErrWrongPhase
	}
	return nil, engine.ErrWrongPhase
}

// applyPassFromSteal implements transition table row 10: declining the
// steal reveals the SAME block index open_block put on offer
// (song.StealWindowIndex, ruling R3.2) — never an arbitrary lowest-closed
// fallback.
func applyPassFromSteal(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if _, err := requireOpponentCaptain(gs, a); err != nil {
		return nil, err
	}
	events := []engine.Event{{
		Type: "steal_passed",
		Data: map[string]any{"team": teamWire(opponentOf(gs.GetActiveTeam()))},
	}}
	return revealAt(gs, gs.GetSong().GetStealWindowIndex(), events)
}

// applyPassFromLastChance implements transition table row 13: declining the
// last chance ends the song 0/0.
func applyPassFromLastChance(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if _, err := requireOpponentCaptain(gs, a); err != nil {
		return nil, err
	}
	return finishSong(gs, pb.Team_TEAM_NONE, 0, false), nil
}

// decodeGuessText decodes a's GuessPayload and trims/validates its Guess
// field via trimmedGuessText, shared by every action that carries an
// advisory guess string (guess, steal).
func decodeGuessText(a engine.Action) (string, error) {
	payload, err := decodePayload[GuessPayload](a.Payload)
	if err != nil {
		return "", err
	}
	return trimmedGuessText(payload.Guess)
}

// isSkippablePhase reports whether skip_song is legal in phase: PHASE_SONG_
// PLAY or PHASE_PENDING_JUDGEMENT (transition table row 22).
func isSkippablePhase(phase pb.Phase) bool {
	switch phase {
	case pb.Phase_PHASE_SONG_PLAY, pb.Phase_PHASE_PENDING_JUDGEMENT:
		return true
	case pb.Phase_PHASE_UNSPECIFIED, pb.Phase_PHASE_ROSTER, pb.Phase_PHASE_ROUND_SELECT,
		pb.Phase_PHASE_SONG_RESULT, pb.Phase_PHASE_JUDGE_VACANT, pb.Phase_PHASE_FINISHED:
		return false
	}
	return false
}

// applySkipSong implements transition table row 22: the host abandons the
// current song 0/0 with skipped=true. It is NOT an adjudication route — the
// song was already marked played (removed from its cell) at select_cell, so
// no cell bookkeeping happens here.
func applySkipSong(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if _, err := requireHost(gs, a); err != nil {
		return nil, err
	}
	if !isSkippablePhase(gs.GetPhase()) {
		return nil, engine.ErrWrongPhase
	}

	song := gs.GetSong()
	events := []engine.Event{{
		Type: "song_skipped",
		Data: map[string]any{"language": song.GetLanguageName(), "bucket": song.GetBucket()},
	}}
	return append(events, finishSong(gs, pb.Team_TEAM_NONE, 0, true)...), nil
}
