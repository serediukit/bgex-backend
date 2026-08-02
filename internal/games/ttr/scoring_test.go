package ttr

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// This file covers Step 12: final scoring (rules §12, §13) — route points,
// station-aware ticket connectivity (§13.2, §13.4), the longest-trail bonus
// (§13.3), the unbuilt-station bonus (§13.5), and §13.6 tiebreak ranking.
//
// No additive extension to testMap() (engine_setup_test.go) was needed for
// any case below: routes 1-2-3 (a-b, b-c, a-c) already form a triangle,
// which is enough for the longest-trail loop case, and the fixture's other
// routes/cities/tickets are more than enough for every disconnected-
// component, station-borrow, and tiebreak scenario. Where a case needs a
// hand-built *pb.GameState rather than a fully-played one, it starts from
// newNormalState (engine_draw_test.go) and sets only the fields the
// scenario cares about directly — the same pattern Steps 9-11 use.

// scoringState builds a bare n-player *pb.GameState via newNormalState and
// returns it together with player pointers seat 0..n-1, for tests that only
// care about final-scoring inputs (ClaimedRouteIds, TicketIds, StationLeft/
// Cities, Resigned) and never touch turn-flow fields.
func scoringState(m *Map, n int) (*pb.GameState, []*pb.PlayerState) {
	gs, _ := newNormalState(m, n)
	return gs, gs.Players
}

// --- Case 1: route point table + fail-loudly on undefined lengths --------

// TestScoringRoutePointsTable exercises sumRoutePoints (the §13.1 aggregate
// scoring.go uses) over the exact table from rules §12: length 1/2/3/4/6/8
// map to 1/2/4/7/15/21 points. (PointsForLength itself already has its own
// direct table test in maploader_test.go; this test's job is to confirm
// scoring.go's aggregate wraps it faithfully rather than reimplementing the
// table.)
func TestScoringRoutePointsTable(t *testing.T) {
	m := testMap(t)

	// testMap()'s routes cover every defined length exactly once each,
	// conveniently: 4(len1)->1, 1(len2)->2, 2(len3)->4, 3(len4, tunnel,
	// irrelevant to scoring)->7, 6(len6, ferry, irrelevant to scoring)->15,
	// 14(len8)->21.
	got, err := sumRoutePoints(m, []int32{4, 1, 2, 3, 6, 14})
	if err != nil {
		t.Fatalf("sumRoutePoints: unexpected error: %v", err)
	}
	want := 1 + 2 + 4 + 7 + 15 + 21
	if got != want {
		t.Errorf("sumRoutePoints = %d, want %d", got, want)
	}
}

// TestScoringInvalidRouteLengthFailsLoudly checks that FinalScore propagates
// ErrInvalidRouteLength rather than silently interpolating or swallowing it
// (rules §12: "fail loudly rather than interpolating"). A real ParseMap
// output can never contain a route of length 5 or 7 (the loader itself
// rejects them — maploader_test.go), so this test injects a synthetic
// invalid-length route directly into the parsed Map's index, exactly the
// defensive scenario PointsForLength exists to catch.
func TestScoringInvalidRouteLengthFailsLoudly(t *testing.T) {
	m := testMap(t)
	m.RouteByID[9999] = &Route{ID: 9999, A: "a", B: "b", ColorName: "Red", Color: ColorRed, Length: 5}

	gs, players := scoringState(m, 1)
	players[0].ClaimedRouteIds = []int32{9999}

	if _, err := FinalScore(m, gs); !errors.Is(err, ErrInvalidRouteLength) {
		t.Fatalf("FinalScore with an invalid route length: err = %v, want ErrInvalidRouteLength", err)
	}
}

// --- Case 2: direct ticket connectivity (+points / -points) ---------------

