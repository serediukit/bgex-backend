package song_guess

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// This file implements plan "Chosen Approach" transition table rows 1-5: the
// five host-only PHASE_ROSTER actions (Q3, Req 17). assign_judge is also
// legal in PHASE_JUDGE_VACANT per row 27 (Step 10's vacancy-repair branch,
// applyAssignJudgeInVacant) — the other four actions remain PHASE_ROSTER-only.

// requireHost resolves the acting participant and confirms they occupy
// gs.HostSeat, returning engine.ErrNotSeated / engine.ErrNotYourTurn
// otherwise. Shared by every action in this file.
func requireHost(gs *pb.GameState, a engine.Action) (*pb.Participant, error) {
	p := participantByUser(gs, a.UserID.String())
	if p == nil {
		return nil, engine.ErrNotSeated
	}
	if p.GetSeatIndex() != gs.GetHostSeat() {
		return nil, engine.ErrNotYourTurn
	}
	return p, nil
}

// requireRosterPhase rejects anything but PHASE_ROSTER.
func requireRosterPhase(gs *pb.GameState) error {
	if gs.GetPhase() != pb.Phase_PHASE_ROSTER {
		return engine.ErrWrongPhase
	}
	return nil
}

// rosterHostGuard is the combined "host, and PHASE_ROSTER" guard shared by
// all five actions in this file.
func rosterHostGuard(gs *pb.GameState, a engine.Action) (*pb.Participant, error) {
	p, err := requireHost(gs, a)
	if err != nil {
		return nil, err
	}
	if err := requireRosterPhase(gs); err != nil {
		return nil, err
	}
	return p, nil
}

// rosterChangedEvent is the roster_changed event payload for a single
// participant mutation (plan "Engine events" table): seat/team/is_captain/
// is_judge are all public roster facts.
func rosterChangedEvent(p *pb.Participant) engine.Event {
	return engine.Event{
		Type: "roster_changed",
		Data: map[string]any{
			"seat":       p.GetSeatIndex(),
			"team":       teamWire(p.GetTeam()),
			"is_captain": p.GetIsCaptain(),
			"is_judge":   p.GetIsJudge(),
		},
	}
}

// promoteCaptain sets is_captain on the lowest-seat active member of team,
// or does nothing if team has no active member (e.g. it was just emptied).
// gs.Participants is sorted by seat_index (state.go's contract), so the
// first match is the lowest seat. Shared by InitState's host-demotion
// default (engine.go) and assign_judge below.
func promoteCaptain(gs *pb.GameState, team pb.Team) *pb.Participant {
	for _, p := range gs.GetParticipants() {
		if p.GetActive() && p.GetTeam() == team {
			p.IsCaptain = true
			return p
		}
	}
	return nil
}

// applyAssignTeam implements transition table row 1.
func applyAssignTeam(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if _, err := rosterHostGuard(gs, a); err != nil {
		return nil, err
	}
	payload, err := decodePayload[AssignTeamPayload](a.Payload)
	if err != nil {
		return nil, err
	}
	target := participantBySeat(gs, payload.Seat)
	if target == nil || !target.GetActive() {
		return nil, fmt.Errorf("%w: seat %d is not an active participant", engine.ErrIllegalAction, payload.Seat)
	}
	team, ok := parseTeam(payload.Team)
	if !ok {
		return nil, fmt.Errorf("%w: unknown team %q", engine.ErrIllegalAction, payload.Team)
	}
	if team != pb.Team_TEAM_NONE && target.GetIsJudge() {
		return nil, fmt.Errorf("%w: seat %d is the judge; reassign the judgeship first", engine.ErrIllegalAction, payload.Seat)
	}

	prevTeam := target.GetTeam()
	target.Team = team
	if target.GetIsCaptain() && team != prevTeam {
		// Moving a captain off their team clears their captaincy (plan row 1).
		target.IsCaptain = false
	}

	return []engine.Event{rosterChangedEvent(target)}, nil
}

// applyAssignCaptain implements transition table row 2.
func applyAssignCaptain(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if _, err := rosterHostGuard(gs, a); err != nil {
		return nil, err
	}
	payload, err := decodePayload[AssignCaptainPayload](a.Payload)
	if err != nil {
		return nil, err
	}
	target := participantBySeat(gs, payload.Seat)
	if target == nil || !target.GetActive() {
		return nil, fmt.Errorf("%w: seat %d is not an active participant", engine.ErrIllegalAction, payload.Seat)
	}
	if target.GetTeam() == pb.Team_TEAM_NONE {
		return nil, fmt.Errorf("%w: seat %d is not on a team", engine.ErrIllegalAction, payload.Seat)
	}

	events := make([]engine.Event, 0, 2)
	if prev := captainOf(gs, target.GetTeam()); prev != nil && prev.GetSeatIndex() != target.GetSeatIndex() {
		prev.IsCaptain = false
		events = append(events, rosterChangedEvent(prev))
	}
	target.IsCaptain = true
	events = append(events, rosterChangedEvent(target))

	return events, nil
}

