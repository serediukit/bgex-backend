package ttr

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	"github.com/serediukit/bgex-backend/internal/games/ttr/mapdata"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// testMap returns a small, hand-authored board fixture reused by every TTR
// engine test from this step onward (Steps 8-13 all build on it), so its ids
// are stable and documented here rather than re-derived per test.
//
// Cities (id = lowercase letter, name = uppercase): a, b, c, d, e, f, g, h.
//
// City "z" was added additively in Step 10 (engine_station_test.go): it has
// no incident routes at all, exercising the rules §10.1 "a city may be
// routeless" station-target rule without touching any existing city or
// route id.
//
// Routes (id: cities  color  length  flags):
//
//	 1: a-b  Red     2
//	 2: b-c  Gray    3
//	 3: a-c  Blue    4   tunnel
//	 4: c-d  Green   1
//	 5: d-e  Yellow  2
//	 6: e-f  Gray    6   ferry (2 locomotives)
//	 7: f-g  Black   3
//	 8: g-h  White   2
//	 9: a-h  Purple  3   paired with 10
//	10: a-h  Orange  3   paired with 9
//
// The graph is fully connected: a-b-c form a triangle, then c-d-e-f-g-h
// chains back to a via the double a-h pair.
//
// Routes 11-15 were added additively in Step 9 (engine_claim_test.go) for
// route shapes rules 1-10 don't cover, without touching any existing id:
//
//	11: b-g  Blue    3
//	12: d-g  Gray    2
//	13: e-h  Gray    2   tunnel
//	14: b-f  Black   8
//	15: c-g  White   4
//
// Tickets: 6 long (101-106) + 10 regular (1-10) = 16 total.
//
// Long tickets (id: cities  points):
//
//	101: a-f  20
//	102: b-g  18
//	103: c-h  15
//	104: d-a  12
//	105: e-b  14
//	106: f-h  10
//
// Regular tickets (id: cities  points):
//
//	 1: a-b  2
//	 2: a-c  3
//	 3: b-d  4
//	 4: c-e  5
//	 5: d-f  6
//	 6: e-g  5
//	 7: f-h  4
//	 8: g-a  3
//	 9: h-b  6
//	10: c-g  7
//
// rules.players is deliberately narrowed to {min:2, max:3} — unlike Europe's
// 2-5 — so this fixture's modest 10-regular-ticket deck (need 3*max_players)
// stays sufficient and so setup tests here directly exercise the §8.5 2-3p
// double-route sibling closure invariant. Larger player counts (4-5) are
// covered separately against the real EuropeV1 map (see
// TestSetupWithEuropeMap).
const (
	testMapID      = "11111111-1111-1111-1111-111111111111"
	testMapVersion = int32(1)
)

type routeFixture struct {
	id     int32
	a, b   string
	color  string
	length int
	tunnel bool
	locos  int
	pair   *int32
}

type ticketFixture struct {
	id     int32
	a, b   string
	points int
	long   bool
}