// TestScoringTicketConnectivityDirect checks rules §13.2's base case: a
// ticket whose two cities are connected by the player's own routes scores
// +points; one that is not scores -points. No stations are involved.
func TestScoringTicketConnectivityDirect(t *testing.T) {
	m := testMap(t)
	gs, players := scoringState(m, 2)
	p := players[0]

	p.ClaimedRouteIds = []int32{1} // a-b, len 2
	p.TicketIds = []int32{1, 2}    // 1: a-b (2 pts, connected) / 2: a-c (3 pts, not connected)

	_, net, completed, missed := bestStationAssignment(m, gs, p)

	if net != 2-3 {
		t.Errorf("net = %d, want %d", net, 2-3)
	}
	if len(completed) != 1 || completed[0] != 1 {
		t.Errorf("completed = %v, want [1]", completed)
	}
	if len(missed) != 1 || missed[0] != 2 {
		t.Errorf("missed = %v, want [2]", missed)
	}
}

// --- Case 3: ticket completed only via a station borrow -------------------

// TestScoringTicketCompletedOnlyViaStationBorrow builds a scenario where a
// ticket cannot be completed from the player's own routes alone, but can be
// once the optimizer chooses the single opponent route available at the
// player's station city (rules §13.2/§13.4). p owns route 8 (g-h); an
// opponent owns route 11 (b-g); p has a station at "g". Ticket 9 (h-b, 6
// pts) needs h connected to b: impossible from route 8 alone (only reaches
// g and h), but the station lets p borrow route 11 (b-g), bridging b-g-h.
func TestScoringTicketCompletedOnlyViaStationBorrow(t *testing.T) {
	m := testMap(t)
	gs, players := scoringState(m, 2)
	p, opponent := players[0], players[1]

	p.ClaimedRouteIds = []int32{8} // g-h
	p.StationCities = []string{"g"}
	p.StationsLeft = 2
	p.TicketIds = []int32{9} // h-b, 6 pts

	gs.RouteOwner = map[int32]int32{11: opponent.SeatIndex} // b-g, opponent-owned

	assignment, net, completed, missed := bestStationAssignment(m, gs, p)

	if net != 6 {
		t.Errorf("net = %d, want 6 (ticket 9 completed via the borrow)", net)
	}
	if len(completed) != 1 || completed[0] != 9 {
		t.Errorf("completed = %v, want [9]", completed)
	}
	if len(missed) != 0 {
		t.Errorf("missed = %v, want none", missed)
	}
	if assignment["g"] != 11 {
		t.Errorf(`assignment["g"] = %d, want 11 (route b-g)`, assignment["g"])
	}
}

// --- Case 4: two tickets needing different borrows at the SAME station ----

// TestScoringStationBorrowConflictPicksHigherValue is the crux of §13.4: a
// station's borrowed route is a single choice shared across ALL of the
// player's tickets, so when two tickets each depend on a *different* opponent
// route incident to the same station city, at most one can be satisfied —
// the optimizer must pick whichever choice maximizes the NET total, not
// simply satisfy every ticket it can find a route for independently.
//
// p has a station at "g" only (owns no routes of their own). An opponent
// owns both route 11 (b-g) and route 15 (c-g). Ticket 8 (g-a, 3 pts) can
// only complete if p's component reaches "a" — impossible here regardless of
// which route is borrowed, since p owns nothing touching "a" or "b" — so
// instead this test uses ticket 10 (c-g, 7 pts), which the borrowed edge
// alone satisfies (c and g are its own two endpoints), against ticket 9
// (h-b, 6 pts), which route 11 alone cannot satisfy either (p never reaches
// h). To build a genuine same-station conflict where each candidate route
// is independently "the" way to complete one specific ticket, this test
// gives p ONE owned route (1: a-b) so route 11 (b-g) extends that component
// to include g, completing ticket 8 (g-a, 3 pts); while route 15 (c-g) on
// its own completes ticket 10 (c-g, 7 pts). Both routes are incident to "g",
// p's only station, so only one can ever be borrowed:
//
//	borrow route 11 (b-g): ticket 8 completes (+3), ticket 10 misses (-7)  => net -4
//	borrow route 15 (c-g): ticket 8 misses (-3), ticket 10 completes (+7)  => net +4
//	borrow nothing:        both miss (-3, -7)                              => net -10
//
// A naive per-ticket optimizer would wrongly conclude both are
// simultaneously achievable (route 11 for ticket 8 *and* route 15 for ticket
// 10, total +10) — impossible, since "g" has only one borrow slot. The
// correct, shared-choice optimum is +4 (borrow route 15).
func TestScoringStationBorrowConflictPicksHigherValue(t *testing.T) {
	m := testMap(t)
	gs, players := scoringState(m, 2)
	p, opponent := players[0], players[1]

	p.ClaimedRouteIds = []int32{1} // a-b
	p.StationCities = []string{"g"}
	p.StationsLeft = 2
	p.TicketIds = []int32{8, 10} // 8: g-a (3 pts) / 10: c-g (7 pts)

	gs.RouteOwner = map[int32]int32{
		11: opponent.SeatIndex, // b-g
		15: opponent.SeatIndex, // c-g
	}

	assignment, net, completed, missed := bestStationAssignment(m, gs, p)

	if net != 4 {
		t.Fatalf("net = %d, want 4 (borrow route 15, complete ticket 10, miss ticket 8)", net)
	}
	if len(completed) != 1 || completed[0] != 10 {
		t.Errorf("completed = %v, want [10]", completed)
	}
	if len(missed) != 1 || missed[0] != 8 {
		t.Errorf("missed = %v, want [8]", missed)
	}
	if assignment["g"] != 15 {
		t.Errorf(`assignment["g"] = %d, want 15 (the higher-value borrow)`, assignment["g"])
	}
}

