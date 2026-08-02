package ttr

import (
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	"github.com/serediukit/bgex-backend/internal/games/ttr/mapdata"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// claimRoute is applyAction specialized for ActionClaimRoute.
func claimRoute(t *testing.T, e *Engine, gs *pb.GameState, userID uuid.UUID, routeID int32, payment map[string]int) (*pb.GameState, []engine.Event, error) {
	t.Helper()
	return applyAction(t, e, gs, userID, ActionClaimRoute, ClaimRoutePayload{RouteID: routeID, Payment: payment})
}

// resolveTunnelDecision is applyAction specialized for
// resolve_decision{tunnel}.
func resolveTunnelDecision(t *testing.T, e *Engine, gs *pb.GameState, userID uuid.UUID, accept bool, extra map[string]int) (*pb.GameState, []engine.Event, error) {
	t.Helper()
	return applyAction(t, e, gs, userID, ActionResolveDecision, ResolveDecisionPayload{Kind: DecisionKindTunnel, Accept: &accept, ExtraPayment: extra})
}

// handFromColors converts a Color -> count map into the proto wire shape
// (Color(int32) -> count) PlayerState.Hand uses.
func handFromColors(h map[Color]int) map[int32]int32 {
	out := make(map[int32]int32, len(h))
	for c, n := range h {
		out[int32(c)] = int32(n) // #nosec G115 -- n is a small test fixture count
	}
	return out
}

// handColorTotal sums every count in a Color -> count map.
func handColorTotal(h map[Color]int) int {
	n := 0
	for _, v := range h {
		n += v
	}
	return n
}

// stableFaceUp is a fixed, loco-free 5-card face-up row for claim/tunnel
// fixtures that never exercise draw_card: assertInvariants' face_up-shape
// check (rules §14) requires exactly faceUpSlots cards whenever draw_pile or
// discard_pile is non-empty, so every such fixture below sets FaceUp to this
// and folds its length into the filler math.
func stableFaceUp() []int32 {
	return colorInts(ColorPurple, ColorBlue, ColorOrange, ColorWhite, ColorGreen)
}

// TestClaimRoutePaymentLegality covers rules §15 rows 1-4: colour/gray
// substitution legality for non-tunnel, non-ferry routes (rules §8.2).
func TestClaimRoutePaymentLegality(t *testing.T) {
	m := testMap(t)

	cases := []struct {
		name    string
		routeID int32 // see testMap's doc comment for shapes
		hand    map[Color]int
		payment map[string]int
		legal   bool
	}{
		{
			name:    "colored route: exact colour plus locomotives is legal",
			routeID: 11, // Blue, length 3
			hand:    map[Color]int{ColorBlue: 1, ColorLoco: 2},
			payment: map[string]int{"Blue": 1, "Locomotive": 2},
			legal:   true,
		},
		{
			name:    "gray route: single colour plus one locomotive is legal",
			routeID: 12, // Gray, length 2
			hand:    map[Color]int{ColorYellow: 1, ColorLoco: 1},
			payment: map[string]int{"Yellow": 1, "Locomotive": 1},
			legal:   true,
		},
		{
			name:    "gray route: two different non-locomotive colours is illegal",
			routeID: 12, // Gray, length 2
			hand:    map[Color]int{ColorYellow: 1, ColorRed: 1},
			payment: map[string]int{"Yellow": 1, "Red": 1},
			legal:   false,
		},
		{
			name:    "colored route: wrong colour is illegal",
			routeID: 1, // Red, length 2
			hand:    map[Color]int{ColorBlue: 2},
			payment: map[string]int{"Blue": 2},
			legal:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(m, 1)
			gs, ids := newNormalState(m, 2)
			gs.Players[0].Hand = handFromColors(tc.hand)
			gs.FaceUp = stableFaceUp()
			gs.DiscardPile = fillerCards(TotalTrainCards - handColorTotal(tc.hand) - len(gs.FaceUp))

			ngs, _, err := claimRoute(t, e, gs, ids[0], tc.routeID, tc.payment)
			if !tc.legal {
				if !isIllegalAction(err) {
					t.Errorf("claim: err = %v, want engine.ErrIllegalAction", err)
				}
				assertInvariants(t, m, gs)
				return
			}
			if err != nil {
				t.Fatalf("claim: unexpected error: %v", err)
			}
			assertInvariants(t, m, ngs)
			if _, owned := ngs.RouteOwner[tc.routeID]; !owned {
				t.Errorf("route_owner[%d] missing after a legal claim", tc.routeID)
			}
		})
	}
}

