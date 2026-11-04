package song_guess

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
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

// participantBySeat returns the participant at the given seat index, or nil
// if no such seat exists. It never creates an entry — callers must handle
// absence explicitly, which is what makes the actor-existence invariant
// meaningful.
func participantBySeat(gs *pb.GameState, seat int32) *pb.Participant {
	for _, p := range gs.GetParticipants() {
		if p.GetSeatIndex() == seat {
			return p
		}
	}
	return nil
}

// participantByUser returns the participant seated under the given user id,
// or nil if that user is not seated.
func participantByUser(gs *pb.GameState, userID string) *pb.Participant {
	for _, p := range gs.GetParticipants() {
		if p.GetUserId() == userID {
			return p
		}
	}
	return nil
}

// cellFor returns the selection-table cell for the given (language, bucket)
// pair, or nil if no such cell exists. It never creates an entry.
func cellFor(gs *pb.GameState, language string, bucket int32) *pb.Cell {
	for _, c := range gs.GetCells() {
		if c.GetLanguageName() == language && c.GetBucket() == bucket {
			return c
		}
	}
	return nil
}

// teamState returns the TeamState entry for the given team, or nil for
// Team_TEAM_NONE (the judge's team, which has no TeamState of its own) or a
// team with no matching entry.
func teamState(gs *pb.GameState, t pb.Team) *pb.TeamState {
	if t == pb.Team_TEAM_NONE {
		return nil
	}
	for _, ts := range gs.GetTeams() {
		if ts.GetTeam() == t {
			return ts
		}
	}
	return nil
}

// captainOf returns the active captain of the given team, or nil if the
// team has no captain (e.g. mid-ROSTER, or Team_TEAM_NONE).
func captainOf(gs *pb.GameState, t pb.Team) *pb.Participant {
	if t == pb.Team_TEAM_NONE {
		return nil
	}
	for _, p := range gs.GetParticipants() {
		if p.GetTeam() == t && p.GetIsCaptain() {
			return p
		}
	}
	return nil
}

// judgeOf returns the current judge, or nil if the judgeship is vacant.
func judgeOf(gs *pb.GameState) *pb.Participant {
	for _, p := range gs.GetParticipants() {
		if p.GetIsJudge() {
			return p
		}
	}
	return nil
}

// hostParticipant returns the participant seated at gs.HostSeat, or nil if
// that seat is unoccupied.
func hostParticipant(gs *pb.GameState) *pb.Participant {
	return participantBySeat(gs, gs.GetHostSeat())
}

// opponentOf returns the other team. Team_TEAM_NONE maps to itself: the
// judge has no opponent.
func opponentOf(t pb.Team) pb.Team {
	switch t {
	case pb.Team_TEAM_NONE:
		return pb.Team_TEAM_NONE
	case pb.Team_TEAM_A:
		return pb.Team_TEAM_B
	case pb.Team_TEAM_B:
		return pb.Team_TEAM_A
	default:
		return pb.Team_TEAM_NONE
	}
}

// effectivePhase returns gs.Phase, except gs.ResumePhase when the judge
// seat is currently parked as PHASE_JUDGE_VACANT: parking leaves
// active_team, history and round_no untouched, so any check that reads
// those fields (or asks "is a song genuinely in flight/finished right
// now?") must evaluate against the phase that was interrupted, not the
// interruption itself.
//
// Originally lived in invariants_test.go (laws 12b/12d); moved here in Step
// 11 because view.go's redaction seam needs the identical resolution — a
// song parked mid-PENDING_JUDGEMENT must stay redacted, but one parked
// right after SONG_RESULT is already public — and production code cannot
// import a _test.go file. scoring.go's finalize still carries its own
// short-lived inline duplicate of this same predicate (see finalize's doc
// comment for why); that duplication predates this move and is out of this
// step's scope to collapse.
func effectivePhase(gs *pb.GameState) pb.Phase {
	if gs.GetPhase() == pb.Phase_PHASE_JUDGE_VACANT {
		return gs.GetResumePhase()
	}
	return gs.GetPhase()
}