// --- Case 5: three stations with overlapping/conflicting options ----------

// TestScoringStationOptimizerGlobalMax exercises the full §13.4 brute force
// across three station cities at once, including one route (12: d-g) that
// is a candidate at TWO different stations (an overlapping option) and one
// city (h) where two candidates compete for a single slot (a conflict, as
// in case 4, but now embedded inside a larger combination).
//
// p owns no routes at all; stations at "d", "g", "h". An opponent owns
// routes 4 (c-d), 12 (d-g), 8 (g-h) and 13 (e-h):
//
//   - route 12 (d-g) is incident to BOTH "d" and "g" — an overlapping
//     candidate, satisfiable by hosting it at either station.
//   - "h" only ever offers ONE of {8, 13} at a time (both touch h, but h has
//     a single borrow slot) — the same kind of conflict as case 4.
//
// Ticket 103 (long, c-h, 15 pts) requires a path c -(4)- d -(12)- g -(8)- h:
// all three of routes 4, 12, 8 active simultaneously. Because route 12 can
// be hosted at "g" independently of route 4 being hosted at "d", both can
// be active together even though "d" itself could only ever host one route
// — this is exactly why the search must consider all three stations jointly
// rather than resolving each city's slot in isolation.
//
// Ticket 6 (e-g, 5 pts) requires e connected to g, which would need BOTH
// route 13 (e-h) and route 8 (g-h) active at once to bridge through h — but
// h has only one slot, so this is never achievable under any assignment: a
// correctness check that the optimizer never wrongly marks it complete.
//
// The unique optimum: d=4, g=12, h=8 -> ticket 103 completes (+15), ticket 6
// always misses (-5), net = +10. (Choosing h=13 instead loses ticket 103
// for no gain, since e-g still can't complete either: net = -15-5 = -20.
// Borrowing nothing: net = -15-5 = -20.)
func TestScoringStationOptimizerGlobalMax(t *testing.T) {
	m := testMap(t)
	gs, players := scoringState(m, 2)
	p, opponent := players[0], players[1]

	p.ClaimedRouteIds = nil
	p.StationCities = []string{"d", "g", "h"}
	p.StationsLeft = 0
	p.TicketIds = []int32{103, 6} // 103: c-h (15 pts, long) / 6: e-g (5 pts)

	gs.RouteOwner = map[int32]int32{
		4:  opponent.SeatIndex, // c-d
		12: opponent.SeatIndex, // d-g
		8:  opponent.SeatIndex, // g-h
		13: opponent.SeatIndex, // e-h
	}

	assignment, net, completed, missed := bestStationAssignment(m, gs, p)

	if net != 10 {
		t.Fatalf("net = %d, want 10", net)
	}
	if len(completed) != 1 || completed[0] != 103 {
		t.Errorf("completed = %v, want [103]", completed)
	}
	if len(missed) != 1 || missed[0] != 6 {
		t.Errorf("missed = %v, want [6] (e-g is never achievable: h has only one borrow slot)", missed)
	}
	if assignment["d"] != 4 {
		t.Errorf(`assignment["d"] = %d, want 4`, assignment["d"])
	}
	if assignment["g"] != 12 {
		t.Errorf(`assignment["g"] = %d, want 12`, assignment["g"])
	}
	if assignment["h"] != 8 {
		t.Errorf(`assignment["h"] = %d, want 8`, assignment["h"])
	}
}

