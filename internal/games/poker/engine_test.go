package poker

import (
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/poker/pb"
)

func seats(n int, stack int64) ([]engine.SeatInit, []uuid.UUID) {
	inits := make([]engine.SeatInit, n)
	ids := make([]uuid.UUID, n)
	for i := 0; i < n; i++ {
		ids[i] = uuid.New()
		inits[i] = engine.SeatInit{Seat: i, UserID: ids[i], Stack: stack}
	}
	return inits, ids
}

// currentUser returns the uuid of the seat whose turn it is.
func currentUser(t *testing.T, state []byte) uuid.UUID {
	t.Helper()
	ts, err := unmarshal(state)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	s := seatByIndex(ts, ts.CurrentTurn)
	if s == nil {
		t.Fatalf("no seat to act (current_turn=%d)", ts.CurrentTurn)
	}
	return uuid.MustParse(s.UserId)
}

func act(t *testing.T, e *Engine, state []byte, actionType string, amount int64) []byte {
	t.Helper()
	u := currentUser(t, state)
	next, _, err := e.Apply(state, engine.Action{UserID: u, Type: actionType, Amount: amount})
	if err != nil {
		t.Fatalf("apply %s %d: %v", actionType, amount, err)
	}
	return next
}

func mustState(t *testing.T, state []byte) *pb.TableState {
	t.Helper()
	ts, err := unmarshal(state)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return ts
}

func totalChips(ts *pb.TableState) int64 {
	var total int64
	for _, s := range ts.Seats {
		total += s.Stack + s.TotalCommitted
	}
	return total
}

func TestInitStatePostsBlinds(t *testing.T) {
	e := New()
	inits, _ := seats(3, 1000)
	state, _, err := e.InitState(inits)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	ts := mustState(t, state)

	if ts.HandNo != 1 {
		t.Errorf("hand_no = %d, want 1", ts.HandNo)
	}
	if ts.CurrentBet != 10 {
		t.Errorf("current_bet = %d, want 10 (big blind)", ts.CurrentBet)
	}
	// 3-handed: button = seat 0 acts first preflop (UTG); SB=1, BB=2.
	if ts.CurrentTurn != 0 {
		t.Errorf("current_turn = %d, want 0", ts.CurrentTurn)
	}
	var pot int64
	for _, s := range ts.Seats {
		pot += s.TotalCommitted
		if len(s.HoleCards) != 2 {
			t.Errorf("seat %d has %d hole cards, want 2", s.SeatIndex, len(s.HoleCards))
		}
	}
	if pot != 15 {
		t.Errorf("pot = %d, want 15 (5+10 blinds)", pot)
	}
	if totalChips(ts) != 3000 {
		t.Errorf("total chips = %d, want 3000", totalChips(ts))
	}
}

func TestFoldToWin(t *testing.T) {
	e := New()
	inits, _ := seats(3, 1000)
	state, _, err := e.InitState(inits)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	state = act(t, e, state, "fold", 0)  // seat 0 folds
	state = act(t, e, state, "fold", 0)  // seat 1 (SB) folds
	ts := mustState(t, state)

	if ts.Stage != pb.Stage_STAGE_HAND_OVER {
		t.Fatalf("stage = %v, want HAND_OVER", ts.Stage)
	}
	// Seat 2 (BB) wins the 15-chip pot uncontested: 1000 - 10 + 15 = 1005.
	winner := seatByIndex(ts, 2)
	if winner.Stack != 1005 {
		t.Errorf("winner stack = %d, want 1005", winner.Stack)
	}
	if totalChips(ts) != 3000 {
		t.Errorf("total chips = %d, want 3000 (conserved)", totalChips(ts))
	}
}

func TestHeadsUpCheckDownToShowdown(t *testing.T) {
	e := New()
	inits, _ := seats(2, 1000)
	state, _, err := e.InitState(inits)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	// Preflop: button/SB calls, BB checks option.
	state = act(t, e, state, "call", 0)
	state = act(t, e, state, "check", 0)
	// Flop, turn, river: check-check each street.
	for street := 0; street < 3; street++ {
		state = act(t, e, state, "check", 0)
		state = act(t, e, state, "check", 0)
	}
	ts := mustState(t, state)

	if ts.Stage != pb.Stage_STAGE_HAND_OVER {
		t.Fatalf("stage = %v, want HAND_OVER", ts.Stage)
	}
	if len(ts.Board) != 5 {
		t.Errorf("board has %d cards, want 5", len(ts.Board))
	}
	if totalChips(ts) != 2000 {
		t.Errorf("total chips = %d, want 2000 (conserved)", totalChips(ts))
	}
	if ts.Result == nil || len(ts.Result.Payouts) == 0 {
		t.Errorf("expected a showdown result with payouts")
	}
}

func TestSidePotLayering(t *testing.T) {
	// Three players all-in for different totals, no folds:
	// seat0=50, seat1=100, seat2=100  =>  main 150 (all eligible), side 100 (1,2).
	ts := &pb.TableState{
		Seats: []*pb.Seat{
			{SeatIndex: 0, TotalCommitted: 50, Status: allIn},
			{SeatIndex: 1, TotalCommitted: 100, Status: allIn},
			{SeatIndex: 2, TotalCommitted: 100, Status: active},
		},
	}
	returnUncalled(ts)
	pots := buildPots(ts)
	if len(pots) != 2 {
		t.Fatalf("got %d pots, want 2: %+v", len(pots), pots)
	}
	if pots[0].Amount != 150 || len(pots[0].EligibleSeats) != 3 {
		t.Errorf("main pot = %d eligible %v, want 150 / 3 seats", pots[0].Amount, pots[0].EligibleSeats)
	}
	if pots[1].Amount != 100 || len(pots[1].EligibleSeats) != 2 {
		t.Errorf("side pot = %d eligible %v, want 100 / 2 seats", pots[1].Amount, pots[1].EligibleSeats)
	}
}

func TestReturnUncalledBet(t *testing.T) {
	// Seat0 shoves 200 but only seat1's 80 can call; 120 must be returned.
	ts := &pb.TableState{
		Seats: []*pb.Seat{
			{SeatIndex: 0, Stack: 0, TotalCommitted: 200, Status: active},
			{SeatIndex: 1, Stack: 0, TotalCommitted: 80, Status: allIn},
		},
	}
	returnUncalled(ts)
	s0 := seatByIndex(ts, 0)
	if s0.TotalCommitted != 80 {
		t.Errorf("seat0 committed = %d, want 80 after return", s0.TotalCommitted)
	}
	if s0.Stack != 120 {
		t.Errorf("seat0 stack = %d, want 120 returned", s0.Stack)
	}
}

func TestNextHandRotatesButton(t *testing.T) {
	e := New()
	inits, _ := seats(3, 1000)
	state, _, err := e.InitState(inits)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	btn0 := mustState(t, state).Button
	// Fold around to end hand 1 quickly.
	state = act(t, e, state, "fold", 0)
	state = act(t, e, state, "fold", 0)

	next, _, err := e.NextHand(state)
	if err != nil {
		t.Fatalf("next hand: %v", err)
	}
	ts := mustState(t, next)
	if ts.Button == btn0 {
		t.Errorf("button did not rotate (still %d)", ts.Button)
	}
	if ts.HandNo != 2 {
		t.Errorf("hand_no = %d, want 2", ts.HandNo)
	}
	if totalChips(ts) != 3000 {
		t.Errorf("total chips = %d, want 3000", totalChips(ts))
	}
}
