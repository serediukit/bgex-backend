// Package ttr implements Ticket to Ride: Europe as an engine.Engine. See
// rules/ticket_to_ride_europe.md for the full rules specification this
// package implements.
package ttr

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

const (
	// GameKey identifies TTR lobbies.
	GameKey = "ttr"

	minSeats = 2 // rules §3.1 PLAYERS_MIN
	maxSeats = 5 // rules §3.1 PLAYERS_MAX

	initialTrainCards     = 4 // rules §3.1 INITIAL_TRAIN_CARDS
	faceUpSlots           = 5 // rules §3.1 FACE_UP_SLOTS
	locoFlushThreshold    = 3 // rules §3.1 LOCOMOTIVE_FLUSH_THRESHOLD
	initialRegularTickets = 3 // rules §3.1 INITIAL_REGULAR_TICKETS
	minTicketsKeptAtSetup = 2 // rules §3.1 MIN_TICKETS_KEPT_AT_SETUP

	// maxFlushIterations bounds the §7.3 flush loop as a defensive guard
	// against the degenerate case where every train card outside players'
	// hands is part of the very set being flushed, so reshuffling cannot
	// change its composition — a scenario the rules do not address. In
	// ordinary play the loop stabilizes within one or two iterations.
	maxFlushIterations = 25
)

// Engine implements engine.Engine for Ticket to Ride. It deliberately does
// NOT implement engine.HandBased: a TTR lobby plays exactly one game.
type Engine struct {
	maps MapProvider
	sh   Shuffler
}

// New returns an Engine backed by maps and math/rand/v2's global source.
func New(maps MapProvider) *Engine {
	return &Engine{maps: maps, sh: DefaultShuffler{}}
}

// NewWithShuffler returns an Engine backed by maps and sh. Tests use this
// with NewSeededShuffler for deterministic setup.
func NewWithShuffler(maps MapProvider, sh Shuffler) *Engine {
	return &Engine{maps: maps, sh: sh}
}

// GameKey identifies TTR lobbies.
func (*Engine) GameKey() string { return GameKey }

// MinSeats is the fewest players a TTR game supports (rules §3.1).
func (*Engine) MinSeats() int { return minSeats }

// MaxSeats is the most players a TTR game supports (rules §3.1).
func (*Engine) MaxSeats() int { return maxSeats }

// InitState resolves the pinned map from cfg ("map_id", "map_version") via
// e.maps and builds the initial state per rules §5, in order: trains,
// stations and score (§5.1); the shuffled 110-card deck and 4-card initial
// hands (§5.2); the face-up layout with locomotive flush (§5.3, §7.3); the
// ticket split, deal and setup discard (§5.4-§5.6); the simultaneous
// setup-keep offer (§5.7); and a random first player (§5.8).
func (e *Engine) InitState(ctx context.Context, cfg map[string]any, seats []engine.SeatInit) ([]byte, []engine.Event, error) {
	mapID, version, err := mapConfigFrom(cfg)
	if err != nil {
		return nil, nil, err
	}

	m, err := e.maps.Get(ctx, mapID, version)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve map %s@%d: %w", mapID, version, err)
	}
	if len(seats) < m.Rules.Players.Min || len(seats) > m.Rules.Players.Max {
		return nil, nil, engine.ErrNotEnoughPlayers
	}

	gs := &pb.GameState{
		MapId:          mapID,
		MapVersion:     version,
		FinalTurnsLeft: -1,
	}
	if err := initPlayers(gs, m, seats); err != nil {
		return nil, nil, err
	}

	// §5.2 — shuffle the 110-card train deck, deal 4 to each player.
	deck := newShuffledTrainDeck(e.sh)
	gs.DrawPile = dealInitialHands(gs, deck)

	// §5.3 / §7.3 — flip 5 face-up, then flush while 3+ are locomotives. No
	// events are emitted for this pre-game shuffling (nil).
	e.fillFaceUp(gs, nil)
	e.flushFaceUpLocos(gs, nil)

	// §5.4-§5.6 — split, shuffle and deal the ticket decks.
	if err := e.dealTickets(gs, m); err != nil {
		return nil, nil, err
	}

	// §5.7 — every player answers the setup keep simultaneously.
	gs.Phase = pb.Phase_PHASE_SETUP_TICKETS

	// §5.8 — first player is random; play proceeds in seat order.
	gs.CurrentSeat = gs.Players[e.sh.IntN(len(gs.Players))].SeatIndex

	events := []engine.Event{{
		Type: "game_started",
		Data: map[string]any{"map_id": mapID, "map_version": version, "seats": len(gs.Players)},
	}}

	b, err := marshal(gs)
	if err != nil {
		return nil, nil, err
	}
	return b, events, nil
}

