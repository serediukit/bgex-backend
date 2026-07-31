// Package poker implements No-Limit Texas Hold'em as an engine.Engine. The
// hand state is a protobuf blob (see pb/) that the platform persists as the
// source of truth; every mutation goes through Apply/NextHand here.
//
// Rules coverage: blinds (incl. heads-up), preflop/flop/turn/river betting,
// fold/check/call/bet/raise/all-in, side pots for unequal all-ins, and
// multi-way showdown with split pots. Simplification: a short all-in that does
// not constitute a full raise does not strictly re-cap re-raising rights for
// players who already acted (acceptable for play-money).
package poker

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/poker/pb"
)

const (
	// GameKey identifies poker lobbies.
	GameKey = "poker"

	defaultBuyIn      = 1000
	defaultSmallBlind = 5
	defaultBigBlind   = 10
	minSeats          = 2
	maxSeats          = 9
)

// Engine is the Texas Hold'em implementation of engine.Engine.
type Engine struct{}

// New returns a poker Engine.
func New() *Engine { return &Engine{} }

func (*Engine) GameKey() string     { return GameKey }
func (*Engine) MinSeats() int       { return minSeats }
func (*Engine) MaxSeats() int       { return maxSeats }
func (*Engine) DefaultBuyIn() int64 { return defaultBuyIn }

// marshal/unmarshal isolate the protobuf wire format from the rest of the code.
func marshal(ts *pb.TableState) ([]byte, error) {
	b, err := proto.Marshal(ts)
	if err != nil {
		return nil, fmt.Errorf("marshal table state: %w", err)
	}
	return b, nil
}

func unmarshal(state []byte) (*pb.TableState, error) {
	var ts pb.TableState
	if err := proto.Unmarshal(state, &ts); err != nil {
		return nil, fmt.Errorf("unmarshal table state: %w", err)
	}
	return &ts, nil
}

// InitState builds the first-hand state for the seated players.
func (e *Engine) InitState(seats []engine.SeatInit) ([]byte, []engine.Event, error) {
	if len(seats) < minSeats {
		return nil, nil, engine.ErrNotEnoughPlayers
	}
	ts := &pb.TableState{
		HandNo:        0,
		SmallBlind:    defaultSmallBlind,
		BigBlind:      defaultBigBlind,
		Button:        -1,
		LastAggressor: -1,
	}
	lowest := int32(-1)
	for _, si := range seats {
		ts.Seats = append(ts.Seats, &pb.Seat{
			SeatIndex: int32(si.Seat),
			UserId:    si.UserID.String(),
			Stack:     si.Stack,
			Status:    sittingOut,
		})
		if lowest < 0 || int32(si.Seat) < lowest {
			lowest = int32(si.Seat)
		}
	}
	// The first hand's button sits on the lowest seat index; NextHand rotates it.
	ts.Button = lowest
	ts.HandNo = 1

	events := []engine.Event{}
	if err := dealHand(ts, &events); err != nil {
		return nil, nil, err
	}
	b, err := marshal(ts)
	if err != nil {
		return nil, nil, err
	}
	return b, events, nil
}

// NextHand rotates the button and deals a fresh hand, carrying stacks over.
func (e *Engine) NextHand(state []byte) ([]byte, []engine.Event, error) {
	ts, err := unmarshal(state)
	if err != nil {
		return nil, nil, err
	}
	// Drop players who busted so they no longer take a seat in the hand.
	ts.Button = nextSeatWithChips(ts, ts.Button)
	ts.HandNo++
	ts.Result = nil

	events := []engine.Event{}
	if err := dealHand(ts, &events); err != nil {
		return nil, nil, err
	}
	b, err := marshal(ts)
	if err != nil {
		return nil, nil, err
	}
	return b, events, nil
}

