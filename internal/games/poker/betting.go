package poker

import (
	"sort"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	"github.com/serediukit/bgex-backend/internal/games/poker/eval"
	pb "github.com/serediukit/bgex-backend/internal/games/poker/pb"
)

// Status shorthands for the protobuf enum values.
const (
	active     = pb.SeatStatus_SEAT_STATUS_ACTIVE
	folded     = pb.SeatStatus_SEAT_STATUS_FOLDED
	allIn      = pb.SeatStatus_SEAT_STATUS_ALLIN
	sittingOut = pb.SeatStatus_SEAT_STATUS_SITTING_OUT
)

// --- seat lookups -----------------------------------------------------------

func seatByIndex(ts *pb.TableState, idx int32) *pb.Seat {
	for _, s := range ts.Seats {
		if s.SeatIndex == idx {
			return s
		}
	}
	return nil
}

func seatByUser(ts *pb.TableState, userID string) *pb.Seat {
	for _, s := range ts.Seats {
		if s.UserId == userID {
			return s
		}
	}
	return nil
}

// inHandSeats returns the seats dealt into the current hand (everyone not
// sitting out), ordered by seat index.
func inHandSeats(ts *pb.TableState) []*pb.Seat {
	var out []*pb.Seat
	for _, s := range ts.Seats {
		if s.Status != sittingOut {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SeatIndex < out[j].SeatIndex })
	return out
}

func indexOfSeat(seats []*pb.Seat, idx int32) int {
	for i, s := range seats {
		if s.SeatIndex == idx {
			return i
		}
	}
	return 0
}

// isActionable reports whether a seat still owes an action this betting round.
func isActionable(ts *pb.TableState, s *pb.Seat) bool {
	if s == nil || s.Status != active || s.Stack <= 0 {
		return false
	}
	return !s.HasActed || s.Committed < ts.CurrentBet
}

// nextActor returns the seat index of the next player to act after afterIdx, or
// -1 when the betting round is complete.
func nextActor(ts *pb.TableState, afterIdx int32) int32 {
	order := inHandSeats(ts)
	n := len(order)
	if n == 0 {
		return -1
	}
	start := 0
	for i, s := range order {
		if s.SeatIndex == afterIdx {
			start = i + 1
			break
		}
	}
	for k := 0; k < n; k++ {
		s := order[(start+k)%n]
		if isActionable(ts, s) {
			return s.SeatIndex
		}
	}
	return -1
}

func countCanAct(ts *pb.TableState) int {
	n := 0
	for _, s := range ts.Seats {
		if s.Status == active && s.Stack > 0 {
			n++
		}
	}
	return n
}

func nonFoldedCount(ts *pb.TableState) int {
	n := 0
	for _, s := range ts.Seats {
		if s.Status == active || s.Status == allIn {
			n++
		}
	}
	return n
}

func maxCommitted(ts *pb.TableState) int64 {
	var m int64
	for _, s := range ts.Seats {
		if s.Committed > m {
			m = s.Committed
		}
	}
	return m
}

// --- dealing helpers ---------------------------------------------------------

func drawCard(ts *pb.TableState) int32 {
	c := ts.Deck[0]
	ts.Deck = ts.Deck[1:]
	return c
}

func dealBoard(ts *pb.TableState, n int) {
	for i := 0; i < n; i++ {
		ts.Board = append(ts.Board, drawCard(ts))
	}
}

func postBlind(s *pb.Seat, amount int64) {
	pay := amount
	if pay > s.Stack {
		pay = s.Stack
	}
	s.Stack -= pay
	s.Committed += pay
	s.TotalCommitted += pay
	if s.Stack == 0 {
		s.Status = allIn
	}
}

// --- betting-round transitions ----------------------------------------------