// TestClaimRouteFerry covers rules §15 row "6-length ferry with 2 loco
// symbols": the mandatory locomotive floor plus single-colour remainder
// (rules §8.3), including the all-locomotive edge case.
func TestClaimRouteFerry(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[6] // Gray, length 6, ferry (2 locomotives)

	cases := []struct {
		name    string
		hand    map[Color]int
		payment map[string]int
		legal   bool
	}{
		{
			name:    "2 locomotives + 4 same colour is legal",
			hand:    map[Color]int{ColorLoco: 2, ColorBlue: 4},
			payment: map[string]int{"Locomotive": 2, "Blue": 4},
			legal:   true,
		},
		{
			name:    "1 locomotive + 5 same colour is illegal (below the mandatory 2 locomotives)",
			hand:    map[Color]int{ColorLoco: 1, ColorBlue: 5},
			payment: map[string]int{"Locomotive": 1, "Blue": 5},
			legal:   false,
		},
		{
			name:    "6 locomotives is legal",
			hand:    map[Color]int{ColorLoco: 6},
			payment: map[string]int{"Locomotive": 6},
			legal:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(m, 4)
			gs, ids := newNormalState(m, 2)
			gs.Players[0].Hand = handFromColors(tc.hand)
			gs.FaceUp = stableFaceUp()
			gs.DiscardPile = fillerCards(TotalTrainCards - handColorTotal(tc.hand) - len(gs.FaceUp))

			ngs, _, err := claimRoute(t, e, gs, ids[0], r.ID, tc.payment)
			if !tc.legal {
				if !isIllegalAction(err) {
					t.Errorf("ferry claim: err = %v, want engine.ErrIllegalAction", err)
				}
				assertInvariants(t, m, gs)
				return
			}
			if err != nil {
				t.Fatalf("ferry claim: unexpected error: %v", err)
			}
			assertInvariants(t, m, ngs)
		})
	}
}

// TestClaimRouteRejectsUnknownRoute covers claimability's unknown-route guard
// (rules §8.1): a route id absent from the map is illegal, not a panic or a
// silent no-op (m6 in the Step 11 review).
func TestClaimRouteRejectsUnknownRoute(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 99)
	gs, ids := newNormalState(m, 2)
	gs.Players[0].Hand = map[int32]int32{int32(ColorRed): 2}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 2 - len(gs.FaceUp))

	if _, _, err := claimRoute(t, e, gs, ids[0], 9999, map[string]int{"Red": 2}); !isIllegalAction(err) {
		t.Errorf("claim an unknown route id: err = %v, want engine.ErrIllegalAction", err)
	}
}

// TestClaimRouteInsufficientTrains covers rules §15 row "TrainsLeft <
// Length": claimability rejects the route before payment is even inspected.
// The pre-claim state deliberately sets trains_left out of sync with any
// claimed-route bookkeeping (the whole point of the test is to isolate this
// guard clause), so only card conservation is asserted rather than the full
// assertInvariants — the trains_left+claimed_length==45 invariant does not
// apply to this synthetic fixture.
func TestClaimRouteInsufficientTrains(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 2)
	gs, ids := newNormalState(m, 2)

	r := m.RouteByID[1] // Red, length 2
	gs.Players[0].TrainsLeft = int32(r.Length - 1)
	gs.Players[0].Hand = map[int32]int32{int32(ColorRed): int32(r.Length)}
	gs.DiscardPile = fillerCards(TotalTrainCards - r.Length)

	_, _, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{"Red": r.Length})
	if !isIllegalAction(err) {
		t.Errorf("claim with insufficient trains: err = %v, want engine.ErrIllegalAction", err)
	}
	assertTrainCardConservation(t, gs)
}

// TestClaimRouteOwnedClosedAndDoubleRouteTwoPlayers covers rules §15 rows
// "claim an owned/closed route" and "2-player game, one half of a double
// route claimed": claiming route 9 (paired with 10) in a 2-player game
// closes 10 permanently, re-claiming 9 is illegal, and claiming the now-
// closed 10 is illegal too.
func TestClaimRouteOwnedClosedAndDoubleRouteTwoPlayers(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 3)
	gs, ids := newNormalState(m, 2)

	r9 := m.RouteByID[9] // Purple, length 3, paired with 10
	gs.Players[0].Hand = map[int32]int32{int32(ColorPurple): int32(r9.Length)}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - r9.Length - len(gs.FaceUp))

	ngs, _, err := claimRoute(t, e, gs, ids[0], r9.ID, map[string]int{"Purple": r9.Length})
	if err != nil {
		t.Fatalf("claim route %d: unexpected error: %v", r9.ID, err)
	}
	assertInvariants(t, m, ngs)
	if !slices.Contains(ngs.ClosedRoutes, int32(10)) {
		t.Fatalf("closed_routes = %v, want to contain 10 (2p sibling closure)", ngs.ClosedRoutes)
	}

	// Re-claiming the now-owned route 9 is illegal (turn belongs to seat 1
	// after seat 0's claim ended the turn, so this is rejected on ownership,
	// not turn order).
	if _, _, err := claimRoute(t, e, ngs, ids[1], r9.ID, map[string]int{"Purple": r9.Length}); !isIllegalAction(err) {
		t.Errorf("re-claim owned route: err = %v, want engine.ErrIllegalAction", err)
	}

	// Claiming the now-closed sibling route 10 is illegal.
	if _, _, err := claimRoute(t, e, ngs, ids[1], 10, map[string]int{"Orange": 3}); !isIllegalAction(err) {
		t.Errorf("claim closed sibling route: err = %v, want engine.ErrIllegalAction", err)
	}
}

