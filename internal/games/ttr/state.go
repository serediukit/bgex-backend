package ttr

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// marshal isolates the protobuf wire format from the rest of the code.
func marshal(gs *pb.GameState) ([]byte, error) {
	b, err := proto.Marshal(gs)
	if err != nil {
		return nil, fmt.Errorf("marshal game state: %w", err)
	}
	return b, nil
}

// unmarshal isolates the protobuf wire format from the rest of the code.
func unmarshal(state []byte) (*pb.GameState, error) {
	var gs pb.GameState
	if err := proto.Unmarshal(state, &gs); err != nil {
		return nil, fmt.Errorf("unmarshal game state: %w", err)
	}
	return &gs, nil
}

// playerBySeat returns the player at the given seat index, or nil if no such
// seat exists.
func playerBySeat(gs *pb.GameState, seat int32) *pb.PlayerState {
	for _, p := range gs.Players {
		if p.SeatIndex == seat {
			return p
		}
	}
	return nil
}

// playerByUser returns the player seated under the given user id, or nil if
// that user is not seated.
func playerByUser(gs *pb.GameState, userID string) *pb.PlayerState {
	for _, p := range gs.Players {
		if p.UserId == userID {
			return p
		}
	}
	return nil
}

// activePlayers returns the non-resigned players, preserving seat order.
func activePlayers(gs *pb.GameState) []*pb.PlayerState {
	active := make([]*pb.PlayerState, 0, len(gs.Players))
	for _, p := range gs.Players {
		if !p.Resigned {
			active = append(active, p)
		}
	}
	return active
}
