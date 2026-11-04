// Package song_guess implements "Guess the Song by Words" as an
// engine.Engine. See the plan's "Chosen Approach" section (state-machine
// transition table, protobuf state message, assertInvariants conservation
// laws) for the design this package implements; there is no rules/*.md file
// reference here because the rules digest lives inside the plan itself
// (rules/song_guess.md).
package song_guess

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

const (
	// GameKey identifies song_guess lobbies.
	GameKey = "song_guess"

	minSeats = 3  // Q2: player + player + judge, minimum. Req 18.
	maxSeats = 10 // Q3: the platform ceiling (game_lobbies.max_seats), not widened. Req 18.
)

// Engine implements engine.Engine for "Guess the Song by Words". It
// deliberately does NOT implement engine.HandBased: a song_guess lobby plays
// exactly one pack to a final score (Q4), so realtime.Handler finishes the
// lobby instead of redealing — the ttr.Engine precedent, not poker's.
type Engine struct {
	packs PackProvider
	sh    Shuffler
}

// New returns an Engine backed by packs and math/rand/v2's global source.
func New(packs PackProvider) *Engine {
	return &Engine{packs: packs, sh: DefaultShuffler{}}
}

// NewWithShuffler returns an Engine backed by packs and sh. Tests use this
// with NewSeededShuffler for deterministic cell draws (Steps 7+).
func NewWithShuffler(packs PackProvider, sh Shuffler) *Engine {
	return &Engine{packs: packs, sh: sh}
}

// GameKey identifies song_guess lobbies.
func (*Engine) GameKey() string { return GameKey }

// MinSeats is the fewest players a song_guess game supports (Req 18).
func (*Engine) MinSeats() int { return minSeats }

// MaxSeats is the most players a song_guess game supports (Req 18).
func (*Engine) MaxSeats() int { return maxSeats }

// InitState resolves the pinned pack from cfg ("pack_id", "pack_version")
// via e.packs and builds the initial PHASE_ROSTER state (Req 17, Q3):
// participants with the default team/captain/judge assignment already
// applied, the two TeamState entries, and the flattened selection table.
//
// InitState's events are discarded by lobby.Service.Start (this already
// affects TTR) — that is why game_started is emitted from begin_game
// instead (roster.go), not here. Do not "fix" lobby.Service.
func (e *Engine) InitState(ctx context.Context, cfg map[string]any, seats []engine.SeatInit) ([]byte, []engine.Event, error) {
	if len(seats) < minSeats || len(seats) > maxSeats {
		return nil, nil, engine.ErrNotEnoughPlayers
	}

	packID, version, err := packConfigFrom(cfg)
	if err != nil {
		return nil, nil, err
	}
	pack, err := e.packs.Get(ctx, packID, version)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve pack %s@%d: %w", packID, version, err)
	}
	if pack.TotalSongs == 0 {
		return nil, nil, fmt.Errorf("%w: pack has no songs", ErrInvalidPackDoc)
	}

	gs := &pb.GameState{
		PackId:      packID,
		PackVersion: version,
		Phase:       pb.Phase_PHASE_ROSTER,
		HostSeat:    0,
		ActiveTeam:  pb.Team_TEAM_A,
		RoundNo:     0,
	}
	gs.Participants = defaultParticipants(seats)
	applyHostJudgeDefault(gs)
	gs.Teams = []*pb.TeamState{
		{Team: pb.Team_TEAM_A, Name: "Team A", Score: 0},
		{Team: pb.Team_TEAM_B, Name: "Team B", Score: 0},
	}
	gs.Cells = buildCells(pack)

	b, err := marshal(gs)
	if err != nil {
		return nil, nil, err
	}
	return b, nil, nil
}

// defaultParticipants builds one Participant per seat, sorted by seat index,
// with the parity default team applied (Req 17): even seat -> TEAM_A, odd
// seat -> TEAM_B. Captaincy is assigned afterward by assignDefaultCaptains,
// once the slice is in its final, sorted order.
func defaultParticipants(seats []engine.SeatInit) []*pb.Participant {
	participants := make([]*pb.Participant, len(seats))
	for i, si := range seats {
		team := pb.Team_TEAM_A
		if si.Seat%2 != 0 {
			team = pb.Team_TEAM_B
		}
		participants[i] = &pb.Participant{
			SeatIndex: int32(si.Seat), // #nosec G115 -- seat index is bounded by maxSeats (10)
			UserId:    si.UserID.String(),
			Team:      team,
			Active:    true,
		}
	}
	slices.SortFunc(participants, func(a, b *pb.Participant) int { return int(a.SeatIndex - b.SeatIndex) })
	assignDefaultCaptains(participants)
	return participants
}

