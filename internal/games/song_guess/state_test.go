package song_guess

import (
	"testing"

	"google.golang.org/protobuf/proto"

	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// populatedState builds a GameState touching every field of every message so
// the round-trip test below can assert nothing gets lost on the wire.
func populatedState() *pb.GameState {
	return &pb.GameState{
		PackId:      "11111111-1111-1111-1111-111111111111",
		PackVersion: 3,
		Phase:       pb.Phase_PHASE_SONG_PLAY,
		Seq:         42,
		HostSeat:    0,
		Participants: []*pb.Participant{
			{SeatIndex: 0, UserId: "host-user", Team: pb.Team_TEAM_A, IsCaptain: true, IsJudge: false, Active: true, DisplaySeatOrder: 0},
			{SeatIndex: 1, UserId: "user-b", Team: pb.Team_TEAM_B, IsCaptain: true, IsJudge: false, Active: true, DisplaySeatOrder: 1},
			{SeatIndex: 2, UserId: "judge-user", Team: pb.Team_TEAM_NONE, IsCaptain: false, IsJudge: true, Active: true, DisplaySeatOrder: 2},
		},
		Teams: []*pb.TeamState{
			{Team: pb.Team_TEAM_A, Name: "Alpha", Score: 5},
			{Team: pb.Team_TEAM_B, Name: "Beta", Score: 3},
		},
		ActiveTeam: pb.Team_TEAM_A,
		RoundNo:    2,
		Cells: []*pb.Cell{
			{LanguageIndex: 0, LanguageName: "english", Bucket: 4, UnplayedSongIds: []string{"song-1", "song-2"}, PlayedCount: 1},
			{LanguageIndex: 0, LanguageName: "english", Bucket: 5, UnplayedSongIds: []string{}, PlayedCount: 2},
			{LanguageIndex: 1, LanguageName: "ukrainian", Bucket: 4, UnplayedSongIds: []string{"song-3"}, PlayedCount: 0},
		},
		Song: &pb.SongState{
			SongId:        "song-1",
			LanguageName:  "english",
			Bucket:        4,
			Blocks:        []string{"Is this", "the real", "life", "or"},
			RevealedMask:  []bool{true, false, false, false},
			StealAllowed:  true,
			Awaiting:      pb.Awaiting_AWAITING_ACTIVE,
			Title:         "Bohemian Rhapsody",
			Artist:        "Queen",
			VideoId:       "fJ9rUzIMcZQ",
			StartSec:      10,
			EndSec:        40,
			AwardedTeam:   pb.Team_TEAM_NONE,
			AwardedPoints: 0,
			Skipped:       false,
			LastGuessText: "is this real life",
		},
		Pending: &pb.Pending{
			Kind:                 pb.JudgeKind_JUDGE_ACTIVE_GUESS,
			ClaimantTeam:         pb.Team_TEAM_A,
			ClaimantSeat:         0,
			GuessText:            "is this real life",
			PotentialAtClaim:     3,
			OwedRevealIndex:      -1,
			ResumeAwaiting:       pb.Awaiting_AWAITING_ACTIVE,
			RevealAfterIncorrect: false,
		},
		ResumePhase: pb.Phase_PHASE_UNSPECIFIED,
		Results: []*pb.ResultEntry{
			{SeatIndex: 0, UserId: "host-user", Team: pb.Team_TEAM_A, TeamTotal: 5, Rank: 1, IsJudge: false},
			{SeatIndex: 2, UserId: "judge-user", Team: pb.Team_TEAM_NONE, TeamTotal: 0, Rank: 0, IsJudge: true},
		},
		History: []*pb.SongOutcome{
			{SongId: "song-0", LanguageName: "english", Bucket: 4, RevealedAtEnd: 2, AwardedTeam: pb.Team_TEAM_B, Points: 2, Title: "Old Song", Artist: "Old Artist", Skipped: false},
		},
	}
}

// TestMarshalUnmarshalRoundTrip asserts every field of a fully populated
// GameState survives a marshal/unmarshal round trip. proto.Equal performs a
// full recursive structural comparison (all scalar fields, nested messages,
// and repeated fields), which is both more complete and far lower-complexity
// than hand-rolled field-by-field comparisons.
func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	want := populatedState()

	b, err := marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := unmarshal(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !proto.Equal(got, want) {
		t.Errorf("round trip mismatch:\n got:  %s\nwant: %s", got.String(), want.String())
	}
}

// TestEmptyStateRoundTrip asserts that unmarshalling a GameState with zero
// participants/cells yields empty (not nil-panicking) slices — the hard
// constraint this proto is designed around: every collection is repeated,
// never map<...>, precisely to avoid the "zero-entry map unmarshals as nil"
// hazard. A repeated field can also come back nil when empty, but ranging
// over a nil slice is always safe in Go, so no caller needs to special-case
// it.
func TestEmptyStateRoundTrip(t *testing.T) {
	empty := &pb.GameState{PackId: "pack", PackVersion: 1, Phase: pb.Phase_PHASE_ROSTER}

	b, err := marshal(empty)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := unmarshal(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.GetPackId() != "pack" {
		t.Errorf("PackId: got %q, want %q", got.GetPackId(), "pack")
	}
	if len(got.GetParticipants()) != 0 {
		t.Errorf("Participants: got %d entries, want 0", len(got.GetParticipants()))
	}
	if len(got.GetCells()) != 0 {
		t.Errorf("Cells: got %d entries, want 0", len(got.GetCells()))
	}
	if len(got.GetTeams()) != 0 {
		t.Errorf("Teams: got %d entries, want 0", len(got.GetTeams()))
	}
	if len(got.GetResults()) != 0 {
		t.Errorf("Results: got %d entries, want 0", len(got.GetResults()))
	}
	if len(got.GetHistory()) != 0 {
		t.Errorf("History: got %d entries, want 0", len(got.GetHistory()))
	}
	if got.GetSong() != nil {
		t.Errorf("Song: got %+v, want nil", got.GetSong())
	}
	if got.GetPending() != nil {
		t.Errorf("Pending: got %+v, want nil", got.GetPending())
	}

	// Iterating the empty collections must not panic — the entire point of
	// avoiding map<...> fields.
	for range got.GetParticipants() {
		t.Fatal("unexpected participant")
	}
	for range got.GetCells() {
		t.Fatal("unexpected cell")
	}
}

func TestParticipantBySeat(t *testing.T) {
	gs := populatedState()

	if p := participantBySeat(gs, 1); p == nil || p.GetUserId() != "user-b" {
		t.Errorf("participantBySeat(1): got %+v, want seat 1 (user-b)", p)
	}
	if p := participantBySeat(gs, 99); p != nil {
		t.Errorf("participantBySeat(99): got %+v, want nil", p)
	}
}

func TestParticipantByUser(t *testing.T) {
	gs := populatedState()

	if p := participantByUser(gs, "judge-user"); p == nil || p.GetSeatIndex() != 2 {
		t.Errorf("participantByUser(judge-user): got %+v, want seat 2", p)
	}
	if p := participantByUser(gs, "nobody"); p != nil {
		t.Errorf("participantByUser(nobody): got %+v, want nil", p)
	}
}

func TestCellFor(t *testing.T) {
	gs := populatedState()

	if c := cellFor(gs, "ukrainian", 4); c == nil || c.GetLanguageIndex() != 1 {
		t.Errorf("cellFor(ukrainian, 4): got %+v, want language index 1", c)
	}
	if c := cellFor(gs, "english", 99); c != nil {
		t.Errorf("cellFor(english, 99): got %+v, want nil", c)
	}
	if c := cellFor(gs, "french", 4); c != nil {
		t.Errorf("cellFor(french, 4): got %+v, want nil", c)
	}
}

func TestTeamState(t *testing.T) {
	gs := populatedState()

	if ts := teamState(gs, pb.Team_TEAM_A); ts == nil || ts.GetName() != "Alpha" {
		t.Errorf("teamState(TEAM_A): got %+v, want Alpha", ts)
	}
	if ts := teamState(gs, pb.Team_TEAM_NONE); ts != nil {
		t.Errorf("teamState(TEAM_NONE): got %+v, want nil", ts)
	}
}

func TestCaptainOf(t *testing.T) {
	gs := populatedState()

	if c := captainOf(gs, pb.Team_TEAM_B); c == nil || c.GetSeatIndex() != 1 {
		t.Errorf("captainOf(TEAM_B): got %+v, want seat 1", c)
	}
	if c := captainOf(gs, pb.Team_TEAM_NONE); c != nil {
		t.Errorf("captainOf(TEAM_NONE): got %+v, want nil", c)
	}

	noCaptains := &pb.GameState{Participants: []*pb.Participant{
		{SeatIndex: 0, Team: pb.Team_TEAM_A, IsCaptain: false},
	}}
	if c := captainOf(noCaptains, pb.Team_TEAM_A); c != nil {
		t.Errorf("captainOf(TEAM_A) with no captain: got %+v, want nil", c)
	}
}

func TestJudgeOf(t *testing.T) {
	gs := populatedState()

	if j := judgeOf(gs); j == nil || j.GetSeatIndex() != 2 {
		t.Errorf("judgeOf: got %+v, want seat 2", j)
	}

	noJudge := &pb.GameState{Participants: []*pb.Participant{
		{SeatIndex: 0, Team: pb.Team_TEAM_A},
	}}
	if j := judgeOf(noJudge); j != nil {
		t.Errorf("judgeOf with no judge: got %+v, want nil", j)
	}
}

func TestHostParticipant(t *testing.T) {
	gs := populatedState()

	if h := hostParticipant(gs); h == nil || h.GetSeatIndex() != 0 {
		t.Errorf("hostParticipant: got %+v, want seat 0", h)
	}

	gs.HostSeat = 99
	if h := hostParticipant(gs); h != nil {
		t.Errorf("hostParticipant with vacant host seat: got %+v, want nil", h)
	}
}

func TestOpponentOf(t *testing.T) {
	cases := []struct {
		in, want pb.Team
	}{
		{pb.Team_TEAM_A, pb.Team_TEAM_B},
		{pb.Team_TEAM_B, pb.Team_TEAM_A},
		{pb.Team_TEAM_NONE, pb.Team_TEAM_NONE},
	}
	for _, c := range cases {
		if got := opponentOf(c.in); got != c.want {
			t.Errorf("opponentOf(%v): got %v, want %v", c.in, got, c.want)
		}
	}
}
