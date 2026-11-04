package song_guess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// fakeRosterPack is a fixed *Pack fixture for engine tests in this file,
// built directly (not through ParsePack/JSON) since these tests exercise
// InitState/Apply, not pack parsing. Named to avoid colliding with
// packloader_test.go's validPackDoc / packcache_test.go's
// validPackCacheDoc / invalidPackCacheDoc (all in this same package
// namespace — see the plan's "Discovered during implementation" section).
func fakeRosterPack() *Pack {
	return &Pack{
		Name: "Roster Test Pack",
		Languages: []Language{
			{
				Name: "English",
				Buckets: []Bucket{
					{Blocks: 4, Songs: []Song{
						{ID: "s1", Title: "One", Artist: "Artist One", Line: []string{"a", "b", "c", "d"}},
						{ID: "s2", Title: "Two", Artist: "Artist Two", Line: []string{"e", "f", "g", "h"}},
					}},
				},
			},
		},
		TotalSongs: 2,
	}
}

// fakeRosterPackProvider is a fixed-pack PackProvider for engine tests,
// mirroring how ttr engine tests supply a static MapProvider.
type fakeRosterPackProvider struct {
	pack *Pack
	err  error
}

func (f *fakeRosterPackProvider) Get(_ context.Context, _ string, _ int32) (*Pack, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.pack, nil
}

// newRosterTestEngine returns an Engine backed by pack, with no shuffler
// dependency (roster actions never draw a song).
func newRosterTestEngine(pack *Pack) *Engine {
	return New(&fakeRosterPackProvider{pack: pack})
}

// rosterTestCfg is the lobby config InitState expects.
func rosterTestCfg() map[string]any {
	return map[string]any{"pack_id": "pack-1", "pack_version": int32(1)}
}

// rosterTestSeats returns n sequential seats (0..n-1), each with a fresh
// random user id — the seat-index-contiguous-from-0 shape every SeatInit
// producer in this codebase (lobby.Service, poker/ttr tests) already
// assumes.
func rosterTestSeats(n int) []engine.SeatInit {
	seats := make([]engine.SeatInit, n)
	for i := range n {
		seats[i] = engine.SeatInit{Seat: i, UserID: uuid.New()}
	}
	return seats
}

// initRosterState is the common InitState-and-check-error setup shared by
// every test below.
func initRosterState(t *testing.T, pack *Pack, seats []engine.SeatInit) []byte {
	t.Helper()
	eng := newRosterTestEngine(pack)
	state, events, err := eng.InitState(context.Background(), rosterTestCfg(), seats)
	if err != nil {
		t.Fatalf("InitState: %v", err)
	}
	if events != nil {
		t.Errorf("InitState events = %v, want nil (lobby.Service.Start discards them; see engine.go doc comment)", events)
	}
	return state
}

func requireRosterState(t *testing.T, state []byte) *pb.GameState {
	t.Helper()
	gs, err := unmarshal(state)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return gs
}

