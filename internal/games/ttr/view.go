package ttr

import (
	"maps"

	"github.com/google/uuid"

	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// TTRView is the per-viewer, redacted projection of a GameState sent to
// clients (plan.md "Action / wire protocol › Server → client"). Every field
// name/JSON tag below is a cross-repo contract: the frontend's TypeScript
// mirror (Step 16) is generated from this exact shape, so nothing here may
// be renamed without updating both sides.
//
// Redaction rules (non-negotiable, enforced by buildView and its helpers):
//   - draw_pile / discard_pile / ticket_deck contents are NEVER emitted, only
//     their counts (DrawPileCount / DiscardPileCount / TicketDeckCount).
//   - Players[].HandCount / TicketCount are counts only; no other viewer ever
//     sees a per-colour breakdown of another player's hand.
//   - YourHand / YourTickets are populated only when the viewer occupies a
//     seat (forUser matches a player's user id).
//   - Pending is populated only when the viewer IS the deciding player for
//     whichever decision is in flight; Pending.Revealed (a tunnel's secret
//     reveal) is the single most sensitive field in the wire protocol and
//     must never reach anyone but the player who must pay the surcharge.
//   - A non-seated viewer (spectator) gets YourSeat == -1 and no YourHand,
//     YourTickets, Pending or Legal.
type TTRView struct {
	MapID            string   `json:"map_id"`
	MapVersion       int32    `json:"map_version"`
	Phase            string   `json:"phase"`
	TurnNo           int32    `json:"turn_no"`
	CurrentSeat      int32    `json:"current_seat"`
	YourSeat         int32    `json:"your_seat"`
	FinalTurnsLeft   *int32   `json:"final_turns_left"`
	FaceUp           []string `json:"face_up"`
	DrawPileCount    int      `json:"draw_pile_count"`
	DiscardPileCount int      `json:"discard_pile_count"`
	TicketDeckCount  int      `json:"ticket_deck_count"`
	// RouteOwner/StationOwner are public knowledge (who owns what), never
	// redacted; keyed by route id / city id respectively. A map[int32]int32
	// marshals with its integer keys stringified ("17"), matching the wire
	// contract's { "17": 2 } shape.
	RouteOwner   map[int32]int32  `json:"route_owner"`
	ClosedRoutes []int32          `json:"closed_routes"`
	StationOwner map[string]int32 `json:"station_owner"`
	Players      []TTRPlayerView  `json:"players"`

	// YourHand/YourTickets: only ever set when the viewer occupies a seat
	// (see buildView). omitempty means a legitimately-empty owned collection
	// (e.g. a hand spent to exactly zero cards, or tickets not yet kept
	// during PHASE_SETUP_TICKETS) is omitted the same way a redacted one
	// would be — harmless, because the frontend disambiguates "is this my
	// seat" via YourSeat, never via the presence of these keys.
	YourHand    map[string]int32 `json:"your_hand,omitempty"`
	YourTickets []TTRTicketView  `json:"your_tickets,omitempty"`

	Draw    *TTRDrawView    `json:"draw,omitempty"`
	Pending *TTRPendingView `json:"pending,omitempty"`
	Legal   *TTRLegalView   `json:"legal,omitempty"`
	Results []TTRResultView `json:"results,omitempty"`
}

// TTRPlayerView is one player's public seat state, as seen by every viewer
// (seated or spectating) alike — hand/ticket contents are never in here,
// only counts.
type TTRPlayerView struct {
	SeatIndex       int32   `json:"seat_index"`
	UserID          string  `json:"user_id"`
	TrainsLeft      int32   `json:"trains_left"`
	StationsLeft    int32   `json:"stations_left"`
	RouteScore      int32   `json:"route_score"`
	HandCount       int     `json:"hand_count"`
	TicketCount     int     `json:"ticket_count"`
	Resigned        bool    `json:"resigned"`
	ClaimedRouteIds []int32 `json:"claimed_route_ids"`
}

// TTRTicketView is one destination ticket as seen by its owner (your_tickets)
// or offered to them (pending.offered_tickets). Completed is a live,
// advisory hint — see ticketsView's doc comment — never the scored value.
type TTRTicketView struct {
	ID        int32  `json:"id"`
	A         string `json:"a"`
	B         string `json:"b"`
	Points    int    `json:"points"`
	Long      bool   `json:"long"`
	Completed bool   `json:"completed"`
}

// TTRDrawView mirrors DrawProgress (rules §7.1-§7.2). It is public — whether
// the current player has already taken their first card this turn is not a
// secret — so it is populated for every viewer whenever gs.Draw is set,
// regardless of seat.
type TTRDrawView struct {
	CardsTaken       int32 `json:"cards_taken"`
	FaceUpLocoLocked bool  `json:"face_up_loco_locked"`
}

// TTRPendingView describes whichever resolve_decision-shaped decision is
// currently open, from the point of view of the one player (or, during
// PHASE_SETUP_TICKETS, one of possibly several simultaneous players) who
// must answer it. It is never sent to anyone else.
//
// OfferedTickets is populated for Kind == "setup_tickets" or "ticket_keep"
// (the tickets the deciding player must choose among); RouteID/PaymentColor/
// Revealed/Surcharge are populated for Kind == "tunnel". This struct/field
// (OfferedTickets) is an addition beyond the plan's worked JSON example,
// which only illustrated the tunnel shape — see the Step 13 report for why
// it's necessary (the UI cannot render a ticket-keep choice from bare ids).
type TTRPendingView struct {
	Kind           string          `json:"kind"`
	OfferedTickets []TTRTicketView `json:"offered_tickets,omitempty"`
	RouteID        int32           `json:"route_id,omitempty"`
	PaymentColor   string          `json:"payment_color,omitempty"`
	Revealed       []string        `json:"revealed,omitempty"`
	Surcharge      int32           `json:"surcharge,omitempty"`
}

// TTRLegalView is the viewer's own legal-move summary (LegalMoves, legal.go).
// Only ever populated for a seated viewer; nil (omitted) for a spectator.
// For a seated viewer it is always non-nil, taking the empty/false form
// whenever that viewer is not the one who may currently act.
type TTRLegalView struct {
	CanDrawCard     bool             `json:"can_draw_card"`
	CanDrawTickets  bool             `json:"can_draw_tickets"`
	ClaimableRoutes []ClaimableRoute `json:"claimable_routes"`
	StationCities   []string         `json:"station_cities"`
	StationCost     int              `json:"station_cost"`
	// PendingKind names whichever resolve_decision kind is blocking the four
	// turn actions for this viewer right now ("setup_tickets", "ticket_keep",
	// "tunnel", "end_draw"), or "" when nothing is blocking (including when
	// it simply isn't this viewer's turn). Unlike Pending, this never carries
	// secret content — it is only a label the UI uses to pick which decision
	// dialog to show.
	PendingKind string `json:"pending_kind,omitempty"`
}

// ClaimableRoute is one route the viewer may legally claim right now
// (rules §8.1), together with the minimal-locomotive payment composition
// for each of its viable base colours (see PaymentOptions' doc comment) —
// NOT every distinct way the viewer's hand could pay for it. A single base
// colour only ever contributes the one option that spends as few
// locomotives as that colour's own hand count allows: e.g. hand {Blue:3,
// Loco:4} on a length-3 Blue route is offered only {Blue:3}, even though
// paying {Locomotive:3} instead (saving the Blue cards for a longer route
// later) is also legal per validateSingleColourPayment and sometimes the
// better play. Reworded rather than widened to enumerate the full ladder
// (m4 in the scoring/redaction review): the frontend already consumes this
// exact shape, so changing what payment_options contains would be a
// wire-contract change, not a doc fix.
type ClaimableRoute struct {
	RouteID        int32           `json:"route_id"`
	PaymentOptions []PaymentOption `json:"payment_options"`
}

// PaymentOption is one concrete, submittable payment composition: color name
// (per Color.String — omitted if 0 cards of a real colour are spent, i.e. a
// pure-locomotive payment) mapped to count, "Locomotive" mapped to its own
// count. It is exactly the shape the wire protocol's claim_route payload
// expects as "payment", so the UI can hand an option straight back to the
// server unmodified.
type PaymentOption struct {
	Payment map[string]int `json:"payment"`
}

// TTRResultView mirrors pb.ScoreBreakdown (rules §12-§13) for the wire. It
// is only ever populated in phase "finished", at which point every field is
// public information for every viewer (scoring itself carries no secrets).
type TTRResultView struct {
	SeatIndex              int32            `json:"seat_index"`
	RoutePoints            int32            `json:"route_points"`
	TicketsCompletedPoints int32            `json:"tickets_completed_points"`
	TicketsMissedPoints    int32            `json:"tickets_missed_points"`
	StationBonus           int32            `json:"station_bonus"`
	LongestPathBonus       int32            `json:"longest_path_bonus"`
	LongestPathLength      int32            `json:"longest_path_length"`
	Total                  int32            `json:"total"`
	Rank                   int32            `json:"rank"`
	CompletedTicketIds     []int32          `json:"completed_ticket_ids"`
	MissedTicketIds        []int32          `json:"missed_ticket_ids"`
	BorrowedRoutes         map[string]int32 `json:"borrowed_routes"`
	SharedVictory          bool             `json:"shared_victory"`
}

// phaseNames renders Phase to the lower_snake_case wire form (plan.md's
// "phase": "normal" / "awaiting_tunnel" / ...).
var phaseNames = map[pb.Phase]string{
	pb.Phase_PHASE_SETUP_TICKETS:        "setup_tickets",
	pb.Phase_PHASE_NORMAL:               "normal",
	pb.Phase_PHASE_AWAITING_SECOND_DRAW: "awaiting_second_draw",
	pb.Phase_PHASE_AWAITING_TUNNEL:      "awaiting_tunnel",
	pb.Phase_PHASE_AWAITING_TICKET_KEEP: "awaiting_ticket_keep",
	pb.Phase_PHASE_SCORING:              "scoring",
	pb.Phase_PHASE_FINISHED:             "finished",
}

// ViewFor builds the redacted TTRView for forUser given an already-resolved
// map m. Step 14's Session.View (which has a real ctx, unlike this package's
// Apply/View seam) resolves the map itself via e.maps and calls this
// directly, avoiding View's context.Background() cache lookup.
func (e *Engine) ViewFor(m *Map, state []byte, forUser uuid.UUID) (any, error) {
	gs, err := unmarshal(state)
	if err != nil {
		return nil, err
	}
	return buildView(m, gs, forUser), nil
}

// View builds the redacted TTRView for forUser, resolving the pinned map via
// e.maps first. See resolveMap's doc comment for why context.Background() is
// safe here.
func (e *Engine) View(state []byte, forUser uuid.UUID) (any, error) {
	gs, err := unmarshal(state)
	if err != nil {
		return nil, err
	}
	m, err := e.resolveMap(gs)
	if err != nil {
		return nil, err
	}
	return buildView(m, gs, forUser), nil
}

// buildView constructs the full TTRView for forUser. p is nil for a
// spectator (forUser occupies no seat), in which case YourSeat stays -1 and
// YourHand/YourTickets/Pending/Legal are all left unset.
func buildView(m *Map, gs *pb.GameState, forUser uuid.UUID) *TTRView {
	p := playerByUser(gs, forUser.String())

	view := &TTRView{
		MapID:            gs.MapId,
		MapVersion:       gs.MapVersion,
		Phase:            phaseNames[gs.Phase],
		TurnNo:           gs.TurnNo,
		CurrentSeat:      gs.CurrentSeat,
		YourSeat:         -1,
		FinalTurnsLeft:   finalTurnsLeftPtr(gs.FinalTurnsLeft),
		FaceUp:           colorNameList(gs.FaceUp),
		DrawPileCount:    len(gs.DrawPile),
		DiscardPileCount: len(gs.DiscardPile),
		TicketDeckCount:  len(gs.TicketDeck),
		RouteOwner:       nonNilInt32Map(gs.RouteOwner),
		ClosedRoutes:     append(make([]int32, 0, len(gs.ClosedRoutes)), gs.ClosedRoutes...),
		StationOwner:     nonNilStringMap(gs.StationOwner),
		Players:          make([]TTRPlayerView, 0, len(gs.Players)),
	}

	for _, pl := range gs.Players {
		view.Players = append(view.Players, playerView(pl))
	}

	if gs.Draw != nil {
		view.Draw = &TTRDrawView{CardsTaken: gs.Draw.CardsTaken, FaceUpLocoLocked: gs.Draw.FaceUpLocoLocked}
	}

	if p != nil {
		view.YourSeat = p.SeatIndex
		view.YourHand = handView(p.Hand)
		view.YourTickets = ticketsView(m, gs, p)
		view.Legal = LegalMoves(m, gs, p)
		view.Pending = pendingView(m, gs, p)
	}

	if gs.Phase == pb.Phase_PHASE_FINISHED {
		view.Results = resultsView(gs.Results)
	}

	return view
}

// playerView projects one PlayerState to its public TTRPlayerView: counts
// only, never a hand/ticket-id breakdown.
func playerView(p *pb.PlayerState) TTRPlayerView {
	return TTRPlayerView{
		SeatIndex:       p.SeatIndex,
		UserID:          p.UserId,
		TrainsLeft:      p.TrainsLeft,
		StationsLeft:    p.StationsLeft,
		RouteScore:      p.RouteScore,
		HandCount:       handCount(p),
		TicketCount:     len(p.TicketIds),
		Resigned:        p.Resigned,
		ClaimedRouteIds: append(make([]int32, 0, len(p.ClaimedRouteIds)), p.ClaimedRouteIds...),
	}
}

// handCount sums every card count in p.Hand, for TTRPlayerView.HandCount.
func handCount(p *pb.PlayerState) int {
	n := 0
	for _, c := range p.Hand {
		n += int(c)
	}
	return n
}

// handView converts a PlayerState.Hand (Color(int32) -> count) into the wire
// shape (colour name -> count), always non-nil.
func handView(hand map[int32]int32) map[string]int32 {
	out := make(map[string]int32, len(hand))
	for c, n := range hand {
		out[Color(c).String()] = n // #nosec G115 -- c is always one of the 10 Color enum values written by this package
	}
	return out
}

// ticketsView builds the viewer's own your_tickets: every kept ticket, with
// a live "completed" hint from bestStationAssignment (rules §13.2/§13.4) —
// advisory only, not the scored value (that is only computed once, at
// finalizeGame). This is the one place buildView pays for the optimizer, and
// only for the viewer's own tickets (at most once per View/ViewFor call, not
// per opponent) — bestStationAssignment's branching is bounded by <=3
// stations (see its doc comment in scoring.go), so this stays cheap enough
// to run on every broadcast.
func ticketsView(m *Map, gs *pb.GameState, p *pb.PlayerState) []TTRTicketView {
	_, _, completed, _ := bestStationAssignment(m, gs, p)
	completedSet := make(map[int32]bool, len(completed))
	for _, id := range completed {
		completedSet[id] = true
	}

	out := make([]TTRTicketView, 0, len(p.TicketIds))
	for _, id := range p.TicketIds {
		tk := m.TicketByID[id]
		if tk == nil {
			continue
		}
		out = append(out, TTRTicketView{
			ID: tk.ID, A: tk.A, B: tk.B, Points: tk.Points, Long: tk.Long,
			Completed: completedSet[id],
		})
	}
	return out
}

// offeredTicketViews resolves a list of ticket ids (a setup offer or an
// in-game ticket draw, still being decided) to their full TTRTicketView
// shape, so the UI can render the actual choice (cities, points) rather than
// bare ids. Completed is left false — a not-yet-kept ticket has no owner to
// compute connectivity against.
func offeredTicketViews(m *Map, ids []int32) []TTRTicketView {
	out := make([]TTRTicketView, 0, len(ids))
	for _, id := range ids {
		tk := m.TicketByID[id]
		if tk == nil {
			continue
		}
		out = append(out, TTRTicketView{ID: tk.ID, A: tk.A, B: tk.B, Points: tk.Points, Long: tk.Long})
	}
	return out
}

// pendingView builds p's own TTRPendingView if, and only if, p is the (or a)
// deciding player for whichever resolve_decision-shaped phase gs is
// currently in. Every other case (including "it's a resolve_decision phase
// but p already answered / isn't the current seat", and unconditionally a
// resigned p — see LegalMoves's doc comment on m3 in the scoring/redaction
// review: a player who resigned mid-PHASE_SETUP_TICKETS never gets
// SetupDone set, and must not still be shown an unsubmittable setup_tickets
// dialog) returns nil, which buildView leaves as an omitted "pending" key.
func pendingView(m *Map, gs *pb.GameState, p *pb.PlayerState) *TTRPendingView {
	if p.Resigned {
		return nil
	}

	switch {
	case gs.Phase == pb.Phase_PHASE_SETUP_TICKETS && !p.SetupDone:
		return &TTRPendingView{
			Kind:           DecisionKindSetupTickets,
			OfferedTickets: offeredTicketViews(m, p.SetupTicketOffer),
		}
	case gs.Phase == pb.Phase_PHASE_AWAITING_TICKET_KEEP && gs.PendingTicketDraw != nil && isCurrentPlayer(gs, p):
		return &TTRPendingView{
			Kind:           DecisionKindTicketKeep,
			OfferedTickets: offeredTicketViews(m, gs.PendingTicketDraw.OfferedTicketIds),
		}
	case gs.Phase == pb.Phase_PHASE_AWAITING_TUNNEL && gs.PendingTunnel != nil && isCurrentPlayer(gs, p):
		pt := gs.PendingTunnel
		return &TTRPendingView{
			Kind:         DecisionKindTunnel,
			RouteID:      pt.RouteId,
			PaymentColor: Color(pt.PaymentColor).String(), // #nosec G115 -- pt.PaymentColor is always one of the 10 Color enum values written by this package
			Revealed:     colorNameList(pt.Revealed),
			Surcharge:    pt.Surcharge,
		}
	default:
		return nil
	}
}

// resultsView converts gs.Results (populated only in PHASE_FINISHED) to the
// wire shape, guaranteeing every nested slice/map is non-nil.
func resultsView(results []*pb.ScoreBreakdown) []TTRResultView {
	out := make([]TTRResultView, 0, len(results))
	for _, sb := range results {
		out = append(out, TTRResultView{
			SeatIndex:              sb.SeatIndex,
			RoutePoints:            sb.RoutePoints,
			TicketsCompletedPoints: sb.TicketsCompletedPoints,
			TicketsMissedPoints:    sb.TicketsMissedPoints,
			StationBonus:           sb.StationBonus,
			LongestPathBonus:       sb.LongestPathBonus,
			LongestPathLength:      sb.LongestPathLength,
			Total:                  sb.Total,
			Rank:                   sb.Rank,
			CompletedTicketIds:     append(make([]int32, 0, len(sb.CompletedTicketIds)), sb.CompletedTicketIds...),
			MissedTicketIds:        append(make([]int32, 0, len(sb.MissedTicketIds)), sb.MissedTicketIds...),
			BorrowedRoutes:         nonNilStringMap(sb.BorrowedRoutes),
			SharedVictory:          sb.SharedVictory,
		})
	}
	return out
}

// colorNameList renders a slice of Color(int32) wire values to their canonical
// names, always non-nil (used for both face_up and a tunnel's revealed).
func colorNameList(colors []int32) []string {
	out := make([]string, 0, len(colors))
	for _, c := range colors {
		out = append(out, Color(c).String()) // #nosec G115 -- c is always one of the 10 Color enum values written by this package
	}
	return out
}

// finalTurnsLeftPtr returns nil (JSON null) while v < 0 — the "not yet
// triggered" sentinel (rules §11) — or a pointer to v once armed.
func finalTurnsLeftPtr(v int32) *int32 {
	if v < 0 {
		return nil
	}
	n := v
	return &n
}

// nonNilInt32Map returns a fresh copy of m (or a fresh empty map if m is
// nil) — a proto3 map field with zero entries never round-trips through
// marshal/unmarshal (comes back nil), and a nil map marshals to JSON null,
// not {} (rules: "slices/collections must always serialize as arrays,
// never null"). Copying rather than returning m by reference matches every
// sibling collection buildView hands to a TTRView (ClosedRoutes,
// ClaimedRouteIds, CompletedTicketIds, ...), which are all defensively
// copied: today the caller's map is a private gs (View/ViewFor's own
// unmarshal, unshared with anything else), so returning it directly happens
// to be safe, but that safety is a property of the current two call sites,
// not of this function's contract — the copy makes the contract hold on
// its own (m5 in the scoring/redaction review).
func nonNilInt32Map(m map[int32]int32) map[int32]int32 {
	out := make(map[int32]int32, len(m))
	maps.Copy(out, m)
	return out
}

// nonNilStringMap is nonNilInt32Map's string-keyed counterpart.
func nonNilStringMap(m map[string]int32) map[string]int32 {
	out := make(map[string]int32, len(m))
	maps.Copy(out, m)
	return out
}