// TestClaimRouteSiblingClosureIgnoresResignations is an M1 regression: the
// double-route sibling closure rule (rules §8.5) is keyed on the game's
// *seated* player count ("2-3 player games"), fixed at setup — not on the
// live non-resigned count, which a resignation can shrink mid-game.
// finishClaim used to key closure on len(activePlayers(gs)) <= 3, so a
// resignation in a 4-5 player game could turn on sibling closure mid-game,
// removing legal moves from survivors and even flagging an already-claimed
// route as "closed" once its sibling was claimed by someone else. This
// reproduces the reviewer's probe exactly: a 4-player Europe game, seat 0
// claims route 1 (no closure, correctly), seat 3 resigns (3 active players
// remain), then seat 1 claims route 2 — closed_routes must still be empty,
// since the game itself seats 4 players regardless of who has resigned.
func TestClaimRouteSiblingClosureIgnoresResignations(t *testing.T) {
	mEurope, err := ParseMap(mapdata.EuropeV1)
	if err != nil {
		t.Fatalf("parse EuropeV1: %v", err)
	}
	e := newTestEngine(mEurope, 6)
	gs, ids := newNormalState(mEurope, 4)

	r1 := mEurope.RouteByID[1] // Edinburgh-London, Orange, length 4, pair 2
	r2 := mEurope.RouteByID[2] // Edinburgh-London, Black, length 4, pair 1

	gs.Players[0].Hand = map[int32]int32{int32(ColorOrange): int32(r1.Length)}
	gs.Players[1].Hand = map[int32]int32{int32(ColorBlack): int32(r2.Length)}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - r1.Length - r2.Length - len(gs.FaceUp))

	ngs, _, err := claimRoute(t, e, gs, ids[0], r1.ID, map[string]int{"Orange": r1.Length})
	if err != nil {
		t.Fatalf("seat 0 claims route %d: unexpected error: %v", r1.ID, err)
	}
	if len(ngs.ClosedRoutes) != 0 {
		t.Fatalf("closed_routes = %v after seat 0's claim, want empty (4 players)", ngs.ClosedRoutes)
	}

	resigned, _, err := applyAction(t, e, ngs, ids[3], ActionResign, map[string]any{})
	if err != nil {
		t.Fatalf("seat 3 resigns: unexpected error: %v", err)
	}
	if n := len(activePlayers(resigned)); n != 3 {
		t.Fatalf("test setup: %d active players after seat 3 resigns, want 3", n)
	}

	final, _, err := claimRoute(t, e, resigned, ids[1], r2.ID, map[string]int{"Black": r2.Length})
	if err != nil {
		t.Fatalf("seat 1 claims sibling route %d: unexpected error: %v", r2.ID, err)
	}
	if len(final.ClosedRoutes) != 0 {
		t.Errorf("closed_routes = %v after a resignation shrank active players to 3, want empty — the game still seats %d players, and route %d is already validly owned by seat 0",
			final.ClosedRoutes, len(final.Players), r1.ID)
	}
	assertInvariants(t, mEurope, final)
}

// TestClaimRouteDoubleRouteFourPlayers covers rules §15 row "4-player game,
// one half claimed" together with the never-own-both-halves rule (rules
// §8.1, §8.5): unlike the 2-3p case, the sibling is not auto-closed at 4
// players, so it stays claimable by a *different* player, but the original
// claimant may still never own it. testMap() caps at 3 players (rules
// players.max), so this uses the real Europe map instead, whose route pair
// (1, 2) is Edinburgh-London Orange/Black, both length 4.
func TestClaimRouteDoubleRouteFourPlayers(t *testing.T) {
	mEurope, err := ParseMap(mapdata.EuropeV1)
	if err != nil {
		t.Fatalf("parse EuropeV1: %v", err)
	}
	e := newTestEngine(mEurope, 5)
	gs, ids := newNormalState(mEurope, 4)

	r1 := mEurope.RouteByID[1] // Edinburgh-London, Orange, length 4, pair 2
	r2 := mEurope.RouteByID[2] // Edinburgh-London, Black, length 4, pair 1

	gs.Players[0].Hand = map[int32]int32{int32(ColorOrange): int32(r1.Length)}
	// Seat 1's hand for the later sibling claim is dealt up front, not
	// injected after seat 0's claim, so train-card conservation (rules §14)
	// holds throughout rather than only after the fact.
	gs.Players[1].Hand = map[int32]int32{int32(ColorBlack): int32(r2.Length)}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - r1.Length - r2.Length - len(gs.FaceUp))

	ngs, _, err := claimRoute(t, e, gs, ids[0], r1.ID, map[string]int{"Orange": r1.Length})
	if err != nil {
		t.Fatalf("seat 0 claims route %d: unexpected error: %v", r1.ID, err)
	}
	assertInvariants(t, mEurope, ngs)
	if slices.Contains(ngs.ClosedRoutes, r2.ID) {
		t.Errorf("closed_routes = %v, must not contain %d at 4 players", ngs.ClosedRoutes, r2.ID)
	}

	// Case: the same player can never own both halves, even though the
	// sibling is not closed (rules §8.1, any player count). current_seat is
	// forced back to seat 0 to isolate this check from turn order.
	ngs.CurrentSeat = 0
	if _, _, err := claimRoute(t, e, ngs, ids[0], r2.ID, map[string]int{"Black": r2.Length}); !isIllegalAction(err) {
		t.Errorf("seat 0 claims its own route's sibling: err = %v, want engine.ErrIllegalAction", err)
	}

	// Case: a *different* player can claim the sibling, since it was never
	// closed at 4 players.
	ngs.CurrentSeat = 1
	final, _, err := claimRoute(t, e, ngs, ids[1], r2.ID, map[string]int{"Black": r2.Length})
	if err != nil {
		t.Fatalf("seat 1 claims the sibling route %d: unexpected error: %v", r2.ID, err)
	}
	assertInvariants(t, mEurope, final)
	if owner, ok := final.RouteOwner[r2.ID]; !ok || owner != 1 {
		t.Errorf("route_owner[%d] = %v, want seat 1", r2.ID, final.RouteOwner[r2.ID])
	}
}