func testMap(t *testing.T) *Map {
	t.Helper()

	nine, ten := int32(9), int32(10)
	routes := []routeFixture{
		{id: 1, a: "a", b: "b", color: "Red", length: 2},
		{id: 2, a: "b", b: "c", color: "Gray", length: 3},
		{id: 3, a: "a", b: "c", color: "Blue", length: 4, tunnel: true},
		{id: 4, a: "c", b: "d", color: "Green", length: 1},
		{id: 5, a: "d", b: "e", color: "Yellow", length: 2},
		{id: 6, a: "e", b: "f", color: "Gray", length: 6, locos: 2},
		{id: 7, a: "f", b: "g", color: "Black", length: 3},
		{id: 8, a: "g", b: "h", color: "White", length: 2},
		{id: 9, a: "a", b: "h", color: "Purple", length: 3, pair: &ten},
		{id: 10, a: "a", b: "h", color: "Orange", length: 3, pair: &nine},
		// Additive Step 9 routes (engine_claim_test.go) — see the doc comment
		// above. None of these have a pair or reuse an id 1-10 depended on by
		// earlier steps.
		{id: 11, a: "b", b: "g", color: "Blue", length: 3},
		{id: 12, a: "d", b: "g", color: "Gray", length: 2},
		{id: 13, a: "e", b: "h", color: "Gray", length: 2, tunnel: true},
		{id: 14, a: "b", b: "f", color: "Black", length: 8},
		{id: 15, a: "c", b: "g", color: "White", length: 4},
	}

	tickets := []ticketFixture{
		{id: 101, a: "a", b: "f", points: 20, long: true},
		{id: 102, a: "b", b: "g", points: 18, long: true},
		{id: 103, a: "c", b: "h", points: 15, long: true},
		{id: 104, a: "d", b: "a", points: 12, long: true},
		{id: 105, a: "e", b: "b", points: 14, long: true},
		{id: 106, a: "f", b: "h", points: 10, long: true},
		{id: 1, a: "a", b: "b", points: 2},
		{id: 2, a: "a", b: "c", points: 3},
		{id: 3, a: "b", b: "d", points: 4},
		{id: 4, a: "c", b: "e", points: 5},
		{id: 5, a: "d", b: "f", points: 6},
		{id: 6, a: "e", b: "g", points: 5},
		{id: 7, a: "f", b: "h", points: 4},
		{id: 8, a: "g", b: "a", points: 3},
		{id: 9, a: "h", b: "b", points: 6},
		{id: 10, a: "c", b: "g", points: 7},
	}

	cityIDs := []string{"a", "b", "c", "d", "e", "f", "g", "h", "z"}
	cities := make([]any, len(cityIDs))
	layoutCities := make(map[string]any, len(cityIDs))
	for i, id := range cityIDs {
		cities[i] = map[string]any{"id": id, "name": strings.ToUpper(id)}
		layoutCities[id] = map[string]any{"x": float64(i) / float64(len(cityIDs)-1), "y": 0.5}
	}

	routeDocs := make([]any, len(routes))
	layoutRoutes := make(map[string]any, len(routes))
	for i, r := range routes {
		var pair any
		if r.pair != nil {
			pair = *r.pair
		}
		routeDocs[i] = map[string]any{
			"id": r.id, "a": r.a, "b": r.b, "color": r.color,
			"length": r.length, "tunnel": r.tunnel, "locos": r.locos, "pair": pair,
		}
		layoutRoutes[strconv.Itoa(int(r.id))] = map[string]any{"slots": genSlots(r.length)}
	}

	ticketDocs := make([]any, len(tickets))
	for i, tk := range tickets {
		ticketDocs[i] = map[string]any{"id": tk.id, "a": tk.a, "b": tk.b, "points": tk.points, "long": tk.long}
	}

	doc := map[string]any{
		"schema_version": 1,
		"name":           "Test Map",
		"rules": map[string]any{
			"players":             map[string]any{"min": 2, "max": 3},
			"trains_per_player":   45,
			"stations_per_player": 3,
			"long_tickets":        6,
			"regular_tickets":     10,
			"cities":              cities,
			"routes":              routeDocs,
			"tickets":             ticketDocs,
		},
		"layout": map[string]any{
			"view_box":   map[string]any{"width": 1000, "height": 800},
			"background": map[string]any{"asset_id": nil, "width": 1000, "height": 800},
			"cities":     layoutCities,
			"routes":     layoutRoutes,
			"slot":       map[string]any{"width": 10, "height": 5, "corner_radius": 1},
		},
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal test map doc: %v", err)
	}
	m, err := ParseMap(raw)
	if err != nil {
		t.Fatalf("parse test map doc: %v", err)
	}
	return m
}

// genSlots returns n normalized (0..1) train-car slots, enough to satisfy
// ParseMap's "len(slots) == route.length" layout check. Their exact
// placement is irrelevant to engine tests.
func genSlots(n int) []any {
	slots := make([]any, n)
	for i := range n {
		slots[i] = map[string]any{"x": 0.5, "y": 0.5, "angle": 0}
	}
	return slots
}

// newTestEngine wires up an Engine over m, pinned at (testMapID,
// testMapVersion), with a deterministic seeded Shuffler.
func newTestEngine(m *Map, seed uint64) *Engine {
	provider := NewStaticMapProvider(m, testMapID, testMapVersion)
	return NewWithShuffler(provider, NewSeededShuffler(seed))
}

// seatInits builds n distinct seated players, seat indices 0..n-1.
func seatInits(n int) ([]engine.SeatInit, []uuid.UUID) {
	inits := make([]engine.SeatInit, n)
	ids := make([]uuid.UUID, n)
	for i := range n {
		ids[i] = uuid.New()
		inits[i] = engine.SeatInit{Seat: i, UserID: ids[i]}
	}
	return inits, ids
}