// nextSeatWithChips returns the next seat index (clockwise from `from`) whose
// player still has chips. Falls back to `from` if none other qualifies.
func nextSeatWithChips(ts *pb.TableState, from int32) int32 {
	order := make([]*pb.Seat, len(ts.Seats))
	copy(order, ts.Seats)
	// order by seat index
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if order[j].SeatIndex < order[i].SeatIndex {
				order[i], order[j] = order[j], order[i]
			}
		}
	}
	n := len(order)
	start := 0
	for i, s := range order {
		if s.SeatIndex == from {
			start = i + 1
			break
		}
	}
	for k := 0; k < n; k++ {
		s := order[(start+k)%n]
		if s.Stack > 0 {
			return s.SeatIndex
		}
	}
	return from
}

// dealHand shuffles, deals hole cards, posts blinds, and sets the first actor.
func dealHand(ts *pb.TableState, events *[]engine.Event) error {
	for _, s := range ts.Seats {
		s.Committed = 0
		s.TotalCommitted = 0
		s.HoleCards = nil
		s.HasActed = false
		if s.Stack > 0 {
			s.Status = active
		} else {
			s.Status = sittingOut
		}
	}

	inHand := inHandSeats(ts)
	if len(inHand) < minSeats {
		return engine.ErrNotEnoughPlayers
	}

	ts.Deck = engine.ShuffledDeck()
	ts.Board = nil
	ts.Pots = nil
	ts.Result = nil
	ts.Stage = pb.Stage_STAGE_PREFLOP
	ts.CurrentBet = 0
	ts.MinRaise = ts.BigBlind
	ts.LastAggressor = -1

	// Ensure the button sits on an in-hand seat.
	if seatByIndex(ts, ts.Button) == nil || seatByIndex(ts, ts.Button).Status == sittingOut {
		ts.Button = inHand[0].SeatIndex
	}

	n := len(inHand)
	bpos := indexOfSeat(inHand, ts.Button)
	var sbPos, bbPos, firstPos int
	if n == 2 {
		sbPos, bbPos, firstPos = bpos, (bpos+1)%n, bpos
	} else {
		sbPos, bbPos, firstPos = (bpos+1)%n, (bpos+2)%n, (bpos+3)%n
	}

	// Deal two hole cards to each in-hand seat.
	for round := 0; round < 2; round++ {
		for k := 0; k < n; k++ {
			s := inHand[(sbPos+k)%n]
			s.HoleCards = append(s.HoleCards, drawCard(ts))
		}
	}

	postBlind(inHand[sbPos], ts.SmallBlind)
	postBlind(inHand[bbPos], ts.BigBlind)
	ts.CurrentBet = maxCommitted(ts)
	ts.MinRaise = ts.BigBlind
	ts.LastAggressor = inHand[bbPos].SeatIndex

	*events = append(*events, engine.Event{Type: "hand_start", Data: map[string]any{
		"hand_no": ts.HandNo, "button": ts.Button,
	}})

	first := inHand[firstPos]
	if first.Status == active && first.Stack > 0 {
		ts.CurrentTurn = first.SeatIndex
	} else {
		ts.CurrentTurn = nextActor(ts, first.SeatIndex)
	}

	// If the blinds put everyone all-in, there is nothing to bet — run it out.
	if ts.CurrentTurn < 0 || countCanAct(ts) < 2 {
		runOutAndShowdown(ts, events)
	}
	return nil
}

// Apply validates and applies a player's action.
func (e *Engine) Apply(state []byte, a engine.Action) ([]byte, []engine.Event, error) {
	ts, err := unmarshal(state)
	if err != nil {
		return nil, nil, err
	}
	if ts.Stage == pb.Stage_STAGE_HAND_OVER {
		return nil, nil, fmt.Errorf("%w: hand is over", engine.ErrIllegalAction)
	}
	seat := seatByUser(ts, a.UserID.String())
	if seat == nil {
		return nil, nil, fmt.Errorf("%w: not seated", engine.ErrIllegalAction)
	}
	if ts.CurrentTurn != seat.SeatIndex {
		return nil, nil, engine.ErrNotYourTurn
	}

	events := []engine.Event{}
	if err := applyAction(ts, seat, a); err != nil {
		return nil, nil, err
	}
	events = append(events, engine.Event{Type: "action", Data: map[string]any{
		"seat": seat.SeatIndex, "user_id": seat.UserId, "action": a.Type, "amount": a.Amount,
	}})

	// Resolve what happens next.
	if nonFoldedCount(ts) == 1 {
		endHandUncontested(ts, lastLiveSeat(ts), &events)
	} else if nxt := nextActor(ts, seat.SeatIndex); nxt < 0 {
		advanceStage(ts, &events)
	} else {
		ts.CurrentTurn = nxt
	}

	b, err := marshal(ts)
	if err != nil {
		return nil, nil, err
	}
	return b, events, nil
}

