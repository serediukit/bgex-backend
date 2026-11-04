package song_guess

import (
	"github.com/google/uuid"

	pb "github.com/serediukit/bgex-backend/internal/games/song_guess/pb"
)

// viewerClass is buildView's classification of forUser against a GameState
// (Req 24): viewerPlayer/viewerSpectator get the identical shape (a
// spectator is simply a player with YourSeat == -1); viewerJudge gets that
// same shape plus the current song's answer while it is still in play.
type viewerClass int

const (
	viewerSpectator viewerClass = iota
	viewerPlayer
	viewerJudge
)

// SongGuessView is the per-viewer, redacted projection of a GameState sent
// to clients (plan.md "Redaction seam", Req 24-30). Every field name/JSON
// tag below is a cross-repo contract: the frontend's TypeScript mirror
// (Step 14) is generated from this exact shape, so nothing here may be
// renamed without updating both sides.
//
// Redaction rules (non-negotiable, enforced by buildView and its helpers):
//   - Cell.unplayed_song_ids is NEVER emitted; cells only ever carry the
//     Remaining/Total counts (a count, never an identity).
//   - The current song's SongId, and any unplayed song's title, artist,
//     media or raw YouTube URL, are NEVER emitted to anyone, in any phase
//     — history (finished songs) carries no song id either.
//   - Song.Blocks: an unrevealed block serializes as EXACTLY
//     {"revealed":false} — no "text" key at all, not an empty string, not
//     a length hint (see SongGuessBlockView). A revealed block serializes
//     as {"revealed":true,"text":"..."}.
//   - Song.Answer (title/artist) is populated for the JUDGE viewer class
//     only while the song is still in play (isSongFinished false); once
//     the song has ended (SONG_RESULT/FINISHED) it is public to every
//     class, because the title became public knowledge the instant
//     song_finished fired. Song.Media/AwardedTeam/AwardedPoints/Skipped/
//     LastGuessText follow the same "only once ended" rule, for every
//     class including the judge.
//   - A spectator — any connected user matching no ACTIVE participant, per
//     viewerOf — gets the PLAYER shape with YourSeat == -1, never the
//     judge shape. This holds even for a user id that matches an INACTIVE
//     (e.g. resigned) former judge: once inactive, they are a stranger
//     like any other spectator.
//   - Phase is always the LITERAL gs.Phase, specifically so the frontend
//     can render the PHASE_JUDGE_VACANT blocking banner. Every other
//     phase-dependent redaction decision in this file (whether the song
//     has ended, whether a song is even in flight) goes through
//     effectivePhase/isSongFinished instead — see their doc comments and
//     the plan's "Discovered during implementation" PHASE_JUDGE_VACANT
//     note: reading gs.Phase directly for those decisions is the single
//     most productive bug pattern in this build.
//   - Pending.GuessText (Architect ruling R2) is populated only for the
//     judge and for an active participant on pending.claimant_team; the
//     opposing team and spectators get has_guess only, with guess_text
//     omitted entirely — see SongGuessPendingView's doc comment.
type SongGuessView struct {
	PackName      string `json:"pack_name"`
	Phase         string `json:"phase"`
	RoundNo       int32  `json:"round_no"`
	ActiveTeam    string `json:"active_team"`
	YourSeat      int32  `json:"your_seat"`
	YourTeam      string `json:"your_team"`
	YouAreCaptain bool   `json:"you_are_captain"`
	YouAreJudge   bool   `json:"you_are_judge"`
	YouAreHost    bool   `json:"you_are_host"`

	Teams        []SongGuessTeamView        `json:"teams"`
	Participants []SongGuessParticipantView `json:"participants"`
	Cells        []SongGuessCellView        `json:"cells"`
	History      []SongGuessHistoryView     `json:"history"`
	Legal        []string                   `json:"legal"`
	Results      []SongGuessResultView      `json:"results,omitempty"`

	Song    *SongGuessSongView    `json:"song,omitempty"`
	Pending *SongGuessPendingView `json:"pending,omitempty"`
}