// --- Case 6: longest trail with a loop -------------------------------------

// TestScoringLongestTrailWithLoop checks that a cycle in the player's own
// subgraph is fully traversable, each edge counted once (rules §13.3):
// routes 1 (a-b, 2), 2 (b-c, 3) and 3 (a-c, 4) form a triangle. The longest
// trail uses all three edges exactly once: 2+3+4 = 9.
func TestScoringLongestTrailWithLoop(t *testing.T) {
	m := testMap(t)
	got := longestTrail(m, []int32{1, 2, 3})
	if got != 9 {
		t.Errorf("longestTrail(triangle a-b-c) = %d, want 9", got)
	}
}

// --- Case 7: longest trail across a disconnected subgraph ------------------

// TestScoringLongestTrailDisconnectedComponent checks that the longest
// trail is the max over connected components, not their sum (rules §13.3):
// route 1 (a-b, len 2) is its own isolated component; routes 7 (f-g, len 3)
// and 8 (g-h, len 2) chain into a 5-length trail. The answer must be 5 (the
// larger component), not 2+5=7.
func TestScoringLongestTrailDisconnectedComponent(t *testing.T) {
	m := testMap(t)
	got := longestTrail(m, []int32{1, 7, 8})
	if got != 5 {
		t.Errorf("longestTrail(disconnected {a-b} + {f-g-h}) = %d, want 5 (the larger component, not the sum)", got)
	}
}

// --- Case 7b: a >64-edge subgraph terminates with the correct answer -------

