package ttr

import (
	"fmt"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// endTriggerTrains is the trains_left threshold that arms the final round
// (rules §3.1 END_TRIGGER_TRAINS, §11): at or below this many trains left at
// the end of a turn, the final round begins.
const endTriggerTrains = 2

// endTurn resolves the §6 turn-end sequence for the player p whose action
// just resolved: clear per-turn scratch state, bump turn_no, arm-or-decrement
// the §11 end trigger, and hand off to advanceOrFinish for the shared
// "decrement-or-transition" tail.
//
// §11 ships two descriptions of the same rule that disagree with each other,
// and the prose governs (confirmed against plan.md and the published TTR
// rule): "every player, including the triggering player, takes exactly one
// more turn" after the trigger. §11's own pseudocode — arm to numPlayers,
// then unconditionally decrement in that SAME call — is off by one against
// its own prose: tracing it with 3 players (A triggers, then B, then C) ends
// the game after C, and A never gets its "one more turn" at all. The fix is
// to arm OR decrement, never both in the same call: a call that arms the
// counter (FinalTurnsLeft was < 0) does not also spend a decrement on the
// turn that just happened, so the triggering player's own extra turn is what
// finally brings the counter to zero. Traced again with the fix: A triggers
// -> counter = 3. B ends turn -> 2. C ends turn -> 1. A ends turn (their
// promised extra turn) -> 0 -> scoring. Exactly numPlayers FURTHER turns
// after the trigger, A last among them, matching the prose and plan.md's
// "including the triggering player's own further turn." See
// engine_flow_test.go's TestFlowFinalRoundLastsExactlyNumPlayersTurns.
func (e *Engine) endTurn(m *Map, gs *pb.GameState, p *pb.PlayerState, ev *[]engine.Event) error {
	gs.Draw = nil
	gs.PendingTunnel = nil
	gs.PendingTicketDraw = nil
	gs.TurnNo++

	switch {
	case gs.FinalTurnsLeft < 0 && p.TrainsLeft <= endTriggerTrains:
		// Arm only. The turn that trips the trigger does not itself count
		// against the countdown — see the "does not re-arm" guard below,
		// which is exactly this same FinalTurnsLeft < 0 check and therefore
		// also can never fire twice.
		gs.FinalTurnsLeft = int32(len(activePlayers(gs))) // #nosec G115 -- bounded by maxSeats (5)
		if ev != nil {
			*ev = append(*ev, engine.Event{
				Type: "final_round_started",
				Data: map[string]any{"turns_left": gs.FinalTurnsLeft},
			})
		}
	case gs.FinalTurnsLeft >= 0:
		gs.FinalTurnsLeft--
	}

	return e.advanceOrFinish(m, gs, ev)
}

// advanceOrFinish is the tail shared by endTurn and applyResign once
// FinalTurnsLeft has already been armed/decremented by the caller (this
// function performs no arithmetic on it itself, only a read): if the
// countdown has just reached exactly zero, transition into scoring;
// otherwise hand control to the next active seat, emitting "turn_started".
func (e *Engine) advanceOrFinish(m *Map, gs *pb.GameState, ev *[]engine.Event) error {
	if gs.FinalTurnsLeft == 0 {
		return e.finalizeGame(m, gs, ev)
	}

	gs.Phase = pb.Phase_PHASE_NORMAL
	gs.CurrentSeat = nextActiveSeat(gs, gs.CurrentSeat)
	if ev != nil {
		*ev = append(*ev, engine.Event{Type: "turn_started", Data: map[string]any{"seat": gs.CurrentSeat}})
	}
	return nil
}

// nextActiveSeat returns the next non-resigned seat clockwise (ascending
// seat index, wrapping) after seat. If seat itself is not (or is no longer)
// an active seat, it falls back to the first active seat. It returns seat
// unchanged if no active players remain, which cannot happen for an
// otherwise-valid game state (applyResign routes to finalizeGame before that
// point, rules/plan Q14).
func nextActiveSeat(gs *pb.GameState, seat int32) int32 {
	active := activePlayers(gs)
	if len(active) == 0 {
		return seat
	}
	for i, p := range active {
		if p.SeatIndex == seat {
			return active[(i+1)%len(active)].SeatIndex
		}
	}
	return active[0].SeatIndex
}

// isCurrentPlayer reports whether p occupies gs.CurrentSeat.
func isCurrentPlayer(gs *pb.GameState, p *pb.PlayerState) bool {
	cur := playerBySeat(gs, gs.CurrentSeat)
	return cur != nil && cur.UserId == p.UserId
}

// applyResign implements plan Q14: p is marked resigned; their claimed
// routes, built stations and remaining trains stay on the board exactly as
// they are (still blocking future claims/stations) and they will score 0 at
// final scoring, ranked last. Any cards still in p's hand are discarded to
// the discard pile so the §14 110-card conservation invariant keeps holding.
//
// If p currently held the turn — PHASE_NORMAL, or mid a pending decision
// (PHASE_AWAITING_SECOND_DRAW / _TUNNEL / _TICKET_KEEP) — any decision-scoped
// train cards or tickets return to their pile first, and the turn passes to
// the next active seat. Unlike endTurn, this never ARMS the §11 countdown
// against the resigning player's own trains_left (a resignation is not a
// trigger event) — it only decrements an already-armed one (FinalTurnsLeft
// >= 0), because it still consumes one turn of an in-progress final round,
// then defers to advanceOrFinish for the same "finish-or-advance" tail
// endTurn uses. If fewer than two active players remain afterward, the game
// ends immediately and is scored (plan Q14), regardless of any §11 countdown
// already in progress and regardless of whether p held the turn.
func (e *Engine) applyResign(m *Map, gs *pb.GameState, p *pb.PlayerState, ev *[]engine.Event) error {
	if p.Resigned {
		return fmt.Errorf("%w: you have already resigned", engine.ErrIllegalAction)
	}

	holdsTurn := (gs.Phase == pb.Phase_PHASE_NORMAL || isAwaitingPhase(gs.Phase)) && isCurrentPlayer(gs, p)

	if holdsTurn {
		releasePendingDecisionOnResign(gs)
	}

	// Iterate in canonicalDiscardOrder rather than ranging over p.Hand
	// directly (a Go map iterates in randomized order) — see its doc
	// comment (m1 in the Step 11 review).
	for _, c := range canonicalDiscardOrder {
		for range p.Hand[int32(c)] {
			gs.DiscardPile = append(gs.DiscardPile, int32(c))
		}
	}
	p.Hand = nil
	p.Resigned = true

	if ev != nil {
		*ev = append(*ev, engine.Event{Type: "player_resigned", Data: map[string]any{"seat": p.SeatIndex}})
	}

	if len(activePlayers(gs)) < 2 {
		return e.finalizeGame(m, gs, ev)
	}

	// A resignation shrinks the active-player count by one, but
	// FinalTurnsLeft (once armed by endTurn) was set to the active count AT
	// TRIGGER TIME and is never otherwise re-derived. An out-of-turn
	// resignation (holdsTurn false, below) used to leave it completely
	// unadjusted: with 3 players, A triggers (FinalTurnsLeft = 3), C is
	// current, B resigns out of turn -> still 3, so the round then runs
	// C->2, A->1, C->0 - C gets two more turns and A only one, breaking
	// §11's "every player takes exactly one more turn" (m2 in the scoring/
	// redaction review). Clamping to the new active count on every
	// resignation - not only an in-turn one - keeps the remaining countdown
	// consistent with how many turns are actually left to hand out,
	// regardless of whether p itself held the turn.
	if gs.FinalTurnsLeft >= 0 {
		gs.FinalTurnsLeft = min(gs.FinalTurnsLeft, int32(len(activePlayers(gs)))) // #nosec G115 -- bounded by maxSeats (5)
	}

	if holdsTurn {
		gs.TurnNo++
		if gs.FinalTurnsLeft >= 0 {
			gs.FinalTurnsLeft--
		}
		return e.advanceOrFinish(m, gs, ev)
	}
	return nil
}

// releasePendingDecisionOnResign returns any state held outside the
// resigning current-seat player's hand back to its rightful pile before that
// hand is wiped, so a resignation mid-decision loses no cards or tickets:
// a pending tunnel's committed base payment and secret reveals go to the
// discard pile (mirroring rules §8.4.5's refund-and-discard, except the
// "refund" side is moot since the hand is about to be discarded anyway), and
// a pending in-game ticket draw's offered tickets return to the bottom of
// ticket_deck exactly like a rules §9.3 reject. gs.Draw (the §7.1 mid-draw
// scratch) is simply cleared — the one card it may already have credited was
// added straight to p.Hand by applyDrawCard and is accounted for there.
func releasePendingDecisionOnResign(gs *pb.GameState) {
	if pt := gs.PendingTunnel; pt != nil {
		// Iterate in canonicalDiscardOrder rather than ranging over
		// pt.BasePayment directly — see its doc comment (m1 in the Step 11
		// review).
		for _, c := range canonicalDiscardOrder {
			for range pt.BasePayment[int32(c)] {
				gs.DiscardPile = append(gs.DiscardPile, int32(c))
			}
		}
		gs.DiscardPile = append(gs.DiscardPile, pt.Revealed...)
		gs.PendingTunnel = nil
	}
	if pd := gs.PendingTicketDraw; pd != nil {
		gs.TicketDeck = append(gs.TicketDeck, pd.OfferedTicketIds...)
		gs.PendingTicketDraw = nil
	}
	gs.Draw = nil
}

// finalizeGame transitions gs through PHASE_SCORING into PHASE_FINISHED,
// populating gs.Results (one ScoreBreakdown per player, via FinalScore —
// rules §12/§13, scoring.go) and emitting "game_over". It is the single seam
// every end-of-game path (the §11 trigger reaching zero, or an applyResign
// that drops the active player count below 2) routes through.
//
// m is required (unlike the placeholder this replaced): FinalScore needs
// the board to look up route lengths/colors and ticket endpoints. Every
// caller already has it in hand (endTurn/advanceOrFinish and applyResign
// both resolve it before reaching here), so this is a signature-compatible
// tightening, not a behavior change for callers.
func (e *Engine) finalizeGame(m *Map, gs *pb.GameState, ev *[]engine.Event) error {
	gs.Phase = pb.Phase_PHASE_SCORING

	results, err := FinalScore(m, gs)
	if err != nil {
		return fmt.Errorf("final score: %w", err)
	}
	gs.Results = results

	gs.Phase = pb.Phase_PHASE_FINISHED
	if ev != nil {
		*ev = append(*ev, engine.Event{Type: "game_over"})
	}
	return nil
}
