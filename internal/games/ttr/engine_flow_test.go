package ttr

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// This file covers Step 11: turn advancement (nextActiveSeat), the §11
// end-game trigger and its "checked once, does not re-arm" property,
// resignation (plan Q14), and the transition into scoring.
//
// gs.Results in every test below comes from finalizeGame's Step-12
// placeholder (turnflow.go: placeholderScoreBreakdown) — it only guarantees
// one ScoreBreakdown per player with a correct seat_index and resigned
// players zeroed/ranked last. Real route/ticket/station/longest-path point
// totals arrive in Step 12 with their own scoring_test.go; nothing here
// asserts a real score.

// callEndTurn invokes e.endTurn directly against gs (in place) for the
// acting player p and returns whatever events it emitted. Calling endTurn
// directly, rather than round-tripping through an Apply-dispatched action,
// lets these scenarios drive the §11 trigger/countdown precisely (e.g.
// forcing a specific trains_left) without needing a fully legal action to
// produce it.
func callEndTurn(t *testing.T, e *Engine, m *Map, gs *pb.GameState, p *pb.PlayerState) []engine.Event {
	t.Helper()
	var events []engine.Event
	if err := e.endTurn(m, gs, p, &events); err != nil {
		t.Fatalf("endTurn: unexpected error: %v", err)
	}
	return events
}

// hasEvent reports whether events contains one of type evType.
func hasEvent(events []engine.Event, evType string) bool {
	for _, ev := range events {
		if ev.Type == evType {
			return true
		}
	}
	return false
}

func TestFlowEndTurnArmsFinalRoundAtTwoTrainsLeft(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, _ := newNormalState(m, 3)
	p := gs.Players[0]
	p.TrainsLeft = 2

	events := callEndTurn(t, e, m, gs, p)

	if gs.FinalTurnsLeft != int32(len(gs.Players)) {
		t.Errorf("final_turns_left = %d, want %d (arm only — the triggering call must not also decrement)",
			gs.FinalTurnsLeft, len(gs.Players))
	}
	if !hasEvent(events, "final_round_started") {
		t.Error("expected a final_round_started event")
	}
}

func TestFlowEndTurnArmsFinalRoundAtZeroTrainsLeft(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 2)
	gs, _ := newNormalState(m, 2)
	p := gs.Players[1]
	p.TrainsLeft = 0

	events := callEndTurn(t, e, m, gs, p)

	if gs.FinalTurnsLeft != int32(len(gs.Players)) {
		t.Errorf("final_turns_left = %d, want %d (arm only — the triggering call must not also decrement)",
			gs.FinalTurnsLeft, len(gs.Players))
	}
	if !hasEvent(events, "final_round_started") {
		t.Error("expected a final_round_started event")
	}
}

func TestFlowEndTurnDoesNotArmAtThreeTrainsLeft(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 3)
	gs, _ := newNormalState(m, 3)
	p := gs.Players[0]
	p.TrainsLeft = 3

	events := callEndTurn(t, e, m, gs, p)

	if gs.FinalTurnsLeft != -1 {
		t.Errorf("final_turns_left = %d, want -1 (untriggered)", gs.FinalTurnsLeft)
	}
	if hasEvent(events, "final_round_started") {
		t.Error("did not expect a final_round_started event")
	}
}