// SongGuessTeamView is one team's public scoreboard row.
type SongGuessTeamView struct {
	Team  string `json:"team"`
	Name  string `json:"name"`
	Score int32  `json:"score"`
}

// SongGuessParticipantView is one seat's public roster row: team/captaincy/
// judgeship/active are all public per rules §8.
type SongGuessParticipantView struct {
	SeatIndex int32  `json:"seat_index"`
	UserID    string `json:"user_id"`
	Team      string `json:"team"`
	IsCaptain bool   `json:"is_captain"`
	IsJudge   bool   `json:"is_judge"`
	Active    bool   `json:"active"`
}

// SongGuessCellView is one selection-table cell: Remaining/Total are counts
// only (Req 4) — the pack's unplayed_song_ids never appears on the wire.
type SongGuessCellView struct {
	Language  string `json:"language"`
	Bucket    int32  `json:"bucket"`
	Remaining int    `json:"remaining"`
	Total     int    `json:"total"`
}

// SongGuessHistoryView is one finished song, fully public (Q5): the title
// became public knowledge the instant song_finished fired, so there is
// nothing left to redact here except the song id itself (never emitted, in
// any phase — history does not need it, and it would be the one durable
// trace of exactly which song from the pack was played). ActiveTeam
// (ruling R1.1) is who picked the cell, deliberately distinct from
// AwardedTeam (who scored): a successful steal or last-chance guess awards
// the active team's OPPONENT, so the results screen can show who picked vs
// who scored each round.
type SongGuessHistoryView struct {
	Language      string `json:"language"`
	Bucket        int32  `json:"bucket"`
	RevealedAtEnd int32  `json:"revealed_at_end"`
	AwardedTeam   string `json:"awarded_team"`
	Points        int32  `json:"points"`
	Title         string `json:"title"`
	Artist        string `json:"artist"`
	Skipped       bool   `json:"skipped"`
	ActiveTeam    string `json:"active_team"`
}

// SongGuessResultView mirrors pb.ResultEntry (Req 36), public for every
// class once phase == FINISHED.
type SongGuessResultView struct {
	SeatIndex int32  `json:"seat_index"`
	UserID    string `json:"user_id"`
	Team      string `json:"team"`
	TeamTotal int32  `json:"team_total"`
	Rank      int32  `json:"rank"`
	IsJudge   bool   `json:"is_judge"`
}

// SongGuessBlockView is one block of the current song's authored line. An
// unrevealed block serializes as EXACTLY {"revealed":false} (Req 25): Text
// is a pointer, left nil, so encoding/json omits the "text" key entirely —
// never an empty string, never a placeholder, never a length hint.
type SongGuessBlockView struct {
	Revealed bool    `json:"revealed"`
	Text     *string `json:"text,omitempty"`
}

// SongGuessAnswerView is the current song's title/artist, plus its full
// authored line (Line, one entry per block, in order). See SongGuessView's
// doc comment for exactly when this is populated. Line is deliberately
// SEPARATE from Song.Blocks: Blocks stays redacted per the mask so the
// judge can still see which blocks the players themselves can see (block
// count is the score, so the slicing is game-relevant context for
// adjudicating) — Line exists so the judge can also read every word,
// including still-closed blocks, without that redaction disappearing.
// Line is populated for the judge while the song is in play (2026-08-15
// user request: the judge must see the whole line the entire time the song
// is shown, not only while a team is guessing) and, once the song has
// ended, for every viewer class — at that point it deliberately duplicates
// Song.Blocks, since the whole line is public to everyone anyway and a
// single shape keeps the frontend to one code path.
type SongGuessAnswerView struct {
	Title  string   `json:"title"`
	Artist string   `json:"artist"`
	Line   []string `json:"line"`
}

// SongGuessMediaView is the current song's YouTube fragment, already
// server-parsed to a bare VideoID (Req 30) — never a raw URL. Populated
// only once the song has ended (see SongGuessView's doc comment) and only
// if the song carried a "youtube" field at all.
type SongGuessMediaView struct {
	VideoID  string `json:"video_id"`
	StartSec int32  `json:"start_sec"`
	EndSec   int32  `json:"end_sec"`
}