// initPlayers creates one PlayerState per seat with 45 trains / 3 stations /
// score 0 (rules §5.1), sorted by seat index.
func initPlayers(gs *pb.GameState, m *Map, seats []engine.SeatInit) error {
	trainsLeft, ok := int32FromInt(m.Rules.TrainsPerPlayer)
	if !ok {
		return fmt.Errorf("%w: rules.trains_per_player %d out of range", ErrInvalidMapDoc, m.Rules.TrainsPerPlayer)
	}
	stationsLeft, ok := int32FromInt(m.Rules.StationsPerPlayer)
	if !ok {
		return fmt.Errorf("%w: rules.stations_per_player %d out of range", ErrInvalidMapDoc, m.Rules.StationsPerPlayer)
	}

	players := make([]*pb.PlayerState, len(seats))
	for i, si := range seats {
		players[i] = &pb.PlayerState{
			SeatIndex:    int32(si.Seat), // #nosec G115 -- seat index is bounded by maxSeats (5)
			UserId:       si.UserID.String(),
			Hand:         make(map[int32]int32, len(CardColors)+1),
			TrainsLeft:   trainsLeft,
			StationsLeft: stationsLeft,
		}
	}
	slices.SortFunc(players, func(a, b *pb.PlayerState) int { return int(a.SeatIndex - b.SeatIndex) })
	gs.Players = players
	return nil
}

// int32FromInt safely converts n to int32, reporting false if n is out of
// range.
func int32FromInt(n int) (int32, bool) {
	if n < 0 || n > math.MaxInt32 {
		return 0, false
	}
	return int32(n), true
}

// newShuffledTrainDeck builds and shuffles the 110-card train deck (rules
// §3.2) using sh.
func newShuffledTrainDeck(sh Shuffler) []int32 {
	deck := make([]int32, 0, TotalTrainCards)
	for _, c := range CardColors {
		for range DeckComposition[c] {
			deck = append(deck, int32(c))
		}
	}
	for range DeckComposition[ColorLoco] {
		deck = append(deck, int32(ColorLoco))
	}
	sh.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	return deck
}

// dealInitialHands deals initialTrainCards cards to each player in seat
// order, one round at a time (rules §5.2), and returns what remains of deck
// as the new draw pile.
func dealInitialHands(gs *pb.GameState, deck []int32) []int32 {
	for range initialTrainCards {
		for _, p := range gs.Players {
			p.Hand[deck[0]]++
			deck = deck[1:]
		}
	}
	return deck
}

// fillFaceUp tops up the face-up layout to faceUpSlots cards, drawing from
// the draw pile and reshuffling the discard pile in as needed (rules §7.4).
// Fewer than faceUpSlots may remain if the draw and discard piles are both
// exhausted (rules §14). ev collects a "deck_reshuffled" event if a reshuffle
// happens along the way; pass nil to discard events (e.g. during InitState's
// pre-game setup, which has no players to broadcast to yet).
func (e *Engine) fillFaceUp(gs *pb.GameState, ev *[]engine.Event) {
	for len(gs.FaceUp) < faceUpSlots {
		e.refillDrawPile(gs, ev)
		if len(gs.DrawPile) == 0 {
			return
		}
		gs.FaceUp = append(gs.FaceUp, gs.DrawPile[0])
		gs.DrawPile = gs.DrawPile[1:]
	}
}

// refillDrawPile reshuffles the discard pile into the draw pile when the
// draw pile is empty but the discard pile is not (rules §7.4), emitting a
// "deck_reshuffled" event into ev (if non-nil).
func (e *Engine) refillDrawPile(gs *pb.GameState, ev *[]engine.Event) {
	if len(gs.DrawPile) > 0 || len(gs.DiscardPile) == 0 {
		return
	}
	gs.DrawPile, gs.DiscardPile = gs.DiscardPile, nil
	e.sh.Shuffle(len(gs.DrawPile), func(i, j int) { gs.DrawPile[i], gs.DrawPile[j] = gs.DrawPile[j], gs.DrawPile[i] })
	if ev != nil {
		*ev = append(*ev, engine.Event{Type: "deck_reshuffled", Data: map[string]any{"count": len(gs.DrawPile)}})
	}
}