// TestClaimRouteScoring covers rules §15 row "route scoring" / rules §12's
// full table: lengths 1/2/3/4/6/8 award 1/2/4/7/15/21 points.
func TestClaimRouteScoring(t *testing.T) {
	m := testMap(t)

	cases := []struct {
		routeID    int32
		colorName  string
		wantPoints int
	}{
		{routeID: 4, colorName: "Green", wantPoints: 1},       // length 1
		{routeID: 1, colorName: "Red", wantPoints: 2},         // length 2
		{routeID: 7, colorName: "Black", wantPoints: 4},       // length 3
		{routeID: 15, colorName: "White", wantPoints: 7},      // length 4
		{routeID: 6, colorName: "Locomotive", wantPoints: 15}, // length 6, ferry
		{routeID: 14, colorName: "Black", wantPoints: 21},     // length 8
	}

	for _, tc := range cases {
		t.Run(tc.colorName, func(t *testing.T) {
			r := m.RouteByID[tc.routeID]
			e := newTestEngine(m, 9)
			gs, ids := newNormalState(m, 2)

			c, ok := ParseColor(tc.colorName)
			if !ok {
				t.Fatalf("test setup: unknown color %q", tc.colorName)
			}
			gs.Players[0].Hand = map[int32]int32{int32(c): int32(r.Length)}
			gs.FaceUp = stableFaceUp()
			gs.DiscardPile = fillerCards(TotalTrainCards - r.Length - len(gs.FaceUp))

			ngs, _, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{tc.colorName: r.Length})
			if err != nil {
				t.Fatalf("claim route %d: unexpected error: %v", r.ID, err)
			}
			assertInvariants(t, m, ngs)

			if got := int(playerBySeat(ngs, 0).RouteScore); got != tc.wantPoints {
				t.Errorf("route %d (length %d) scored %d points, want %d", r.ID, r.Length, got, tc.wantPoints)
			}
		})
	}
}

// TestClaimRouteTunnelColorReveal covers rules §15 row "Tunnel: pay 2 red,
// reveal red/orange/white": the revealed card matching the declared payment
// colour drives a surcharge payable in that colour or a locomotive.
func TestClaimRouteTunnelColorReveal(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[13] // Gray, length 2, tunnel
	e := newTestEngine(m, 11)
	gs, ids := newNormalState(m, 2)
	gs.Players[0].Hand = map[int32]int32{int32(ColorRed): 3} // 2 for the base payment, 1 spare for the surcharge
	gs.DrawPile = colorInts(ColorRed, ColorOrange, ColorWhite)
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 3 - len(gs.DrawPile) - len(gs.FaceUp))

	pending, _, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{"Red": 2})
	if err != nil {
		t.Fatalf("begin tunnel claim: unexpected error: %v", err)
	}
	assertInvariants(t, m, pending)
	if pending.Phase != pb.Phase_PHASE_AWAITING_TUNNEL {
		t.Fatalf("phase = %v, want PHASE_AWAITING_TUNNEL", pending.Phase)
	}
	pt := pending.PendingTunnel
	if pt == nil || pt.Surcharge != 1 {
		t.Fatalf("pending_tunnel = %+v, want surcharge 1", pt)
	}
	if Color(pt.PaymentColor) != ColorRed {
		t.Errorf("payment_color = %v, want Red", Color(pt.PaymentColor))
	}

	accepted, _, err := resolveTunnelDecision(t, e, pending, ids[0], true, map[string]int{"Red": 1})
	if err != nil {
		t.Fatalf("accept surcharge with red: unexpected error: %v", err)
	}
	assertInvariants(t, m, accepted)
	if _, owned := accepted.RouteOwner[r.ID]; !owned {
		t.Error("route not owned after accepting the tunnel surcharge")
	}
}

// TestClaimRouteTunnelLocomotiveReveal covers rules §15 row "Tunnel: pay 2
// green, reveal locomotive": a revealed locomotive always matches,
// regardless of the declared payment colour.
func TestClaimRouteTunnelLocomotiveReveal(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[13] // Gray, length 2, tunnel
	e := newTestEngine(m, 12)
	gs, ids := newNormalState(m, 2)
	gs.Players[0].Hand = map[int32]int32{int32(ColorGreen): 3} // 2 for the base payment, 1 spare for the surcharge
	gs.DrawPile = colorInts(ColorLoco, ColorBlue, ColorBlack)
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 3 - len(gs.DrawPile) - len(gs.FaceUp))

	pending, _, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{"Green": 2})
	if err != nil {
		t.Fatalf("begin tunnel claim: unexpected error: %v", err)
	}
	assertInvariants(t, m, pending)
	if pending.PendingTunnel == nil || pending.PendingTunnel.Surcharge != 1 {
		t.Fatalf("pending_tunnel = %+v, want surcharge 1", pending.PendingTunnel)
	}

	accepted, _, err := resolveTunnelDecision(t, e, pending, ids[0], true, map[string]int{"Green": 1})
	if err != nil {
		t.Fatalf("accept surcharge with green: unexpected error: %v", err)
	}
	assertInvariants(t, m, accepted)
	if _, owned := accepted.RouteOwner[r.ID]; !owned {
		t.Error("route not owned after accepting the tunnel surcharge")
	}
}

