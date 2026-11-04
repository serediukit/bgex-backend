package song_guess

import (
	"encoding/json"
	"fmt"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// This file implements plan "Chosen Approach" transition table rows 23-29:
// resign (leaving mid-game), PHASE_JUDGE_VACANT and its repair, and the
// terminal transition into PHASE_FINISHED (both row 21's natural-exhaustion
// path in round.go and row 26's abandonment path here) — finalize is the
// single place that computes final ranks/results and emits game_over,
// exactly once per game, regardless of which path reached PHASE_FINISHED.

// applyResign implements transition table rows 23-26: any participant may
// resign from any non-FINISHED phase (PHASE_FINISHED is already rejected
// with engine.ErrGameOver by Engine.Apply before dispatch ever runs). It:
//  1. marks the leaver inactive and emits player_left{seat, team};
//  2. if the leaver was a captain, promotes the lowest-seat remaining
//     active teammate (promoteCaptain already implements exactly that
//     selection) and emits captain_changed, or emits nothing if the team is
//     now empty;
//  3. if the leaver was the judge, parks the game in PHASE_JUDGE_VACANT
//     (remembering the interrupted phase in ResumePhase) and emits
//     judge_vacant;
//  4. if the leaver was host, refreshes the gs.HostSeat cache — the ONE
//     place in this package that cache is ever rewritten, since every
//     host-only guard (requireHost, and applySkipSong through it) trusts it
//     rather than re-deriving "earliest remaining seat" itself;
//  5. evaluated LAST, so it always wins over step 3's parking: if the
//     terminal condition holds, finalize the game immediately. finalize
//     inlines the same PHASE_JUDGE_VACANT-aware in-flight check invariants_
//     test.go's effectivePhase documents (R1.4/R1.8), so a judge resigning
//     and a team consequently emptying in the very same call still records
//     an abandoned in-flight song correctly (R1.3) — see finalize's doc
//     comment.
func applyResign(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	actor := participantByUser(gs, a.UserID.String())
	if actor == nil {
		return nil, engine.ErrNotSeated
	}
	if !actor.GetActive() {
		// Already left. lobby.Service.resignFromInProgressGame tolerates
		// engine.ErrIllegalAction as a no-op (Req 21), so a second resign
		// from the same user is safely absorbed rather than mutating
		// anything further.
		return nil, fmt.Errorf("%w: seat %d has already left", engine.ErrIllegalAction, actor.GetSeatIndex())
	}

	seat := actor.GetSeatIndex()
	team := actor.GetTeam()
	wasCaptain := actor.GetIsCaptain()
	wasJudge := actor.GetIsJudge()
	wasHost := seat == gs.GetHostSeat()

	actor.Active = false
	// captainOf/judgeOf (state.go) deliberately do not filter by Active —
	// every OTHER place these flags change (applyAssignCaptain,
	// reassignJudge) explicitly clears the previous holder's flag before
	// installing a new one, so a stale true here would make captainOf/
	// judgeOf keep returning this now-inactive participant ahead of
	// whoever is promoted below.
	actor.IsCaptain = false
	actor.IsJudge = false

	events := []engine.Event{{
		Type: "player_left",
		Data: map[string]any{"seat": seat, "team": teamWire(team)},
	}}

	if wasCaptain {
		if promoted := promoteCaptain(gs, team); promoted != nil {
			events = append(events, engine.Event{
				Type: "captain_changed",
				Data: map[string]any{"team": teamWire(team), "seat": promoted.GetSeatIndex()},
			})
		}
	}

	if wasJudge {
		gs.ResumePhase = gs.GetPhase()
		gs.Phase = pb.Phase_PHASE_JUDGE_VACANT
		events = append(events, engine.Event{
			Type: "judge_vacant",
			Data: map[string]any{"seat": seat},
		})
	}

	if wasHost {
		// gs.HostSeat is a cache (GameState field #5's doc comment) so
		// requireHost never needs a lobby read. lobby.Service.Leave already
		// promotes the earliest remaining lobby seat to host, but this
		// engine-side copy is the one place that must be refreshed to match
		// — nothing else in this package ever writes it.
		gs.HostSeat = lowestActiveSeat(gs)
	}

	if terminalCondition(gs) {
		return append(events, finalize(gs)...), nil
	}
	return events, nil
}

// lowestActiveSeat returns the lowest seat_index among active participants,
// or -1 if none remain active (gs.Participants is sorted by seat_index per
// state.go's contract, so the first active match is the lowest).
func lowestActiveSeat(gs *pb.GameState) int32 {
	for _, p := range gs.GetParticipants() {
		if p.GetActive() {
			return p.GetSeatIndex()
		}
	}
	return -1
}

// terminalCondition reports whether the game must end immediately (row 26):
// either team has no active member, or fewer than 3 participants remain
// active overall (Q10/Req 20). Shared by applyResign's row 26 proper and
// applyAssignJudgeInVacant's row 27 note that emptying a team while
// repairing a vacancy fires this same check instead of resuming play.
func terminalCondition(gs *pb.GameState) bool {
	if countActiveTeamMembers(gs, pb.Team_TEAM_A) == 0 || countActiveTeamMembers(gs, pb.Team_TEAM_B) == 0 {
		return true
	}
	return countActiveParticipants(gs) < 3
}

// countActiveParticipants counts every active participant regardless of
// role — players, captains and the judge alike.
func countActiveParticipants(gs *pb.GameState) int {
	n := 0
	for _, p := range gs.GetParticipants() {
		if p.GetActive() {
			n++
		}
	}
	return n
}

// finalize implements the terminal transition into PHASE_FINISHED: row 21's
// natural-exhaustion case (round.go's applyNextRound) and row 26's
// abandonment case (applyResign and applyAssignJudgeInVacant above). It is
// the ONLY place gs.Results is populated and the ONLY place game_over is
// emitted, so "game_over fires exactly once" has one call site to be right.
//
// R1.3: if a song is still in flight when finalize runs, it must be
// recorded before ranks are computed, or law 1 (song conservation) fails at
// PHASE_FINISHED — the drawn song would sit in neither a cell nor history.
// "In flight" is resolved the same way invariants_test.go's effectivePhase
// resolves it for law 12b/12d (R1.4/R1.8): gs.Phase directly, except
// gs.ResumePhase when gs.Phase == PHASE_JUDGE_VACANT, because applyResign
// may have parked the game there in the very same call that also satisfies
// terminalCondition (a judge resigning from a 3-participant game mid-song,
// for instance) — the real interrupted phase lives behind ResumePhase in
// that case, not in gs.Phase itself. (This package cannot import
// invariants_test.go's helper — test files never link into the production
// build — so the same short predicate is duplicated here rather than
// shared.)
//
// The in-flight append deliberately reuses play.go's finishSong rather than
// hand-rolling a second history-append site: awarded_team=TEAM_NONE,
// points=0, skipped=true, revealed_at_end=revealedCount(gs.Song) and
// active_team=gs.ActiveTeam are exactly finishSong's existing "0/0, ended"
// shape (see applyPassFromLastChance/applyJudgeLastChanceIncorrect for the
// same call). finishSong also sets gs.Phase=PHASE_SONG_RESULT and emits
// song_finished as a side effect; both are harmless here — the title is
// already public once a song is over (Q5), and finalize overwrites
// gs.Phase to PHASE_FINISHED immediately afterward.
func finalize(gs *pb.GameState) []engine.Event {
	var events []engine.Event

	phase := gs.GetPhase()
	if phase == pb.Phase_PHASE_JUDGE_VACANT {
		phase = gs.GetResumePhase()
	}
	if phase == pb.Phase_PHASE_SONG_PLAY || phase == pb.Phase_PHASE_PENDING_JUDGEMENT {
		events = append(events, finishSong(gs, pb.Team_TEAM_NONE, 0, true)...)
	}

	gs.Phase = pb.Phase_PHASE_FINISHED
	gs.ResumePhase = pb.Phase_PHASE_UNSPECIFIED
	gs.Results = buildResults(gs)

	return append(events, gameOverEvent(gs))
}

// buildResults populates one ResultEntry per participant (players AND the
// judge, Req 36), with team_total/rank filled in for team members and left
// at their zero value (team_total=0, rank=0 — the judge is on no team and
// is unranked) for the judge. Rank 1 goes to the higher-scoring team; both
// teams rank 1 on a tie (a shared victory, Req 16/§8.1).
func buildResults(gs *pb.GameState) []*pb.ResultEntry {
	scoreA := teamState(gs, pb.Team_TEAM_A).GetScore()
	scoreB := teamState(gs, pb.Team_TEAM_B).GetScore()
	rankA, rankB := teamRanks(scoreA, scoreB)

	results := make([]*pb.ResultEntry, 0, len(gs.GetParticipants()))
	for _, p := range gs.GetParticipants() {
		entry := &pb.ResultEntry{
			SeatIndex: p.GetSeatIndex(),
			UserId:    p.GetUserId(),
			Team:      p.GetTeam(),
			IsJudge:   p.GetIsJudge(),
		}
		switch p.GetTeam() {
		case pb.Team_TEAM_A:
			entry.TeamTotal = scoreA
			entry.Rank = rankA
		case pb.Team_TEAM_B:
			entry.TeamTotal = scoreB
			entry.Rank = rankB
		case pb.Team_TEAM_NONE:
			// The judge: team_total=0, rank=0 (proto zero values), exactly as
			// the step brief specifies.
		}
		results = append(results, entry)
	}
	return results
}

// teamRanks returns TEAM_A's and TEAM_B's rank given their final scores:
// both rank 1 on a tie, otherwise 1 for the higher score and 2 for the
// lower.
func teamRanks(scoreA, scoreB int32) (rankA, rankB int32) {
	switch {
	case scoreA > scoreB:
		return 1, 2
	case scoreB > scoreA:
		return 2, 1
	default:
		return 1, 1
	}
}

// gameOverEvent builds the game_over event (plan "Engine events" table):
// {teams: [{team, name, score}], winner_teams: [...]}. Final scores and
// team names are public per rules §8, so this leaks nothing — no
// participant hands, no remaining pack content, no unplayed song titles.
func gameOverEvent(gs *pb.GameState) engine.Event {
	teamA := teamState(gs, pb.Team_TEAM_A)
	teamB := teamState(gs, pb.Team_TEAM_B)
	scoreA, scoreB := teamA.GetScore(), teamB.GetScore()

	teams := []map[string]any{
		{"team": teamWire(pb.Team_TEAM_A), "name": teamA.GetName(), "score": scoreA},
		{"team": teamWire(pb.Team_TEAM_B), "name": teamB.GetName(), "score": scoreB},
	}

	var winners []string
	switch {
	case scoreA > scoreB:
		winners = []string{teamWire(pb.Team_TEAM_A)}
	case scoreB > scoreA:
		winners = []string{teamWire(pb.Team_TEAM_B)}
	default:
		winners = []string{teamWire(pb.Team_TEAM_A), teamWire(pb.Team_TEAM_B)}
	}

	return engine.Event{
		Type: "game_over",
		Data: map[string]any{"teams": teams, "winner_teams": winners},
	}
}

// songAward is breakdownFor's JSON shape: one completed song credited to
// the team the breakdown is for.
type songAward struct {
	SongID        string `json:"song_id"`
	Language      string `json:"language"`
	Bucket        int32  `json:"bucket"`
	RevealedAtEnd int32  `json:"revealed_at_end"`
	Points        int32  `json:"points"`
	ActiveTeam    string `json:"active_team"`
}

// breakdownFor returns JSON of team's per-song awards drawn from
// gs.History, for song_guess.game_results.breakdown (Req 36). Only entries
// this team was actually AWARDED are included — active_team is carried per
// entry so the breakdown still shows whether the team picked or stole each
// of its awarded songs (R1.7's Step 14 payoff).
func breakdownFor(gs *pb.GameState, team pb.Team) ([]byte, error) {
	awards := make([]songAward, 0, len(gs.GetHistory()))
	for _, h := range gs.GetHistory() {
		if h.GetAwardedTeam() != team {
			continue
		}
		awards = append(awards, songAward{
			SongID:        h.GetSongId(),
			Language:      h.GetLanguageName(),
			Bucket:        h.GetBucket(),
			RevealedAtEnd: h.GetRevealedAtEnd(),
			Points:        h.GetPoints(),
			ActiveTeam:    teamWire(h.GetActiveTeam()),
		})
	}
	b, err := json.Marshal(awards)
	if err != nil {
		return nil, fmt.Errorf("marshal breakdown for team %s: %w", teamWire(team), err)
	}
	return b, nil
}