// assignDefaultCaptains sets is_captain on the lowest-seat member of each
// team (Req 17). participants must already be sorted by seat index.
func assignDefaultCaptains(participants []*pb.Participant) {
	for _, team := range []pb.Team{pb.Team_TEAM_A, pb.Team_TEAM_B} {
		for _, p := range participants {
			if p.GetActive() && p.GetTeam() == team {
				p.IsCaptain = true
				break
			}
		}
	}
}

// applyHostJudgeDefault moves gs.HostSeat's occupant to the judge role (Req
// 17): is_judge = true, team = TEAM_NONE, is_captain = false. If that leaves
// their former team without a captain (true whenever the host was that
// team's default captain, i.e. always when the host is seat 0 — the lowest
// seat of its parity team), the next lowest-seat active member of that team
// is promoted, so the default roster is already begin_game-valid (Req 17;
// verified for 3/4/5 seats in engine_roster_test.go).
func applyHostJudgeDefault(gs *pb.GameState) {
	host := participantBySeat(gs, gs.GetHostSeat())
	if host == nil {
		return
	}
	prevTeam := host.GetTeam()
	host.IsJudge = true
	host.Team = pb.Team_TEAM_NONE
	host.IsCaptain = false

	if prevTeam != pb.Team_TEAM_NONE && captainOf(gs, prevTeam) == nil {
		promoteCaptain(gs, prevTeam)
	}
}

// buildCells flattens pack into the selection table's Cell rows (Req 4):
// language_index in document order, one Cell per bucket present in that
// language (already ascending per packmodel.go's Language.Buckets
// contract). A cell is never removed later, even once its
// unplayed_song_ids empties — "disabled" is derived from that slice's
// length, never from the cell's absence, so the table's shape (row/column
// count) holds unchanged to the end of the game.
func buildCells(pack *Pack) []*pb.Cell {
	var cells []*pb.Cell
	for li, lang := range pack.Languages {
		for _, b := range lang.Buckets {
			ids := make([]string, len(b.Songs))
			for i, s := range b.Songs {
				ids[i] = s.ID
			}
			cells = append(cells, &pb.Cell{
				LanguageIndex:   int32(li), // #nosec G115 -- bounded by maxLanguages (32)
				LanguageName:    lang.Name,
				Bucket:          int32(b.Blocks), // #nosec G115 -- bounded by maxBlocksPerLine (64)
				UnplayedSongIds: ids,
				PlayedCount:     0,
			})
		}
	}
	return cells
}

// packConfigFrom extracts and validates the "pack_id"/"pack_version" lobby
// config keys InitState needs. In production the lobby's ConfigValidator
// seam (Session, Step 12) normalizes config before InitState is ever
// called, so failures here indicate a genuinely malformed call — mirrors
// ttr.mapConfigFrom exactly, with song_guess's own config keys.
func packConfigFrom(cfg map[string]any) (packID string, version int32, err error) {
	rawID, ok := cfg["pack_id"]
	if !ok {
		return "", 0, fmt.Errorf("%w: missing %q", ErrInvalidConfig, "pack_id")
	}
	packID, ok = rawID.(string)
	if !ok || packID == "" {
		return "", 0, fmt.Errorf("%w: %q must be a non-empty string", ErrInvalidConfig, "pack_id")
	}

	rawVersion, ok := cfg["pack_version"]
	if !ok {
		return "", 0, fmt.Errorf("%w: missing %q", ErrInvalidConfig, "pack_version")
	}
	version, ok = toPositiveInt32(rawVersion)
	if !ok {
		return "", 0, fmt.Errorf("%w: %q must be a positive integer", ErrInvalidConfig, "pack_version")
	}
	return packID, version, nil
}

// toPositiveInt32 converts v to a positive int32, accepting the numeric
// types a JSONB-backed lobby config plausibly decodes to (float64 from
// encoding/json, or a plain int/int32/int64 set programmatically). Copied
// from ttr.toPositiveInt32.
func toPositiveInt32(v any) (int32, bool) {
	switch n := v.(type) {
	case int32:
		return n, n > 0
	case int:
		if n <= 0 || n > math.MaxInt32 {
			return 0, false
		}
		return int32(n), true
	case int64:
		if n <= 0 || n > math.MaxInt32 {
			return 0, false
		}
		return int32(n), true
	case float64:
		if n <= 0 || n > math.MaxInt32 || n != float64(int64(n)) {
			return 0, false
		}
		return int32(n), true
	default:
		return 0, false
	}
}