// flushFaceUpLocos applies the locomotive-flush rule (rules §7.3): whenever
// the face-up layout changes, if 3 or more of its cards are locomotives,
// discard all of them and deal a fresh layout, repeating until stable (or
// the deck+discard cannot refill to faceUpSlots cards). ev collects a
// "face_up_flushed" event per flush (if non-nil); see fillFaceUp for the nil
// convention.
func (e *Engine) flushFaceUpLocos(gs *pb.GameState, ev *[]engine.Event) {
	for range maxFlushIterations {
		if countLocos(gs.FaceUp) < locoFlushThreshold {
			return
		}
		gs.DiscardPile = append(gs.DiscardPile, gs.FaceUp...)
		gs.FaceUp = nil
		e.fillFaceUp(gs, ev)
		if ev != nil {
			*ev = append(*ev, engine.Event{Type: "face_up_flushed"})
		}
		if len(gs.FaceUp) < faceUpSlots {
			return
		}
	}
}

// countLocos reports how many of faceUp's cards are locomotives.
func countLocos(faceUp []int32) int {
	n := 0
	for _, c := range faceUp {
		if pb.Color(c) == pb.Color_COLOR_LOCO {
			n++
		}
	}
	return n
}

// dealTickets implements rules §5.4-§5.6: split, shuffle and deal the long
// and regular ticket decks, offering each player's 4 dealt tickets via
// SetupTicketOffer for the §5.7 keep decision. Unallocated long tickets are
// discarded unseen (never written to any state field); unallocated regular
// tickets become the face-down ticket_deck.
func (e *Engine) dealTickets(gs *pb.GameState, m *Map) error {
	n := len(gs.Players)

	longIDs := slices.Clone(m.LongTicketIDs)
	if len(longIDs) < n {
		return fmt.Errorf("%w: map has %d long ticket(s), need at least %d for %d players", ErrInvalidMapDoc, len(longIDs), n, n)
	}
	e.sh.Shuffle(len(longIDs), func(i, j int) { longIDs[i], longIDs[j] = longIDs[j], longIDs[i] })
	for i, p := range gs.Players {
		p.SetupTicketOffer = append(p.SetupTicketOffer, longIDs[i])
	}

	regIDs := slices.Clone(m.RegularTicketIDs)
	needed := initialRegularTickets * n
	if len(regIDs) < needed {
		return fmt.Errorf("%w: map has %d regular ticket(s), need at least %d for %d players", ErrInvalidMapDoc, len(regIDs), needed, n)
	}
	e.sh.Shuffle(len(regIDs), func(i, j int) { regIDs[i], regIDs[j] = regIDs[j], regIDs[i] })
	for i, p := range gs.Players {
		offset := i * initialRegularTickets
		p.SetupTicketOffer = append(p.SetupTicketOffer, regIDs[offset:offset+initialRegularTickets]...)
	}
	gs.TicketDeck = regIDs[needed:]

	return nil
}

// mapConfigFrom extracts and validates the "map_id"/"map_version" lobby
// config keys InitState needs. In production the lobby's ConfigValidator
// seam (ttr.Session, a later step) normalizes config before InitState is
// ever called, so failures here indicate a genuinely malformed call.
func mapConfigFrom(cfg map[string]any) (mapID string, version int32, err error) {
	rawID, ok := cfg["map_id"]
	if !ok {
		return "", 0, fmt.Errorf("%w: missing %q", ErrInvalidConfig, "map_id")
	}
	mapID, ok = rawID.(string)
	if !ok || mapID == "" {
		return "", 0, fmt.Errorf("%w: %q must be a non-empty string", ErrInvalidConfig, "map_id")
	}

	rawVersion, ok := cfg["map_version"]
	if !ok {
		return "", 0, fmt.Errorf("%w: missing %q", ErrInvalidConfig, "map_version")
	}
	version, ok = toPositiveInt32(rawVersion)
	if !ok {
		return "", 0, fmt.Errorf("%w: %q must be a positive integer", ErrInvalidConfig, "map_version")
	}
	return mapID, version, nil
}