func mustMarshalState(t *testing.T, gs *pb.GameState) []byte {
	t.Helper()
	b, err := marshal(gs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// TestDefaultRosterSatisfiesBeginGame is Req 17's central claim: after
// InitState, the default roster is ALREADY valid for begin_game, at every
// seat count in [minSeats, minSeats+2] = {3, 4, 5}. The host's happy path is
// meant to be a single begin_game click with no roster editing at all.
func TestDefaultRosterSatisfiesBeginGame(t *testing.T) {
	for _, n := range []int{3, 4, 5} {
		t.Run(fmt.Sprintf("%d_seats", n), func(t *testing.T) {
			pack := fakeRosterPack()
			seats := rosterTestSeats(n)
			state := initRosterState(t, pack, seats)
			gs := requireRosterState(t, state)
			assertInvariants(t, pack, gs)

			if gs.GetPhase() != pb.Phase_PHASE_ROSTER {
				t.Fatalf("phase = %s, want PHASE_ROSTER", gs.GetPhase())
			}

			eng := newRosterTestEngine(pack)
			next, _, err := eng.Apply(state, engine.Action{
				UserID:  seats[0].UserID,
				Type:    ActionBeginGame,
				Payload: json.RawMessage(`{}`),
			})
			if err != nil {
				t.Fatalf("begin_game with untouched default roster failed for %d seats: %v", n, err)
			}
			gs2 := requireRosterState(t, next)
			assertInvariants(t, pack, gs2)
			if gs2.GetPhase() != pb.Phase_PHASE_ROUND_SELECT {
				t.Errorf("phase = %s, want PHASE_ROUND_SELECT", gs2.GetPhase())
			}
			if gs2.GetActiveTeam() != pb.Team_TEAM_A {
				t.Errorf("active_team = %s, want TEAM_A (default)", gs2.GetActiveTeam())
			}
			if gs2.GetRoundNo() != 1 {
				t.Errorf("round_no = %d, want 1", gs2.GetRoundNo())
			}
		})
	}
}

// TestRosterActionsRequireHost checks that a seated non-host is rejected on
// every one of the five PHASE_ROSTER actions with engine.ErrNotYourTurn.
func TestRosterActionsRequireHost(t *testing.T) {
	pack := fakeRosterPack()
	seats := rosterTestSeats(4)
	state := initRosterState(t, pack, seats)
	nonHost := seats[1].UserID // seat 1; host is always seat 0.

	cases := []struct {
		name   string
		action engine.Action
	}{
		{"assign_team", engine.Action{UserID: nonHost, Type: ActionAssignTeam, Payload: json.RawMessage(`{"seat":1,"team":"b"}`)}},
		{"assign_captain", engine.Action{UserID: nonHost, Type: ActionAssignCaptain, Payload: json.RawMessage(`{"seat":1}`)}},
		{"assign_judge", engine.Action{UserID: nonHost, Type: ActionAssignJudge, Payload: json.RawMessage(`{"seat":1}`)}},
		{"set_team_name", engine.Action{UserID: nonHost, Type: ActionSetTeamName, Payload: json.RawMessage(`{"team":"a","name":"X"}`)}},
		{"begin_game", engine.Action{UserID: nonHost, Type: ActionBeginGame, Payload: json.RawMessage(`{}`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng := newRosterTestEngine(pack)
			_, _, err := eng.Apply(state, tc.action)
			if !errors.Is(err, engine.ErrNotYourTurn) {
				t.Fatalf("err = %v, want engine.ErrNotYourTurn", err)
			}
		})
	}
}

// TestBeginGameRejectsInvalidRoster drives applyBeginGame's four guards
// individually, each via a directly-mutated state (bypassing the row 1-3
// actions, some of which cannot reach every guard-violating shape on their
// own — e.g. there is no action that removes the judge without installing a
// replacement). Each case asserts both the sentinel and a distinct message
// substring, so a wrong guard firing for the right reason still fails the
// test.
func TestBeginGameRejectsInvalidRoster(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(gs *pb.GameState)
		wantMsg string
	}{
		{
			name: "team_has_no_member",
			mutate: func(gs *pb.GameState) {
				for _, p := range gs.Participants {
					if p.GetTeam() == pb.Team_TEAM_A {
						p.Team = pb.Team_TEAM_NONE
						p.IsCaptain = false
					}
				}
			},
			wantMsg: "no active member",
		},
		{
			name: "team_has_no_captain",
			mutate: func(gs *pb.GameState) {
				for _, p := range gs.Participants {
					if p.GetIsCaptain() {
						p.IsCaptain = false
						break // strip exactly one captain; that team keeps its member(s).
					}
				}
			},
			wantMsg: "exactly one captain",
		},
		{
			name: "no_judge",
			mutate: func(gs *pb.GameState) {
				for _, p := range gs.Participants {
					if p.GetIsJudge() {
						p.IsJudge = false
					}
				}
			},
			wantMsg: "no judge",
		},
		{
			name: "judge_on_team",
			mutate: func(gs *pb.GameState) {
				for _, p := range gs.Participants {
					if p.GetIsJudge() {
						p.Team = pb.Team_TEAM_A
					}
				}
			},
			wantMsg: "cannot also be on a team",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pack := fakeRosterPack()
			seats := rosterTestSeats(4)
			state := initRosterState(t, pack, seats)
			gs := requireRosterState(t, state)
			tc.mutate(gs)
			state = mustMarshalState(t, gs)

			eng := newRosterTestEngine(pack)
			_, _, err := eng.Apply(state, engine.Action{
				UserID:  seats[0].UserID,
				Type:    ActionBeginGame,
				Payload: json.RawMessage(`{}`),
			})
			if !errors.Is(err, engine.ErrIllegalAction) {
				t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("err = %q, want it to mention %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

// TestAssignCaptainMovesCaptaincy checks that assign_captain both installs
// the new captain and clears the old one, so a team never ends up with two.
func TestAssignCaptainMovesCaptaincy(t *testing.T) {
	pack := fakeRosterPack()
	seats := rosterTestSeats(4) // default: seat0 host/judge, seat1 B-capt, seat2 A-capt, seat3 B-member.
	state := initRosterState(t, pack, seats)

	eng := newRosterTestEngine(pack)
	next, _, err := eng.Apply(state, engine.Action{
		UserID:  seats[0].UserID,
		Type:    ActionAssignCaptain,
		Payload: json.RawMessage(`{"seat":3}`),
	})
	if err != nil {
		t.Fatalf("assign_captain: %v", err)
	}
	gs := requireRosterState(t, next)
	assertInvariants(t, pack, gs)

	var captainSeat int32 = -1
	captains := 0
	for _, p := range gs.GetParticipants() {
		if p.GetTeam() == pb.Team_TEAM_B && p.GetIsCaptain() {
			captains++
			captainSeat = p.GetSeatIndex()
		}
	}
	if captains != 1 {
		t.Fatalf("team B captains = %d, want exactly 1", captains)
	}
	if captainSeat != 3 {
		t.Fatalf("team B captain seat = %d, want 3", captainSeat)
	}
}

// TestAssignJudgePromotesReplacementCaptain checks that assign_judge clears
// the previous judge and, when the new judge was a team's sole captain,
// promotes the lowest-seat remaining active member of that team.
func TestAssignJudgePromotesReplacementCaptain(t *testing.T) {
	pack := fakeRosterPack()
	// 5 seats: seat0 host/judge, TeamA={seat2 capt, seat4}, TeamB={seat1 capt, seat3}.
	seats := rosterTestSeats(5)
	state := initRosterState(t, pack, seats)

	eng := newRosterTestEngine(pack)
	next, _, err := eng.Apply(state, engine.Action{
		UserID:  seats[0].UserID,
		Type:    ActionAssignJudge,
		Payload: json.RawMessage(`{"seat":2}`),
	})
	if err != nil {
		t.Fatalf("assign_judge: %v", err)
	}
	gs := requireRosterState(t, next)
	assertInvariants(t, pack, gs)

	seat0 := participantBySeat(gs, 0)
	if seat0.GetIsJudge() {
		t.Errorf("seat 0 still marked judge after being replaced")
	}
	seat2 := participantBySeat(gs, 2)
	if !seat2.GetIsJudge() || seat2.GetTeam() != pb.Team_TEAM_NONE || seat2.GetIsCaptain() {
		t.Errorf("seat 2 not installed as judge correctly: is_judge=%v team=%s is_captain=%v",
			seat2.GetIsJudge(), seat2.GetTeam(), seat2.GetIsCaptain())
	}
	seat4 := participantBySeat(gs, 4)
	if !seat4.GetIsCaptain() || seat4.GetTeam() != pb.Team_TEAM_A {
		t.Errorf("seat 4 should have been promoted to captain of team A: is_captain=%v team=%s",
			seat4.GetIsCaptain(), seat4.GetTeam())
	}
}

// TestSetTeamNameRejectsTooLong checks the 1-32 char guard (plan transition
// table row 4).
func TestSetTeamNameRejectsTooLong(t *testing.T) {
	pack := fakeRosterPack()
	seats := rosterTestSeats(3)
	state := initRosterState(t, pack, seats)

	payload, err := json.Marshal(SetTeamNamePayload{Team: "a", Name: strings.Repeat("x", 33)})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	eng := newRosterTestEngine(pack)
	_, _, err = eng.Apply(state, engine.Action{UserID: seats[0].UserID, Type: ActionSetTeamName, Payload: payload})
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
	}
}

// TestSetTeamNameAcceptsCyrillicUpToLimit is the M2 regression, applied to
// set_team_name's 1-32 *character* cap (transition table row 4): a 32-rune
// Cyrillic name is 64 bytes, and a byte-length check wrongly rejected it.
// Confirmed to FAIL before the fix: this Apply call returned
// engine.ErrIllegalAction instead of succeeding.
func TestSetTeamNameAcceptsCyrillicUpToLimit(t *testing.T) {
	pack := fakeRosterPack()
	seats := rosterTestSeats(3)
	state := initRosterState(t, pack, seats)

	name := strings.Repeat("д", 32)
	payload, err := json.Marshal(SetTeamNamePayload{Team: "a", Name: name})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	eng := newRosterTestEngine(pack)
	_, events, err := eng.Apply(state, engine.Action{UserID: seats[0].UserID, Type: ActionSetTeamName, Payload: payload})
	if err != nil {
		t.Fatalf("set_team_name(32 Cyrillic runes): unexpected error: %v", err)
	}
	if len(events) != 1 || events[0].Type != "team_renamed" {
		t.Fatalf("events = %v, want exactly one team_renamed event", events)
	}
}

// TestSetTeamNameRejectsCyrillicOverLimit checks the 33-rune boundary on the
// Cyrillic side: still rejected, but by a *character* count.
func TestSetTeamNameRejectsCyrillicOverLimit(t *testing.T) {
	pack := fakeRosterPack()
	seats := rosterTestSeats(3)
	state := initRosterState(t, pack, seats)

	payload, err := json.Marshal(SetTeamNamePayload{Team: "a", Name: strings.Repeat("д", 33)})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	eng := newRosterTestEngine(pack)
	_, _, err = eng.Apply(state, engine.Action{UserID: seats[0].UserID, Type: ActionSetTeamName, Payload: payload})
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
	}
}

// TestSetTeamNameEmitsTeamRenamed checks ruling R1.6: a rename has no single
// target participant, so it emits its own team_renamed{team, name} event
// instead of roster_changed — and, since roster_changed is now strictly a
// per-participant delta, a rename must emit no roster_changed events at all.
func TestSetTeamNameEmitsTeamRenamed(t *testing.T) {
	pack := fakeRosterPack()
	seats := rosterTestSeats(3)
	state := initRosterState(t, pack, seats)

	payload, err := json.Marshal(SetTeamNamePayload{Team: "a", Name: "The Sharks"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	eng := newRosterTestEngine(pack)
	_, events, err := eng.Apply(state, engine.Action{UserID: seats[0].UserID, Type: ActionSetTeamName, Payload: payload})
	if err != nil {
		t.Fatalf("set_team_name: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %v, want exactly 1", events)
	}
	if events[0].Type != "team_renamed" {
		t.Fatalf("events[0].Type = %q, want %q", events[0].Type, "team_renamed")
	}
	want := map[string]any{"team": "a", "name": "The Sharks"}
	if !reflect.DeepEqual(events[0].Data, want) {
		t.Fatalf("events[0].Data = %#v, want %#v", events[0].Data, want)
	}
	for _, ev := range events {
		if ev.Type == "roster_changed" {
			t.Fatalf("got roster_changed event %v, want none: a rename has no single target participant", ev)
		}
	}
}

// TestTrimmedGuessTextRejectsTooLong checks the Req 12 guess-text cap
// directly against the helper Steps 8-9 will call from guess/steal — those
// actions aren't implemented yet, so this test targets the payload-level
// validation this step does own.
func TestTrimmedGuessTextRejectsTooLong(t *testing.T) {
	_, err := trimmedGuessText(strings.Repeat("x", 201))
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
	}
}

// TestTrimmedGuessTextAcceptsCyrillicUpToLimit is the M2 regression: Req 12
// caps the guess text at 200 *characters* (the clients enforce
// maxLength={200}, a rune count), but trimmedGuessText originally capped
// len(s) — a Go byte count. A Cyrillic character encodes to 2 bytes in
// UTF-8, so a 200-rune Cyrillic guess is 400 bytes and was wrongly rejected.
// Confirmed to FAIL before the fix: trimmedGuessText returned
// engine.ErrIllegalAction for this input instead of accepting it.
func TestTrimmedGuessTextAcceptsCyrillicUpToLimit(t *testing.T) {
	guess := strings.Repeat("д", 200)
	got, err := trimmedGuessText(guess)
	if err != nil {
		t.Fatalf("trimmedGuessText(200 Cyrillic runes) error = %v, want nil", err)
	}
	if got != guess {
		t.Fatalf("trimmedGuessText(200 Cyrillic runes) = %q, want the input unchanged", got)
	}
}

// TestTrimmedGuessTextRejectsCyrillicOverLimit checks the 201-rune boundary
// on the Cyrillic side: still rejected, but by a *character* count, not a
// byte count.
func TestTrimmedGuessTextRejectsCyrillicOverLimit(t *testing.T) {
	_, err := trimmedGuessText(strings.Repeat("д", 201))
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
	}
}

// TestTrimmedGuessTextTrimsAndAllowsEmpty checks trimmedGuessText's happy
// path: whitespace is trimmed, and an empty (or all-whitespace) guess is
// allowed since Req 12 makes the guess text optional.
func TestTrimmedGuessTextTrimsAndAllowsEmpty(t *testing.T) {
	got, err := trimmedGuessText("  hello  ")
	if err != nil || got != "hello" {
		t.Fatalf("got %q, %v; want %q, nil", got, err, "hello")
	}
	got, err = trimmedGuessText("   ")
	if err != nil || got != "" {
		t.Fatalf("got %q, %v; want empty, nil", got, err)
	}
}

// TestDecodePayloadRejectsUnknownField checks that an unknown field in a
// payload fails loudly (DisallowUnknownFields), for one representative
// payload type — decodePayload is shared verbatim by every action.
func TestDecodePayloadRejectsUnknownField(t *testing.T) {
	_, err := decodePayload[AssignTeamPayload](json.RawMessage(`{"seat":1,"team":"a","bogus":true}`))
	if !errors.Is(err, engine.ErrIllegalAction) {
		t.Fatalf("err = %v, want engine.ErrIllegalAction", err)
	}
}