// IsOver reports whether the game itself has concluded (phase ==
// PHASE_FINISHED).
func (*Engine) IsOver(state []byte) bool {
	gs, err := unmarshal(state)
	if err != nil {
		return false
	}
	return gs.GetPhase() == pb.Phase_PHASE_FINISHED
}

// Apply validates and applies a player action: unmarshal, reject
// PHASE_FINISHED, dispatch by action type, and bump the monotonic sequence
// counter on success (Req 39: no ctx, no I/O — every dispatch target in this
// step needs only gs and the action).
func (e *Engine) Apply(state []byte, a engine.Action) ([]byte, []engine.Event, error) {
	gs, err := unmarshal(state)
	if err != nil {
		return nil, nil, err
	}
	if gs.GetPhase() == pb.Phase_PHASE_FINISHED {
		return nil, nil, engine.ErrGameOver
	}

	events, err := e.dispatch(gs, a)
	if err != nil {
		return nil, nil, err
	}
	gs.Seq++

	b, err := marshal(gs)
	if err != nil {
		return nil, nil, err
	}
	return b, events, nil
}

// dispatch routes a to its handler. The five PHASE_ROSTER actions
// (roster.go, rows 1-5) and select_cell/next_round (round.go, rows 6 and 21)
// are handled directly; everything else — reveal/steal/last-chance/skip
// (play.go), judge_result/void (judge.go) and resign (scoring.go) — is
// routed through dispatchPlay, split out to keep this switch's cyclomatic
// complexity under the lint gate.
//
// Row 28's guard sits here, ahead of every other dispatch: in
// PHASE_JUDGE_VACANT, only assign_judge (which resolves the vacancy) and
// resign (which must never be blocked, per Req 21) are legal; every other
// action is rejected with engine.ErrWrongPhase before it can reach its own
// handler, so no future action added to dispatchPlay can accidentally slip
// through the parked phase.
func (e *Engine) dispatch(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if gs.GetPhase() == pb.Phase_PHASE_JUDGE_VACANT && a.Type != ActionAssignJudge && a.Type != ActionResign {
		return nil, engine.ErrWrongPhase
	}

	switch a.Type {
	case ActionAssignTeam:
		return applyAssignTeam(gs, a)
	case ActionAssignCaptain:
		return applyAssignCaptain(gs, a)
	case ActionAssignJudge:
		return applyAssignJudge(gs, a)
	case ActionSetTeamName:
		return applySetTeamName(gs, a)
	case ActionBeginGame:
		return applyBeginGame(gs, a)
	case ActionSelectCell:
		pack, err := e.resolvePack(gs)
		if err != nil {
			return nil, err
		}
		return applySelectCell(gs, a, pack, e.sh)
	case ActionNextRound:
		return applyNextRound(gs, a)
	default:
		return dispatchPlay(gs, a)
	}
}

// dispatchPlay routes the reveal/steal/last-chance/skip actions (play.go,
// rows 7-13 and 22), the judgement actions (judge.go, rows 14-20), and
// resign (scoring.go, rows 23-26).
func dispatchPlay(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	switch a.Type {
	case ActionOpenBlock:
		return applyOpenBlock(gs, a)
	case ActionGuess:
		return applyGuess(gs, a)
	case ActionSteal:
		return applySteal(gs, a)
	case ActionPass:
		return applyPass(gs, a)
	case ActionSkipSong:
		return applySkipSong(gs, a)
	case ActionJudgeResult:
		return applyJudgeResult(gs, a)
	case ActionVoid:
		return applyVoid(gs, a)
	case ActionResign:
		return applyResign(gs, a)
	default:
		return nil, fmt.Errorf("%w: unknown action %q", engine.ErrIllegalAction, a.Type)
	}
}

// resolvePack resolves gs's pinned (pack_id, pack_version) via e.packs for
// View, which — like Apply — has no context of its own (Req 39).
// context.Background() is safe here because every PackProvider a running
// game uses (PackCache, or the fixed-pack fake engine tests use) does no
// true I/O on this path: the pack was already parsed once at InitState time
// and this is an in-process cache lookup. Mirrors ttr.Engine.resolveMap.
func (e *Engine) resolvePack(gs *pb.GameState) (*Pack, error) {
	p, err := e.packs.Get(context.Background(), gs.GetPackId(), gs.GetPackVersion())
	if err != nil {
		return nil, fmt.Errorf("resolve pack %s@%d: %w", gs.GetPackId(), gs.GetPackVersion(), err)
	}
	return p, nil
}

// View and ViewFor — the full four-viewer-class redaction seam (player /
// judge / spectator, with a fully-revealed shape at SONG_RESULT/FINISHED)
// described in the plan's "Redaction seam" section — live in view.go.
