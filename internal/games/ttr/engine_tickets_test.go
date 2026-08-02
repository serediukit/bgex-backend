package ttr

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// drawTickets is applyAction specialized for ActionDrawTickets. draw_tickets
// carries no payload fields (the wire shape is "{}"), and dispatch never
// decodes one, so an empty struct is marshaled purely to exercise the same
// applyAction/e.Apply path every other action test uses.
func drawTickets(t *testing.T, e *Engine, gs *pb.GameState, userID uuid.UUID) (*pb.GameState, []engine.Event, error) {
	t.Helper()
	return applyAction(t, e, gs, userID, ActionDrawTickets, struct{}{})
}

// resolveTicketKeepDecision is applyAction specialized for
// resolve_decision{ticket_keep} (the in-game §9 keep, distinct from the
// setup keep resolveSetupTickets in engine_setup_test.go resolves).
func resolveTicketKeepDecision(t *testing.T, e *Engine, gs *pb.GameState, userID uuid.UUID, keep []int32) (*pb.GameState, []engine.Event, error) {
	t.Helper()
	return applyAction(t, e, gs, userID, ActionResolveDecision, ResolveDecisionPayload{Kind: DecisionKindTicketKeep, KeepTicketIDs: keep})
}

// countAllTickets sums every ticket id currently accounted for in gs: the
// face-down deck, any pending in-game draw offer, and every player's
// permanent hand plus (during setup) still-outstanding setup offer. It never
// counts a ticket twice as long as gs is internally consistent, which makes
// it useful for asserting that a setup discard (rules §5.7) actually removes
// tickets from the game rather than merely moving them somewhere unchecked.
func countAllTickets(gs *pb.GameState) int {
	n := len(gs.TicketDeck)
	if gs.PendingTicketDraw != nil {
		n += len(gs.PendingTicketDraw.OfferedTicketIds)
	}
	for _, p := range gs.Players {
		n += len(p.TicketIds) + len(p.SetupTicketOffer)
	}
	return n
}

func TestTicketsDrawIllegalWhenDeckEmpty(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, ids := newNormalState(m, 2)
	gs.TicketDeck = nil
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp))

	_, _, err := drawTickets(t, e, gs, ids[0])
	if !isIllegalAction(err) {
		t.Errorf("draw_tickets with an empty ticket deck: err = %v, want engine.ErrIllegalAction (rules §16.4 — illegal, not a no-op)", err)
	}
	assertInvariants(t, m, gs)
}

func TestTicketsDrawOffersFewerThanThreeWhenDeckIsShort(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 2)
	gs, ids := newNormalState(m, 2)
	gs.TicketDeck = []int32{1, 2}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp))

	ngs, _, err := drawTickets(t, e, gs, ids[0])
	if err != nil {
		t.Fatalf("draw_tickets: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)

	if ngs.Phase != pb.Phase_PHASE_AWAITING_TICKET_KEEP {
		t.Fatalf("phase = %v, want PHASE_AWAITING_TICKET_KEEP", ngs.Phase)
	}
	if ngs.PendingTicketDraw == nil || len(ngs.PendingTicketDraw.OfferedTicketIds) != 2 {
		t.Fatalf("pending_ticket_draw = %+v, want exactly 2 offered tickets", ngs.PendingTicketDraw)
	}
	if len(ngs.TicketDeck) != 0 {
		t.Errorf("ticket_deck = %v, want empty (both remaining tickets were offered)", ngs.TicketDeck)
	}
}

func TestTicketsKeepCounts(t *testing.T) {
	m := testMap(t)

	cases := []struct {
		name  string
		keep  []int32
		legal bool
	}{
		{name: "keep 0 is illegal", keep: nil, legal: false},
		{name: "keep 1 is legal", keep: []int32{1}, legal: true},
		{name: "keep all 3 is legal", keep: []int32{1, 2, 3}, legal: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(m, 3)
			gs, ids := newNormalState(m, 2)
			gs.TicketDeck = []int32{4, 5}
			gs.PendingTicketDraw = &pb.PendingTicketDraw{OfferedTicketIds: []int32{1, 2, 3}}
			gs.Phase = pb.Phase_PHASE_AWAITING_TICKET_KEEP
			gs.FaceUp = stableFaceUp()
			gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp))

			ngs, _, err := resolveTicketKeepDecision(t, e, gs, ids[0], tc.keep)
			if !tc.legal {
				if !isIllegalAction(err) {
					t.Errorf("keep %v: err = %v, want engine.ErrIllegalAction", tc.keep, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("keep %v: unexpected error: %v", tc.keep, err)
			}
			assertInvariants(t, m, ngs)

			p0 := playerBySeat(ngs, 0)
			if len(p0.TicketIds) != len(tc.keep) {
				t.Errorf("ticket_ids = %v, want %d kept", p0.TicketIds, len(tc.keep))
			}
			if ngs.PendingTicketDraw != nil {
				t.Errorf("pending_ticket_draw = %+v, want nil after resolution", ngs.PendingTicketDraw)
			}
			if ngs.Phase != pb.Phase_PHASE_NORMAL {
				t.Errorf("phase = %v, want PHASE_NORMAL (turn ended)", ngs.Phase)
			}
			if wantDeck := 2 + (3 - len(tc.keep)); len(ngs.TicketDeck) != wantDeck {
				t.Errorf("len(ticket_deck) = %d, want %d (2 untouched + %d rejected)", len(ngs.TicketDeck), wantDeck, 3-len(tc.keep))
			}
		})
	}
}