// toPositiveInt32 converts v to a positive int32, accepting the numeric
// types a JSONB-backed lobby config plausibly decodes to (float64 from
// encoding/json, or a plain int/int32/int64 set programmatically).
func toPositiveInt32(v any) (int32, bool) {
	switch n := v.(type) {
	case int32:
		return n, n > 0
	case int:
		if n <= 0 || n > math.MaxInt32 {
			return 0, false
		}
		return int32(n), true
	case int64:
		if n <= 0 || n > math.MaxInt32 {
			return 0, false
		}
		return int32(n), true
	case float64:
		if n <= 0 || n > math.MaxInt32 || n != float64(int64(n)) {
			return 0, false
		}
		return int32(n), true
	default:
		return 0, false
	}
}

// IsOver reports whether the game itself has concluded (phase ==
// PHASE_FINISHED).
func (*Engine) IsOver(state []byte) bool {
	gs, err := unmarshal(state)
	if err != nil {
		return false
	}
	return gs.Phase == pb.Phase_PHASE_FINISHED
}

// Apply validates and applies a player action: unmarshal, require the actor
// is seated, gate on the current phase, dispatch by action type, and bump
// the monotonic sequence counter on success.
func (e *Engine) Apply(state []byte, a engine.Action) ([]byte, []engine.Event, error) {
	gs, err := unmarshal(state)
	if err != nil {
		return nil, nil, err
	}

	p := playerByUser(gs, a.UserID.String())
	if p == nil {
		return nil, nil, engine.ErrNotSeated
	}

	if err := checkPhaseGate(gs, p, a); err != nil {
		return nil, nil, err
	}

	events, err := e.dispatch(gs, p, a)
	if err != nil {
		return nil, nil, err
	}
	gs.Seq++

	b, err := marshal(gs)
	if err != nil {
		return nil, nil, err
	}
	return b, events, nil
}

// resolveMap resolves gs's pinned (map_id, map_version) via e.maps for an
// in-flight Apply call. Apply, unlike InitState, has no context of its own;
// context.Background() is safe here because every MapProvider a running game
// uses (MapCache, or the static provider engine tests use) does no true I/O
// on this path — the map was already parsed once at InitState time and this
// is an in-process cache lookup, not a network call.
func (e *Engine) resolveMap(gs *pb.GameState) (*Map, error) {
	m, err := e.maps.Get(context.Background(), gs.MapId, gs.MapVersion)
	if err != nil {
		return nil, fmt.Errorf("resolve map %s@%d: %w", gs.MapId, gs.MapVersion, err)
	}
	return m, nil
}