// TestFlowFinalRoundLastsExactlyNumPlayersTurns exercises the exact
// accounting rules §11 + §16.5 require. §11 ships a prose description and a
// pseudocode that disagree with each other (see endTurn's doc comment for
// the trace); the prose governs: "every player, including the triggering
// player, takes exactly one more turn" after the trigger. This test asserts
// precisely that: after the triggering call arms the counter (without also
// decrementing it), exactly numPlayers FURTHER endTurn calls occur before
// PHASE_FINISHED, and the LAST of those further calls belongs to the
// triggering player themselves — their own promised "one more turn" — not
// to whoever happens to act numPlayers seats later. This is deliberately an
// exact-count-AND-exact-actor assertion, not just "the game eventually
// ends" (see the Step 11 brief's case 4 note): asserting only the count
// would not have caught the original off-by-one, since a naive
// arm-and-decrement-together implementation also produces a fixed count of
// numPlayers total calls — it just seats the wrong player last.
func TestFlowFinalRoundLastsExactlyNumPlayersTurns(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 4)
	const n = 3
	gs, _ := newNormalState(m, n)

	trigger := gs.Players[0]
	trigger.TrainsLeft = 2

	events := callEndTurn(t, e, m, gs, trigger)
	if gs.FinalTurnsLeft != int32(n) {
		t.Fatalf("final_turns_left after the triggering turn = %d, want %d (arm only, no decrement yet)", gs.FinalTurnsLeft, n)
	}
	if !hasEvent(events, "final_round_started") {
		t.Error("expected a final_round_started event on the triggering turn")
	}

	furtherTurns := 0
	lastActor := int32(-1)
	for gs.Phase != pb.Phase_PHASE_FINISHED {
		if furtherTurns >= n {
			t.Fatalf("game did not reach PHASE_FINISHED within %d further turns after the trigger", n)
		}
		acting := playerBySeat(gs, gs.CurrentSeat)
		lastActor = acting.SeatIndex
		ev := callEndTurn(t, e, m, gs, acting)
		furtherTurns++
		if gs.Phase == pb.Phase_PHASE_FINISHED && !hasEvent(ev, "game_over") {
			t.Error("expected a game_over event when the game reaches PHASE_FINISHED")
		}
	}

	if furtherTurns != n {
		t.Errorf("final round lasted %d further turns after the trigger, want exactly %d", furtherTurns, n)
	}
	if lastActor != trigger.SeatIndex {
		t.Errorf("last actor before PHASE_FINISHED was seat %d, want seat %d (the triggering player's own promised extra turn)",
			lastActor, trigger.SeatIndex)
	}
	if len(gs.Results) != n {
		t.Errorf("len(results) = %d, want %d (one ScoreBreakdown per player)", len(gs.Results), n)
	}
}

// TestFlowSecondPlayerDroppingToTwoTrainsDoesNotReArm is the highest-risk
// property from the Step 11 brief: once armed, a second player also ending
// their turn at <= 2 trains must not reset final_turns_left back up, and
// must not extend the round by even one turn. The `case gs.FinalTurnsLeft <
// 0 && ...` arming branch in endTurn is mutually exclusive with the `case
// gs.FinalTurnsLeft >= 0` decrement branch in the same switch, so once armed
// (FinalTurnsLeft >= 0) a later trigger condition can only ever hit the
// decrement branch — this test proves that holds in practice.
func TestFlowSecondPlayerDroppingToTwoTrainsDoesNotReArm(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 5)
	const n = 3
	gs, _ := newNormalState(m, n)

	gs.Players[0].TrainsLeft = 2
	callEndTurn(t, e, m, gs, gs.Players[0]) // arms to n; this call does not decrement
	if gs.FinalTurnsLeft != int32(n) {
		t.Fatalf("final_turns_left after arming = %d, want %d", gs.FinalTurnsLeft, n)
	}

	gs.Players[1].TrainsLeft = 1 // also <= endTriggerTrains, mid-final-round
	events := callEndTurn(t, e, m, gs, gs.Players[1])

	if gs.FinalTurnsLeft != int32(n-1) {
		t.Fatalf("final_turns_left = %d, want %d (must not re-arm back to %d)", gs.FinalTurnsLeft, n-1, n)
	}
	if hasEvent(events, "final_round_started") {
		t.Error("a second player dropping to <= 2 trains must not emit another final_round_started")
	}

	// Two more further turns remain: seat 2, then seat 0's own promised
	// extra turn (the triggering player, per §11's prose). The round must
	// finish exactly there — not one turn early, not one turn late.
	callEndTurn(t, e, m, gs, gs.Players[2])
	if gs.Phase == pb.Phase_PHASE_FINISHED {
		t.Fatalf("phase = %v after only 2 of %d further turns since the trigger — finished too early", gs.Phase, n)
	}

	events = callEndTurn(t, e, m, gs, gs.Players[0])
	if gs.Phase != pb.Phase_PHASE_FINISHED {
		t.Fatalf("phase = %v, want PHASE_FINISHED after exactly %d further turns since the trigger", gs.Phase, n)
	}
	if !hasEvent(events, "game_over") {
		t.Error("expected a game_over event")
	}
}

