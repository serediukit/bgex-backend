package ttr

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// newNormalState builds a minimal *pb.GameState for testing a single engine
// transition directly, bypassing InitState entirely: n players seated
// 0..n-1 (each with an empty hand and full trains_left/stations_left per
// m.Rules), phase PHASE_NORMAL, seat 0 to move, turn_no 1,
// final_turns_left -1, and empty draw/discard/face_up/ticket piles. A test
// then hand-sets exactly the fields its scenario needs (DrawPile,
// DiscardPile, FaceUp, a player's Hand, Draw, ...) directly on the returned
// state before calling applyAction. Steps 9-12 reuse this identical
// facility for their own hand-built fixtures.
func newNormalState(m *Map, n int) (*pb.GameState, []uuid.UUID) {
	ids := make([]uuid.UUID, n)
	players := make([]*pb.PlayerState, n)
	for i := range n {
		ids[i] = uuid.New()
		players[i] = &pb.PlayerState{
			SeatIndex:    int32(i), // #nosec G115 -- n is a small test player count
			UserId:       ids[i].String(),
			Hand:         make(map[int32]int32),
			TrainsLeft:   int32(m.Rules.TrainsPerPlayer),
			StationsLeft: int32(m.Rules.StationsPerPlayer),
		}
	}
	gs := &pb.GameState{
		MapId:          testMapID,
		MapVersion:     testMapVersion,
		Phase:          pb.Phase_PHASE_NORMAL,
		Players:        players,
		CurrentSeat:    0,
		TurnNo:         1,
		FinalTurnsLeft: -1,
	}
	return gs, ids
}

// colorInts converts colors to their int32 wire values, for building
// DrawPile/DiscardPile/FaceUp fixtures inline.
func colorInts(colors ...Color) []int32 {
	out := make([]int32, len(colors))
	for i, c := range colors {
		out[i] = int32(c)
	}
	return out
}

// fillerCards returns n copies of ColorPurple as int32 values, used to pad a
// hand-built test state's piles up to TotalTrainCards so assertInvariants'
// conservation check (rules §14) holds. The exact color of filler never
// matters to the transition under test; DiscardPile is the usual place to
// stash it since it is untouched unless the scenario itself reshuffles.
func fillerCards(n int) []int32 {
	return colorInts(sliceOf(n, ColorPurple)...)
}