func testCfg() map[string]any {
	return map[string]any{"map_id": testMapID, "map_version": testMapVersion}
}

func mustInitState(t *testing.T, e *Engine, n int) ([]byte, []uuid.UUID) {
	t.Helper()
	inits, ids := seatInits(n)
	state, _, err := e.InitState(t.Context(), testCfg(), inits)
	if err != nil {
		t.Fatalf("init state: %v", err)
	}
	return state, ids
}

func mustGameState(t *testing.T, state []byte) *pb.GameState {
	t.Helper()
	gs, err := unmarshal(state)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return gs
}

// resolveSetupTickets applies a resolve_decision{setup_tickets} action for
// userID, keeping the given ticket ids.
func resolveSetupTickets(t *testing.T, e *Engine, state []byte, userID uuid.UUID, keep []int32) ([]byte, []engine.Event, error) {
	t.Helper()
	raw, err := json.Marshal(ResolveDecisionPayload{Kind: DecisionKindSetupTickets, KeepTicketIDs: keep})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return e.Apply(state, engine.Action{UserID: userID, Type: ActionResolveDecision, Payload: raw})
}

// isIllegalAction reports whether err wraps engine.ErrIllegalAction.
func isIllegalAction(err error) bool {
	return errors.Is(err, engine.ErrIllegalAction)
}

func TestSetupDealsHandsAndTicketOffers(t *testing.T) {
	m := testMap(t)

	for _, n := range []int{2, 3} {
		t.Run(strconv.Itoa(n)+"_players", func(t *testing.T) {
			e := newTestEngine(m, 42)
			state, _ := mustInitState(t, e, n)
			gs := mustGameState(t, state)
			assertInvariants(t, m, gs)

			assertSetupPhaseShape(t, gs, n)
			assertHandsAndTicketOffers(t, m, gs, n)
			assertSetupDeckAccounting(t, gs, n)
			assertSetupTicketDeck(t, m, gs, n)
		})
	}
}

// assertSetupPhaseShape checks the phase/turn bookkeeping InitState must
// leave behind (rules §5.7-§5.8).
func assertSetupPhaseShape(t *testing.T, gs *pb.GameState, n int) {
	t.Helper()
	if gs.Phase != pb.Phase_PHASE_SETUP_TICKETS {
		t.Fatalf("phase = %v, want PHASE_SETUP_TICKETS", gs.Phase)
	}
	if gs.FinalTurnsLeft != -1 {
		t.Errorf("final_turns_left = %d, want -1", gs.FinalTurnsLeft)
	}
	if gs.TurnNo != 0 {
		t.Errorf("turn_no = %d, want 0 (not yet in PHASE_NORMAL)", gs.TurnNo)
	}
	if len(gs.Players) != n {
		t.Fatalf("len(players) = %d, want %d", len(gs.Players), n)
	}
}

// assertHandsAndTicketOffers checks that every player was dealt
// initialTrainCards train cards and exactly 4 distinct setup ticket offers
// (1 long + 3 regular), with no ticket offered twice (rules §5.2, §5.5-§5.6).
func assertHandsAndTicketOffers(t *testing.T, m *Map, gs *pb.GameState, n int) {
	t.Helper()

	allOffered := make(map[int32]bool)
	for _, p := range gs.Players {
		handTotal := 0
		for _, c := range p.Hand {
			handTotal += int(c)
		}
		if handTotal != initialTrainCards {
			t.Errorf("seat %d hand size = %d, want %d", p.SeatIndex, handTotal, initialTrainCards)
		}
		if len(p.SetupTicketOffer) != 4 {
			t.Errorf("seat %d setup_ticket_offer size = %d, want 4", p.SeatIndex, len(p.SetupTicketOffer))
		}

		longCount := 0
		for _, id := range p.SetupTicketOffer {
			if allOffered[id] {
				t.Errorf("ticket %d offered to more than one seat", id)
			}
			allOffered[id] = true
			if tk := m.TicketByID[id]; tk != nil && tk.Long {
				longCount++
			}
		}
		if longCount != 1 {
			t.Errorf("seat %d offered %d long tickets, want exactly 1", p.SeatIndex, longCount)
		}
	}
	if len(allOffered) != 4*n {
		t.Errorf("total distinct tickets offered = %d, want %d", len(allOffered), 4*n)
	}
}