// TestFlowOutOfTurnResignationClampsFinalTurnsLeft is the regression test
// for m2 in the scoring/redaction review: an out-of-turn resignation during
// an already-armed final round used to leave FinalTurnsLeft completely
// unadjusted, even though it was armed to the active-player count AT
// TRIGGER TIME, and the active-player count just shrank by one.
//
// 3 players: seat 0 triggers the final round (ends its turn at 2 trains
// left) -> FinalTurnsLeft arms to 3, and turn passes to seat 1. Seat 2 (NOT
// the current seat) then resigns. Pre-fix, FinalTurnsLeft stayed at 3, so
// the round would run seat1->2, seat0->1, seat1->0 - seat 1 taking two
// further turns and seat 0 only one, violating §11's "every player takes
// exactly one more turn". Post-fix, the resignation clamps FinalTurnsLeft
// down to len(activePlayers) == 2, so the round finishes after exactly 2
// further turns - one each for seat 1 and seat 0 - matching §11 for the
// now-2-player field, with seat 0's own promised extra turn last.
func TestFlowOutOfTurnResignationClampsFinalTurnsLeft(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 8)
	const n = 3
	gs, ids := newNormalState(m, n)

	trigger := gs.Players[0]
	trigger.TrainsLeft = 2
	callEndTurn(t, e, m, gs, trigger)
	if gs.FinalTurnsLeft != int32(n) {
		t.Fatalf("final_turns_left after arming = %d, want %d", gs.FinalTurnsLeft, n)
	}
	if gs.CurrentSeat != 1 {
		t.Fatalf("current_seat after seat 0's triggering turn = %d, want 1", gs.CurrentSeat)
	}

	// Seat 2 - NOT the current seat (seat 1 is) - resigns.
	next, _, err := applyAction(t, e, gs, ids[2], ActionResign, map[string]any{})
	if err != nil {
		t.Fatalf("out-of-turn resign: unexpected error: %v", err)
	}
	if next.FinalTurnsLeft != 2 {
		t.Fatalf("final_turns_left after the out-of-turn resignation = %d, want 2 (clamped to the 2 remaining active players)", next.FinalTurnsLeft)
	}
	if next.CurrentSeat != 1 {
		t.Fatalf("current_seat changed by an out-of-turn resignation: got %d, want unchanged 1", next.CurrentSeat)
	}

	furtherTurns := 0
	lastActor := int32(-1)
	for next.Phase != pb.Phase_PHASE_FINISHED {
		if furtherTurns >= 2 {
			t.Fatalf("game did not reach PHASE_FINISHED within 2 further turns after the out-of-turn resignation")
		}
		acting := playerBySeat(next, next.CurrentSeat)
		lastActor = acting.SeatIndex
		callEndTurn(t, e, m, next, acting)
		furtherTurns++
	}

	if furtherTurns != 2 {
		t.Errorf("final round lasted %d further turns after the resignation, want exactly 2 (one each for seats 0 and 1)", furtherTurns)
	}
	if lastActor != 0 {
		t.Errorf("last actor before PHASE_FINISHED was seat %d, want seat 0 (its own promised extra turn, last per §11)", lastActor)
	}
}

func TestFlowNextActiveSeatWrapsAndSkipsResigned(t *testing.T) {
	m := testMap(t)
	gs, _ := newNormalState(m, 4)
	gs.Players[2].Resigned = true

	if got := nextActiveSeat(gs, 1); got != 3 {
		t.Errorf("nextActiveSeat(1) = %d, want 3 (skip resigned seat 2)", got)
	}
	if got := nextActiveSeat(gs, 3); got != 0 {
		t.Errorf("nextActiveSeat(3) = %d, want 0 (wrap around ascending)", got)
	}
	if got := nextActiveSeat(gs, 0); got != 1 {
		t.Errorf("nextActiveSeat(0) = %d, want 1", got)
	}
}

// mustResolveAllSetupTickets drives every seat's §5.7 simultaneous keep
// (each keeps exactly its first 2 offered tickets), returning the resulting
// PHASE_NORMAL state.
func mustResolveAllSetupTickets(t *testing.T, e *Engine, gs *pb.GameState, ids []uuid.UUID) *pb.GameState {
	t.Helper()
	for _, id := range ids {
		p := playerByUser(gs, id.String())
		next, _, err := applyAction(t, e, gs, id, ActionResolveDecision, ResolveDecisionPayload{
			Kind:          DecisionKindSetupTickets,
			KeepTicketIDs: p.SetupTicketOffer[:minTicketsKeptAtSetup],
		})
		if err != nil {
			t.Fatalf("resolve setup tickets for seat %d: %v", p.SeatIndex, err)
		}
		gs = next
	}
	return gs
}