// advanceStage closes the current betting round and moves to the next street,
// dealing community cards. When no further betting is possible it runs the hand
// out to showdown. It appends any broadcastable events.
func advanceStage(ts *pb.TableState, events *[]engine.Event) {
	// Reset per-round bookkeeping.
	for _, s := range ts.Seats {
		s.Committed = 0
		if s.Status == active {
			s.HasActed = false
		}
	}
	ts.CurrentBet = 0
	ts.MinRaise = ts.BigBlind
	ts.LastAggressor = -1

	switch ts.Stage {
	case pb.Stage_STAGE_PREFLOP:
		dealBoard(ts, 3)
		ts.Stage = pb.Stage_STAGE_FLOP
		*events = append(*events, boardEvent("flop", ts))
	case pb.Stage_STAGE_FLOP:
		dealBoard(ts, 1)
		ts.Stage = pb.Stage_STAGE_TURN
		*events = append(*events, boardEvent("turn", ts))
	case pb.Stage_STAGE_TURN:
		dealBoard(ts, 1)
		ts.Stage = pb.Stage_STAGE_RIVER
		*events = append(*events, boardEvent("river", ts))
	case pb.Stage_STAGE_RIVER:
		runShowdown(ts, events)
		return
	default:
		return
	}

	if countCanAct(ts) < 2 {
		// Betting is closed for the rest of the hand; deal out and show down.
		runOutAndShowdown(ts, events)
		return
	}
	ts.CurrentTurn = nextActor(ts, ts.Button)
}

// runOutAndShowdown deals any remaining community cards without betting, then
// resolves the showdown.
func runOutAndShowdown(ts *pb.TableState, events *[]engine.Event) {
	for ts.Stage != pb.Stage_STAGE_SHOWDOWN {
		switch ts.Stage {
		case pb.Stage_STAGE_PREFLOP:
			dealBoard(ts, 3)
			ts.Stage = pb.Stage_STAGE_FLOP
		case pb.Stage_STAGE_FLOP:
			dealBoard(ts, 1)
			ts.Stage = pb.Stage_STAGE_TURN
		case pb.Stage_STAGE_TURN:
			dealBoard(ts, 1)
			ts.Stage = pb.Stage_STAGE_RIVER
		case pb.Stage_STAGE_RIVER:
			ts.Stage = pb.Stage_STAGE_SHOWDOWN
		}
	}
	runShowdown(ts, events)
}

func boardEvent(kind string, ts *pb.TableState) engine.Event {
	return engine.Event{Type: kind, Data: map[string]any{"board": append([]int32(nil), ts.Board...)}}
}

// --- showdown & pots ---------------------------------------------------------

// endHandUncontested awards the whole pot to the last remaining player (everyone
// else folded); no cards are revealed.
func endHandUncontested(ts *pb.TableState, winner *pb.Seat, events *[]engine.Event) {
	var pot int64
	for _, s := range ts.Seats {
		pot += s.TotalCommitted
	}
	winner.Stack += pot
	clearCommitments(ts)
	ts.Stage = pb.Stage_STAGE_HAND_OVER
	ts.CurrentTurn = -1
	ts.Result = &pb.HandResult{
		Payouts: map[int32]int64{winner.SeatIndex: pot},
		Board:   append([]int32(nil), ts.Board...),
	}
	*events = append(*events, engine.Event{Type: "hand_over", Data: map[string]any{
		"winners": []int32{winner.SeatIndex}, "uncontested": true,
	}})
}

// clearCommitments zeroes per-round and per-hand contributions once a hand is
// resolved: the pot chips have been moved into winners' stacks, so keeping the
// committed buckets around would double-count them.
func clearCommitments(ts *pb.TableState) {
	for _, s := range ts.Seats {
		s.Committed = 0
		s.TotalCommitted = 0
	}
}

// returnUncalled refunds the portion of the top contributor's chips that no one
// matched (the uncalled bet), so pot construction never strands live chips.
func returnUncalled(ts *pb.TableState) {
	type c struct {
		s     *pb.Seat
		total int64
	}
	var cs []c
	for _, s := range ts.Seats {
		if s.TotalCommitted > 0 {
			cs = append(cs, c{s, s.TotalCommitted})
		}
	}
	if len(cs) < 2 {
		return
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].total > cs[j].total })
	// Only the single highest contributor can have an uncalled amount.
	if cs[0].total > cs[1].total {
		diff := cs[0].total - cs[1].total
		cs[0].s.Stack += diff
		cs[0].s.TotalCommitted -= diff
	}
}