// SongGuessSongView is the currently drawn song, redacted per viewer class
// and completion state — see SongGuessView's doc comment. It is only ever
// populated when gs.Song != nil (buildView checks that directly, not any
// phase — a song parked mid-play by PHASE_JUDGE_VACANT must keep this
// panel, which is exactly what checking gs.Song directly, instead of a
// phase, guarantees for free).
type SongGuessSongView struct {
	Language     string               `json:"language"`
	Bucket       int32                `json:"bucket"`
	TotalBlocks  int                  `json:"total_blocks"`
	Revealed     int32                `json:"revealed"`
	Potential    int32                `json:"potential"`
	StealAllowed bool                 `json:"steal_allowed"`
	Awaiting     string               `json:"awaiting"`
	Blocks       []SongGuessBlockView `json:"blocks"`

	Answer *SongGuessAnswerView `json:"answer,omitempty"`
	Media  *SongGuessMediaView  `json:"media,omitempty"`

	AwardedTeam   string `json:"awarded_team,omitempty"`
	AwardedPoints int32  `json:"awarded_points,omitempty"`
	Skipped       bool   `json:"skipped,omitempty"`
	LastGuessText string `json:"last_guess_text,omitempty"`
}

// SongGuessPendingView describes the in-flight judge decision (Architect
// ruling R2, Option C — supersedes Step 11's original "visible to
// everyone" doc comment, which contradicted Req 26 and has been corrected).
// Kind/ClaimantTeam/HasGuess are public to every viewer class, judge and
// spectator included: they only say a guess happened, of what kind, and
// whether it carried text. GuessText is populated ONLY for the judge, or
// for an active participant on pending.ClaimantTeam — never for the
// opposing team, and never for a spectator (a spectator is on no team, and
// relaying the text to the opponent is exactly the leak this closes). When
// unauthorized, GuessText is left nil so encoding/json omits the
// "guess_text" key entirely — never an empty string, matching
// SongGuessBlockView's established pattern for withheld text.
//
// Why this matters even though a correct guess reveals the answer anyway:
// judge_result's void action can return the song to play "as if it never
// happened" (rules semantics), but knowledge already shown to the opponent
// cannot be un-shown. Confining GuessText to the claimant's own team keeps
// void meaningful. The full text becomes public to every class at
// SONG_RESULT via SongGuessSongView.LastGuessText, once the song has
// genuinely ended.
type SongGuessPendingView struct {
	Kind         string  `json:"kind"`
	ClaimantTeam string  `json:"claimant_team"`
	HasGuess     bool    `json:"has_guess"`
	GuessText    *string `json:"guess_text,omitempty"`
}

// phaseWire renders Phase to its lower_snake_case wire form. This is always
// the LITERAL gs.Phase — see SongGuessView's doc comment on why
// PHASE_JUDGE_VACANT must show through as "judge_vacant" rather than being
// resolved via effectivePhase.
var phaseWire = map[pb.Phase]string{
	pb.Phase_PHASE_UNSPECIFIED:       "unspecified",
	pb.Phase_PHASE_ROSTER:            "roster",
	pb.Phase_PHASE_ROUND_SELECT:      "round_select",
	pb.Phase_PHASE_SONG_PLAY:         "song_play",
	pb.Phase_PHASE_PENDING_JUDGEMENT: "pending_judgement",
	pb.Phase_PHASE_SONG_RESULT:       "song_result",
	pb.Phase_PHASE_JUDGE_VACANT:      "judge_vacant",
	pb.Phase_PHASE_FINISHED:          "finished",
}

// songAwaitingWire renders Awaiting to its lower_snake_case wire form.
var songAwaitingWire = map[pb.Awaiting]string{
	pb.Awaiting_AWAITING_UNSPECIFIED: "unspecified",
	pb.Awaiting_AWAITING_ACTIVE:      "active",
	pb.Awaiting_AWAITING_STEAL:       "steal",
	pb.Awaiting_AWAITING_LAST_CHANCE: "last_chance",
}

// songFinishedPhases is isSongFinished's lookup table: the two effective
// phases at which a song's full answer is already public knowledge.
var songFinishedPhases = map[pb.Phase]bool{
	pb.Phase_PHASE_SONG_RESULT: true,
	pb.Phase_PHASE_FINISHED:    true,
}

