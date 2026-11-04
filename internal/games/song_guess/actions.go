package song_guess

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// Action type names — the value of engine.Action.Type for song_guess. Every
// action the state machine ever accepts is enumerated here now (plan
// "Chosen Approach" transition table rows 1-29), even though only the
// PHASE_ROSTER handlers (rows 1-5, roster.go) are implemented in this step —
// so later steps (7-10) only ever replace a dispatch case body, never add a
// new one.
const (
	ActionAssignTeam    = "assign_team"
	ActionAssignCaptain = "assign_captain"
	ActionAssignJudge   = "assign_judge"
	ActionSetTeamName   = "set_team_name"
	ActionBeginGame     = "begin_game"
	ActionSelectCell    = "select_cell"
	ActionOpenBlock     = "open_block"
	ActionGuess         = "guess"
	ActionSteal         = "steal"
	ActionPass          = "pass"
	ActionJudgeResult   = "judge_result"
	ActionVoid          = "void"
	ActionSkipSong      = "skip_song"
	ActionNextRound     = "next_round"
	ActionResign        = "resign"
)

// maxGuessTextLen caps the advisory guess text carried by guess/steal (Req
// 12, Q2): shown to the judge while a decision is pending and to everyone at
// SONG_RESULT, but never itself an adjudication input.
const maxGuessTextLen = 200

// maxTeamNameLen / minTeamNameLen bound set_team_name (plan transition table
// row 4): 1-32 chars after strings.TrimSpace.
const (
	minTeamNameLen = 1
	maxTeamNameLen = 32
)

// AssignTeamPayload is the payload for ActionAssignTeam. Team is the wire
// form parseTeam accepts ("a" | "b" | "none").
type AssignTeamPayload struct {
	Seat int32  `json:"seat"`
	Team string `json:"team"`
}

// AssignCaptainPayload is the payload for ActionAssignCaptain.
type AssignCaptainPayload struct {
	Seat int32 `json:"seat"`
}

// AssignJudgePayload is the payload for ActionAssignJudge.
type AssignJudgePayload struct {
	Seat int32 `json:"seat"`
}

// SetTeamNamePayload is the payload for ActionSetTeamName.
type SetTeamNamePayload struct {
	Team string `json:"team"`
	Name string `json:"name"`
}

// BeginGamePayload is the payload for ActionBeginGame. StartingTeam is
// optional wire team form ("a" | "b"); an empty string defaults to TEAM_A
// (plan transition table row 5).
type BeginGamePayload struct {
	StartingTeam string `json:"starting_team,omitempty"`
}

// SelectCellPayload is the payload for ActionSelectCell (Steps 7+).
type SelectCellPayload struct {
	Language string `json:"language"`
	Bucket   int32  `json:"bucket"`
}

// OpenBlockPayload is the payload for ActionOpenBlock (ruling R3.2/C1): the
// active captain names any still-closed block index, in any order — free
// choice, not "the next one". Index bounds/already-revealed are validated
// by applyOpenBlock/revealAt, not here.
type OpenBlockPayload struct {
	Index int32 `json:"index"`
}

// GuessPayload is the payload shared by ActionGuess and ActionSteal (Steps
// 8-9). Guess is optional, trimmed, and capped at maxGuessTextLen — see
// trimmedGuessText.
type GuessPayload struct {
	Guess string `json:"guess,omitempty"`
}

// JudgeResultPayload is the payload for ActionJudgeResult (Step 9).
type JudgeResultPayload struct {
	Correct bool `json:"correct"`
}

// decodePayload strictly decodes raw into a T, rejecting unknown fields so a
// malformed client payload fails loudly instead of silently ignoring typos.
// Copied verbatim from ttr/actions.go's decodePayload.
func decodePayload[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, fmt.Errorf("%w: missing action payload", engine.ErrIllegalAction)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("%w: decode payload: %w", engine.ErrIllegalAction, err)
	}
	return v, nil
}

// parseTeam parses the wire form of a team ("a" | "b" | "none") into its
// pb.Team value. The bool result is false for any other input, including
// case variants — the wire vocabulary is fixed (Q3).
func parseTeam(s string) (pb.Team, bool) {
	switch s {
	case "a":
		return pb.Team_TEAM_A, true
	case "b":
		return pb.Team_TEAM_B, true
	case "none":
		return pb.Team_TEAM_NONE, true
	default:
		return pb.Team_TEAM_NONE, false
	}
}

// teamWire is parseTeam's inverse: the wire form of t for event payloads and
// the (temporary, Step 6) View projection.
func teamWire(t pb.Team) string {
	switch t {
	case pb.Team_TEAM_A:
		return "a"
	case pb.Team_TEAM_B:
		return "b"
	case pb.Team_TEAM_NONE:
		return "none"
	}
	return "none"
}

// trimmedGuessText validates and trims a guess/steal payload's advisory
// guess text (Req 12): optional, trimmed, and rejected outright over
// maxGuessTextLen characters (an oversized input cannot be rescued by
// trimming). Shared by ActionGuess/ActionSteal, whose bodies Steps 8-9 add;
// defined now alongside GuessPayload since payload validation is this step's
// scope.
func trimmedGuessText(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(s); n > maxGuessTextLen {
		return "", fmt.Errorf("%w: guess text exceeds %d characters", engine.ErrIllegalAction, maxGuessTextLen)
	}
	return s, nil
}