// TestScoringLongestTrailOver64EdgesTerminates is the regression test for C1
// in the scoring/redaction review: newTrailDFS used to track its used-edge
// set as a bare uint64 bitmask on BOTH the memoized and unmemoized paths.
// uint64(1) << eIdx is 0 in Go once eIdx >= 64, so edge index 64 (the 65th
// edge) was never marked used and the DFS recursed across it forever — a
// fatal, unrecoverable stack overflow, not a panic middleware.Recovery could
// catch (reproduced standalone pre-fix: it crashes the test binary with
// "fatal error: stack overflow" rather than hanging silently).
//
// This builds a 65-edge path graph directly against buildTrailGraph's own
// output shape (trailEdge/adjacency), sidestepping Map/route ids entirely
// (a 65-route map document is not needed to exercise the DFS itself, and the
// loader now rejects trains_per_player > 64 separately anyway — see
// maploader_test.go's "trains_per_player 65" case). Every edge has length 1,
// so the correct longest trail is exactly 65 (walk the whole path once).
func TestScoringLongestTrailOver64EdgesTerminates(t *testing.T) {
	const n = 65
	edges := make([]trailEdge, n)
	adjacency := make(map[string][]int, n+1)
	cityID := func(i int) string { return fmt.Sprintf("city%02d", i) }
	for i := range n {
		a, b := cityID(i), cityID(i+1)
		edges[i] = trailEdge{a: a, b: b, length: 1}
		adjacency[a] = append(adjacency[a], i)
		adjacency[b] = append(adjacency[b], i)
	}

	done := make(chan int, 1)
	go func() {
		dfs := newTrailDFS(edges, adjacency)
		done <- dfs(cityID(0), make([]bool, n))
	}()

	select {
	case got := <-done:
		if got != n {
			t.Errorf("dfs over a %d-edge path graph = %d, want %d", n, got, n)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("dfs over a %d-edge path graph did not return within 10s (pre-fix: infinite recursion on edge index 64)", n)
	}
}

// --- Case 8: two players tied on longest path both get the bonus ----------

// TestScoringLongestPathTieBothGetBonus checks that the 10-point longest-
// path bonus is awarded in FULL to every player tied for the maximum, not
// split (rules §13.3). Both players own exactly one route of length 2
// (routes 1 and 8, disjoint), so both tie at longestTrail = 2.
func TestScoringLongestPathTieBothGetBonus(t *testing.T) {
	m := testMap(t)
	gs, players := scoringState(m, 2)
	players[0].ClaimedRouteIds = []int32{1} // a-b, len 2
	players[1].ClaimedRouteIds = []int32{8} // g-h, len 2

	results, err := FinalScore(m, gs)
	if err != nil {
		t.Fatalf("FinalScore: unexpected error: %v", err)
	}

	for _, sb := range results {
		if sb.LongestPathLength != 2 {
			t.Errorf("seat %d longest_path_length = %d, want 2", sb.SeatIndex, sb.LongestPathLength)
		}
		if sb.LongestPathBonus != longestPathBonusPoints {
			t.Errorf("seat %d longest_path_bonus = %d, want %d (both tied)", sb.SeatIndex, sb.LongestPathBonus, longestPathBonusPoints)
		}
	}
}

// --- Case 9: unbuilt-station bonus -----------------------------------------

// TestScoringUnbuiltStationBonus checks +4 points per station never built
// (rules §13.5): 3 unbuilt -> +12, 1 built + 2 unbuilt -> +8, 3 built -> +0.
// maxLongest is passed as an impossible value (-1) so the longest-path
// bonus (irrelevant to this case) never fires and can't muddy StationBonus.
func TestScoringUnbuiltStationBonus(t *testing.T) {
	m := testMap(t)

	tests := []struct {
		name         string
		stationsLeft int32
		want         int32
	}{
		{"3 unbuilt", 3, 12},
		{"1 built 2 unbuilt", 2, 8},
		{"3 built 0 unbuilt", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs, players := scoringState(m, 1)
			p := players[0]
			p.StationsLeft = tt.stationsLeft

			sb, err := buildBreakdown(m, gs, p, 0, -1)
			if err != nil {
				t.Fatalf("buildBreakdown: unexpected error: %v", err)
			}
			if sb.StationBonus != tt.want {
				t.Errorf("station_bonus = %d, want %d", sb.StationBonus, tt.want)
			}
		})
	}
}

// --- Case 10: §13.6 tiebreak ranking, one level at a time ------------------

// rankFixture is one entry's synthetic ScoreBreakdown/rankable pair for
// assignRanks-focused tests: it lets each §13.6 tiebreak level be exercised
// in isolation (equal total but different everything else, one level at a
// time) without needing to construct a real route/ticket graph that happens
// to produce those exact numbers.
func rankFixture(seat, total int32, completed, builtStations int, hasLongest bool) (*pb.ScoreBreakdown, rankable) {
	ids := make([]int32, completed)
	for i := range ids {
		ids[i] = int32(i + 1)
	}
	var bonus int32
	if hasLongest {
		bonus = longestPathBonusPoints
	}
	sb := &pb.ScoreBreakdown{SeatIndex: seat, Total: total, CompletedTicketIds: ids, LongestPathBonus: bonus}
	p := &pb.PlayerState{StationCities: make([]string, builtStations)}
	return sb, rankableFor(int(seat), sb, p)
}