// ViewFor builds the redacted SongGuessView for forUser given an
// already-resolved pack. Session.View (Step 12), which has a real ctx
// unlike this package's Apply/View seam, resolves the pinned pack itself
// via its own PackCache warmed under the request ctx and calls this
// directly — the Session path never relies on View's context.Background()
// fallback below.
func (e *Engine) ViewFor(pack *Pack, state []byte, forUser uuid.UUID) (any, error) {
	gs, err := unmarshal(state)
	if err != nil {
		return nil, err
	}
	return buildView(pack, gs, forUser), nil
}

// View builds the redacted SongGuessView for forUser, resolving the pinned
// pack via e.resolvePack first. It exists only to satisfy engine.Engine's
// interface (View(state []byte, forUser uuid.UUID) (any, error) — no ctx);
// see resolvePack's doc comment for why context.Background() is safe on
// this path. The real, production path is Session.View -> ViewFor.
func (e *Engine) View(state []byte, forUser uuid.UUID) (any, error) {
	gs, err := unmarshal(state)
	if err != nil {
		return nil, err
	}
	pack, err := e.resolvePack(gs)
	if err != nil {
		return nil, err
	}
	return buildView(pack, gs, forUser), nil
}

// viewerOf classifies forUser against gs and returns the participant they
// occupy, or nil for a spectator. A spectator is any connected user not
// matching an ACTIVE participant (Req 24) — an inactive (resigned)
// participant, even a former judge, is treated exactly like any other
// stranger: the player shape, YourSeat == -1, never the judge shape.
func viewerOf(gs *pb.GameState, forUser uuid.UUID) (viewerClass, *pb.Participant) {
	p := participantByUser(gs, forUser.String())
	if p == nil || !p.GetActive() {
		return viewerSpectator, nil
	}
	if p.GetIsJudge() {
		return viewerJudge, p
	}
	return viewerPlayer, p
}

// buildView constructs the full SongGuessView for forUser against gs/pack.
func buildView(pack *Pack, gs *pb.GameState, forUser uuid.UUID) *SongGuessView {
	class, p := viewerOf(gs, forUser)

	view := &SongGuessView{
		PackName:     pack.Name,
		Phase:        phaseWire[gs.GetPhase()],
		RoundNo:      gs.GetRoundNo(),
		ActiveTeam:   teamWire(gs.GetActiveTeam()),
		YourSeat:     -1,
		YourTeam:     teamWire(pb.Team_TEAM_NONE),
		Teams:        teamsView(gs),
		Participants: participantsView(gs),
		Cells:        cellsView(pack, gs),
		History:      historyView(gs),
		Legal:        []string{},
		Results:      resultsView(gs.GetResults()),
	}

	if p != nil {
		view.YourSeat = p.GetSeatIndex()
		view.YourTeam = teamWire(p.GetTeam())
		view.YouAreCaptain = p.GetIsCaptain()
		view.YouAreJudge = p.GetIsJudge()
		view.YouAreHost = p.GetSeatIndex() == gs.GetHostSeat()
		view.Legal = legalActions(gs, p)
	}

	if gs.GetSong() != nil {
		view.Song = songView(gs, class)
	}
	if pending := gs.GetPending(); pending != nil {
		view.Pending = pendingView(pending, class, p)
	}

	return view
}

// teamsView projects gs.Teams (always exactly TEAM_A then TEAM_B).
func teamsView(gs *pb.GameState) []SongGuessTeamView {
	teams := gs.GetTeams()
	out := make([]SongGuessTeamView, 0, len(teams))
	for _, t := range teams {
		out = append(out, SongGuessTeamView{Team: teamWire(t.GetTeam()), Name: t.GetName(), Score: t.GetScore()})
	}
	return out
}