func TestFlowResignAdvancesTurnAndCanFinishGame(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 6)
	state, ids := mustInitState(t, e, 3)
	gs := mustResolveAllSetupTickets(t, e, mustGameState(t, state), ids)
	assertInvariants(t, m, gs)

	currentSeat := gs.CurrentSeat
	acting := playerBySeat(gs, currentSeat)

	next, events, err := applyAction(t, e, gs, uuid.MustParse(acting.UserId), ActionResign, map[string]any{})
	if err != nil {
		t.Fatalf("resign: unexpected error: %v", err)
	}
	if !hasEvent(events, "player_resigned") {
		t.Error("expected a player_resigned event")
	}
	if !playerBySeat(next, currentSeat).Resigned {
		t.Error("resigning player is not marked resigned")
	}
	if next.CurrentSeat == currentSeat {
		t.Error("current_seat did not advance past the resigning player")
	}
	if next.Phase != pb.Phase_PHASE_NORMAL {
		t.Errorf("phase = %v, want PHASE_NORMAL (game continues with 2 active players)", next.Phase)
	}
	assertInvariants(t, m, next)

	// Resign a second player, leaving exactly one active player: the game
	// must finish and score immediately (plan Q14), independent of any
	// trains_left / final-round state.
	remaining := playerBySeat(next, next.CurrentSeat)
	final, events2, err := applyAction(t, e, next, uuid.MustParse(remaining.UserId), ActionResign, map[string]any{})
	if err != nil {
		t.Fatalf("second resign: unexpected error: %v", err)
	}
	if final.Phase != pb.Phase_PHASE_FINISHED {
		t.Fatalf("phase = %v, want PHASE_FINISHED once fewer than 2 active players remain", final.Phase)
	}
	if !hasEvent(events2, "game_over") {
		t.Error("expected a game_over event")
	}
	if len(final.Results) != 3 {
		t.Errorf("len(results) = %d, want 3 (one per seated player, resigned or not)", len(final.Results))
	}
	for _, sb := range final.Results {
		p := playerBySeat(final, sb.SeatIndex)
		if p.Resigned && sb.Total != 0 {
			t.Errorf("seat %d resigned but ScoreBreakdown.total = %d, want 0", sb.SeatIndex, sb.Total)
		}
	}
	assertInvariants(t, m, final)
}

// TestFlowResignRejectsAlreadyResigned checks that a player cannot resign twice.
func TestFlowResignRejectsAlreadyResigned(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 7)
	state, ids := mustInitState(t, e, 3)
	gs := mustResolveAllSetupTickets(t, e, mustGameState(t, state), ids)

	acting := playerBySeat(gs, gs.CurrentSeat)
	next, _, err := applyAction(t, e, gs, uuid.MustParse(acting.UserId), ActionResign, map[string]any{})
	if err != nil {
		t.Fatalf("first resign: unexpected error: %v", err)
	}

	if _, _, err := applyAction(t, e, next, uuid.MustParse(acting.UserId), ActionResign, map[string]any{}); !isIllegalAction(err) {
		t.Errorf("resigning twice: err = %v, want engine.ErrIllegalAction", err)
	}
}

// --- C1 regression: a resigned player must never be able to deadlock the
// game by holding current_seat into PHASE_NORMAL. ---
//
// applyResign deliberately does not perturb current_seat during
// PHASE_SETUP_TICKETS (holdsTurn is false there — see applyResign's doc
// comment), so a player who resigns during setup can still be sitting in
// current_seat when the phase transitions to PHASE_NORMAL. Two independent
// gaps combine to make this a permanent deadlock: (1) checkPhaseGate never
// checked p.Resigned, so a resigned player could still answer their own
// setup keep (and, worse, still take PHASE_NORMAL turns since every action
// but resign requires current_seat, which nobody else can ever hold); (2)
// applySetupTicketsDecision's allSetupDone transition never checked whether
// gs.CurrentSeat itself pointed at a resigned player before handing it
// PHASE_NORMAL control.

