package ttr

import (
	"fmt"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// canDraw reports whether Action A (rules §7) is available at all: at least
// one card is reachable, either blind (draw pile, or discard pile which
// would reshuffle into a fresh draw pile) or face-up. It is side-effect-free
// and package-visible so Step 13's LegalMoves can call it directly.
func canDraw(gs *pb.GameState) bool {
	return len(gs.DrawPile) > 0 || len(gs.DiscardPile) > 0 || len(gs.FaceUp) > 0
}

// secondDrawAvailable reports whether a legal second card exists right now:
// any non-locomotive face-up card (a face-up locomotive is never selectable
// as the second card, rules §7.2), or a non-empty draw/discard pile. This is
// stricter than canDraw, which only guards the *first* card of a turn — by
// the time a second draw is pending, face-up locomotives don't count.
func secondDrawAvailable(gs *pb.GameState) bool {
	if len(gs.DrawPile) > 0 || len(gs.DiscardPile) > 0 {
		return true
	}
	for _, c := range gs.FaceUp {
		if pb.Color(c) != pb.Color_COLOR_LOCO {
			return true
		}
	}
	return false
}

// applyDrawCard implements rules §7.1-§7.4 for one card of Action A's
// two-card draw. dispatch routes every draw_card action here: once while
// gs.Phase == PHASE_NORMAL for the first card, and again while gs.Phase ==
// PHASE_AWAITING_SECOND_DRAW for the second — checkPhaseGate has already
// confirmed both are legal for gs.CurrentSeat. m is only needed for the
// endTurn call at the end of a completed turn.
func (e *Engine) applyDrawCard(m *Map, gs *pb.GameState, p *pb.PlayerState, pl DrawCardPayload, ev *[]engine.Event) error {
	isSecond := gs.Draw != nil && gs.Draw.CardsTaken > 0

	card, fromFaceUp, err := e.resolveDrawSource(gs, pl, ev)
	if err != nil {
		return err
	}
	isLoco := pb.Color(card) == pb.Color_COLOR_LOCO

	if fromFaceUp {
		e.takeFaceUp(gs, pl.Slot, ev)
	}
	addToHand(p, card)
	*ev = append(*ev, engine.Event{
		Type: "cards_drawn",
		Data: map[string]any{"seat": p.SeatIndex, "count": 1, "source": pl.Source},
	})

	// The turn ends here either because this was the mandatory second card,
	// or because a face-up locomotive costs the whole turn even as the
	// first card (§7.2) — a blind locomotive, by contrast, is a normal card
	// and the second draw still follows (§7.2 last bullet).
	if isSecond || (fromFaceUp && isLoco) {
		gs.Draw = nil
		return e.endTurn(m, gs, p, ev)
	}

	gs.Draw = &pb.DrawProgress{CardsTaken: 1, FaceUpLocoLocked: true}
	gs.Phase = pb.Phase_PHASE_AWAITING_SECOND_DRAW
	return nil
}

// resolveDrawSource validates pl's source/slot and returns the chosen card
// without yet mutating the face-up layout or the player's hand.
// gs.Draw.FaceUpLocoLocked gates the §7.2 restriction that a face-up
// locomotive is never selectable as the second card, including one that
// arrived via refill; gs.Draw is nil before the first card of the turn, so a
// nil Draw falls back to "not locked" (the first card of a turn may always
// be a face-up locomotive). This reads the same field applyDrawCard writes
// when the first card is taken, rather than re-deriving the lock from
// gs.Draw.CardsTaken independently — previously the write and the
// enforcement were two unrelated computations that happened to always agree
// (m2 in the Step 11 review): nothing actually consulted FaceUpLocoLocked,
// so a future change to one could silently desync from the other, and only
// view.go's client-facing flag would have reflected the write.
func (e *Engine) resolveDrawSource(gs *pb.GameState, pl DrawCardPayload, ev *[]engine.Event) (card int32, fromFaceUp bool, err error) {
	switch pl.Source {
	case "deck":
		c, ok := e.popDrawPile(gs, ev)
		if !ok {
			return 0, false, fmt.Errorf("%w: draw pile and discard pile are both empty", engine.ErrIllegalAction)
		}
		return c, false, nil
	case "face_up":
		if pl.Slot < 0 || pl.Slot >= faceUpSlots || pl.Slot >= len(gs.FaceUp) {
			return 0, false, fmt.Errorf("%w: face-up slot %d is not available", engine.ErrIllegalAction, pl.Slot)
		}
		c := gs.FaceUp[pl.Slot]
		locoLocked := gs.Draw != nil && gs.Draw.FaceUpLocoLocked
		if locoLocked && pb.Color(c) == pb.Color_COLOR_LOCO {
			return 0, false, fmt.Errorf("%w: a face-up locomotive is not selectable as the second card", engine.ErrIllegalAction)
		}
		return c, true, nil
	default:
		return 0, false, fmt.Errorf("%w: unknown draw source %q", engine.ErrIllegalAction, pl.Source)
	}
}

// applyEndDrawDecision resolves the resolve_decision{end_draw} escape hatch
// (rules §7.1, §7.4): it is only legal once the first card has been taken
// and no legal second card remains (secondDrawAvailable is false), in which
// case it ends the turn without a second draw.
func (e *Engine) applyEndDrawDecision(m *Map, gs *pb.GameState, p *pb.PlayerState, ev *[]engine.Event) error {
	if secondDrawAvailable(gs) {
		return fmt.Errorf("%w: a second card is still available to draw", engine.ErrIllegalAction)
	}
	gs.Draw = nil
	return e.endTurn(m, gs, p, ev)
}

// takeFaceUp removes the card at slot from gs.FaceUp, refills that same slot
// from the draw pile (reshuffling the discard pile in as needed) and
// reapplies the §7.3 locomotive flush check — the "whenever the face-up
// layout changes" trigger (rules §7.1, §7.3). Rules §3.3 models the face-up
// row as 5 fixed slots a client addresses by index, so the refill replaces
// in place at slot rather than shifting every later slot down and appending
// the new card at the end — that used to re-key the whole row on every
// take, silently breaking a client's "the slot I took was refilled" model
// (m3 in the Step 11 review). The row only actually shrinks (compacts) in
// the genuine §14 exhaustion case: no card is available to refill slot with
// at all. A final fillFaceUp call catches up any earlier shortfall the row
// was already carrying (from a prior exhaustion) now that a card is
// available again — that catch-up card, unlike slot's own refill, has no
// original index of its own to restore, so it is appended like any other
// fillFaceUp top-up.
func (e *Engine) takeFaceUp(gs *pb.GameState, slot int, ev *[]engine.Event) {
	e.refillDrawPile(gs, ev)
	if len(gs.DrawPile) > 0 {
		gs.FaceUp[slot] = gs.DrawPile[0]
		gs.DrawPile = gs.DrawPile[1:]
	} else {
		gs.FaceUp = append(gs.FaceUp[:slot], gs.FaceUp[slot+1:]...)
	}
	e.fillFaceUp(gs, ev)
	e.flushFaceUpLocos(gs, ev)
}

// addToHand increments p.Hand[card] by one, initializing the map first if
// necessary. A proto3 map field with zero entries never round-trips through
// marshal/unmarshal (it comes back nil, not an empty map), so a hand that
// was ever fully emptied would otherwise make a plain p.Hand[card]++ panic
// the next time a card is added to it.
func addToHand(p *pb.PlayerState, card int32) {
	if p.Hand == nil {
		p.Hand = make(map[int32]int32, 1)
	}
	p.Hand[card]++
}

// popDrawPile pops the top card of the draw pile, reshuffling the discard
// pile in first if the draw pile is empty (rules §7.4). ok is false only
// when both piles are empty.
func (e *Engine) popDrawPile(gs *pb.GameState, ev *[]engine.Event) (int32, bool) {
	if len(gs.DrawPile) == 0 {
		e.refillDrawPile(gs, ev)
		if len(gs.DrawPile) == 0 {
			return 0, false
		}
	}
	card := gs.DrawPile[0]
	gs.DrawPile = gs.DrawPile[1:]
	return card, true
}