// TestScoringTiebreakLevels drives assignRanks directly (rather than a full
// FinalScore) so each §13.6 criterion can be isolated with a hand-built,
// otherwise-tied pair, plus a clearly-lower-total control entry in every
// sub-test to confirm ranks are still assigned correctly around a tie
// (rules §13.6: total, then completed tickets, then fewest stations built,
// then longest path, then shared victory as the terminal tie).
func TestScoringTiebreakLevels(t *testing.T) {
	t.Run("completed tickets breaks a total tie", func(t *testing.T) {
		sb0, r0 := rankFixture(0, 20, 1, 0, false)
		sb1, r1 := rankFixture(1, 20, 2, 0, false) // more completed tickets -> wins
		sb2, r2 := rankFixture(2, 5, 0, 3, false)  // control: clearly lower total
		assertRanks(t, []int32{2, 1, 3}, []bool{false, false, false}, sb0, sb1, sb2, r0, r1, r2)
	})

	t.Run("fewest stations built breaks a completed-ticket tie", func(t *testing.T) {
		sb0, r0 := rankFixture(0, 20, 1, 2, false)
		sb1, r1 := rankFixture(1, 20, 1, 0, false) // fewer built -> wins
		sb2, r2 := rankFixture(2, 5, 0, 3, false)
		assertRanks(t, []int32{2, 1, 3}, []bool{false, false, false}, sb0, sb1, sb2, r0, r1, r2)
	})

	t.Run("holding the longest path breaks a stations-built tie", func(t *testing.T) {
		sb0, r0 := rankFixture(0, 20, 1, 0, false)
		sb1, r1 := rankFixture(1, 20, 1, 0, true) // holds longest path -> wins
		sb2, r2 := rankFixture(2, 5, 0, 3, false)
		assertRanks(t, []int32{2, 1, 3}, []bool{false, false, false}, sb0, sb1, sb2, r0, r1, r2)
	})

	t.Run("still tied after every criterion shares first place", func(t *testing.T) {
		sb0, r0 := rankFixture(0, 20, 1, 0, true)
		sb1, r1 := rankFixture(1, 20, 1, 0, true) // identical on every criterion
		sb2, r2 := rankFixture(2, 5, 0, 3, false) // control: rank must skip to 3, not 2
		assertRanks(t, []int32{1, 1, 3}, []bool{true, true, false}, sb0, sb1, sb2, r0, r1, r2)
	})
}

// assertRanks calls assignRanks over exactly the 3 (sb, rankable) pairs
// given (in seat order 0,1,2) and checks the resulting Rank/SharedVictory
// against wantRanks/wantShared (also in seat order).
func assertRanks(
	t *testing.T,
	wantRanks []int32, wantShared []bool,
	sb0, sb1, sb2 *pb.ScoreBreakdown,
	r0, r1, r2 rankable,
) {
	t.Helper()
	results := []*pb.ScoreBreakdown{sb0, sb1, sb2}
	ranked := []rankable{r0, r1, r2}
	assignRanks(results, ranked)

	for i, sb := range results {
		if sb.Rank != wantRanks[i] {
			t.Errorf("seat %d rank = %d, want %d", i, sb.Rank, wantRanks[i])
		}
		if sb.SharedVictory != wantShared[i] {
			t.Errorf("seat %d shared_victory = %v, want %v", i, sb.SharedVictory, wantShared[i])
		}
	}
}

// --- Case 11: a resigned player scores 0 and ranks last --------------------