// participantsView projects the full roster — public per rules §8.
func participantsView(gs *pb.GameState) []SongGuessParticipantView {
	participants := gs.GetParticipants()
	out := make([]SongGuessParticipantView, 0, len(participants))
	for _, p := range participants {
		out = append(out, SongGuessParticipantView{
			SeatIndex: p.GetSeatIndex(),
			UserID:    p.GetUserId(),
			Team:      teamWire(p.GetTeam()),
			IsCaptain: p.GetIsCaptain(),
			IsJudge:   p.GetIsJudge(),
			Active:    p.GetActive(),
		})
	}
	return out
}

// cellsView projects the selection table to counts only (Req 4): Remaining
// is a count, Total is looked up from pack — Cell.UnplayedSongIds never
// leaves this function.
func cellsView(pack *Pack, gs *pb.GameState) []SongGuessCellView {
	cells := gs.GetCells()
	out := make([]SongGuessCellView, 0, len(cells))
	for _, c := range cells {
		out = append(out, SongGuessCellView{
			Language:  c.GetLanguageName(),
			Bucket:    c.GetBucket(),
			Remaining: len(c.GetUnplayedSongIds()),
			Total:     pack.CountAt(c.GetLanguageName(), int(c.GetBucket())),
		})
	}
	return out
}

// historyView projects gs.History — every entry is a finished, fully public
// song (see SongGuessHistoryView's doc comment).
func historyView(gs *pb.GameState) []SongGuessHistoryView {
	history := gs.GetHistory()
	out := make([]SongGuessHistoryView, 0, len(history))
	for _, h := range history {
		out = append(out, SongGuessHistoryView{
			Language:      h.GetLanguageName(),
			Bucket:        h.GetBucket(),
			RevealedAtEnd: h.GetRevealedAtEnd(),
			AwardedTeam:   teamWire(h.GetAwardedTeam()),
			Points:        h.GetPoints(),
			Title:         h.GetTitle(),
			Artist:        h.GetArtist(),
			Skipped:       h.GetSkipped(),
			ActiveTeam:    teamWire(h.GetActiveTeam()),
		})
	}
	return out
}

// resultsView projects gs.Results (populated only at PHASE_FINISHED).
func resultsView(results []*pb.ResultEntry) []SongGuessResultView {
	out := make([]SongGuessResultView, 0, len(results))
	for _, r := range results {
		out = append(out, SongGuessResultView{
			SeatIndex: r.GetSeatIndex(),
			UserID:    r.GetUserId(),
			Team:      teamWire(r.GetTeam()),
			TeamTotal: r.GetTeamTotal(),
			Rank:      r.GetRank(),
			IsJudge:   r.GetIsJudge(),
		})
	}
	return out
}

// isSongFinished reports whether the current song has already been decided
// (effective phase SONG_RESULT or FINISHED) — the one predicate that gates
// every field this package must never disclose before a verdict (answer,
// media, awarded_team/points, skipped, last_guess_text, and full block
// text). It is evaluated against effectivePhase, NOT the literal gs.Phase:
// PHASE_JUDGE_VACANT parks the game without touching gs.Song, so a judge
// resigning mid-PENDING_JUDGEMENT must still read as "not finished" (the
// answer stays hidden), while a judge resigning right after SONG_RESULT
// must still read as "finished" (that song's title is already public — it
// would be a bug to re-hide it just because the literal phase changed).
func isSongFinished(gs *pb.GameState) bool {
	return songFinishedPhases[effectivePhase(gs)]
}