// tunnelAllLocoPending sets up the shared pending-tunnel fixture for
// TestClaimRouteTunnelWrongSurchargeColourIsRejected and
// TestClaimRouteTunnelAllLocomotivePaymentAcceptsLocomotiveExtra: an
// all-locomotive base payment, so the declared payment colour is itself
// ColorLoco and only a locomotive can satisfy the surcharge (rules §16.1).
func tunnelAllLocoPending(t *testing.T) (m *Map, e *Engine, r *Route, pending *pb.GameState, ids []uuid.UUID) {
	t.Helper()
	m = testMap(t)
	r = m.RouteByID[13] // Gray, length 2, tunnel
	e = newTestEngine(m, 13)
	gs, ids := newNormalState(m, 2)
	gs.Players[0].Hand = map[int32]int32{int32(ColorLoco): 3, int32(ColorGreen): 1}
	gs.DrawPile = colorInts(ColorGreen, ColorBlue, ColorLoco)
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 4 - len(gs.DrawPile) - len(gs.FaceUp))

	pending, _, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{"Locomotive": 2})
	if err != nil {
		t.Fatalf("begin tunnel claim: unexpected error: %v", err)
	}
	assertInvariants(t, m, pending)
	pt := pending.PendingTunnel
	if pt == nil || pt.Surcharge != 1 {
		t.Fatalf("pending_tunnel = %+v, want surcharge 1", pt)
	}
	if Color(pt.PaymentColor) != ColorLoco {
		t.Fatalf("payment_color = %v, want Locomotive (all-locomotive base payment)", Color(pt.PaymentColor))
	}
	return m, e, r, pending, ids
}

// TestClaimRouteTunnelWrongSurchargeColourIsRejected is an M2 regression
// covering rules §15 row "Tunnel: pay 2 locos, reveal green/blue/loco": a
// green extra_payment cannot satisfy an all-locomotive surcharge, but that
// is a malformed/wrong request, not the genuine §8.4.5 "cannot pay" case —
// the player never even offered a legal colour. This used to silently fall
// back to the refund path (turn over, route unclaimed, success response),
// exactly the bug the M2 finding describes: a client typo or arithmetic slip
// must surface as an error instead, leaving the tunnel decision pending so
// the client can retry with the correct colour.
func TestClaimRouteTunnelWrongSurchargeColourIsRejected(t *testing.T) {
	_, e, r, pending, ids := tunnelAllLocoPending(t)

	if _, _, err := resolveTunnelDecision(t, e, pending, ids[0], true, map[string]int{"Green": 1}); !isIllegalAction(err) {
		t.Errorf("accept with a colour (Green) that is neither the payment colour nor Locomotive: err = %v, want engine.ErrIllegalAction", err)
	}

	// Apply discards state on error, so the tunnel decision must still be
	// pending: a corrected retry with the right colour succeeds.
	accepted, _, err := resolveTunnelDecision(t, e, pending, ids[0], true, map[string]int{"Locomotive": 1})
	if err != nil {
		t.Fatalf("retry with the correct colour after the rejected malformed attempt: unexpected error: %v", err)
	}
	if _, owned := accepted.RouteOwner[r.ID]; !owned {
		t.Error("route not owned after a corrected retry")
	}
}

// redSurchargePendingWithSpare sets up a pending §8.4 tunnel decision on
// route 13 (Gray, length 2, tunnel) with a Red base payment (declared
// payment colour Red) and surcharge 1 (draw pile Red/Orange/White — the
// revealed Red always matches), giving the player spareRed additional Red
// cards beyond the 2 already spent on the base payment. The M2 regressions
// below vary spareRed and the extra_payment sent to isolate
// validateTunnelSurcharge's three distinct failure modes (wrong colour,
// wrong total, hand-insufficient) from one another — a naive test using a
// single fixture for all three risks one failure mode masking another (e.g.
// an over-total request that also happens to exceed the hand).
func redSurchargePendingWithSpare(t *testing.T, spareRed int) (m *Map, e *Engine, r *Route, pending *pb.GameState, ids []uuid.UUID) {
	t.Helper()
	m = testMap(t)
	r = m.RouteByID[13] // Gray, length 2, tunnel
	e = newTestEngine(m, 14)
	gs, ids := newNormalState(m, 2)
	handRed := 2 + spareRed
	gs.Players[0].Hand = map[int32]int32{int32(ColorRed): int32(handRed)} // #nosec G115 -- small test fixture count
	gs.DrawPile = colorInts(ColorRed, ColorOrange, ColorWhite)
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - handRed - len(gs.DrawPile) - len(gs.FaceUp))

	pending, _, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{"Red": 2})
	if err != nil {
		t.Fatalf("begin tunnel claim: unexpected error: %v", err)
	}
	pt := pending.PendingTunnel
	if pt == nil || pt.Surcharge != 1 {
		t.Fatalf("pending_tunnel = %+v, want surcharge 1", pt)
	}
	return m, e, r, pending, ids
}