// buildPots layers the main pot and any side pots from players' total hand
// contributions. Folded players' chips count toward pot amounts (dead money)
// but are never eligible to win.
func buildPots(ts *pb.TableState) []*pb.Pot {
	type contrib struct {
		idx    int32
		total  int64
		folded bool
	}
	var cs []contrib
	levelSet := map[int64]bool{}
	for _, s := range ts.Seats {
		if s.TotalCommitted > 0 {
			cs = append(cs, contrib{s.SeatIndex, s.TotalCommitted, s.Status == folded})
			levelSet[s.TotalCommitted] = true
		}
	}
	levels := make([]int64, 0, len(levelSet))
	for l := range levelSet {
		levels = append(levels, l)
	}
	sort.Slice(levels, func(i, j int) bool { return levels[i] < levels[j] })

	var pots []*pb.Pot
	var prev int64
	for _, lvl := range levels {
		var amount int64
		var eligible []int32
		for _, c := range cs {
			if c.total >= lvl {
				amount += lvl - prev
				if !c.folded {
					eligible = append(eligible, c.idx)
				}
			}
		}
		prev = lvl
		if amount == 0 || len(eligible) == 0 {
			continue
		}
		// Merge into the previous pot if the eligible set is identical.
		if len(pots) > 0 && sameSeats(pots[len(pots)-1].EligibleSeats, eligible) {
			pots[len(pots)-1].Amount += amount
			continue
		}
		pots = append(pots, &pb.Pot{Amount: amount, EligibleSeats: eligible})
	}
	return pots
}

func sameSeats(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// runShowdown evaluates every live hand, builds pots, and distributes chips.
func runShowdown(ts *pb.TableState, events *[]engine.Event) {
	returnUncalled(ts)
	pots := buildPots(ts)
	ts.Pots = pots

	board := engine.CardsFromInts(ts.Board)
	values := map[int32]eval.HandValue{}
	descriptions := map[int32]string{}
	var shown []int32
	for _, s := range ts.Seats {
		if s.Status == active || s.Status == allIn {
			cards := append(engine.CardsFromInts(s.HoleCards), board...)
			v := eval.Evaluate(cards)
			values[s.SeatIndex] = v
			descriptions[s.SeatIndex] = v.String()
			shown = append(shown, s.SeatIndex)
		}
	}

	payouts := map[int32]int64{}
	for _, pot := range pots {
		// Best hand(s) among this pot's eligible, live players.
		var winners []int32
		var best eval.HandValue
		for _, idx := range pot.EligibleSeats {
			v, ok := values[idx]
			if !ok {
				continue // folded; not eligible to win
			}
			if len(winners) == 0 || v.Compare(best) > 0 {
				best = v
				winners = []int32{idx}
			} else if v.Compare(best) == 0 {
				winners = append(winners, idx)
			}
		}
		if len(winners) == 0 {
			continue
		}
		distribute(ts, pot.Amount, winners, payouts)
	}

	clearCommitments(ts)
	ts.Stage = pb.Stage_STAGE_HAND_OVER
	ts.CurrentTurn = -1
	ts.Result = &pb.HandResult{
		Payouts:          payouts,
		Board:            append([]int32(nil), ts.Board...),
		HandDescriptions: descriptions,
		ShownSeats:       shown,
	}
	*events = append(*events, engine.Event{Type: "showdown", Data: map[string]any{
		"payouts": payouts, "board": append([]int32(nil), ts.Board...),
	}})
}

// distribute splits a pot among winners, adding to each winner's stack and to
// the payouts tally. Odd remainder chips go to the earliest seats by index.
func distribute(ts *pb.TableState, amount int64, winners []int32, payouts map[int32]int64) {
	sort.Slice(winners, func(i, j int) bool { return winners[i] < winners[j] })
	share := amount / int64(len(winners))
	remainder := amount % int64(len(winners))
	for i, idx := range winners {
		w := share
		if int64(i) < remainder {
			w++
		}
		payouts[idx] += w
		seatByIndex(ts, idx).Stack += w
	}
}
