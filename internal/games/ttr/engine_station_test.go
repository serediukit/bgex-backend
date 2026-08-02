package ttr

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// buildStation is applyAction specialized for ActionBuildStation.
func buildStation(t *testing.T, e *Engine, gs *pb.GameState, userID uuid.UUID, cityID string, payment map[string]int) (*pb.GameState, []engine.Event, error) {
	t.Helper()
	return applyAction(t, e, gs, userID, ActionBuildStation, BuildStationPayload{CityID: cityID, Payment: payment})
}

// TestStationCostsEscalateThenIllegal covers rules §10.2's cost ladder
// (1, 2, 3 for the 1st/2nd/3rd station) against testMap's
// stations_per_player = 3, and that a 4th attempt is illegal outright once
// stations_left has reached 0 (rules §10.2 "<= 3/game").
func TestStationCostsEscalateThenIllegal(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, ids := newNormalState(m, 2)

	const totalCost = 1 + 2 + 3
	gs.Players[0].Hand = map[int32]int32{int32(ColorBlue): totalCost}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - totalCost - len(gs.FaceUp))

	cities := []string{"z", "a", "b"} // z is routeless (see testMap's doc comment); a, b just need to be distinct, unstationed cities
	wantCosts := []int{1, 2, 3}

	for i, city := range cities {
		gs.CurrentSeat = 0 // build_station ends the turn; force it back so this test can build repeatedly for the same seat
		if got := stationCost(gs.Players[0], m); got != wantCosts[i] {
			t.Fatalf("stationCost before build %d = %d, want %d", i+1, got, wantCosts[i])
		}

		ngs, _, err := buildStation(t, e, gs, ids[0], city, map[string]int{"Blue": wantCosts[i]})
		if err != nil {
			t.Fatalf("build station %d (city %q, cost %d): unexpected error: %v", i+1, city, wantCosts[i], err)
		}
		assertInvariants(t, m, ngs)
		if owner, ok := ngs.StationOwner[city]; !ok || owner != 0 {
			t.Fatalf("station_owner[%q] = %v, want seat 0", city, ngs.StationOwner[city])
		}
		gs = ngs
	}

	if gs.Players[0].StationsLeft != 0 {
		t.Fatalf("test setup: stations_left = %d after 3 builds, want 0", gs.Players[0].StationsLeft)
	}

	gs.CurrentSeat = 0
	if _, _, err := buildStation(t, e, gs, ids[0], "c", map[string]int{"Blue": 1}); !isIllegalAction(err) {
		t.Errorf("4th station build: err = %v, want engine.ErrIllegalAction", err)
	}
}