// TestClaimRouteTunnelUnknownSurchargeColourIsRejected is an M2 regression
// for a decode-level malformed payload: an unknown/typo'd colour name in
// extra_payment must surface as engine.ErrIllegalAction (from
// paymentToColors), not silently fall back to the refund path.
func TestClaimRouteTunnelUnknownSurchargeColourIsRejected(t *testing.T) {
	_, e, r, pending, ids := redSurchargePendingWithSpare(t, 1)

	if _, _, err := resolveTunnelDecision(t, e, pending, ids[0], true, map[string]int{"Blak": 1}); !isIllegalAction(err) {
		t.Errorf("accept with an unknown colour %q: err = %v, want engine.ErrIllegalAction", "Blak", err)
	}

	accepted, _, err := resolveTunnelDecision(t, e, pending, ids[0], true, map[string]int{"Red": 1})
	if err != nil {
		t.Fatalf("retry with the correct payload after the rejected malformed attempt: unexpected error: %v", err)
	}
	if _, owned := accepted.RouteOwner[r.ID]; !owned {
		t.Error("route not owned after a corrected retry")
	}
}

// TestClaimRouteTunnelWrongSurchargeTotalIsRejected is an M2 regression for
// an arithmetically wrong payload: a legal colour, affordable from hand, but
// whose total does not equal the required surcharge (an off-by-one
// over-payment) must surface as engine.ErrIllegalAction rather than being
// silently swallowed into the refund path — this is exactly the reviewer's
// "off-by-one" failure scenario.
func TestClaimRouteTunnelWrongSurchargeTotalIsRejected(t *testing.T) {
	_, e, r, pending, ids := redSurchargePendingWithSpare(t, 2) // hand can afford 2 Red, so this isn't also a hand-insufficient case

	if _, _, err := resolveTunnelDecision(t, e, pending, ids[0], true, map[string]int{"Red": 2}); !isIllegalAction(err) {
		t.Errorf("accept with surcharge total 2 (want exactly 1): err = %v, want engine.ErrIllegalAction", err)
	}

	accepted, _, err := resolveTunnelDecision(t, e, pending, ids[0], true, map[string]int{"Red": 1})
	if err != nil {
		t.Fatalf("retry with the correct total after the rejected malformed attempt: unexpected error: %v", err)
	}
	if _, owned := accepted.RouteOwner[r.ID]; !owned {
		t.Error("route not owned after a corrected retry")
	}
}

// TestClaimRouteTunnelGenuineUnaffordableSurchargeRefunds is an M2
// regression for the one case that legitimately still takes the §8.4.5
// refund path: extra_payment names a legal colour and the exact required
// total, but the hand genuinely does not hold enough of it (spareRed: 0, so
// the base payment consumed every Red the player had). This must still
// refund the base payment exactly and end the turn with no error —
// distinguishing this from the malformed cases above (which must error
// instead) is the entire point of the M2 fix.
func TestClaimRouteTunnelGenuineUnaffordableSurchargeRefunds(t *testing.T) {
	m, e, r, pending, ids := redSurchargePendingWithSpare(t, 0)

	declined, _, err := resolveTunnelDecision(t, e, pending, ids[0], true, map[string]int{"Red": 1})
	if err != nil {
		t.Fatalf("accept a surcharge the hand cannot cover: unexpected error: %v (want the refund path, not an error)", err)
	}
	assertInvariants(t, m, declined)

	p0 := playerBySeat(declined, 0)
	wantHand := map[int32]int32{int32(ColorRed): 2}
	if !reflect.DeepEqual(p0.Hand, wantHand) {
		t.Errorf("hand after refund = %v, want %v (base payment returned exactly)", p0.Hand, wantHand)
	}
	if _, owned := declined.RouteOwner[r.ID]; owned {
		t.Error("route claimed despite a hand-insufficient surcharge")
	}
	if declined.PendingTunnel != nil {
		t.Errorf("pending_tunnel = %+v, want nil after resolution", declined.PendingTunnel)
	}
}

// TestClaimRouteTunnelAllLocomotivePaymentAcceptsLocomotiveExtra is the
// positive counterpart of
// TestClaimRouteTunnelAllLocomotivePaymentDeclinesInvalidExtra: a locomotive
// extra is exactly what an all-locomotive surcharge requires.
func TestClaimRouteTunnelAllLocomotivePaymentAcceptsLocomotiveExtra(t *testing.T) {
	m, e, r, pending, ids := tunnelAllLocoPending(t)

	accepted, _, err := resolveTunnelDecision(t, e, pending, ids[0], true, map[string]int{"Locomotive": 1})
	if err != nil {
		t.Fatalf("accept with a locomotive surcharge: unexpected error: %v", err)
	}
	assertInvariants(t, m, accepted)
	if _, owned := accepted.RouteOwner[r.ID]; !owned {
		t.Error("route not owned after a valid locomotive surcharge")
	}
}

