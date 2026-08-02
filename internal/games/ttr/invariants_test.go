package ttr

import (
	"fmt"
	"testing"

	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// assertInvariants checks every rules §14 invariant against gs (for board m)
// and fails t on any violation, with a distinct, greppable message per
// check so a failure immediately identifies which invariant broke. It is
// shared by every TTR engine test from this step onward (Steps 7-13), so it
// deliberately reads m.Rules.TrainsPerPlayer / StationsPerPlayer and
// TotalTrainCards rather than hardcoding 45/3/110 — that way it works
// unchanged against both testMap() and the real Europe map.
func assertInvariants(t *testing.T, m *Map, gs *pb.GameState) {
	t.Helper()

	assertTrainCardConservation(t, gs)
	assertTrainsAndStations(t, m, gs)
	assertNoDuplicateStations(t, gs)
	assertNoDuplicateRouteOwners(t, m, gs)
	assertFaceUpShape(t, gs)
	assertNoDuplicateTickets(t, gs)
}

// assertTrainCardConservation checks that every one of the 110 train cards
// is accounted for exactly once across hands, draw pile, discard pile,
// face-up row, and a pending tunnel's held base payment + reveals if one is
// in flight (rules §14 bullet 1). A pending tunnel (rules §8.4) is the one
// place cards can legitimately sit outside the four canonical piles: the
// base payment is held out of the hand (not yet discarded) so a decline can
// refund it, and the 3 reveals are held secret until resolution.
func assertTrainCardConservation(t *testing.T, gs *pb.GameState) {
	t.Helper()

	total := len(gs.DrawPile) + len(gs.DiscardPile) + len(gs.FaceUp)
	for _, p := range gs.Players {
		for _, n := range p.Hand {
			total += int(n)
		}
	}
	if pt := gs.PendingTunnel; pt != nil {
		for _, n := range pt.BasePayment {
			total += int(n)
		}
		total += len(pt.Revealed)
	}
	if total != TotalTrainCards {
		t.Errorf("invariant [train card conservation]: hands+draw+discard+face_up+pending_tunnel = %d, want %d", total, TotalTrainCards)
	}
}

// assertTrainsAndStations checks, per player, that trains_left plus the
// length of every owned route equals m.Rules.TrainsPerPlayer, and that
// stations_left plus the number of built stations equals
// m.Rules.StationsPerPlayer (rules §14 bullets 2-3). It also checks that no
// player owns both routes of a double-route pair (bullet: "no player owns
// both halves of a double-route pair").
func assertTrainsAndStations(t *testing.T, m *Map, gs *pb.GameState) {
	t.Helper()

	for _, p := range gs.Players {
		claimedLen := 0
		claimed := make(map[int32]bool, len(p.ClaimedRouteIds))
		for _, rid := range p.ClaimedRouteIds {
			claimed[rid] = true
			r := m.RouteByID[rid]
			if r == nil {
				t.Errorf("invariant [trains_left+routes]: seat %d claims unknown route %d", p.SeatIndex, rid)
				continue
			}
			claimedLen += r.Length
		}
		if int(p.TrainsLeft)+claimedLen != m.Rules.TrainsPerPlayer {
			t.Errorf("invariant [trains_left+routes]: seat %d trains_left(%d)+claimed_length(%d) = %d, want %d",
				p.SeatIndex, p.TrainsLeft, claimedLen, int(p.TrainsLeft)+claimedLen, m.Rules.TrainsPerPlayer)
		}

		if int(p.StationsLeft)+len(p.StationCities) != m.Rules.StationsPerPlayer {
			t.Errorf("invariant [stations_left+built]: seat %d stations_left(%d)+built(%d) = %d, want %d",
				p.SeatIndex, p.StationsLeft, len(p.StationCities), int(p.StationsLeft)+len(p.StationCities), m.Rules.StationsPerPlayer)
		}

		for _, rid := range p.ClaimedRouteIds {
			r := m.RouteByID[rid]
			if r == nil || r.Pair == nil {
				continue
			}
			if claimed[*r.Pair] {
				t.Errorf("invariant [no double-route self-ownership]: seat %d owns both route %d and its pair %d", p.SeatIndex, rid, *r.Pair)
			}
		}
	}

	assertClosedSiblings(t, m, gs)
}

// assertClosedSiblings checks that in 2-3 player games, every claimed route
// with a double-route sibling has that sibling recorded in closed_routes
// (rules §14 bullet 6, §8.5).
func assertClosedSiblings(t *testing.T, m *Map, gs *pb.GameState) {
	t.Helper()

	if len(gs.Players) > 3 {
		return
	}
	closed := make(map[int32]bool, len(gs.ClosedRoutes))
	for _, rid := range gs.ClosedRoutes {
		closed[rid] = true
	}
	for rid := range gs.RouteOwner {
		r := m.RouteByID[rid]
		if r == nil || r.Pair == nil {
			continue
		}
		if !closed[*r.Pair] {
			t.Errorf("invariant [2-3p sibling closure]: route %d is claimed but its pair %d is not closed", rid, *r.Pair)
		}
	}
}

// assertNoDuplicateStations checks that no city holds more than one
// station, cross-checked between every player's station_cities and
// gs.station_owner (rules §14 bullet 4).
func assertNoDuplicateStations(t *testing.T, gs *pb.GameState) {
	t.Helper()

	seen := make(map[string]int32, len(gs.StationOwner))
	for _, p := range gs.Players {
		for _, city := range p.StationCities {
			if owner, dup := seen[city]; dup {
				t.Errorf("invariant [no duplicate stations]: city %q has stations from both seat %d and seat %d", city, owner, p.SeatIndex)
				continue
			}
			seen[city] = p.SeatIndex
		}
	}
	for city, owner := range gs.StationOwner {
		if seen[city] != owner {
			t.Errorf("invariant [no duplicate stations]: station_owner[%q]=%d disagrees with player station_cities", city, owner)
		}
	}
}

// assertNoDuplicateRouteOwners checks that no route is claimed by more than
// one player, cross-checked between every player's claimed_route_ids and
// gs.route_owner (rules §14 bullet 5).
func assertNoDuplicateRouteOwners(t *testing.T, m *Map, gs *pb.GameState) {
	t.Helper()

	seen := make(map[int32]int32, len(gs.RouteOwner))
	for _, p := range gs.Players {
		for _, rid := range p.ClaimedRouteIds {
			if owner, dup := seen[rid]; dup {
				t.Errorf("invariant [no duplicate route owners]: route %d is claimed by both seat %d and seat %d", rid, owner, p.SeatIndex)
				continue
			}
			seen[rid] = p.SeatIndex
			if _, ok := m.RouteByID[rid]; !ok {
				t.Errorf("invariant [no duplicate route owners]: seat %d claims route %d, unknown on this map", p.SeatIndex, rid)
			}
		}
	}
	for rid, owner := range gs.RouteOwner {
		if seen[rid] != owner {
			t.Errorf("invariant [no duplicate route owners]: route_owner[%d]=%d disagrees with player claimed_route_ids", rid, owner)
		}
	}
}

// assertNoDuplicateTickets checks that no ticket id is credited in more than
// one place at once (rules §14, §9 vs §5.7): the face-down ticket_deck, any
// player's permanently kept tickets, any player's still-outstanding setup
// offer, and a pending in-game draw's offered tickets must all be pairwise
// disjoint.
//
// This is deliberately a uniqueness check, not a total-conservation check: a
// full conservation invariant (every ticket accounted for exactly once
// across the whole game) is NOT cleanly expressible from GameState alone.
// Rules §5.5 and §5.7 intentionally remove tickets from the game forever —
// unallocated long tickets are discarded unseen at deal time, and setup
// discards are dropped — and neither removal leaves any counter in the
// protobuf schema recording how many tickets were removed. Without such a
// field (out of scope for this step — it would need a proto change), there
// is no fixed expected total to check gs's ticket count against. What IS
// always true, and worth checking, is that no single ticket is ever counted
// twice: a duplicate credit is a real, detectable bug in a way that a
// dropped-without-a-trace ticket, by the rules' own design, is not.
func assertNoDuplicateTickets(t *testing.T, gs *pb.GameState) {
	t.Helper()

	seen := make(map[int32]string)
	check := func(id int32, where string) {
		if prev, dup := seen[id]; dup {
			t.Errorf("invariant [no duplicate tickets]: ticket %d appears in both %s and %s", id, prev, where)
			return
		}
		seen[id] = where
	}

	for _, id := range gs.TicketDeck {
		check(id, "ticket_deck")
	}
	if gs.PendingTicketDraw != nil {
		for _, id := range gs.PendingTicketDraw.OfferedTicketIds {
			check(id, "pending_ticket_draw")
		}
	}
	for _, p := range gs.Players {
		ticketsWhere := fmt.Sprintf("seat %d's ticket_ids", p.SeatIndex)
		for _, id := range p.TicketIds {
			check(id, ticketsWhere)
		}
		offerWhere := fmt.Sprintf("seat %d's setup_ticket_offer", p.SeatIndex)
		for _, id := range p.SetupTicketOffer {
			check(id, offerWhere)
		}
	}
}

// assertFaceUpShape checks that the face-up row holds exactly faceUpSlots
// cards unless the draw and discard piles are both exhausted (rules §14
// bullet 7), and that fewer than locoFlushThreshold of them are locomotives
// (rules §14 bullet 8).
func assertFaceUpShape(t *testing.T, gs *pb.GameState) {
	t.Helper()

	if len(gs.FaceUp) != faceUpSlots && (len(gs.DrawPile) != 0 || len(gs.DiscardPile) != 0) {
		t.Errorf("invariant [face_up shape]: len(face_up) = %d, want %d (draw_pile=%d discard_pile=%d)",
			len(gs.FaceUp), faceUpSlots, len(gs.DrawPile), len(gs.DiscardPile))
	}
	if n := countLocos(gs.FaceUp); n >= locoFlushThreshold {
		t.Errorf("invariant [face_up loco flush]: %d face-up locomotives, want < %d", n, locoFlushThreshold)
	}
}