// TestStationCityOwnershipIsGlobal covers rules §10.1: a station belongs to
// a city, not a (player, city) pair — once any player has built there, no
// other player may, regardless of their own payment or stations_left.
func TestStationCityOwnershipIsGlobal(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 2)
	gs, ids := newNormalState(m, 2)

	gs.Players[0].Hand = map[int32]int32{int32(ColorBlue): 1}
	gs.Players[1].Hand = map[int32]int32{int32(ColorRed): 1}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 1 - 1 - len(gs.FaceUp))

	ngs, _, err := buildStation(t, e, gs, ids[0], "z", map[string]int{"Blue": 1})
	if err != nil {
		t.Fatalf("seat 0 builds station at z: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)
	if ngs.CurrentSeat != 1 {
		t.Fatalf("current_seat = %d, want 1 (turn advanced)", ngs.CurrentSeat)
	}

	if _, _, err := buildStation(t, e, ngs, ids[1], "z", map[string]int{"Red": 1}); !isIllegalAction(err) {
		t.Errorf("seat 1 builds at already-stationed city z: err = %v, want engine.ErrIllegalAction", err)
	}
}

// TestStationRoutelessCityIsLegalTarget covers rules §10.1: a city with no
// incident routes at all is still a legal station target. testMap's city
// "z" (added additively for this step; see its doc comment) has no route
// referencing it.
func TestStationRoutelessCityIsLegalTarget(t *testing.T) {
	m := testMap(t)
	if got := len(m.RoutesByCity["z"]); got != 0 {
		t.Fatalf("test setup: city z has %d incident routes, want 0 (routeless fixture)", got)
	}

	e := newTestEngine(m, 3)
	gs, ids := newNormalState(m, 2)
	gs.Players[0].Hand = map[int32]int32{int32(ColorGreen): 1}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 1 - len(gs.FaceUp))

	ngs, _, err := buildStation(t, e, gs, ids[0], "z", map[string]int{"Green": 1})
	if err != nil {
		t.Fatalf("build station at routeless city: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)
	if owner, ok := ngs.StationOwner["z"]; !ok || owner != 0 {
		t.Errorf("station_owner[z] = %v, want seat 0", ngs.StationOwner["z"])
	}
}

// stationSecondBuildState returns a fixture where seat 0 has already built
// its 1st station (at city "a"), so its next build costs 2 (rules §10.2).
func stationSecondBuildState(m *Map, hand map[Color]int) (*pb.GameState, []uuid.UUID) {
	gs, ids := newNormalState(m, 2)
	gs.Players[0].StationsLeft = 2
	gs.Players[0].StationCities = []string{"a"}
	gs.StationOwner = map[string]int32{"a": 0}
	gs.Players[0].Hand = handFromColors(hand)
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - handColorTotal(hand) - len(gs.FaceUp))
	return gs, ids
}

// TestStationPaymentSingleColourWithLocomotiveSubstitution covers rules
// §10.2's payment rule, shared with route claiming (rules §8.2): one single
// non-locomotive colour plus any number of locomotives, never two different
// non-locomotive colours.
func TestStationPaymentSingleColourWithLocomotiveSubstitution(t *testing.T) {
	m := testMap(t)

	t.Run("two different colours is illegal", func(t *testing.T) {
		e := newTestEngine(m, 4)
		gs, ids := stationSecondBuildState(m, map[Color]int{ColorBlue: 1, ColorRed: 1})
		if got := stationCost(gs.Players[0], m); got != 2 {
			t.Fatalf("test setup: stationCost = %d, want 2", got)
		}

		_, _, err := buildStation(t, e, gs, ids[0], "z", map[string]int{"Blue": 1, "Red": 1})
		if !isIllegalAction(err) {
			t.Errorf("2nd station paid with 2 different colours: err = %v, want engine.ErrIllegalAction", err)
		}
	})

	t.Run("one colour plus one locomotive is legal", func(t *testing.T) {
		e := newTestEngine(m, 5)
		gs, ids := stationSecondBuildState(m, map[Color]int{ColorBlue: 1, ColorLoco: 1})

		ngs, _, err := buildStation(t, e, gs, ids[0], "z", map[string]int{"Blue": 1, "Locomotive": 1})
		if err != nil {
			t.Fatalf("2nd station paid with colour+locomotive: unexpected error: %v", err)
		}
		assertInvariants(t, m, ngs)
		if ngs.Players[0].StationsLeft != 1 {
			t.Errorf("stations_left = %d, want 1", ngs.Players[0].StationsLeft)
		}
	})
}

// TestStationBuildAwardsNoImmediatePoints covers rules §10.2's "building a
// station awards no immediate points" (points are deferred to §13.5
// scoring): route_score must be unchanged by a build_station action, unlike
// claim_route (m6 in the Step 11 review).
func TestStationBuildAwardsNoImmediatePoints(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 9)
	gs, ids := newNormalState(m, 2)
	gs.Players[0].RouteScore = 7
	gs.Players[0].Hand = map[int32]int32{int32(ColorBlue): 1}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 1 - len(gs.FaceUp))

	ngs, _, err := buildStation(t, e, gs, ids[0], "z", map[string]int{"Blue": 1})
	if err != nil {
		t.Fatalf("build station: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)
	if got := playerBySeat(ngs, 0).RouteScore; got != 7 {
		t.Errorf("route_score = %d after building a station, want unchanged 7", got)
	}
}

// TestStationBuildRejectsUnknownCity covers station.go's unknown-city guard
// (rules §10.1: the target city must exist on the map).
func TestStationBuildRejectsUnknownCity(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 10)
	gs, ids := newNormalState(m, 2)
	gs.Players[0].Hand = map[int32]int32{int32(ColorBlue): 1}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 1 - len(gs.FaceUp))

	if _, _, err := buildStation(t, e, gs, ids[0], "does-not-exist", map[string]int{"Blue": 1}); !isIllegalAction(err) {
		t.Errorf("build station at an unknown city: err = %v, want engine.ErrIllegalAction", err)
	}
}

// TestStationOncePerTurn covers rules §6's "at most one station per turn":
// build_station ends the turn like every other action, so a second attempt
// by the same seat immediately after is rejected on turn order, not on any
// separate per-turn flag.
func TestStationOncePerTurn(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 6)
	gs, ids := newNormalState(m, 2)
	gs.Players[0].Hand = map[int32]int32{int32(ColorBlue): 2}
	gs.FaceUp = stableFaceUp()
	gs.DiscardPile = fillerCards(TotalTrainCards - 2 - len(gs.FaceUp))

	ngs, _, err := buildStation(t, e, gs, ids[0], "z", map[string]int{"Blue": 1})
	if err != nil {
		t.Fatalf("first station build: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)
	if ngs.CurrentSeat != 1 {
		t.Fatalf("current_seat = %d, want 1 (turn ended by the build)", ngs.CurrentSeat)
	}

	if _, _, err := buildStation(t, e, ngs, ids[0], "a", map[string]int{"Blue": 1}); !errors.Is(err, engine.ErrNotYourTurn) {
		t.Errorf("second station build in the same turn: err = %v, want engine.ErrNotYourTurn", err)
	}
}