// TestClaimRouteTunnelDeclineRefundsExactly covers rules §15 row "tunnel
// surcharge declined": the held base payment must return to the hand
// byte-for-byte (the highest-risk logic in this step — a leak or
// duplication would slip past a looser check), the reveals go to discard,
// the route stays unclaimed and the turn ends.
func TestClaimRouteTunnelDeclineRefundsExactly(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[13] // Gray, length 2, tunnel
	e := newTestEngine(m, 21)
	gs, ids := newNormalState(m, 2)

	originalHand := map[int32]int32{int32(ColorRed): 2}
	gs.Players[0].Hand = map[int32]int32{int32(ColorRed): 2}
	gs.DrawPile = colorInts(ColorRed, ColorOrange, ColorWhite)
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 2 - len(gs.DrawPile) - len(gs.FaceUp))
	discardBefore := len(gs.DiscardPile)

	pending, _, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{"Red": 2})
	if err != nil {
		t.Fatalf("begin tunnel claim: unexpected error: %v", err)
	}
	if pending.PendingTunnel == nil || pending.PendingTunnel.Surcharge != 1 {
		t.Fatalf("pending_tunnel = %+v, want surcharge 1", pending.PendingTunnel)
	}

	declined, _, err := resolveTunnelDecision(t, e, pending, ids[0], false, nil)
	if err != nil {
		t.Fatalf("decline tunnel surcharge: unexpected error: %v", err)
	}
	assertInvariants(t, m, declined)

	p0 := playerBySeat(declined, 0)
	if !reflect.DeepEqual(p0.Hand, originalHand) {
		t.Errorf("hand after decline = %v, want byte-identical %v", p0.Hand, originalHand)
	}
	if _, owned := declined.RouteOwner[r.ID]; owned {
		t.Error("route claimed despite a declined tunnel surcharge")
	}
	if declined.PendingTunnel != nil {
		t.Errorf("pending_tunnel = %+v, want nil after resolution", declined.PendingTunnel)
	}
	if declined.Phase != pb.Phase_PHASE_NORMAL {
		t.Errorf("phase = %v, want PHASE_NORMAL (turn ended)", declined.Phase)
	}
	if declined.CurrentSeat != 1 {
		t.Errorf("current_seat = %d, want 1 (turn advanced)", declined.CurrentSeat)
	}
	if want := discardBefore + tunnelRevealCount; len(declined.DiscardPile) != want {
		t.Errorf("len(discard_pile) = %d, want %d (the 3 revealed cards were added)", len(declined.DiscardPile), want)
	}
}

// TestClaimRouteTunnelDeclineRefundsMixedColourAndLocomotiveExactly is an
// m5(b) regression strengthening TestClaimRouteTunnelDeclineRefundsExactly's
// per-colour exactness check with a base payment that mixes a colour and a
// locomotive, rather than a single colour: a bug crediting the refund back
// under the wrong colour key would still leave the hand's *total* card count
// correct (so assertTrainCardConservation would not catch it), which is
// exactly why this uses reflect.DeepEqual on the whole hand.
func TestClaimRouteTunnelDeclineRefundsMixedColourAndLocomotiveExactly(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[13] // Gray, length 2, tunnel
	e := newTestEngine(m, 55)
	gs, ids := newNormalState(m, 2)

	originalHand := map[int32]int32{int32(ColorRed): 1, int32(ColorLoco): 1}
	gs.Players[0].Hand = map[int32]int32{int32(ColorRed): 1, int32(ColorLoco): 1}
	gs.DrawPile = colorInts(ColorRed, ColorBlue, ColorBlack) // 1 Red match -> surcharge 1
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 2 - len(gs.DrawPile) - len(gs.FaceUp))

	pending, _, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{"Red": 1, "Locomotive": 1})
	if err != nil {
		t.Fatalf("begin tunnel claim: unexpected error: %v", err)
	}
	if pending.PendingTunnel == nil || pending.PendingTunnel.Surcharge != 1 {
		t.Fatalf("pending_tunnel = %+v, want surcharge 1", pending.PendingTunnel)
	}

	declined, _, err := resolveTunnelDecision(t, e, pending, ids[0], false, nil)
	if err != nil {
		t.Fatalf("decline tunnel surcharge: unexpected error: %v", err)
	}
	assertInvariants(t, m, declined)

	p0 := playerBySeat(declined, 0)
	if !reflect.DeepEqual(p0.Hand, originalHand) {
		t.Errorf("hand after decline = %v, want byte-identical %v (mixed colour+locomotive base payment)", p0.Hand, originalHand)
	}
}

// TestClaimRouteTunnelGenuineUnaffordableRefundsMixedColourAndLocomotiveExactly
// is the m5(b) counterpart for the accept-but-genuinely-unaffordable refund
// path (as opposed to an explicit decline): same mixed colour+locomotive
// base payment, same per-colour reflect.DeepEqual exactness requirement.
func TestClaimRouteTunnelGenuineUnaffordableRefundsMixedColourAndLocomotiveExactly(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[13] // Gray, length 2, tunnel
	e := newTestEngine(m, 56)
	gs, ids := newNormalState(m, 2)

	originalHand := map[int32]int32{int32(ColorRed): 1, int32(ColorLoco): 1}
	gs.Players[0].Hand = map[int32]int32{int32(ColorRed): 1, int32(ColorLoco): 1}
	gs.DrawPile = colorInts(ColorRed, ColorBlue, ColorBlack) // 1 Red match -> surcharge 1
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 2 - len(gs.DrawPile) - len(gs.FaceUp))

	pending, _, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{"Red": 1, "Locomotive": 1})
	if err != nil {
		t.Fatalf("begin tunnel claim: unexpected error: %v", err)
	}
	if pending.PendingTunnel == nil || pending.PendingTunnel.Surcharge != 1 {
		t.Fatalf("pending_tunnel = %+v, want surcharge 1", pending.PendingTunnel)
	}

	// The base payment already spent the hand's only Red and only
	// Locomotive, so this accept is genuinely unaffordable (not malformed):
	// it must refund, not error.
	declined, _, err := resolveTunnelDecision(t, e, pending, ids[0], true, map[string]int{"Red": 1})
	if err != nil {
		t.Fatalf("accept a surcharge the hand cannot cover: unexpected error: %v (want the refund path)", err)
	}
	assertInvariants(t, m, declined)

	p0 := playerBySeat(declined, 0)
	if !reflect.DeepEqual(p0.Hand, originalHand) {
		t.Errorf("hand after refund = %v, want byte-identical %v (mixed colour+locomotive base payment)", p0.Hand, originalHand)
	}
}