// TestScoringResignedPlayerZeroedAndLast checks plan Q14's scoring contract:
// a resigned player's entire ScoreBreakdown is zeroed (not just Total),
// Rank is fixed at len(gs.Players) regardless of the active-player
// standings, and their claimed routes/tickets — left deliberately non-empty
// here — are proven NOT to feed into their own breakdown (they still block
// as opponent-owned routes for others' station borrows, exercised
// separately by cases 3-5, but are excluded from this player's own score).
func TestScoringResignedPlayerZeroedAndLast(t *testing.T) {
	m := testMap(t)
	gs, players := scoringState(m, 3)
	resigned, active1, active2 := players[0], players[1], players[2]

	resigned.Resigned = true
	resigned.ClaimedRouteIds = []int32{1, 2, 3} // would otherwise score route+longest-path points
	resigned.TicketIds = []int32{1}
	resigned.StationsLeft = 3

	active1.ClaimedRouteIds = []int32{4} // len 1 -> 1 point
	active2.ClaimedRouteIds = []int32{7} // len 3 -> 4 points

	results, err := FinalScore(m, gs)
	if err != nil {
		t.Fatalf("FinalScore: unexpected error: %v", err)
	}

	sb := results[0]
	assertZeroedBreakdown(t, sb)
	if sb.Rank != int32(len(gs.Players)) {
		t.Errorf("resigned player's rank = %d, want %d (last)", sb.Rank, len(gs.Players))
	}

	if results[1].Rank == sb.Rank || results[2].Rank == sb.Rank {
		t.Errorf("an active player shares the resigned player's rank %d: results = %+v", sb.Rank, results)
	}

	// m6 in the scoring/redaction review: every assertion above still passes
	// even if the resigned player's routes (1,2,3 — the a-b-c triangle,
	// longestTrail == 9) were wrongly folded into maxLongest, since neither
	// active player's own trail (1 and 3 respectively) would then tie it and
	// NOBODY would get the bonus — a false negative on the exclusion this
	// test claims to prove. Asserting the bonus DOES land on seat 2 (whose
	// own trail, length 3 from route 7 alone, is the true max among the two
	// active players) is what actually discriminates "resigned player
	// excluded" from "resigned player silently included."
	if results[2].LongestPathBonus != longestPathBonusPoints {
		t.Errorf("seat 2 longest_path_bonus = %d, want %d (its own trail, length 3, is the max among ACTIVE players — "+
			"if the resigned player's length-9 trail were wrongly counted, maxLongest would be 9 and nobody would get this)",
			results[2].LongestPathBonus, longestPathBonusPoints)
	}
	if results[1].LongestPathBonus != 0 {
		t.Errorf("seat 1 longest_path_bonus = %d, want 0 (its trail, length 1, does not tie seat 2's length-3 max)", results[1].LongestPathBonus)
	}

	if results[2].Total <= results[1].Total {
		t.Errorf("seat 2 (4 route pts) should outrank seat 1 (1 route pt): results[1]=%+v results[2]=%+v", results[1], results[2])
	}
	if results[2].Rank != 1 || results[1].Rank != 2 {
		t.Errorf("want active ranks 1,2 for seats 2,1; got seat1=%d seat2=%d", results[1].Rank, results[2].Rank)
	}
}

// assertZeroedBreakdown checks that every scoring field of sb except
// SeatIndex/Rank is at its zero value — the resigned-player contract
// (plan Q14; see FinalScore's doc comment on why it's a full zero, not just
// Total).
func assertZeroedBreakdown(t *testing.T, sb *pb.ScoreBreakdown) {
	t.Helper()
	zero := &pb.ScoreBreakdown{SeatIndex: sb.SeatIndex, Rank: sb.Rank}
	if sb.Total != zero.Total || sb.RoutePoints != zero.RoutePoints ||
		sb.TicketsCompletedPoints != zero.TicketsCompletedPoints || sb.TicketsMissedPoints != zero.TicketsMissedPoints ||
		sb.StationBonus != zero.StationBonus || sb.LongestPathBonus != zero.LongestPathBonus ||
		sb.LongestPathLength != zero.LongestPathLength || len(sb.CompletedTicketIds) != 0 ||
		len(sb.MissedTicketIds) != 0 || len(sb.BorrowedRoutes) != 0 || sb.SharedVictory {
		t.Errorf("breakdown = %+v, want every field zero except seat_index/rank", sb)
	}
}

// --- Case 12: end-to-end scripted game, §13.1 checksum ---------------------

// forceEndgameTrigger drives gs from wherever the scripted portion of
// TestScoringEndToEndScriptedGame left it straight into PHASE_FINISHED, by
// calling the real (already independently tested in engine_flow_test.go)
// e.endTurn repeatedly for the current seat.
//
// This is a deliberate departure from letting the §11 trains<=2 trigger
// fire naturally: testMap()'s entire claimable board sums to exactly 45
// train-car lengths (see engine_setup_test.go's fixture doc comment) - by
// coincidence exactly one player's full starting trains_left - split across
// 3 players racing for routes with only the cards their own hands happen to
// draw. Empirically (see this step's implementation report) a scripted
// bot never gets close to any player's trains_left <= 2 within a reasonable
// turn budget on this fixture, and eventually deadlocks (no affordable
// route, no buildable station, both card piles empty, ticket deck empty)
// long before that could happen. Forcing the current player's trains_left
// down and invoking the real endTurn/finalizeGame path exercises exactly
// the scoring code under test against a REAL played-out board (genuine
// claimed routes, tickets, hands) without depending on an economy this
// small fixture cannot produce in a fair multi-player race. The §11 trigger
// mechanics themselves already have dedicated, direct coverage in
// engine_flow_test.go; this test's job is FinalScore, not the trigger.
func forceEndgameTrigger(t *testing.T, e *Engine, m *Map, gs *pb.GameState) {
	t.Helper()

	acting := playerBySeat(gs, gs.CurrentSeat)
	acting.TrainsLeft = endTriggerTrains
	var events []engine.Event
	if err := e.endTurn(m, gs, acting, &events); err != nil {
		t.Fatalf("forceEndgameTrigger: initial endTurn: %v", err)
	}

	for gs.Phase != pb.Phase_PHASE_FINISHED {
		acting = playerBySeat(gs, gs.CurrentSeat)
		events = events[:0]
		if err := e.endTurn(m, gs, acting, &events); err != nil {
			t.Fatalf("forceEndgameTrigger: endTurn: %v", err)
		}
	}
}

