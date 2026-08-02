package ttr

import (
	"fmt"
	"slices"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// ticketDrawCount is the number of tickets offered by Action C (rules §3.1
// TICKET_DRAW_SIZE, §9.1). Fewer are offered if the ticket deck holds fewer.
const ticketDrawCount = 3

// minTicketsKeptInGame is the minimum number of tickets a player must keep
// from an in-game §9 ticket draw (rules §3.1 MIN_TICKETS_KEPT_ON_DRAW). This
// is distinct from minTicketsKeptAtSetup (2): an in-game draw only requires
// keeping at least 1.
const minTicketsKeptInGame = 1

// applyDrawTickets implements rules §9.1: draw up to ticketDrawCount tickets
// from the front of ticket_deck into a pending decision. An empty deck makes
// Action C illegal outright rather than a wasted turn (rules §16.4) — unlike
// draw_card, there is no substitute effect to fall back to.
func (e *Engine) applyDrawTickets(gs *pb.GameState, p *pb.PlayerState, ev *[]engine.Event) error {
	if len(gs.TicketDeck) == 0 {
		return fmt.Errorf("%w: the ticket deck is empty", engine.ErrIllegalAction)
	}

	n := min(ticketDrawCount, len(gs.TicketDeck))
	offered := slices.Clone(gs.TicketDeck[:n])
	gs.TicketDeck = gs.TicketDeck[n:]

	gs.PendingTicketDraw = &pb.PendingTicketDraw{OfferedTicketIds: offered}
	gs.Phase = pb.Phase_PHASE_AWAITING_TICKET_KEEP

	if ev != nil {
		*ev = append(*ev, engine.Event{
			Type: "tickets_drawn",
			Data: map[string]any{"seat": p.SeatIndex, "count": n},
		})
	}
	return nil
}

// resolveTicketKeep implements rules §9.2-§9.4 for the pending in-game ticket
// draw: keep must be a non-empty subset (no duplicates) of the offered
// tickets. Kept ticket ids append permanently to p.TicketIds; rejected ones
// return to the BOTTOM of ticket_deck (rules §9.3) — the crucial difference
// from a §5.7 setup discard, which removes tickets from the game entirely.
// The pending decision is cleared and the turn ends either way.
func (e *Engine) resolveTicketKeep(m *Map, gs *pb.GameState, p *pb.PlayerState, keep []int32, ev *[]engine.Event) error {
	pending := gs.PendingTicketDraw
	if pending == nil {
		return fmt.Errorf("%w: no ticket draw is pending", engine.ErrIllegalAction)
	}
	if len(keep) < minTicketsKeptInGame {
		return fmt.Errorf("%w: must keep at least %d ticket(s), got %d", engine.ErrIllegalAction, minTicketsKeptInGame, len(keep))
	}

	offered := make(map[int32]bool, len(pending.OfferedTicketIds))
	for _, id := range pending.OfferedTicketIds {
		offered[id] = true
	}
	kept := make(map[int32]bool, len(keep))
	for _, id := range keep {
		if !offered[id] {
			return fmt.Errorf("%w: ticket %d was not offered to you", engine.ErrIllegalAction, id)
		}
		if kept[id] {
			return fmt.Errorf("%w: duplicate ticket id %d", engine.ErrIllegalAction, id)
		}
		kept[id] = true
	}

	var rejected []int32
	for _, id := range pending.OfferedTicketIds {
		if !kept[id] {
			rejected = append(rejected, id)
		}
	}

	p.TicketIds = append(p.TicketIds, keep...)
	gs.TicketDeck = append(gs.TicketDeck, rejected...)
	gs.PendingTicketDraw = nil

	if ev != nil {
		*ev = append(*ev, engine.Event{
			Type: "ticket_keep_resolved",
			Data: map[string]any{"seat": p.SeatIndex, "kept": len(keep), "rejected": len(rejected)},
		})
	}

	return e.endTurn(m, gs, p, ev)
}