// TestClaimRouteTunnelZeroSurchargeWithRevealsDiscardsThemExactly is an
// m5(a) regression: unlike TestClaimRouteTunnelEmptyPiles (a zero surcharge
// because nothing was left to reveal), this forces a genuine zero-match
// surcharge with all 3 tunnelRevealCount cards actually revealed. A bug
// dropping revealedInts on this immediate-commit path (beginTunnelClaim's
// `matches == 0` branch) would silently lose up to 3 cards — caught here by
// both the discard-pile length and, transitively, assertInvariants' train
// card conservation check.
func TestClaimRouteTunnelZeroSurchargeWithRevealsDiscardsThemExactly(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[13] // Gray, length 2, tunnel
	e := newTestEngine(m, 44)
	gs, ids := newNormalState(m, 2)

	gs.Players[0].Hand = map[int32]int32{int32(ColorGreen): 2}
	gs.DrawPile = colorInts(ColorBlue, ColorBlack, ColorWhite) // none match Green or Locomotive
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 2 - len(gs.DrawPile) - len(gs.FaceUp))
	discardBefore := len(gs.DiscardPile)

	ngs, _, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{"Green": 2})
	if err != nil {
		t.Fatalf("tunnel claim with a zero-match surcharge: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)
	if _, owned := ngs.RouteOwner[r.ID]; !owned {
		t.Error("route not owned after a zero-surcharge tunnel claim")
	}
	if ngs.PendingTunnel != nil {
		t.Errorf("pending_tunnel = %+v, want nil (surcharge resolved immediately)", ngs.PendingTunnel)
	}
	if want := discardBefore + tunnelRevealCount + r.Length; len(ngs.DiscardPile) != want {
		t.Errorf("len(discard_pile) = %d, want %d (the base payment's %d cards plus the 3 non-matching revealed cards, none lost)",
			len(ngs.DiscardPile), want, r.Length)
	}
}

// TestClaimRouteTunnelRevealReshuffleEmitsEvent is an m4 regression:
// revealTunnel used to always call popDrawPile with a nil events pointer,
// silently dropping the "deck_reshuffled" event even when a tunnel reveal
// exhausts the draw pile mid-reveal and reshuffles the discard pile in.
// Pile counts are public (unlike the reveals themselves, which stay secret
// until resolution), so that event must still reach the caller's ev.
func TestClaimRouteTunnelRevealReshuffleEmitsEvent(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[13] // Gray, length 2, tunnel
	e := newTestEngine(m, 33)
	gs, ids := newNormalState(m, 2)

	gs.Players[0].Hand = map[int32]int32{int32(ColorRed): 2}
	gs.FaceUp = stableFaceUp()
	gs.DrawPile = nil
	gs.DiscardPile = fillerCards(TotalTrainCards - 2 - len(gs.FaceUp))

	_, events, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{"Red": 2})
	if err != nil {
		t.Fatalf("begin tunnel claim: unexpected error: %v", err)
	}
	if !hasEvent(events, "deck_reshuffled") {
		t.Error("expected a deck_reshuffled event when a tunnel reveal triggers a reshuffle")
	}
}

// TestClaimRouteTunnelEmptyPiles covers rules §15 row "tunnel with empty
// draw+discard": no cards are left to reveal, so the surcharge is 0 and the
// claim commits immediately with no pending decision (rules §8.4.2-§8.4.3).
func TestClaimRouteTunnelEmptyPiles(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[13] // Gray, length 2, tunnel
	e := newTestEngine(m, 22)
	gs, ids := newNormalState(m, 2)

	gs.Players[0].Hand = map[int32]int32{int32(ColorRed): 2}
	gs.FaceUp = stableFaceUp()
	// The remaining cards are parked in the other player's hand so that
	// train-card conservation (rules §14) still holds with draw_pile and
	// discard_pile both genuinely empty (the scenario this test is about —
	// face_up is untouched by a tunnel reveal, so it can stay a stable 5).
	gs.Players[1].Hand = map[int32]int32{int32(ColorPurple): int32(TotalTrainCards - 2 - len(gs.FaceUp))} // #nosec G115 -- constant test fixture size
	gs.DrawPile = nil
	gs.DiscardPile = nil

	ngs, _, err := claimRoute(t, e, gs, ids[0], r.ID, map[string]int{"Red": 2})
	if err != nil {
		t.Fatalf("tunnel claim with empty piles: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)

	if _, owned := ngs.RouteOwner[r.ID]; !owned {
		t.Error("route not owned after a no-surcharge tunnel claim")
	}
	if ngs.PendingTunnel != nil {
		t.Errorf("pending_tunnel = %+v, want nil (no surcharge)", ngs.PendingTunnel)
	}
	if ngs.Phase != pb.Phase_PHASE_NORMAL {
		t.Errorf("phase = %v, want PHASE_NORMAL (turn ended immediately)", ngs.Phase)
	}
	if ngs.CurrentSeat != 1 {
		t.Errorf("current_seat = %d, want 1 (turn advanced)", ngs.CurrentSeat)
	}
}
