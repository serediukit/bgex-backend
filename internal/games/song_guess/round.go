package song_guess

import (
	"fmt"
	"slices"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// This file implements plan "Chosen Approach" transition table rows 6 and
// 21: select_cell draws a song and enters PHASE_SONG_PLAY, and next_round
// advances active_team and either opens the next round or reaches
// PHASE_FINISHED. Per ruling R1.1, the single history-append site is
// finishSong in play.go (Step 8) — next_round appends nothing; it only
// clears gs.Song and flips gs.ActiveTeam.

// requireActiveCaptain resolves the acting participant and confirms they
// are the current captain of gs.ActiveTeam, returning engine.ErrNotSeated /
// engine.ErrNotYourTurn otherwise. Row 6's guard.
func requireActiveCaptain(gs *pb.GameState, a engine.Action) (*pb.Participant, error) {
	p := participantByUser(gs, a.UserID.String())
	if p == nil {
		return nil, engine.ErrNotSeated
	}
	captain := captainOf(gs, gs.GetActiveTeam())
	if captain == nil || captain.GetSeatIndex() != p.GetSeatIndex() {
		return nil, engine.ErrNotYourTurn
	}
	return p, nil
}

// applySelectCell implements transition table row 6: the active team's
// captain draws one unplayed song at random from the (language, bucket)
// cell and the song enters play. pack must be the pinned pack the caller
// already resolved via e.resolvePack; sh is the engine's injectable
// Shuffler so tests can seed the draw.
func applySelectCell(gs *pb.GameState, a engine.Action, pack *Pack, sh Shuffler) ([]engine.Event, error) {
	if gs.GetPhase() != pb.Phase_PHASE_ROUND_SELECT {
		return nil, engine.ErrWrongPhase
	}
	if _, err := requireActiveCaptain(gs, a); err != nil {
		return nil, err
	}
	payload, err := decodePayload[SelectCellPayload](a.Payload)
	if err != nil {
		return nil, err
	}

	cell := cellFor(gs, payload.Language, payload.Bucket)
	if cell == nil || len(cell.GetUnplayedSongIds()) == 0 {
		return nil, fmt.Errorf("%w: cell (%s,%d) has no unplayed songs", engine.ErrIllegalAction, payload.Language, payload.Bucket)
	}

	ids := cell.GetUnplayedSongIds()
	idx := sh.IntN(len(ids))
	songID := ids[idx]
	// Order within unplayed_song_ids carries no meaning (it is an unordered
	// pool), so removing by swap-with-last avoids shifting the remainder.
	ids[idx] = ids[len(ids)-1]
	cell.UnplayedSongIds = ids[:len(ids)-1]
	cell.PlayedCount++

	song := pack.SongByID(songID)
	if song == nil {
		return nil, fmt.Errorf("%w: drawn song %q not found in pinned pack", ErrInvalidPackDoc, songID)
	}

	ss := &pb.SongState{
		SongId:       song.ID,
		LanguageName: payload.Language,
		Bucket:       payload.Bucket,
		Blocks:       slices.Clone(song.Line),
		// RevealedMask is sized to the full line, every entry false, the
		// instant a song is drawn (ruling R3.1) — its length is a load-
		// bearing invariant (law 8), never derived on the fly elsewhere.
		RevealedMask: make([]bool, len(song.Line)),
		StealAllowed: true,
		Awaiting:     pb.Awaiting_AWAITING_ACTIVE,
		Title:        song.Title,
		Artist:       song.Artist,
	}
	if song.Media != nil {
		ss.VideoId = song.Media.VideoID
		ss.StartSec = song.Media.StartSec
		ss.EndSec = song.Media.EndSec
	}
	gs.Song = ss
	gs.Phase = pb.Phase_PHASE_SONG_PLAY

	return []engine.Event{{
		Type: "song_selected",
		Data: map[string]any{
			"language":     payload.Language,
			"bucket":       payload.Bucket,
			"total_blocks": len(ss.Blocks),
		},
	}}, nil
}

// unplayedRemaining returns the total number of unplayed songs still sitting
// across every cell of the selection table.
func unplayedRemaining(gs *pb.GameState) int {
	n := 0
	for _, c := range gs.GetCells() {
		n += len(c.GetUnplayedSongIds())
	}
	return n
}

// applyNextRound implements transition table row 21: legal only in
// PHASE_SONG_RESULT, for the captain of the team about to become active or
// the host (row 21's deliberately widened actor set — the one transition
// that would otherwise strand the game if a single captain went silent).
// It appends NOTHING to gs.History (ruling R1.1: finishSong, added in Step
// 8's play.go, is the single append site — whichever handler enters
// PHASE_SONG_RESULT already appended the finished song's outcome before
// next_round is ever called).
func applyNextRound(gs *pb.GameState, a engine.Action) ([]engine.Event, error) {
	if gs.GetPhase() != pb.Phase_PHASE_SONG_RESULT {
		return nil, engine.ErrWrongPhase
	}

	actor := participantByUser(gs, a.UserID.String())
	if actor == nil {
		return nil, engine.ErrNotSeated
	}
	nextTeam := opponentOf(gs.GetActiveTeam())
	nextCaptain := captainOf(gs, nextTeam)
	isNextCaptain := nextCaptain != nil && nextCaptain.GetSeatIndex() == actor.GetSeatIndex()
	isHost := actor.GetSeatIndex() == gs.GetHostSeat()
	if !isNextCaptain && !isHost {
		return nil, engine.ErrNotYourTurn
	}

	gs.ActiveTeam = nextTeam
	gs.Song = nil

	if unplayedRemaining(gs) == 0 {
		// scoring.go's finalize owns computing results/ranks and emitting
		// game_over (Step 10). gs.Phase is still PHASE_SONG_RESULT here, so
		// finalize's in-flight check correctly sees no song in flight — the
		// last song's outcome was already appended before this call.
		return finalize(gs), nil
	}

	gs.RoundNo++
	gs.Phase = pb.Phase_PHASE_ROUND_SELECT
	return []engine.Event{{
		Type: "round_started",
		Data: map[string]any{"round_no": gs.GetRoundNo(), "active_team": teamWire(gs.GetActiveTeam())},
	}}, nil
}