// TestTicketsKeepRejectsNotOfferedOrDuplicateIDs covers resolveTicketKeep's
// "not offered" and "duplicate id" guards (m6 in the Step 11 review): a kept
// ticket id must actually be one of the offered ones, and may not be listed
// twice.
func TestTicketsKeepRejectsNotOfferedOrDuplicateIDs(t *testing.T) {
	m := testMap(t)

	cases := []struct {
		name string
		keep []int32
	}{
		{name: "not offered", keep: []int32{999}},
		{name: "duplicate id", keep: []int32{1, 1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(m, 3)
			gs, ids := newNormalState(m, 2)
			gs.TicketDeck = []int32{4, 5}
			gs.PendingTicketDraw = &pb.PendingTicketDraw{OfferedTicketIds: []int32{1, 2, 3}}
			gs.Phase = pb.Phase_PHASE_AWAITING_TICKET_KEEP
			gs.FaceUp = stableFaceUp()
			gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp))

			if _, _, err := resolveTicketKeepDecision(t, e, gs, ids[0], tc.keep); !isIllegalAction(err) {
				t.Errorf("keep %v: err = %v, want engine.ErrIllegalAction", tc.keep, err)
			}
		})
	}
}

// TestTicketsRejectedGoToBottomOfDeck is the load-bearing test distinguishing
// an in-game §9.3 reject (returns to the deck) from a §5.7 setup discard
// (leaves the game forever): it asserts the exact slice order across two
// draw/reject cycles, and that a ticket rejected in the first cycle (id 2)
// is genuinely redrawn once the tickets ahead of it are exhausted.
func TestTicketsRejectedGoToBottomOfDeck(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 5)
	gs, ids := newNormalState(m, 2)
	gs.TicketDeck = []int32{1, 2, 3, 4, 5}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp))

	// First draw offers the top 3 (1, 2, 3); the deck becomes [4, 5].
	gs1, _, err := drawTickets(t, e, gs, ids[0])
	if err != nil {
		t.Fatalf("first draw_tickets: unexpected error: %v", err)
	}
	if got := gs1.PendingTicketDraw.OfferedTicketIds; !slices.Equal(got, []int32{1, 2, 3}) {
		t.Fatalf("first offered = %v, want [1 2 3]", got)
	}

	// Keep 1, reject 2 and 3: they append, in offered order, to the bottom.
	gs2, _, err := resolveTicketKeepDecision(t, e, gs1, ids[0], []int32{1})
	if err != nil {
		t.Fatalf("keep [1]: unexpected error: %v", err)
	}
	if want := []int32{4, 5, 2, 3}; !slices.Equal(gs2.TicketDeck, want) {
		t.Fatalf("ticket_deck after 1st reject = %v, want %v (rejects appended at the bottom, offered order preserved)", gs2.TicketDeck, want)
	}
	assertInvariants(t, m, gs2)

	// draw_tickets ended seat 0's turn; force current_seat back so this test
	// can keep draining the same deck instead of exercising turn order too.
	gs2.CurrentSeat = 0

	// Second draw takes the top 3 of [4, 5, 2, 3]: offers [4, 5, 2], leaving
	// deck [3]. Ticket 2 — rejected in the first cycle — is offered again.
	gs3, _, err := drawTickets(t, e, gs2, ids[0])
	if err != nil {
		t.Fatalf("second draw_tickets: unexpected error: %v", err)
	}
	if got := gs3.PendingTicketDraw.OfferedTicketIds; !slices.Equal(got, []int32{4, 5, 2}) {
		t.Fatalf("second offered = %v, want [4 5 2]", got)
	}
	if want := []int32{3}; !slices.Equal(gs3.TicketDeck, want) {
		t.Fatalf("ticket_deck after second draw = %v, want %v", gs3.TicketDeck, want)
	}

	// Keep 4, reject 5 and (again) 2.
	gs4, _, err := resolveTicketKeepDecision(t, e, gs3, ids[0], []int32{4})
	if err != nil {
		t.Fatalf("keep [4]: unexpected error: %v", err)
	}
	if want := []int32{3, 5, 2}; !slices.Equal(gs4.TicketDeck, want) {
		t.Fatalf("ticket_deck after 2nd reject = %v, want %v", gs4.TicketDeck, want)
	}
	assertInvariants(t, m, gs4)

	p0 := playerBySeat(gs4, 0)
	if want := []int32{1, 4}; !slices.Equal(p0.TicketIds, want) {
		t.Errorf("kept ticket_ids = %v, want %v", p0.TicketIds, want)
	}
}