// sliceOf returns n copies of v.
func sliceOf[T any](n int, v T) []T {
	out := make([]T, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// applyAction marshals gs, applies a's Type/Payload for userID via e.Apply,
// and returns the resulting *pb.GameState (unmarshaled) and events on
// success. gs itself is never mutated — Apply operates on its own unmarshal
// of the bytes. Steps 9-12 reuse this same facility.
func applyAction(t *testing.T, e *Engine, gs *pb.GameState, userID uuid.UUID, actionType string, payload any) (*pb.GameState, []engine.Event, error) {
	t.Helper()

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	state, err := marshal(gs)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}

	next, events, err := e.Apply(state, engine.Action{UserID: userID, Type: actionType, Payload: raw})
	if err != nil {
		return nil, events, err
	}
	ngs, err := unmarshal(next)
	if err != nil {
		t.Fatalf("unmarshal next state: %v", err)
	}
	return ngs, events, nil
}

// drawCard is applyAction specialized for ActionDrawCard.
func drawCard(t *testing.T, e *Engine, gs *pb.GameState, userID uuid.UUID, source string, slot int) (*pb.GameState, []engine.Event, error) {
	t.Helper()
	return applyAction(t, e, gs, userID, ActionDrawCard, DrawCardPayload{Source: source, Slot: slot})
}

// handTotal sums every card count in p.Hand.
func handTotal(p *pb.PlayerState) int {
	n := 0
	for _, c := range p.Hand {
		n += int(c)
	}
	return n
}

func TestDrawFaceUpLocomotiveEndsTurnImmediately(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, ids := newNormalState(m, 2)

	gs.FaceUp = colorInts(ColorLoco, ColorRed, ColorBlue, ColorGreen, ColorYellow)
	gs.DrawPile = colorInts(ColorBlack, ColorWhite, ColorOrange, ColorRed, ColorBlue)
	gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp) - len(gs.DrawPile))

	ngs, events, err := drawCard(t, e, gs, ids[0], "face_up", 0)
	if err != nil {
		t.Fatalf("draw face-up locomotive: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)

	p0 := playerBySeat(ngs, 0)
	if handTotal(p0) != 1 || p0.Hand[int32(ColorLoco)] != 1 {
		t.Errorf("seat 0 hand = %v, want exactly {Locomotive: 1}", p0.Hand)
	}
	if ngs.Phase != pb.Phase_PHASE_NORMAL {
		t.Errorf("phase = %v, want PHASE_NORMAL (turn ended)", ngs.Phase)
	}
	if ngs.Draw != nil {
		t.Errorf("draw progress = %+v, want nil (turn ended)", ngs.Draw)
	}
	if ngs.CurrentSeat != 1 {
		t.Errorf("current_seat = %d, want 1 (advanced)", ngs.CurrentSeat)
	}
	if ngs.TurnNo != 2 {
		t.Errorf("turn_no = %d, want 2", ngs.TurnNo)
	}
	if len(events) == 0 {
		t.Error("no events emitted for a face-up locomotive draw")
	}
}

func TestDrawBlindLocomotiveAllowsSecondDraw(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, ids := newNormalState(m, 2)

	gs.FaceUp = colorInts(ColorRed, ColorBlue, ColorGreen, ColorYellow, ColorBlack)
	gs.DrawPile = colorInts(ColorLoco, ColorWhite, ColorOrange)
	gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp) - len(gs.DrawPile))

	ngs, _, err := drawCard(t, e, gs, ids[0], "deck", 0)
	if err != nil {
		t.Fatalf("draw blind locomotive: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)

	p0 := playerBySeat(ngs, 0)
	if handTotal(p0) != 1 || p0.Hand[int32(ColorLoco)] != 1 {
		t.Errorf("seat 0 hand = %v, want exactly {Locomotive: 1}", p0.Hand)
	}
	if ngs.Phase != pb.Phase_PHASE_AWAITING_SECOND_DRAW {
		t.Errorf("phase = %v, want PHASE_AWAITING_SECOND_DRAW (second draw still allowed)", ngs.Phase)
	}
	if ngs.Draw == nil || ngs.Draw.CardsTaken != 1 || !ngs.Draw.FaceUpLocoLocked {
		t.Errorf("draw progress = %+v, want {cards_taken:1, face_up_loco_locked:true}", ngs.Draw)
	}
	if ngs.CurrentSeat != 0 {
		t.Errorf("current_seat = %d, want unchanged 0 (turn not over)", ngs.CurrentSeat)
	}
	if ngs.TurnNo != 1 {
		t.Errorf("turn_no = %d, want unchanged 1", ngs.TurnNo)
	}
}

func TestDrawFaceUpRefillLocomotiveNotSelectableAsSecondCard(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, ids := newNormalState(m, 2)

	gs.FaceUp = colorInts(ColorRed, ColorBlue, ColorGreen, ColorYellow, ColorBlack)
	gs.DrawPile = colorInts(ColorLoco, ColorWhite, ColorOrange)
	gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp) - len(gs.DrawPile))

	// First draw: take slot 0 (Red). The refill pulled from the draw pile's
	// top is a locomotive, landing back in that same slot 0 (rules §3.3
	// models 5 fixed slots a client addresses by index — the refill must
	// replace in place, not shift every later slot down, m3 in the Step 11
	// review).
	ngs, _, err := drawCard(t, e, gs, ids[0], "face_up", 0)
	if err != nil {
		t.Fatalf("first draw: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)
	if got := ngs.FaceUp[0]; pb.Color(got) != pb.Color_COLOR_LOCO {
		t.Fatalf("face_up[0] = %v, want the locomotive refill in place (test setup assumption broken)", pb.Color(got))
	}
	if ngs.Phase != pb.Phase_PHASE_AWAITING_SECOND_DRAW {
		t.Fatalf("phase after first draw = %v, want PHASE_AWAITING_SECOND_DRAW", ngs.Phase)
	}

	// Second draw: attempting to take the refilled locomotive must fail,
	// even though it is only sitting there because of the refill (§7.2).
	_, _, err = drawCard(t, e, ngs, ids[0], "face_up", 0)
	if !isIllegalAction(err) {
		t.Errorf("taking refilled face-up locomotive as second card: err = %v, want engine.ErrIllegalAction", err)
	}
}

// TestDrawFaceUpTakeRefillsSameSlot is an m3 regression: taking a middle
// slot must refill that exact index from the draw pile, not shift every
// later slot down and append the refill at the end. Rules §3.3 models the
// face-up row as 5 fixed slots a client addresses by index, so "the slot you
// took was refilled" must stay true; a shift-and-append implementation would
// silently re-key every slot after the one taken.
func TestDrawFaceUpTakeRefillsSameSlot(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 2)
	gs, ids := newNormalState(m, 2)

	gs.FaceUp = colorInts(ColorRed, ColorBlue, ColorGreen, ColorYellow, ColorBlack)
	gs.DrawPile = colorInts(ColorWhite, ColorOrange, ColorPurple)
	gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp) - len(gs.DrawPile))

	// Take slot 2 (Green). The slots on either side (Blue at 1, Yellow at
	// 3) must not move.
	ngs, _, err := drawCard(t, e, gs, ids[0], "face_up", 2)
	if err != nil {
		t.Fatalf("draw face_up slot 2: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)

	if Color(ngs.FaceUp[2]) != ColorWhite {
		t.Errorf("face_up[2] = %v, want the refill (White) in the same slot that was taken", Color(ngs.FaceUp[2]))
	}
	if Color(ngs.FaceUp[1]) != ColorBlue {
		t.Errorf("face_up[1] = %v, want unchanged Blue (a take must not shift neighboring slots)", Color(ngs.FaceUp[1]))
	}
	if Color(ngs.FaceUp[3]) != ColorYellow {
		t.Errorf("face_up[3] = %v, want unchanged Yellow (a take must not shift neighboring slots)", Color(ngs.FaceUp[3]))
	}
}

func TestDrawFaceUpRefillFlushesRepeatedly(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, ids := newNormalState(m, 2)

	// 2 locomotives face-up (stable, below the flush threshold).
	gs.FaceUp = colorInts(ColorRed, ColorLoco, ColorLoco, ColorGreen, ColorYellow)
	// Pop order: 1 card refills the taken slot (a 3rd locomotive -> flush);
	// the next 5 refill the flush (3 more locomotives -> flush again); the
	// final 5 refill the second flush and are loco-free (stable).
	gs.DrawPile = colorInts(
		ColorLoco,
		ColorLoco, ColorLoco, ColorLoco, ColorBlack, ColorWhite,
		ColorBlack, ColorWhite, ColorOrange, ColorPurple, ColorYellow,
	)
	gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp) - len(gs.DrawPile))

	ngs, events, err := drawCard(t, e, gs, ids[0], "face_up", 0)
	if err != nil {
		t.Fatalf("draw: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)

	p0 := playerBySeat(ngs, 0)
	if handTotal(p0) != 1 || p0.Hand[int32(ColorRed)] != 1 {
		t.Errorf("seat 0 hand = %v, want exactly {Red: 1}", p0.Hand)
	}
	if len(ngs.FaceUp) != faceUpSlots {
		t.Fatalf("len(face_up) = %d, want %d after stabilizing", len(ngs.FaceUp), faceUpSlots)
	}
	if got := countLocos(ngs.FaceUp); got != 0 {
		t.Errorf("face_up locomotives = %d, want 0 after two flushes stabilize", got)
	}
	if len(ngs.DrawPile) != 0 {
		t.Errorf("len(draw_pile) = %d, want 0 (all 11 cards consumed)", len(ngs.DrawPile))
	}
	// 2 flushes of 5 cards each were discarded, on top of the filler.
	wantDiscard := len(gs.DiscardPile) + 2*faceUpSlots
	if len(ngs.DiscardPile) != wantDiscard {
		t.Errorf("len(discard_pile) = %d, want %d (2 flushes of %d)", len(ngs.DiscardPile), wantDiscard, faceUpSlots)
	}

	flushed := 0
	for _, ev := range events {
		if ev.Type == "face_up_flushed" {
			flushed++
		}
	}
	if flushed != 2 {
		t.Errorf("face_up_flushed events = %d, want 2", flushed)
	}
}

func TestDrawReshufflesDiscardWhenDrawPileEmpty(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, ids := newNormalState(m, 2)

	gs.FaceUp = colorInts(ColorRed, ColorBlue, ColorGreen, ColorYellow, ColorBlack)
	gs.DrawPile = nil
	discardSeed := colorInts(ColorWhite, ColorOrange, ColorPurple)
	gs.DiscardPile = make([]int32, 0, TotalTrainCards-len(gs.FaceUp))
	gs.DiscardPile = append(gs.DiscardPile, discardSeed...)
	gs.DiscardPile = append(gs.DiscardPile, fillerCards(TotalTrainCards-len(gs.FaceUp)-len(discardSeed))...)
	discardBefore := len(gs.DiscardPile)

	ngs, events, err := drawCard(t, e, gs, ids[0], "deck", 0)
	if err != nil {
		t.Fatalf("draw with empty draw pile: unexpected error: %v", err)
	}
	assertInvariants(t, m, ngs)

	p0 := playerBySeat(ngs, 0)
	if handTotal(p0) != 1 {
		t.Errorf("seat 0 hand total = %d, want 1", handTotal(p0))
	}
	if len(ngs.DiscardPile) != 0 {
		t.Errorf("len(discard_pile) = %d, want 0 (reshuffled into draw pile)", len(ngs.DiscardPile))
	}
	if len(ngs.DrawPile) != discardBefore-1 {
		t.Errorf("len(draw_pile) = %d, want %d (reshuffled discard minus the card just drawn)", len(ngs.DrawPile), discardBefore-1)
	}

	reshuffled := false
	for _, ev := range events {
		if ev.Type == "deck_reshuffled" {
			reshuffled = true
		}
	}
	if !reshuffled {
		t.Error("no deck_reshuffled event emitted")
	}
}

func TestDrawIllegalWhenAllPilesExhausted(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, ids := newNormalState(m, 2)

	// Every one of the 110 cards is hoarded in hands; draw, discard and
	// face-up are all empty (rules §7.4).
	half := TotalTrainCards / 2
	gs.Players[0].Hand = map[int32]int32{int32(ColorPurple): int32(half)}                 // #nosec G115 -- constant test fixture size
	gs.Players[1].Hand = map[int32]int32{int32(ColorBlue): int32(TotalTrainCards - half)} // #nosec G115 -- constant test fixture size

	if canDraw(gs) {
		t.Fatal("canDraw = true, want false when draw+discard+face_up are all empty")
	}

	_, _, err := drawCard(t, e, gs, ids[0], "deck", 0)
	if !isIllegalAction(err) {
		t.Errorf("draw from deck with everything exhausted: err = %v, want engine.ErrIllegalAction", err)
	}
	_, _, err = drawCard(t, e, gs, ids[0], "face_up", 0)
	if !isIllegalAction(err) {
		t.Errorf("draw from face_up with everything exhausted: err = %v, want engine.ErrIllegalAction", err)
	}
	assertInvariants(t, m, gs)
}

func TestDrawCardOutOfTurn(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, ids := newNormalState(m, 2)

	gs.FaceUp = colorInts(ColorRed, ColorBlue, ColorGreen, ColorYellow, ColorBlack)
	gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp))

	_, _, err := drawCard(t, e, gs, ids[1], "face_up", 0)
	if !errors.Is(err, engine.ErrNotYourTurn) {
		t.Errorf("draw by seat 1 while seat 0 holds current_seat: err = %v, want engine.ErrNotYourTurn", err)
	}
}