func lastLiveSeat(ts *pb.TableState) *pb.Seat {
	for _, s := range ts.Seats {
		if s.Status == active || s.Status == allIn {
			return s
		}
	}
	return nil
}

// applyAction mutates the seat/state for a single legal action.
func applyAction(ts *pb.TableState, seat *pb.Seat, a engine.Action) error {
	toCall := ts.CurrentBet - seat.Committed
	switch a.Type {
	case "fold":
		seat.Status = folded
		seat.HasActed = true

	case "check":
		if toCall > 0 {
			return fmt.Errorf("%w: cannot check facing a bet", engine.ErrIllegalAction)
		}
		seat.HasActed = true

	case "call":
		pay := toCall
		if pay > seat.Stack {
			pay = seat.Stack
		}
		commit(seat, pay)
		seat.HasActed = true

	case "bet":
		if ts.CurrentBet != 0 {
			return fmt.Errorf("%w: cannot bet when a bet exists (raise instead)", engine.ErrIllegalAction)
		}
		target := a.Amount
		pay := target - seat.Committed
		if pay <= 0 || pay > seat.Stack {
			return fmt.Errorf("%w: invalid bet size", engine.ErrIllegalAction)
		}
		allInBet := pay == seat.Stack
		if target < ts.BigBlind && !allInBet {
			return fmt.Errorf("%w: bet below minimum", engine.ErrIllegalAction)
		}
		commit(seat, pay)
		ts.CurrentBet = target
		ts.MinRaise = target
		ts.LastAggressor = seat.SeatIndex
		reopen(ts, seat.SeatIndex)
		seat.HasActed = true

	case "raise":
		if ts.CurrentBet == 0 {
			return fmt.Errorf("%w: nothing to raise (bet instead)", engine.ErrIllegalAction)
		}
		target := a.Amount
		pay := target - seat.Committed
		if target <= ts.CurrentBet || pay > seat.Stack {
			return fmt.Errorf("%w: invalid raise size", engine.ErrIllegalAction)
		}
		increment := target - ts.CurrentBet
		allInRaise := pay == seat.Stack
		if increment < ts.MinRaise && !allInRaise {
			return fmt.Errorf("%w: raise below minimum", engine.ErrIllegalAction)
		}
		commit(seat, pay)
		ts.CurrentBet = target
		if increment >= ts.MinRaise {
			// Full raise: reopens the action for everyone else.
			ts.MinRaise = increment
			ts.LastAggressor = seat.SeatIndex
			reopen(ts, seat.SeatIndex)
		}
		seat.HasActed = true

	default:
		return fmt.Errorf("%w: unknown action %q", engine.ErrIllegalAction, a.Type)
	}
	return nil
}

// commit moves `pay` chips from a seat's stack into the pot, updating status.
func commit(seat *pb.Seat, pay int64) {
	seat.Stack -= pay
	seat.Committed += pay
	seat.TotalCommitted += pay
	if seat.Stack == 0 {
		seat.Status = allIn
	}
}

// reopen clears has_acted for every other active player so they must respond to
// the new aggression.
func reopen(ts *pb.TableState, raiser int32) {
	for _, s := range ts.Seats {
		if s.SeatIndex != raiser && s.Status == active {
			s.HasActed = false
		}
	}
}

// IsHandOver reports whether the current hand has finished.
func (e *Engine) IsHandOver(state []byte) bool {
	ts, err := unmarshal(state)
	if err != nil {
		return false
	}
	return ts.Stage == pb.Stage_STAGE_HAND_OVER
}