// assertSetupDeckAccounting checks that draw+discard+face_up conserves
// 110-4n cards and that the face-up row is full and loco-safe (rules
// §5.2-§5.3, §7.3).
func assertSetupDeckAccounting(t *testing.T, gs *pb.GameState, n int) {
	t.Helper()

	outsideHands := len(gs.DrawPile) + len(gs.DiscardPile) + len(gs.FaceUp)
	wantOutside := TotalTrainCards - initialTrainCards*n
	if outsideHands != wantOutside {
		t.Errorf("draw+discard+face_up = %d, want %d (110 - %d*%d)", outsideHands, wantOutside, initialTrainCards, n)
	}
	if len(gs.FaceUp) != faceUpSlots {
		t.Errorf("len(face_up) = %d, want %d", len(gs.FaceUp), faceUpSlots)
	}
	if got := countLocos(gs.FaceUp); got >= locoFlushThreshold {
		t.Errorf("face_up locomotives = %d, want < %d", got, locoFlushThreshold)
	}
}

// assertSetupTicketDeck checks that ticket_deck holds only regular tickets,
// sized regular_tickets-3n, and never a long ticket (rules §5.5-§5.6:
// unallocated long tickets are discarded unseen, never queued).
func assertSetupTicketDeck(t *testing.T, m *Map, gs *pb.GameState, n int) {
	t.Helper()

	for _, id := range gs.TicketDeck {
		if tk := m.TicketByID[id]; tk != nil && tk.Long {
			t.Errorf("ticket_deck contains long ticket %d, long tickets must never reach it", id)
		}
	}
	wantDeck := m.Rules.RegularTickets - 3*n
	if len(gs.TicketDeck) != wantDeck {
		t.Errorf("len(ticket_deck) = %d, want %d (%d - 3*%d)", len(gs.TicketDeck), wantDeck, m.Rules.RegularTickets, n)
	}
}

func TestSetupKeepRejectsFewerThanTwo(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 7)
	state, ids := mustInitState(t, e, 2)
	gs := mustGameState(t, state)

	p := gs.Players[0]
	_, _, err := resolveSetupTickets(t, e, state, ids[0], p.SetupTicketOffer[:1])
	if !isIllegalAction(err) {
		t.Fatalf("keeping 1 ticket: err = %v, want engine.ErrIllegalAction", err)
	}

	next, _, err := resolveSetupTickets(t, e, state, ids[0], p.SetupTicketOffer[:2])
	if err != nil {
		t.Fatalf("keeping 2 tickets: unexpected error: %v", err)
	}
	ngs := mustGameState(t, next)
	if !playerBySeat(ngs, p.SeatIndex).SetupDone {
		t.Errorf("seat %d setup_done = false after a legal keep", p.SeatIndex)
	}
	assertInvariants(t, m, ngs)

	// Answering again before the other player has answered is still illegal
	// (already answered), even though the phase hasn't flipped yet.
	if _, _, err := resolveSetupTickets(t, e, next, ids[0], p.SetupTicketOffer[2:4]); !isIllegalAction(err) {
		t.Errorf("re-answering setup: err = %v, want engine.ErrIllegalAction", err)
	}
}

func TestSetupPhaseFlipsOnlyAfterEveryoneAnswers(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 99)
	state, ids := mustInitState(t, e, 3)
	initialSeat := mustGameState(t, state).CurrentSeat

	// Answer out of seat order to prove turn order is irrelevant during
	// setup (rules §5.7).
	answerOrder := []int{2, 0, 1}
	for i, seatIdx := range answerOrder {
		gs := mustGameState(t, state)
		p := playerByUser(gs, ids[seatIdx].String())
		if p == nil {
			t.Fatalf("seat for user %d not found", seatIdx)
		}
		next, _, err := resolveSetupTickets(t, e, state, ids[seatIdx], p.SetupTicketOffer[:2])
		if err != nil {
			t.Fatalf("resolve setup tickets for seat %d: %v", p.SeatIndex, err)
		}
		state = next
		ngs := mustGameState(t, state)
		assertInvariants(t, m, ngs)

		wantPhase := pb.Phase_PHASE_SETUP_TICKETS
		if i == len(answerOrder)-1 {
			wantPhase = pb.Phase_PHASE_NORMAL
		}
		if ngs.Phase != wantPhase {
			t.Fatalf("after %d/%d answers: phase = %v, want %v", i+1, len(answerOrder), ngs.Phase, wantPhase)
		}
	}

	final := mustGameState(t, state)
	if final.TurnNo != 1 {
		t.Errorf("turn_no = %d, want 1 once PHASE_NORMAL starts", final.TurnNo)
	}
	// Setup must not have moved the randomly-chosen first player (rules §5.7
	// "does not advance current_seat").
	if final.CurrentSeat != initialSeat {
		t.Errorf("current_seat = %d, want unchanged %d", final.CurrentSeat, initialSeat)
	}

	// Once PHASE_NORMAL has started, resolve_decision is no longer a legal
	// action type at all (there is no pending decision).
	p := playerBySeat(final, final.CurrentSeat)
	if _, _, err := resolveSetupTickets(t, e, state, uuid.MustParse(p.UserId), p.TicketIds[:2]); !errors.Is(err, engine.ErrWrongPhase) {
		t.Errorf("resolve_decision in PHASE_NORMAL: err = %v, want engine.ErrWrongPhase", err)
	}
}

