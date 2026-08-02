package ttr

import (
	"fmt"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// stationCost returns the rules §10.2 cost, in cards, of p's next station:
// the 1st costs 1, the 2nd costs 2, the 3rd costs 3 (for the standard
// stations_per_player = 3; a map with a different count scales the same way,
// counting down from the top).
func stationCost(p *pb.PlayerState, m *Map) int {
	return m.Rules.StationsPerPlayer - int(p.StationsLeft) + 1
}

// applyBuildStation implements rules §10.1-§10.2 for one build_station
// action: the target city must exist on the map (it may be routeless) and
// hold no station yet, regardless of which player would own it; p must have
// a station left to place; and the payment must be exactly stationCost(p, m)
// cards of one single colour, with locomotive substitution (an all-
// locomotive payment is legal; a cost-1 station therefore accepts any single
// card). Building a station awards no immediate points (rules §10.2,
// deferred to §13.5 scoring) and ends the turn.
func (e *Engine) applyBuildStation(m *Map, gs *pb.GameState, p *pb.PlayerState, pl BuildStationPayload, ev *[]engine.Event) error {
	if _, ok := m.CityByID[pl.CityID]; !ok {
		return fmt.Errorf("%w: unknown city %q", engine.ErrIllegalAction, pl.CityID)
	}
	if _, taken := gs.StationOwner[pl.CityID]; taken {
		return fmt.Errorf("%w: city %q already has a station", engine.ErrIllegalAction, pl.CityID)
	}
	if p.StationsLeft < 1 {
		return fmt.Errorf("%w: no stations left to build", engine.ErrIllegalAction)
	}

	cost := stationCost(p, m)
	pay, err := paymentToColors(pl.Payment)
	if err != nil {
		return err
	}
	if _, err := validateSingleColourPayment(p.Hand, pay, cost); err != nil {
		return err
	}

	for c, n := range pay {
		debitHandCount(p, c, n)
	}
	discardPay(gs, pay)

	p.StationsLeft--
	p.StationCities = append(p.StationCities, pl.CityID)
	if gs.StationOwner == nil {
		gs.StationOwner = make(map[string]int32, 1)
	}
	gs.StationOwner[pl.CityID] = p.SeatIndex

	if ev != nil {
		*ev = append(*ev, engine.Event{
			Type: "station_built",
			Data: map[string]any{"seat": p.SeatIndex, "city_id": pl.CityID, "cost": cost},
		})
	}

	return e.endTurn(m, gs, p, ev)
}
