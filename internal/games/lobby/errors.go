package lobby

import "errors"

var (
	ErrNotFound         = errors.New("lobby not found")
	ErrSeatTaken        = errors.New("seat already taken")
	ErrAlreadySeated    = errors.New("you are already seated in a game")
	ErrLobbyFull        = errors.New("lobby is full")
	ErrForbidden        = errors.New("not authorized for this action")
	ErrNotWaiting       = errors.New("lobby is not accepting players")
	ErrNotEnoughPlayers = errors.New("not enough players to start")
	ErrUnknownGame      = errors.New("unknown game")
	ErrInvalidSeat      = errors.New("invalid seat index")
	ErrNotSeated        = errors.New("you are not seated in this lobby")
	ErrInvalidConfig    = errors.New("invalid game configuration")
)
