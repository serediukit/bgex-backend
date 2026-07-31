package poker

import (
	"github.com/google/uuid"

	pb "github.com/serediukit/bgex-backend/internal/games/poker/pb"
)

// TableView is the per-player, redacted projection of a hand sent to clients.
// Opponents' hole cards are never included unless revealed at showdown.
type TableView struct {
	HandNo      int32         `json:"hand_no"`
	Stage       string        `json:"stage"`
	Board       []int32       `json:"board"`
	Pot         int64         `json:"pot"`
	CurrentTurn int32         `json:"current_turn"`
	Button      int32         `json:"button"`
	SmallBlind  int64         `json:"small_blind"`
	BigBlind    int64         `json:"big_blind"`
	CurrentBet  int64         `json:"current_bet"`
	Seats       []SeatView    `json:"seats"`
	YourSeat    int32         `json:"your_seat"`
	Legal       *LegalActions `json:"legal,omitempty"`
	Result      *ResultView   `json:"result,omitempty"`
}

// SeatView is one seat as seen by a particular viewer.
type SeatView struct {
	SeatIndex int32   `json:"seat_index"`
	UserID    string  `json:"user_id"`
	Stack     int64   `json:"stack"`
	Committed int64   `json:"committed"`
	Status    string  `json:"status"`
	HoleCards []int32 `json:"hole_cards,omitempty"`
	HasCards  bool    `json:"has_cards"`
	IsTurn    bool    `json:"is_turn"`
	IsButton  bool    `json:"is_button"`
}

// LegalActions describes what the viewer may do on their turn. Amounts are
// expressed as the target total commitment this round (a "bet/raise to" value).
type LegalActions struct {
	CanFold    bool  `json:"can_fold"`
	CanCheck   bool  `json:"can_check"`
	CanCall    bool  `json:"can_call"`
	CallAmount int64 `json:"call_amount"`
	CanBet     bool  `json:"can_bet"`
	CanRaise   bool  `json:"can_raise"`
	MinBet     int64 `json:"min_bet"`
	MinRaise   int64 `json:"min_raise"`
	MaxAmount  int64 `json:"max_amount"`
}

// ResultView summarizes a finished hand.
type ResultView struct {
	Payouts          map[int32]int64  `json:"payouts"`
	HandDescriptions map[int32]string `json:"hand_descriptions,omitempty"`
	ShownSeats       []int32          `json:"shown_seats,omitempty"`
}

var stageNames = map[pb.Stage]string{
	pb.Stage_STAGE_PREFLOP:   "preflop",
	pb.Stage_STAGE_FLOP:      "flop",
	pb.Stage_STAGE_TURN:      "turn",
	pb.Stage_STAGE_RIVER:     "river",
	pb.Stage_STAGE_SHOWDOWN:  "showdown",
	pb.Stage_STAGE_HAND_OVER: "hand_over",
}

var statusNames = map[pb.SeatStatus]string{
	active:     "active",
	folded:     "folded",
	allIn:      "all_in",
	sittingOut: "sitting_out",
}

// View returns the redacted table view for the given user.
func (e *Engine) View(state []byte, forUser uuid.UUID) (any, error) {
	ts, err := unmarshal(state)
	if err != nil {
		return nil, err
	}
	me := forUser.String()

	shown := map[int32]bool{}
	if ts.Result != nil {
		for _, idx := range ts.Result.ShownSeats {
			shown[idx] = true
		}
	}

	var pot int64
	yourSeat := int32(-1)
	view := TableView{
		HandNo: ts.HandNo,
		Stage:  stageNames[ts.Stage],
		// Always a JSON array (never null) so clients can index safely even
		// before the flop, when the board is empty.
		Board:       append(make([]int32, 0, len(ts.Board)), ts.Board...),
		CurrentTurn: ts.CurrentTurn,
		Button:      ts.Button,
		SmallBlind:  ts.SmallBlind,
		BigBlind:    ts.BigBlind,
		CurrentBet:  ts.CurrentBet,
		YourSeat:    -1,
	}

	for _, s := range ts.Seats {
		pot += s.TotalCommitted
		sv := SeatView{
			SeatIndex: s.SeatIndex,
			UserID:    s.UserId,
			Stack:     s.Stack,
			Committed: s.Committed,
			Status:    statusNames[s.Status],
			HasCards:  (s.Status == active || s.Status == allIn) && len(s.HoleCards) > 0,
			IsTurn:    ts.CurrentTurn == s.SeatIndex,
			IsButton:  ts.Button == s.SeatIndex,
		}
		if s.UserId == me {
			yourSeat = s.SeatIndex
			if len(s.HoleCards) > 0 {
				sv.HoleCards = append([]int32(nil), s.HoleCards...)
			}
		} else if shown[s.SeatIndex] {
			sv.HoleCards = append([]int32(nil), s.HoleCards...)
		}
		view.Seats = append(view.Seats, sv)
	}

	view.Pot = pot
	view.YourSeat = yourSeat

	if ts.Stage != pb.Stage_STAGE_HAND_OVER && yourSeat >= 0 && ts.CurrentTurn == yourSeat {
		view.Legal = legalActions(ts, seatByIndex(ts, yourSeat))
	}

	if ts.Result != nil {
		view.Result = &ResultView{
			Payouts:          ts.Result.Payouts,
			HandDescriptions: ts.Result.HandDescriptions,
			ShownSeats:       ts.Result.ShownSeats,
		}
	}
	return view, nil
}

func legalActions(ts *pb.TableState, seat *pb.Seat) *LegalActions {
	if seat == nil || seat.Status != active || seat.Stack <= 0 {
		return nil
	}
	toCall := ts.CurrentBet - seat.Committed
	maxAmount := seat.Committed + seat.Stack

	la := &LegalActions{
		CanFold:   true,
		MaxAmount: maxAmount,
	}
	if toCall <= 0 {
		la.CanCheck = true
	} else {
		la.CanCall = true
		la.CallAmount = min64(toCall, seat.Stack) // chips needed to call
	}

	if ts.CurrentBet == 0 {
		// Opening bet available.
		la.CanBet = true
		la.MinBet = min64(ts.BigBlind, maxAmount)
	} else if seat.Stack > toCall {
		// Can raise if there are chips beyond the call.
		la.CanRaise = true
		la.MinRaise = min64(ts.CurrentBet+ts.MinRaise, maxAmount)
	}
	return la
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