func TestDrawFaceUpSlotOutOfRange(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, ids := newNormalState(m, 2)

	gs.FaceUp = colorInts(ColorRed, ColorBlue, ColorGreen, ColorYellow, ColorBlack)
	gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp))

	_, _, err := drawCard(t, e, gs, ids[0], "face_up", 7)
	if !isIllegalAction(err) {
		t.Errorf("draw with slot 7: err = %v, want engine.ErrIllegalAction", err)
	}
}

func TestEndDrawDecision(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)

	t.Run("illegal while a second card is still available", func(t *testing.T) {
		gs, ids := newNormalState(m, 2)
		gs.FaceUp = colorInts(ColorRed, ColorBlue, ColorGreen, ColorYellow, ColorBlack)
		gs.DrawPile = colorInts(ColorOrange)
		gs.DiscardPile = fillerCards(TotalTrainCards - len(gs.FaceUp) - len(gs.DrawPile))
		gs.Draw = &pb.DrawProgress{CardsTaken: 1, FaceUpLocoLocked: true}
		gs.Phase = pb.Phase_PHASE_AWAITING_SECOND_DRAW

		_, _, err := applyAction(t, e, gs, ids[0], ActionResolveDecision, ResolveDecisionPayload{Kind: DecisionKindEndDraw})
		if !isIllegalAction(err) {
			t.Errorf("end_draw with a second card available: err = %v, want engine.ErrIllegalAction", err)
		}
	})

	t.Run("legal once no second card is available", func(t *testing.T) {
		gs, ids := newNormalState(m, 2)
		// 2 locomotives face-up is the most that can sit stably; with the
		// draw and discard piles both empty, neither is selectable and
		// nothing else can be drawn either.
		gs.FaceUp = colorInts(ColorLoco, ColorLoco)
		gs.Players[0].Hand = map[int32]int32{int32(ColorPurple): int32(TotalTrainCards - len(gs.FaceUp))} // #nosec G115 -- constant test fixture size
		gs.Draw = &pb.DrawProgress{CardsTaken: 1, FaceUpLocoLocked: true}
		gs.Phase = pb.Phase_PHASE_AWAITING_SECOND_DRAW

		if secondDrawAvailable(gs) {
			t.Fatal("secondDrawAvailable = true, want false (test setup assumption broken)")
		}

		ngs, _, err := applyAction(t, e, gs, ids[0], ActionResolveDecision, ResolveDecisionPayload{Kind: DecisionKindEndDraw})
		if err != nil {
			t.Fatalf("end_draw with no second card available: unexpected error: %v", err)
		}
		assertInvariants(t, m, ngs)
		if ngs.Phase != pb.Phase_PHASE_NORMAL {
			t.Errorf("phase = %v, want PHASE_NORMAL (turn ended)", ngs.Phase)
		}
		if ngs.Draw != nil {
			t.Errorf("draw progress = %+v, want nil", ngs.Draw)
		}
		if ngs.CurrentSeat != 1 {
			t.Errorf("current_seat = %d, want 1 (advanced)", ngs.CurrentSeat)
		}
	})
}