// resolveWithMap resolves gs's pinned map and calls fn(m, &events), the
// "resolve the map, collect events, call, return" shape shared by every
// dispatch*/resolve_decision branch whose underlying apply function needs
// the map. Factoring it out keeps those call sites to one line each instead
// of repeating the same resolveMap-then-events boilerplate, which both kept
// applyResolveDecision's cyclomatic complexity down and avoided the (dupl)
// duplication two near-identical dispatch* bodies produced before this was
// extracted.
func (e *Engine) resolveWithMap(gs *pb.GameState, fn func(m *Map, ev *[]engine.Event) error) ([]engine.Event, error) {
	m, err := e.resolveMap(gs)
	if err != nil {
		return nil, err
	}
	var events []engine.Event
	if err := fn(m, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// checkPhaseGate enforces which action types are legal in gs's current
// phase, and whether p is the one allowed to act:
//   - PHASE_FINISHED: nothing but resign (handled below) is legal.
//   - resign: legal in any non-finished phase, from any seated player.
//   - any other action from a resigned player: always illegal (rules/plan
//     Q14 — a resigned player never acts again, including answering their
//     own still-outstanding §5.7 setup keep during PHASE_SETUP_TICKETS,
//     where applyResign deliberately leaves current_seat untouched; without
//     this check a resigned player who ends up holding current_seat could
//     deadlock the game — see C1 in the Step 11 review).
//   - PHASE_SETUP_TICKETS: only resolve_decision, turn order irrelevant
//     (per-player "already answered" is enforced by the handler).
//   - the AWAITING_* phases: only resolve_decision, from current_seat.
//   - PHASE_NORMAL: the four turn actions, from current_seat.
//   - anything else (e.g. PHASE_SCORING, or an unrecognized action type in a
//     phase that doesn't accept it): ErrWrongPhase.
func checkPhaseGate(gs *pb.GameState, p *pb.PlayerState, a engine.Action) error {
	if gs.Phase == pb.Phase_PHASE_FINISHED {
		return engine.ErrGameOver
	}
	if a.Type == ActionResign {
		return nil
	}
	if p.Resigned {
		return fmt.Errorf("%w: you have resigned and cannot act further", engine.ErrIllegalAction)
	}

	if gs.Phase == pb.Phase_PHASE_SETUP_TICKETS {
		if a.Type != ActionResolveDecision {
			return fmt.Errorf("%w: only resolve_decision is legal during ticket setup", engine.ErrWrongPhase)
		}
		return nil
	}

	// PHASE_AWAITING_SECOND_DRAW is unlike the other AWAITING_* phases: the
	// second card of Action A is submitted as another draw_card action (not
	// a resolve_decision), and resolve_decision{end_draw} is only the
	// voluntary "second draw is impossible" escape hatch (rules §7.1, §7.4).
	if gs.Phase == pb.Phase_PHASE_AWAITING_SECOND_DRAW {
		if a.Type != ActionDrawCard && a.Type != ActionResolveDecision {
			return fmt.Errorf("%w: only draw_card or resolve_decision{end_draw} is legal while a second draw is pending", engine.ErrWrongPhase)
		}
		return requireCurrentSeat(gs, p)
	}

	if isAwaitingPhase(gs.Phase) {
		if a.Type != ActionResolveDecision {
			return fmt.Errorf("%w: only resolve_decision is legal while a decision is pending", engine.ErrWrongPhase)
		}
		return requireCurrentSeat(gs, p)
	}

	if gs.Phase == pb.Phase_PHASE_NORMAL {
		if !isTurnAction(a.Type) {
			return fmt.Errorf("%w: %q is not a valid action in the normal phase", engine.ErrWrongPhase, a.Type)
		}
		return requireCurrentSeat(gs, p)
	}

	return fmt.Errorf("%w: no actions are legal in the current phase", engine.ErrWrongPhase)
}

// isAwaitingPhase reports whether ph is one of the AWAITING_* mid-decision
// phases, which all gate on current_seat the same way.
func isAwaitingPhase(ph pb.Phase) bool {
	return ph == pb.Phase_PHASE_AWAITING_SECOND_DRAW ||
		ph == pb.Phase_PHASE_AWAITING_TUNNEL ||
		ph == pb.Phase_PHASE_AWAITING_TICKET_KEEP
}

// isTurnAction reports whether actionType is one of the four PHASE_NORMAL
// turn actions (rules §6).
func isTurnAction(actionType string) bool {
	switch actionType {
	case ActionDrawCard, ActionClaimRoute, ActionBuildStation, ActionDrawTickets:
		return true
	default:
		return false
	}
}

// requireCurrentSeat returns engine.ErrNotYourTurn unless p occupies
// current_seat.
func requireCurrentSeat(gs *pb.GameState, p *pb.PlayerState) error {
	cur := playerBySeat(gs, gs.CurrentSeat)
	if cur == nil || cur.UserId != p.UserId {
		return engine.ErrNotYourTurn
	}
	return nil
}

// dispatch routes a to its handler once checkPhaseGate has approved it.
// resolve_decision, draw_card, claim_route, draw_tickets, build_station and
// resign are all implemented (Steps 7-11).
func (e *Engine) dispatch(gs *pb.GameState, p *pb.PlayerState, a engine.Action) ([]engine.Event, error) {
	switch a.Type {
	case ActionResolveDecision:
		return e.applyResolveDecision(gs, p, a)
	case ActionDrawCard:
		return e.dispatchDrawCard(gs, p, a)
	case ActionClaimRoute:
		return e.dispatchClaimRoute(gs, p, a)
	case ActionBuildStation:
		return e.dispatchBuildStation(gs, p, a)
	case ActionDrawTickets:
		return e.dispatchDrawTickets(gs, p)
	case ActionResign:
		return e.dispatchResign(gs, p)
	default:
		return nil, fmt.Errorf("%w: unknown action %q", engine.ErrIllegalAction, a.Type)
	}
}

// dispatchDrawCard decodes and applies one ActionDrawCard, split out of
// dispatch purely to keep dispatch itself a flat switch (gocyclo).
func (e *Engine) dispatchDrawCard(gs *pb.GameState, p *pb.PlayerState, a engine.Action) ([]engine.Event, error) {
	payload, err := decodePayload[DrawCardPayload](a.Payload)
	if err != nil {
		return nil, err
	}
	return e.resolveWithMap(gs, func(m *Map, ev *[]engine.Event) error {
		return e.applyDrawCard(m, gs, p, payload, ev)
	})
}

// dispatchResign applies one ActionResign, split out of dispatch purely to
// keep dispatch itself a flat switch (gocyclo). resign carries no payload
// fields (rules/plan Q14), so unlike its siblings above there is nothing to
// decode.
func (e *Engine) dispatchResign(gs *pb.GameState, p *pb.PlayerState) ([]engine.Event, error) {
	return e.resolveWithMap(gs, func(m *Map, ev *[]engine.Event) error {
		return e.applyResign(m, gs, p, ev)
	})
}

// dispatchClaimRoute decodes and applies one ActionClaimRoute, split out of
// dispatch purely to keep dispatch itself a flat switch (gocyclo).
func (e *Engine) dispatchClaimRoute(gs *pb.GameState, p *pb.PlayerState, a engine.Action) ([]engine.Event, error) {
	payload, err := decodePayload[ClaimRoutePayload](a.Payload)
	if err != nil {
		return nil, err
	}
	var events []engine.Event
	if err := e.applyClaimRoute(gs, p, payload, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// dispatchBuildStation decodes and applies one ActionBuildStation, split out
// of dispatch purely to keep dispatch itself a flat switch (gocyclo).
func (e *Engine) dispatchBuildStation(gs *pb.GameState, p *pb.PlayerState, a engine.Action) ([]engine.Event, error) {
	payload, err := decodePayload[BuildStationPayload](a.Payload)
	if err != nil {
		return nil, err
	}
	return e.resolveWithMap(gs, func(m *Map, ev *[]engine.Event) error {
		return e.applyBuildStation(m, gs, p, payload, ev)
	})
}

// dispatchDrawTickets applies one ActionDrawTickets. draw_tickets carries no
// payload fields, so unlike its siblings above there is nothing to decode.
func (e *Engine) dispatchDrawTickets(gs *pb.GameState, p *pb.PlayerState) ([]engine.Event, error) {
	var events []engine.Event
	if err := e.applyDrawTickets(gs, p, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// applyResolveDecision decodes the payload and routes it by which pending
// decision gs.Phase says is open: setup tickets (rules §5.7), the tunnel
// surcharge (§8.4), the in-game ticket keep (§9.2-§9.4) and the voluntary
// end-draw escape hatch (§7.1, §7.4) are all implemented.
func (e *Engine) applyResolveDecision(gs *pb.GameState, p *pb.PlayerState, a engine.Action) ([]engine.Event, error) {
	payload, err := decodePayload[ResolveDecisionPayload](a.Payload)
	if err != nil {
		return nil, err
	}

	if gs.Phase == pb.Phase_PHASE_SETUP_TICKETS {
		if payload.Kind != DecisionKindSetupTickets {
			return nil, fmt.Errorf("%w: expected decision kind %q, got %q", engine.ErrWrongPhase, DecisionKindSetupTickets, payload.Kind)
		}
		return e.applySetupTicketsDecision(gs, p, payload)
	}
	if gs.Phase == pb.Phase_PHASE_AWAITING_TICKET_KEEP {
		if payload.Kind != DecisionKindTicketKeep {
			return nil, fmt.Errorf("%w: expected decision kind %q, got %q", engine.ErrWrongPhase, DecisionKindTicketKeep, payload.Kind)
		}
		return e.resolveWithMap(gs, func(m *Map, ev *[]engine.Event) error {
			return e.resolveTicketKeep(m, gs, p, payload.KeepTicketIDs, ev)
		})
	}
	if gs.Phase == pb.Phase_PHASE_AWAITING_TUNNEL {
		if payload.Kind != DecisionKindTunnel {
			return nil, fmt.Errorf("%w: expected decision kind %q, got %q", engine.ErrWrongPhase, DecisionKindTunnel, payload.Kind)
		}
		return e.resolveWithMap(gs, func(m *Map, ev *[]engine.Event) error {
			return e.resolveTunnel(m, gs, p, payload, ev)
		})
	}
	if gs.Phase == pb.Phase_PHASE_AWAITING_SECOND_DRAW {
		if payload.Kind != DecisionKindEndDraw {
			return nil, fmt.Errorf("%w: expected decision kind %q, got %q", engine.ErrWrongPhase, DecisionKindEndDraw, payload.Kind)
		}
		return e.resolveWithMap(gs, func(m *Map, ev *[]engine.Event) error {
			return e.applyEndDrawDecision(m, gs, p, ev)
		})
	}
	return nil, fmt.Errorf("%w: no decision is pending", engine.ErrWrongPhase)
}

// applySetupTicketsDecision implements rules §5.7: p secretly keeps >= 2 of
// its 4 dealt tickets. Kept tickets append to TicketIds; rejects are dropped
// from the game entirely (they do NOT return to ticket_deck, unlike an
// in-game §9 reject). Once every non-resigned player has answered, the game
// moves to PHASE_NORMAL without advancing current_seat.
func (e *Engine) applySetupTicketsDecision(gs *pb.GameState, p *pb.PlayerState, payload ResolveDecisionPayload) ([]engine.Event, error) {
	if p.SetupDone {
		return nil, fmt.Errorf("%w: you have already made your setup ticket keep", engine.ErrIllegalAction)
	}
	if len(payload.KeepTicketIDs) < minTicketsKeptAtSetup {
		return nil, fmt.Errorf("%w: must keep at least %d tickets, got %d", engine.ErrIllegalAction, minTicketsKeptAtSetup, len(payload.KeepTicketIDs))
	}

	offered := make(map[int32]bool, len(p.SetupTicketOffer))
	for _, id := range p.SetupTicketOffer {
		offered[id] = true
	}
	kept := make(map[int32]bool, len(payload.KeepTicketIDs))
	for _, id := range payload.KeepTicketIDs {
		if !offered[id] {
			return nil, fmt.Errorf("%w: ticket %d was not offered to you", engine.ErrIllegalAction, id)
		}
		if kept[id] {
			return nil, fmt.Errorf("%w: duplicate ticket id %d", engine.ErrIllegalAction, id)
		}
		kept[id] = true
	}

	p.TicketIds = append(p.TicketIds, payload.KeepTicketIDs...)
	p.SetupTicketOffer = nil
	p.SetupDone = true

	events := []engine.Event{{
		Type: "setup_tickets_kept",
		Data: map[string]any{"seat": p.SeatIndex, "count": len(payload.KeepTicketIDs)},
	}}

	if allSetupDone(gs) {
		gs.Phase = pb.Phase_PHASE_NORMAL
		gs.TurnNo = 1
		// current_seat was assigned at InitState time (§5.8), before any
		// resignation could happen, so it may point at a player who has
		// since resigned during setup — applyResign deliberately does not
		// perturb current_seat while PHASE_SETUP_TICKETS is active. Advance
		// past them now, or PHASE_NORMAL would start with current_seat held
		// by a resigned player that no other seat can ever wrest control
		// from (C1 in the Step 11 review). nextActiveSeat's documented
		// fallback already returns the first active seat when its seat
		// argument is not itself an active seat.
		if cur := playerBySeat(gs, gs.CurrentSeat); cur == nil || cur.Resigned {
			gs.CurrentSeat = nextActiveSeat(gs, gs.CurrentSeat)
		}
		events = append(events, engine.Event{
			Type: "setup_complete",
			Data: map[string]any{"current_seat": gs.CurrentSeat},
		})
	}

	return events, nil
}

// allSetupDone reports whether every non-resigned player has answered the
// §5.7 setup ticket keep.
func allSetupDone(gs *pb.GameState) bool {
	for _, p := range activePlayers(gs) {
		if !p.SetupDone {
			return false
		}
	}
	return true
}