// TestFlowResignDuringSetupCannotAnswerKeep covers gap (1): once a player has
// resigned during PHASE_SETUP_TICKETS, they must not be able to answer their
// own setup ticket keep, even though their SetupTicketOffer is left in place
// (applyResign does not clear it) and their SetupDone flag is still false.
func TestFlowResignDuringSetupCannotAnswerKeep(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 100)
	state, _ := mustInitState(t, e, 3)
	gs := mustGameState(t, state)

	resigning := playerBySeat(gs, gs.CurrentSeat)
	resigningID := uuid.MustParse(resigning.UserId)

	next, _, err := applyAction(t, e, gs, resigningID, ActionResign, map[string]any{})
	if err != nil {
		t.Fatalf("resign during setup: unexpected error: %v", err)
	}
	if next.Phase != pb.Phase_PHASE_SETUP_TICKETS {
		t.Fatalf("phase = %v, want PHASE_SETUP_TICKETS (resign during setup must not itself advance the phase)", next.Phase)
	}
	resignedPlayer := playerBySeat(next, resigning.SeatIndex)
	if !resignedPlayer.Resigned {
		t.Fatal("resigning player is not marked resigned")
	}
	if len(resignedPlayer.SetupTicketOffer) < minTicketsKeptAtSetup {
		t.Fatalf("test setup: resigned player's setup_ticket_offer = %v, too short to answer from", resignedPlayer.SetupTicketOffer)
	}

	_, _, err = applyAction(t, e, next, resigningID, ActionResolveDecision, ResolveDecisionPayload{
		Kind:          DecisionKindSetupTickets,
		KeepTicketIDs: resignedPlayer.SetupTicketOffer[:minTicketsKeptAtSetup],
	})
	if !isIllegalAction(err) {
		t.Errorf("resigned player answering the setup keep: err = %v, want engine.ErrIllegalAction", err)
	}
}

// TestFlowResignDuringSetupAdvancesCurrentSeatAndStaysPlayable covers gap (2)
// and reproduces the reviewer's probe-verified deadlock end to end: resign
// the player who happens to hold current_seat during PHASE_SETUP_TICKETS,
// have every remaining active player answer their own setup keep, and check
// that (a) the PHASE_NORMAL transition lands current_seat on an active
// player rather than the resigned one, and (b) the game is then actually
// playable — the active current_seat player can complete a full turn, and
// the resigned player still cannot act.
func TestFlowResignDuringSetupAdvancesCurrentSeatAndStaysPlayable(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 101)
	state, ids := mustInitState(t, e, 3)
	gs := mustGameState(t, state)

	resigningSeat := gs.CurrentSeat
	resigning := playerBySeat(gs, resigningSeat)
	resigningID := uuid.MustParse(resigning.UserId)

	next, _, err := applyAction(t, e, gs, resigningID, ActionResign, map[string]any{})
	if err != nil {
		t.Fatalf("resign during setup: unexpected error: %v", err)
	}

	for _, id := range ids {
		p := playerByUser(next, id.String())
		if p.Resigned {
			continue
		}
		out, _, err := applyAction(t, e, next, id, ActionResolveDecision, ResolveDecisionPayload{
			Kind:          DecisionKindSetupTickets,
			KeepTicketIDs: p.SetupTicketOffer[:minTicketsKeptAtSetup],
		})
		if err != nil {
			t.Fatalf("resolve setup tickets for seat %d: %v", p.SeatIndex, err)
		}
		next = out
	}

	if next.Phase != pb.Phase_PHASE_NORMAL {
		t.Fatalf("phase = %v, want PHASE_NORMAL once every active player has answered", next.Phase)
	}
	if cur := playerBySeat(next, next.CurrentSeat); cur.Resigned {
		t.Fatalf("current_seat = %d, which is the resigned player — no active player can ever hold the turn (deadlock)", next.CurrentSeat)
	}
	assertInvariants(t, m, next)

	// The game must actually be playable: the active current_seat player can
	// complete a full draw_card turn.
	acting := playerBySeat(next, next.CurrentSeat)
	actingID := uuid.MustParse(acting.UserId)
	afterFirst, _, err := applyAction(t, e, next, actingID, ActionDrawCard, DrawCardPayload{Source: "deck"})
	if err != nil {
		t.Fatalf("draw_card (1st) by the active current_seat player: %v", err)
	}
	if afterFirst.Phase == pb.Phase_PHASE_AWAITING_SECOND_DRAW {
		if _, _, err := applyAction(t, e, afterFirst, actingID, ActionDrawCard, DrawCardPayload{Source: "deck"}); err != nil {
			t.Fatalf("draw_card (2nd) by the active current_seat player: %v", err)
		}
	}

	// And the resigned player — even though they were current_seat moments
	// ago — still cannot act.
	if _, _, err := applyAction(t, e, next, resigningID, ActionDrawCard, DrawCardPayload{Source: "deck"}); err == nil {
		t.Error("resigned player was able to draw_card in PHASE_NORMAL, want an error")
	}
}