// TestScoringEndToEndScriptedGame plays a real, deterministic (seeded
// shuffler) 3-player game via the same scripted-bot driver
// TestFlowScriptedThreePlayerGameHoldsInvariantsThroughout uses
// (engine_flow_test.go's playOneScriptedTurn and its helpers), so every
// ScoreBreakdown FinalScore produces reflects genuine accumulated claimed
// routes, tickets and hands rather than a hand-built fixture, then forces
// the game to PHASE_FINISHED (see forceEndgameTrigger) and checks:
//
//   - every ScoreBreakdown's components sum to its own Total (a checksum on
//     finalizeGame's wiring: gs.Results holds real FinalScore output, not a
//     stale/placeholder value);
//   - each player's route_points recomputed by FinalScore from
//     ClaimedRouteIds matches the SAME player's RouteScore accumulated
//     incrementally during play by applyClaimRoute (rules §13.1's own
//     "optionally recompute as a checksum" — the cross-check between the
//     incremental and final scoring paths).
func TestScoringEndToEndScriptedGame(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 2024)
	state, ids := mustInitState(t, e, 3)
	gs := mustResolveAllSetupTickets(t, e, mustGameState(t, state), ids)
	assertInvariants(t, m, gs)

	seen := map[string]bool{}
	const scriptedTurns = 25
	for range scriptedTurns {
		if gs.Phase == pb.Phase_PHASE_FINISHED {
			break
		}
		gs = playOneScriptedTurn(t, e, m, gs, seen)
		assertInvariants(t, m, gs)
	}

	preFinishRouteScore := make(map[int32]int32, len(gs.Players))
	for _, p := range gs.Players {
		preFinishRouteScore[p.SeatIndex] = p.RouteScore
	}

	if gs.Phase != pb.Phase_PHASE_FINISHED {
		forceEndgameTrigger(t, e, m, gs)
	}
	if gs.Phase != pb.Phase_PHASE_FINISHED {
		t.Fatalf("phase = %v after forcing the endgame trigger, want PHASE_FINISHED", gs.Phase)
	}
	if len(gs.Results) != len(gs.Players) {
		t.Fatalf("len(results) = %d, want %d (one per player)", len(gs.Results), len(gs.Players))
	}

	anyNonZeroRoutePoints := false
	for _, sb := range gs.Results {
		sum := sb.RoutePoints + sb.TicketsCompletedPoints - sb.TicketsMissedPoints + sb.StationBonus + sb.LongestPathBonus
		if sum != sb.Total {
			t.Errorf("seat %d: components sum to %d, want Total %d (breakdown = %+v)", sb.SeatIndex, sum, sb.Total, sb)
		}

		wantRoutePoints := preFinishRouteScore[sb.SeatIndex]
		if sb.RoutePoints != wantRoutePoints {
			t.Errorf("seat %d: route_points = %d, want %d (running RouteScore accumulated during play — §13.1 checksum)",
				sb.SeatIndex, sb.RoutePoints, wantRoutePoints)
		}
		if sb.RoutePoints != 0 {
			anyNonZeroRoutePoints = true
		}
	}
	if !anyNonZeroRoutePoints {
		t.Error("every player's route_points is 0 — the scripted portion never claimed a route, so this test isn't exercising the checksum meaningfully")
	}
}