// applyAssignJudge implements transition table rows 3 and 27: the host may
// assign the judgeship during PHASE_ROSTER (row 3, initial setup) or to
// repair a vacancy during PHASE_JUDGE_VACANT (row 27, Step 10). Both phases
// share the same participant-mutation logic (reassignJudge); only the
// phase guard and the post-mutation phase transition differ, so the phase
// switch lives here rather than duplicating reassignJudge.
func applyAssignJudge(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	switch gs.GetPhase() {
	case pb.Phase_PHASE_ROSTER:
		return applyAssignJudgeInRoster(gs, a)
	case pb.Phase_PHASE_JUDGE_VACANT:
		return applyAssignJudgeInVacant(gs, a)
	case pb.Phase_PHASE_UNSPECIFIED, pb.Phase_PHASE_ROUND_SELECT, pb.Phase_PHASE_SONG_PLAY,
		pb.Phase_PHASE_PENDING_JUDGEMENT, pb.Phase_PHASE_SONG_RESULT, pb.Phase_PHASE_FINISHED:
		return nil, engine.ErrWrongPhase
	}
	return nil, engine.ErrWrongPhase
}

// applyAssignJudgeInRoster implements row 3: the ordinary PHASE_ROSTER path.
func applyAssignJudgeInRoster(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	target, err := hostAssignJudgeTarget(gs, a)
	if err != nil {
		return nil, err
	}
	return reassignJudge(gs, target), nil
}

// applyAssignJudgeInVacant implements row 27: the host repairs a vacated
// judgeship. gs.Phase is restored to gs.ResumePhase BEFORE the terminal
// check below (never after) so that if assigning a team member as judge
// empties their team, finalize sees the real interrupted phase — not
// PHASE_JUDGE_VACANT itself — when deciding whether a song is still in
// flight (the row-26 terminal check firing instead, per the step brief).
func applyAssignJudgeInVacant(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	target, err := hostAssignJudgeTarget(gs, a)
	if err != nil {
		return nil, err
	}
	events := reassignJudge(gs, target)
	events = append(events, engine.Event{
		Type: "judge_restored",
		Data: map[string]any{"seat": target.GetSeatIndex()},
	})

	gs.Phase = gs.GetResumePhase()
	gs.ResumePhase = pb.Phase_PHASE_UNSPECIFIED

	if terminalCondition(gs) {
		return append(events, finalize(gs)...), nil
	}
	return events, nil
}

// hostAssignJudgeTarget resolves the host actor and the AssignJudgePayload's
// target seat, shared by both PHASE_ROSTER and PHASE_JUDGE_VACANT
// assign_judge (rows 3 and 27): host-only in both phases, decoded and
// validated identically. It deliberately does NOT check gs.GetPhase() —
// applyAssignJudge's switch already resolved that before either caller runs.
func hostAssignJudgeTarget(gs *pb.GameState, a engine.Action) (*pb.Participant, error) {
	if _, err := requireHost(gs, a); err != nil {
		return nil, err
	}
	payload, err := decodePayload[AssignJudgePayload](a.Payload)
	if err != nil {
		return nil, err
	}
	target := participantBySeat(gs, payload.Seat)
	if target == nil || !target.GetActive() {
		return nil, fmt.Errorf("%w: seat %d is not an active participant", engine.ErrIllegalAction, payload.Seat)
	}
	return target, nil
}

// reassignJudge performs the participant mutation shared by rows 3 and 27:
// clear the previous judge (if any and different from target), install
// target as judge (team=NONE, captain=false), and promote a replacement
// captain for target's previous team if that leaves it captain-less.
// Returns a roster_changed event for every participant actually mutated.
func reassignJudge(gs *pb.GameState, target *pb.Participant) []engine.Event {
	events := make([]engine.Event, 0, 3)
	if prevJudge := judgeOf(gs); prevJudge != nil && prevJudge.GetSeatIndex() != target.GetSeatIndex() {
		prevJudge.IsJudge = false
		events = append(events, rosterChangedEvent(prevJudge))
	}

	prevTeam := target.GetTeam()
	target.IsJudge = true
	target.Team = pb.Team_TEAM_NONE
	target.IsCaptain = false
	events = append(events, rosterChangedEvent(target))

	if prevTeam != pb.Team_TEAM_NONE && captainOf(gs, prevTeam) == nil {
		if promoted := promoteCaptain(gs, prevTeam); promoted != nil {
			events = append(events, rosterChangedEvent(promoted))
		}
	}

	return events
}