// TestFlowActionsAfterFinishedReturnErrGameOver checks that PHASE_FINISHED
// rejects every action type, resign included (rules §6, plan Q14).
func TestFlowActionsAfterFinishedReturnErrGameOver(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 8)
	gs, ids := newNormalState(m, 2)
	gs.Phase = pb.Phase_PHASE_FINISHED

	if _, _, err := applyAction(t, e, gs, ids[0], ActionDrawTickets, map[string]any{}); !errors.Is(err, engine.ErrGameOver) {
		t.Errorf("draw_tickets after finish: err = %v, want engine.ErrGameOver", err)
	}
	if _, _, err := applyAction(t, e, gs, ids[0], ActionResign, map[string]any{}); !errors.Is(err, engine.ErrGameOver) {
		t.Errorf("resign after finish: err = %v, want engine.ErrGameOver", err)
	}
}

// --- Scripted long-game integration test (case 9) ---

// colorPaymentToWire converts a Color -> count map into the wire payment
// shape (color name -> count) ClaimRoutePayload/BuildStationPayload use.
func colorPaymentToWire(pay map[Color]int) map[string]int {
	out := make(map[string]int, len(pay))
	for c, n := range pay {
		out[c.String()] = n
	}
	return out
}

// affordableSingleColorPayment tries to build a valid single-non-locomotive
// colour payment of exactly cost cards from hand, substituting locomotives
// as needed (rules §8.2, §10.2). It is deliberately greedy/simple: it is a
// scripted-test bot, not a legal-move enumerator.
func affordableSingleColorPayment(cost int, hand map[int32]int32) (map[Color]int, bool) {
	haveLoco := int(hand[int32(ColorLoco)])
	for _, c := range CardColors {
		haveColor := int(hand[int32(c)])
		fromColor := min(cost, haveColor)
		fromLoco := cost - fromColor
		if fromLoco > haveLoco {
			continue
		}
		pay := map[Color]int{}
		if fromColor > 0 {
			pay[c] = fromColor
		}
		if fromLoco > 0 {
			pay[ColorLoco] = fromLoco
		}
		return pay, true
	}
	if haveLoco >= cost {
		return map[Color]int{ColorLoco: cost}, true
	}
	return nil, false
}

// affordableRoutePayment builds a valid §8.2/§8.3 payment for r entirely
// from hand: a colored route pays in r.Color (loco-substitutable), a gray
// route (including ferries, whose r.Locos are a mandatory floor) pays in
// whichever single color hand can afford.
func affordableRoutePayment(r *Route, hand map[int32]int32) (map[Color]int, bool) {
	locoFloor := 0
	if r.IsFerry() {
		locoFloor = r.Locos
	}
	rest := r.Length - locoFloor
	haveLoco := int(hand[int32(ColorLoco)]) - locoFloor
	if haveLoco < 0 {
		return nil, false
	}

	tryColor := func(c Color) (map[Color]int, bool) {
		haveColor := int(hand[int32(c)])
		fromColor := min(rest, haveColor)
		fromLoco := rest - fromColor
		if fromLoco > haveLoco {
			return nil, false
		}
		pay := map[Color]int{}
		if locoFloor+fromLoco > 0 {
			pay[ColorLoco] = locoFloor + fromLoco
		}
		if fromColor > 0 {
			pay[c] = fromColor
		}
		return pay, true
	}

	if r.Color == ColorGray {
		for _, c := range CardColors {
			if pay, ok := tryColor(c); ok {
				return pay, true
			}
		}
		return nil, false
	}
	return tryColor(r.Color)
}