// TestSetupDiscardsAreRemovedFromGame exercises the already-implemented
// Step 7 applySetupTicketsDecision (rules §5.7) rather than reimplementing
// it: it asserts that a setup discard never reappears anywhere in the game
// (not in ticket_deck, not in any player's hand) and that the total ticket
// count genuinely drops — the opposite of an in-game §9.3 reject, which
// TestTicketsRejectedGoToBottomOfDeck shows conserves every ticket.
func TestSetupDiscardsAreRemovedFromGame(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 8)
	state, ids := mustInitState(t, e, 2)
	gs := mustGameState(t, state)

	p0 := gs.Players[0]
	offer := slices.Clone(p0.SetupTicketOffer) // 4 tickets: 1 long + 3 regular
	keep := offer[:2]
	discarded := offer[2:]

	totalBefore := countAllTickets(gs)

	next, _, err := resolveSetupTickets(t, e, state, ids[0], keep)
	if err != nil {
		t.Fatalf("setup keep of 2: unexpected error: %v", err)
	}
	ngs := mustGameState(t, next)
	assertInvariants(t, m, ngs)

	for _, id := range discarded {
		if slices.Contains(ngs.TicketDeck, id) {
			t.Errorf("discarded setup ticket %d leaked into ticket_deck", id)
		}
		for _, p := range ngs.Players {
			if slices.Contains(p.TicketIds, id) {
				t.Errorf("discarded setup ticket %d leaked into seat %d's kept tickets", id, p.SeatIndex)
			}
		}
	}

	totalAfter := countAllTickets(ngs)
	if wantDrop := len(discarded); totalBefore-totalAfter != wantDrop {
		t.Errorf("total ticket count dropped by %d, want %d (the discarded setup tickets, gone for good)", totalBefore-totalAfter, wantDrop)
	}
}

func TestSetupKeepOfOneRejectedOfTwoAccepted(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 9)
	state, ids := mustInitState(t, e, 2)
	gs := mustGameState(t, state)
	p := gs.Players[0]

	if _, _, err := resolveSetupTickets(t, e, state, ids[0], p.SetupTicketOffer[:1]); !isIllegalAction(err) {
		t.Errorf("setup keep of 1: err = %v, want engine.ErrIllegalAction", err)
	}

	next, _, err := resolveSetupTickets(t, e, state, ids[0], p.SetupTicketOffer[:2])
	if err != nil {
		t.Fatalf("setup keep of 2: unexpected error: %v", err)
	}
	assertInvariants(t, m, mustGameState(t, next))
}

// TestSetupPhaseCompletesOnlyAfterEveryoneAnswers exercises the already-
// implemented Step 7 applySetupTicketsDecision: the phase only flips to
// PHASE_NORMAL (with turn_no reset to 1) once every seated player has
// answered — see engine_setup_test.go's
// TestSetupPhaseFlipsOnlyAfterEveryoneAnswers for the out-of-seat-order
// variant of this same behaviour.
func TestSetupPhaseCompletesOnlyAfterEveryoneAnswers(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 77)
	state, ids := mustInitState(t, e, 2)

	gs0 := mustGameState(t, state)
	next, _, err := resolveSetupTickets(t, e, state, ids[0], gs0.Players[0].SetupTicketOffer[:2])
	if err != nil {
		t.Fatalf("seat 0 setup keep: unexpected error: %v", err)
	}
	mid := mustGameState(t, next)
	if mid.Phase != pb.Phase_PHASE_SETUP_TICKETS {
		t.Fatalf("phase after 1/2 answers = %v, want PHASE_SETUP_TICKETS", mid.Phase)
	}

	final, _, err := resolveSetupTickets(t, e, next, ids[1], mid.Players[1].SetupTicketOffer[:2])
	if err != nil {
		t.Fatalf("seat 1 setup keep: unexpected error: %v", err)
	}
	fgs := mustGameState(t, final)
	assertInvariants(t, m, fgs)
	if fgs.Phase != pb.Phase_PHASE_NORMAL {
		t.Fatalf("phase after 2/2 answers = %v, want PHASE_NORMAL", fgs.Phase)
	}
	if fgs.TurnNo != 1 {
		t.Errorf("turn_no = %d, want 1", fgs.TurnNo)
	}
}

// TestTicketKeepDecisionIllegalOutsidePendingPhase covers the "kept tickets
// can never be discarded" guarantee: there is no action that lets a player
// revisit a ticket_keep decision once PHASE_NORMAL has resumed, because
// resolve_decision{ticket_keep} is only ever legal while
// PHASE_AWAITING_TICKET_KEEP is active.
func TestTicketKeepDecisionIllegalOutsidePendingPhase(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 6)
	gs, ids := newNormalState(m, 2)
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp))

	_, _, err := resolveTicketKeepDecision(t, e, gs, ids[0], []int32{1})
	if !errors.Is(err, engine.ErrWrongPhase) {
		t.Errorf("resolve_decision{ticket_keep} in PHASE_NORMAL: err = %v, want engine.ErrWrongPhase", err)
	}
}