// applySetTeamName implements transition table row 4.
//
// Unlike the other four roster actions, a rename has no single target
// participant, so it cannot honestly be squeezed into roster_changed's
// {seat, team, is_captain, is_judge} per-participant shape. It emits its own
// team_renamed{team, name} event instead (ruling R1.6): roster_changed stays
// strictly a per-participant delta with a stable, non-optional shape, rather
// than growing an optional seat to accommodate this case.
func applySetTeamName(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if _, err := rosterHostGuard(gs, a); err != nil {
		return nil, err
	}
	payload, err := decodePayload[SetTeamNamePayload](a.Payload)
	if err != nil {
		return nil, err
	}
	team, ok := parseTeam(payload.Team)
	if !ok || team == pb.Team_TEAM_NONE {
		return nil, fmt.Errorf("%w: unknown team %q", engine.ErrIllegalAction, payload.Team)
	}
	name := strings.TrimSpace(payload.Name)
	if n := utf8.RuneCountInString(name); n < minTeamNameLen || n > maxTeamNameLen {
		return nil, fmt.Errorf("%w: team name must be %d-%d characters after trimming", engine.ErrIllegalAction, minTeamNameLen, maxTeamNameLen)
	}
	ts := teamState(gs, team)
	if ts == nil {
		return nil, fmt.Errorf("%w: no such team %q", engine.ErrIllegalAction, payload.Team)
	}
	ts.Name = name

	return []engine.Event{{
		Type: "team_renamed",
		Data: map[string]any{"team": teamWire(team), "name": name},
	}}, nil
}

// applyBeginGame implements transition table row 5.
func applyBeginGame(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if _, err := rosterHostGuard(gs, a); err != nil {
		return nil, err
	}
	payload, err := decodePayload[BeginGamePayload](a.Payload)
	if err != nil {
		return nil, err
	}
	if err := validateRosterForBeginGame(gs); err != nil {
		return nil, err
	}
	if !hasUnplayedSong(gs) {
		return nil, fmt.Errorf("%w: the pack has no unplayed songs", engine.ErrIllegalAction)
	}

	startTeam := pb.Team_TEAM_A
	if payload.StartingTeam != "" {
		t, ok := parseTeam(payload.StartingTeam)
		if !ok || t == pb.Team_TEAM_NONE {
			return nil, fmt.Errorf("%w: invalid starting_team %q", engine.ErrIllegalAction, payload.StartingTeam)
		}
		startTeam = t
	}

	gs.Phase = pb.Phase_PHASE_ROUND_SELECT
	gs.ActiveTeam = startTeam
	gs.RoundNo = 1

	return []engine.Event{
		{
			Type: "game_started",
			Data: map[string]any{
				"pack_id":      gs.GetPackId(),
				"pack_version": gs.GetPackVersion(),
				"participants": len(gs.GetParticipants()),
			},
		},
		{
			Type: "round_started",
			Data: map[string]any{"round_no": gs.GetRoundNo(), "active_team": teamWire(gs.GetActiveTeam())},
		},
	}, nil
}

// validateRosterForBeginGame checks the four roster guards on row 5: exactly
// one judge, judge on no team, and each team has exactly one captain and at
// least one active non-judge member (a judge is never on a team, so
// "non-judge member" and "member" are the same check here).
func validateRosterForBeginGame(gs *pb.GameState) error {
	judge := judgeOf(gs)
	if judge == nil {
		return fmt.Errorf("%w: no judge has been assigned", engine.ErrIllegalAction)
	}
	if judge.GetTeam() != pb.Team_TEAM_NONE {
		return fmt.Errorf("%w: the judge cannot also be on a team", engine.ErrIllegalAction)
	}
	for _, team := range []pb.Team{pb.Team_TEAM_A, pb.Team_TEAM_B} {
		if countActiveTeamMembers(gs, team) == 0 {
			return fmt.Errorf("%w: team %s has no active member", engine.ErrIllegalAction, teamWire(team))
		}
		if countCaptains(gs, team) != 1 {
			return fmt.Errorf("%w: team %s must have exactly one captain", engine.ErrIllegalAction, teamWire(team))
		}
	}
	return nil
}

// countActiveTeamMembers counts active participants on team. The judge is
// always Team_TEAM_NONE (invariant law 6), so this never double-counts them.
func countActiveTeamMembers(gs *pb.GameState, team pb.Team) int {
	n := 0
	for _, p := range gs.GetParticipants() {
		if p.GetActive() && p.GetTeam() == team {
			n++
		}
	}
	return n
}

// countCaptains counts active captains on team.
func countCaptains(gs *pb.GameState, team pb.Team) int {
	n := 0
	for _, p := range gs.GetParticipants() {
		if p.GetActive() && p.GetTeam() == team && p.GetIsCaptain() {
			n++
		}
	}
	return n
}

// hasUnplayedSong reports whether any selection-table cell still holds an
// unplayed song.
func hasUnplayedSong(gs *pb.GameState) bool {
	for _, c := range gs.GetCells() {
		if len(c.GetUnplayedSongIds()) > 0 {
			return true
		}
	}
	return false
}