// findAffordableRoute scans m's routes in ascending id order for one p can
// legally claim right now (rules §8.1) with a payment constructible
// entirely from p's hand. Tunnels are skipped: their §8.4 surcharge
// resolution is exercised by dedicated scenarios in engine_claim_test.go,
// not by this turn-flow-focused long game.
func findAffordableRoute(m *Map, gs *pb.GameState, p *pb.PlayerState) (*Route, map[Color]int) {
	ids := make([]int32, 0, len(m.RouteByID))
	for id := range m.RouteByID {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		r := m.RouteByID[id]
		if r.Tunnel {
			continue
		}
		if err := claimability(m, gs, p, r); err != nil {
			continue
		}
		if pay, ok := affordableRoutePayment(r, p.Hand); ok {
			return r, pay
		}
	}
	return nil, nil
}

// findBuildableStationCity scans m's cities in ascending id order for one p
// can legally build a station in right now (rules §10.1-§10.2).
func findBuildableStationCity(m *Map, gs *pb.GameState, p *pb.PlayerState) (string, map[Color]int, bool) {
	if p.StationsLeft < 1 {
		return "", nil, false
	}
	cost := stationCost(p, m)

	cityIDs := make([]string, 0, len(m.CityByID))
	for id := range m.CityByID {
		cityIDs = append(cityIDs, id)
	}
	slices.Sort(cityIDs)

	for _, id := range cityIDs {
		if _, taken := gs.StationOwner[id]; taken {
			continue
		}
		if pay, ok := affordableSingleColorPayment(cost, p.Hand); ok {
			return id, pay, true
		}
	}
	return "", nil, false
}

// scriptedDrawTickets plays a full draw_tickets turn for acting: draw, then
// immediately keep the first offered ticket (rules §9.2's minimum).
func scriptedDrawTickets(t *testing.T, e *Engine, gs *pb.GameState, userID uuid.UUID, acting *pb.PlayerState, seen map[string]bool) *pb.GameState {
	t.Helper()
	next, _, err := applyAction(t, e, gs, userID, ActionDrawTickets, map[string]any{})
	if err != nil {
		t.Fatalf("turn %d seat %d: draw_tickets: %v", gs.TurnNo, acting.SeatIndex, err)
	}
	seen[ActionDrawTickets] = true
	keep := next.PendingTicketDraw.OfferedTicketIds[:1]
	final, _, err := applyAction(t, e, next, userID, ActionResolveDecision, ResolveDecisionPayload{Kind: DecisionKindTicketKeep, KeepTicketIDs: keep})
	if err != nil {
		t.Fatalf("turn %d seat %d: resolve ticket_keep: %v", gs.TurnNo, acting.SeatIndex, err)
	}
	return final
}

// scriptedClaimRoute claims the first route findAffordableRoute finds for
// acting, if any. ok is false (and gs is unchanged) when no route is
// currently claimable/affordable, letting the caller fall through to its
// next preferred action.
func scriptedClaimRoute(t *testing.T, e *Engine, m *Map, gs *pb.GameState, userID uuid.UUID, acting *pb.PlayerState, seen map[string]bool) (next *pb.GameState, ok bool) {
	t.Helper()
	r, pay := findAffordableRoute(m, gs, acting)
	if r == nil {
		return nil, false
	}
	final, _, err := applyAction(t, e, gs, userID, ActionClaimRoute, ClaimRoutePayload{RouteID: r.ID, Payment: colorPaymentToWire(pay)})
	if err != nil {
		t.Fatalf("turn %d seat %d: claim_route %d: %v", gs.TurnNo, acting.SeatIndex, r.ID, err)
	}
	seen[ActionClaimRoute] = true
	return final, true
}

// scriptedBuildStation builds a station in the first city
// findBuildableStationCity finds for acting, if any. ok is false (and gs is
// unchanged) when no station is currently buildable/affordable.
func scriptedBuildStation(t *testing.T, e *Engine, m *Map, gs *pb.GameState, userID uuid.UUID, acting *pb.PlayerState, seen map[string]bool) (next *pb.GameState, ok bool) {
	t.Helper()
	city, pay, found := findBuildableStationCity(m, gs, acting)
	if !found {
		return nil, false
	}
	final, _, err := applyAction(t, e, gs, userID, ActionBuildStation, BuildStationPayload{CityID: city, Payment: colorPaymentToWire(pay)})
	if err != nil {
		t.Fatalf("turn %d seat %d: build_station %s: %v", gs.TurnNo, acting.SeatIndex, city, err)
	}
	seen[ActionBuildStation] = true
	return final, true
}