// songView redacts gs.Song per class and completion state. buildView only
// ever calls this when gs.Song != nil.
func songView(gs *pb.GameState, class viewerClass) *SongGuessSongView {
	song := gs.GetSong()
	finished := isSongFinished(gs)

	sv := &SongGuessSongView{
		Language:     song.GetLanguageName(),
		Bucket:       song.GetBucket(),
		TotalBlocks:  len(song.GetBlocks()),
		Revealed:     revealedCount(song),
		Potential:    potentialOf(song),
		StealAllowed: song.GetStealAllowed(),
		Awaiting:     songAwaitingWire[song.GetAwaiting()],
		Blocks:       blocksView(song, finished),
	}

	switch {
	case finished:
		// The song is over: every block, the answer and the media (if any)
		// are public to every viewer class (Q5) — the title became public
		// knowledge the instant song_finished fired. Answer.Line duplicates
		// Blocks here deliberately (see SongGuessAnswerView's doc comment):
		// it costs nothing since the whole line is already public, and it
		// keeps the wire shape uniform so the frontend needs only one code
		// path for the answer panel regardless of phase.
		sv.Answer = &SongGuessAnswerView{Title: song.GetTitle(), Artist: song.GetArtist(), Line: songLine(song)}
		if song.GetVideoId() != "" {
			sv.Media = &SongGuessMediaView{VideoID: song.GetVideoId(), StartSec: song.GetStartSec(), EndSec: song.GetEndSec()}
		}
		sv.AwardedTeam = teamWire(song.GetAwardedTeam())
		sv.AwardedPoints = song.GetAwardedPoints()
		sv.Skipped = song.GetSkipped()
		sv.LastGuessText = song.GetLastGuessText()
	case class == viewerJudge:
		// Still in play: only the judge gets the answer early (Req 26), and
		// now also the full line via Answer.Line (2026-08-15 user request:
		// the judge must see every word for the whole time the song is
		// shown, not only while a team is guessing) — but never media, and
		// blocks[] itself is UNCHANGED: it stays redacted per the mask, so
		// the judge can still see exactly which blocks the players
		// themselves can currently see.
		sv.Answer = &SongGuessAnswerView{Title: song.GetTitle(), Artist: song.GetArtist(), Line: songLine(song)}
	}

	return sv
}

// songLine returns song's full authored line, one entry per block, in
// order — the source for SongGuessAnswerView.Line. It is a thin wrapper
// over song.GetBlocks() purely so callers read as "the answer's line", not
// "the same slice blocksView redacts from"; it never itself redacts
// anything, so every call site MUST gate it exactly as songView does
// (judge, or finished).
func songLine(song *pb.SongState) []string {
	return song.GetBlocks()
}

// blocksView redacts song.Blocks (Req 25/26): once finished, every block is
// shown fully revealed; until then, only the indices whose
// song.RevealedMask entry is true are shown, and an unrevealed block carries
// no Text at all — see SongGuessBlockView's doc comment. Reading the mask
// per index (ruling R3.1), rather than a single revealed count, is what
// lets a song with holes (e.g. blocks 0 and 2 open, block 1 still closed)
// show exactly those and no others — a naive "i < revealed" port would
// over-disclose the gap.
func blocksView(song *pb.SongState, finished bool) []SongGuessBlockView {
	blocks := song.GetBlocks()
	mask := song.GetRevealedMask()
	out := make([]SongGuessBlockView, len(blocks))
	for i, text := range blocks {
		show := finished || (i < len(mask) && mask[i])
		out[i] = SongGuessBlockView{Revealed: show}
		if show {
			t := text
			out[i].Text = &t
		}
	}
	return out
}

// pendingView projects the in-flight judge decision, team-scoping GuessText
// per Architect ruling R2 — see SongGuessPendingView's doc comment. class/p
// are buildView's own viewerOf classification of forUser, so a spectator
// (p == nil) can never satisfy pendingGuessTextVisible's team check.
func pendingView(pending *pb.Pending, class viewerClass, p *pb.Participant) *SongGuessPendingView {
	guessText := pending.GetGuessText()
	view := &SongGuessPendingView{
		Kind:         judgeKindWire(pending.GetKind()),
		ClaimantTeam: teamWire(pending.GetClaimantTeam()),
		HasGuess:     guessText != "",
	}
	if pendingGuessTextVisible(class, p, pending) {
		view.GuessText = &guessText
	}
	return view
}

// pendingGuessTextVisible reports whether forUser's viewer class/participant
// is authorized to see pending.GuessText (ruling R2, Option C): the judge,
// or an active participant whose team matches the claimant's team. A
// spectator's p is always nil (viewerOf), so it can never match; an active
// participant on the OPPOSING team has non-nil p but a non-matching team, so
// it is excluded too.
func pendingGuessTextVisible(class viewerClass, p *pb.Participant, pending *pb.Pending) bool {
	if class == viewerJudge {
		return true
	}
	return p != nil && p.GetTeam() == pending.GetClaimantTeam()
}