func TestSetupWithEuropeMap(t *testing.T) {
	m, err := ParseMap(mapdata.EuropeV1)
	if err != nil {
		t.Fatalf("parse EuropeV1: %v", err)
	}

	const n = 4
	provider := NewStaticMapProvider(m, mapdata.EuropeMapID, mapdata.EuropeVersion)
	e := NewWithShuffler(provider, NewSeededShuffler(1234))

	inits, _ := seatInits(n)
	cfg := map[string]any{"map_id": mapdata.EuropeMapID, "map_version": mapdata.EuropeVersion}
	state, _, err := e.InitState(t.Context(), cfg, inits)
	if err != nil {
		t.Fatalf("init state: %v", err)
	}
	gs := mustGameState(t, state)
	assertInvariants(t, m, gs)

	if len(gs.Players) != n {
		t.Fatalf("len(players) = %d, want %d", len(gs.Players), n)
	}
	for _, p := range gs.Players {
		handTotal := 0
		for _, c := range p.Hand {
			handTotal += int(c)
		}
		if handTotal != initialTrainCards {
			t.Errorf("seat %d hand size = %d, want %d", p.SeatIndex, handTotal, initialTrainCards)
		}
		if len(p.SetupTicketOffer) != 4 {
			t.Errorf("seat %d setup_ticket_offer size = %d, want 4", p.SeatIndex, len(p.SetupTicketOffer))
		}
	}
	if len(gs.FaceUp) != faceUpSlots {
		t.Errorf("len(face_up) = %d, want %d", len(gs.FaceUp), faceUpSlots)
	}
	wantDeck := m.Rules.RegularTickets - 3*n
	if len(gs.TicketDeck) != wantDeck {
		t.Errorf("len(ticket_deck) = %d, want %d", len(gs.TicketDeck), wantDeck)
	}
}

func TestPaymentToColors(t *testing.T) {
	got, err := paymentToColors(map[string]int{"Red": 2, "Locomotive": 1})
	if err != nil {
		t.Fatalf("paymentToColors: unexpected error: %v", err)
	}
	if got[ColorRed] != 2 || got[ColorLoco] != 1 {
		t.Errorf("paymentToColors = %v, want {Red:2, Locomotive:1}", got)
	}

	if _, err := paymentToColors(map[string]int{"Gray": 1}); err == nil {
		t.Error("paymentToColors accepted Gray, a route-only color")
	}
	if _, err := paymentToColors(map[string]int{"Neon": 1}); err == nil {
		t.Error("paymentToColors accepted an unknown color name")
	}
	if _, err := paymentToColors(map[string]int{"Red": 0}); err == nil {
		t.Error("paymentToColors accepted a non-positive count")
	}
}

func TestDecodePayloadRejectsUnknownFields(t *testing.T) {
	raw := json.RawMessage(`{"kind":"setup_tickets","keep_ticket_ids":[1,2],"bogus":true}`)
	if _, err := decodePayload[ResolveDecisionPayload](raw); err == nil {
		t.Error("decodePayload accepted an unknown field")
	}

	raw = json.RawMessage(`{"kind":"setup_tickets","keep_ticket_ids":[1,2]}`)
	v, err := decodePayload[ResolveDecisionPayload](raw)
	if err != nil {
		t.Fatalf("decodePayload: unexpected error: %v", err)
	}
	if v.Kind != DecisionKindSetupTickets || len(v.KeepTicketIDs) != 2 {
		t.Errorf("decodePayload = %+v, want kind=setup_tickets, 2 ticket ids", v)
	}
}