// scriptedDrawCards plays a full draw_card turn for acting: two blind draws
// from the deck (or just one, if the first happens to be a face-up
// locomotive — impossible here since Source is always "deck").
func scriptedDrawCards(t *testing.T, e *Engine, gs *pb.GameState, userID uuid.UUID, acting *pb.PlayerState, seen map[string]bool) *pb.GameState {
	t.Helper()
	next, _, err := applyAction(t, e, gs, userID, ActionDrawCard, DrawCardPayload{Source: "deck"})
	if err != nil {
		t.Fatalf("turn %d seat %d: draw_card (1st): %v", gs.TurnNo, acting.SeatIndex, err)
	}
	seen[ActionDrawCard] = true
	if next.Phase != pb.Phase_PHASE_AWAITING_SECOND_DRAW {
		return next
	}
	final, _, err := applyAction(t, e, next, userID, ActionDrawCard, DrawCardPayload{Source: "deck"})
	if err != nil {
		t.Fatalf("turn %d seat %d: draw_card (2nd): %v", gs.TurnNo, acting.SeatIndex, err)
	}
	return final
}

// playOneScriptedTurn plays exactly one full turn for gs.CurrentSeat,
// preferring (in order, so every action type gets exercised over a long
// enough game): a periodic ticket draw, then a claimable route, then a
// buildable station, then two blind card draws, then a ticket draw as a
// last resort. It fails t if the acting player has no legal move at all
// (which should not happen within this test's turn budget).
func playOneScriptedTurn(t *testing.T, e *Engine, m *Map, gs *pb.GameState, seen map[string]bool) *pb.GameState {
	t.Helper()
	acting := playerBySeat(gs, gs.CurrentSeat)
	userID := uuid.MustParse(acting.UserId)

	if len(gs.TicketDeck) > 0 && gs.TurnNo%6 == 0 {
		return scriptedDrawTickets(t, e, gs, userID, acting, seen)
	}
	if next, ok := scriptedClaimRoute(t, e, m, gs, userID, acting, seen); ok {
		return next
	}
	if next, ok := scriptedBuildStation(t, e, m, gs, userID, acting, seen); ok {
		return next
	}
	if canDraw(gs) {
		return scriptedDrawCards(t, e, gs, userID, acting, seen)
	}
	if len(gs.TicketDeck) > 0 {
		return scriptedDrawTickets(t, e, gs, userID, acting, seen)
	}

	t.Fatalf("turn %d seat %d: scripted bot found no legal action", gs.TurnNo, acting.SeatIndex)
	return nil
}

// TestFlowScriptedThreePlayerGameHoldsInvariantsThroughout plays a long,
// deterministic (seeded shuffler) 3-player game exercising every one of the
// four §6 turn actions, asserting rules §14's invariants after every single
// completed turn — the integration test the Step 11 brief calls for (case
// 9). Tunnel resolution and resign each have dedicated, focused coverage
// elsewhere in this package; this test's job is turn-flow robustness over
// many consecutive turns, not re-covering every action's own edge cases.
func TestFlowScriptedThreePlayerGameHoldsInvariantsThroughout(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 2024)
	state, ids := mustInitState(t, e, 3)
	gs := mustResolveAllSetupTickets(t, e, mustGameState(t, state), ids)
	assertInvariants(t, m, gs)

	const minTurns = 40
	seen := map[string]bool{}
	completed := 0
	for completed < minTurns {
		if gs.Phase == pb.Phase_PHASE_FINISHED {
			t.Logf("game reached PHASE_FINISHED early, after %d scripted turns", completed)
			break
		}
		gs = playOneScriptedTurn(t, e, m, gs, seen)
		assertInvariants(t, m, gs)
		completed++
	}

	if completed < minTurns && gs.Phase != pb.Phase_PHASE_FINISHED {
		t.Fatalf("only completed %d turns, want at least %d", completed, minTurns)
	}
	for _, want := range []string{ActionDrawCard, ActionClaimRoute, ActionBuildStation, ActionDrawTickets} {
		if !seen[want] {
			t.Errorf("scripted game never exercised action %q in %d turns", want, completed)
		}
	}
}
