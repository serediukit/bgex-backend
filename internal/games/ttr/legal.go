package ttr

import (
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// LegalMoves enumerates everything p may do right now on m/gs. Used by the
// UI to enable/disable controls and (later) by any bot.
//
// It always returns a non-nil *TTRLegalView, taking the empty/false form
// (CanDrawCard/CanDrawTickets false, ClaimableRoutes/StationCities empty,
// StationCost 0, PendingKind "") for any player who isn't the one currently
// allowed to act — a non-current player in PHASE_NORMAL, any player in an
// AWAITING_* phase other than the deciding one, a PHASE_SETUP_TICKETS
// player who has already answered, or (checked first, unconditionally) any
// resigned player: checkPhaseGate already rejects every action a resigned
// player might submit (rules/plan Q14), but without this early return a
// player who resigned during PHASE_SETUP_TICKETS — before applyResign's
// "held the turn" branch ever touches SetupTicketOffer/SetupDone — would
// still be handed PendingKind == "setup_tickets" forever: an unsubmittable
// dialog the UI has no way to dismiss (m3 in the scoring/redaction review).
// buildView only ever calls this for the viewer's own seat; it is not
// meaningful (and never called) for opponents or spectators.
func LegalMoves(m *Map, gs *pb.GameState, p *pb.PlayerState) *TTRLegalView {
	if p.Resigned {
		return emptyLegalView()
	}
	return legalMovesForPhase(m, gs, p)
}

// legalMovesForPhase is LegalMoves' phase switch, split out purely to keep
// the resigned-player early return (see LegalMoves' doc comment, m3 in the
// scoring/redaction review) from pushing this switch's own branching over
// gocyclo's per-function limit.
func legalMovesForPhase(m *Map, gs *pb.GameState, p *pb.PlayerState) *TTRLegalView {
	switch gs.Phase {
	case pb.Phase_PHASE_SETUP_TICKETS:
		if p.SetupDone {
			return emptyLegalView()
		}
		v := emptyLegalView()
		v.PendingKind = DecisionKindSetupTickets
		return v

	case pb.Phase_PHASE_NORMAL:
		if !isCurrentPlayer(gs, p) {
			return emptyLegalView()
		}
		return legalForNormalTurn(m, gs, p)

	case pb.Phase_PHASE_AWAITING_SECOND_DRAW:
		if !isCurrentPlayer(gs, p) {
			return emptyLegalView()
		}
		v := emptyLegalView()
		if secondDrawAvailable(gs) {
			v.CanDrawCard = true
		} else {
			// The only legal move left is the resolve_decision{end_draw}
			// escape hatch (rules §7.1, §7.4).
			v.PendingKind = DecisionKindEndDraw
		}
		return v

	case pb.Phase_PHASE_AWAITING_TUNNEL:
		if !isCurrentPlayer(gs, p) {
			return emptyLegalView()
		}
		v := emptyLegalView()
		v.PendingKind = DecisionKindTunnel
		return v

	case pb.Phase_PHASE_AWAITING_TICKET_KEEP:
		if !isCurrentPlayer(gs, p) {
			return emptyLegalView()
		}
		v := emptyLegalView()
		v.PendingKind = DecisionKindTicketKeep
		return v

	case pb.Phase_PHASE_SCORING, pb.Phase_PHASE_FINISHED, pb.Phase_PHASE_UNSPECIFIED:
		// Nothing is legal for anyone once the game has moved past normal
		// play, or (defensively) in the never-really-reached zero phase.
		return emptyLegalView()

	default:
		return emptyLegalView()
	}
}

// emptyLegalView is the shared zero/false form every non-deciding branch of
// LegalMoves returns: false booleans, non-nil-but-empty slices (never null
// on the wire), zero station cost, no pending kind.
func emptyLegalView() *TTRLegalView {
	return &TTRLegalView{
		ClaimableRoutes: []ClaimableRoute{},
		StationCities:   []string{},
	}
}

// legalForNormalTurn builds the real TTRLegalView for the current player
// during PHASE_NORMAL: the four turn actions' availability and concrete
// options.
func legalForNormalTurn(m *Map, gs *pb.GameState, p *pb.PlayerState) *TTRLegalView {
	v := emptyLegalView()
	v.CanDrawCard = canDraw(gs)
	v.CanDrawTickets = len(gs.TicketDeck) > 0
	v.ClaimableRoutes = claimableRoutes(m, gs, p)
	v.StationCities, v.StationCost = stationOptions(m, gs, p)
	return v
}

// claimableRoutes lists every route p may legally claim right now: it must
// pass claimability (rules §8.1 — unowned, not closed, sibling not already
// owned by p, enough trains left) AND have at least one PaymentOption given
// p's hand (excludes the unaffordable case claimability itself doesn't
// check). Iterates m.Rules.Routes in document order for a deterministic
// result.
func claimableRoutes(m *Map, gs *pb.GameState, p *pb.PlayerState) []ClaimableRoute {
	out := make([]ClaimableRoute, 0, len(m.Rules.Routes))
	for i := range m.Rules.Routes {
		r := &m.Rules.Routes[i]
		if err := claimability(m, gs, p, r); err != nil {
			continue
		}
		opts := PaymentOptions(r, p.Hand)
		if len(opts) == 0 {
			continue
		}
		out = append(out, ClaimableRoute{RouteID: r.ID, PaymentOptions: opts})
	}
	return out
}

// stationOptions lists every city p may build a station in right now (rules
// §10.1: any city with no existing station, routeless cities included) and
// the cost of that next station (rules §10.2). Both are the empty/zero form
// once p.StationsLeft is exhausted, regardless of what stationCost would
// otherwise compute.
func stationOptions(m *Map, gs *pb.GameState, p *pb.PlayerState) ([]string, int) {
	if p.StationsLeft <= 0 {
		return []string{}, 0
	}

	cities := make([]string, 0, len(m.Rules.Cities))
	for i := range m.Rules.Cities {
		id := m.Rules.Cities[i].ID
		if _, taken := gs.StationOwner[id]; taken {
			continue
		}
		cities = append(cities, id)
	}
	return cities, stationCost(p, m)
}

// PaymentOptions enumerates the distinct minimal payment compositions for
// route r from hand (rules §8.2 colours/gray, §8.3 ferries): one per viable
// base colour, each using the fewest locomotives possible to make up
// r.Length. A colored (non-gray, non-ferry) route has exactly one candidate
// base colour — r.Color itself; a gray route (ferry or not) considers every
// one of the 8 payable colours. Options needing more locomotives than the
// hand holds are omitted entirely ("nothing invalid"). Every candidate that
// resolves to spending zero of its nominal colour (because the hand holds
// none of it) is a purely-locomotive payment indistinguishable from every
// other such candidate, so those collapse into a single entry rather than
// one per unused colour.
func PaymentOptions(r *Route, hand map[int32]int32) []PaymentOption {
	if r == nil {
		return nil
	}

	forcedLoco := 0
	remaining := r.Length
	if r.IsFerry() {
		forcedLoco = r.Locos
		remaining = r.Length - r.Locos
	}
	availableLoco := int(hand[int32(ColorLoco)])
	if forcedLoco > availableLoco {
		return nil // can't even cover the ferry's mandatory locomotives
	}
	availableLoco -= forcedLoco

	candidates := CardColors[:]
	if !r.IsFerry() && r.Color != ColorGray {
		candidates = []Color{r.Color}
	}

	var options []PaymentOption
	sawPureLoco := false
	for _, c := range candidates {
		used := min(int(hand[int32(c)]), remaining)
		neededLoco := remaining - used
		if neededLoco > availableLoco {
			continue
		}
		if used == 0 {
			if sawPureLoco {
				continue
			}
			sawPureLoco = true
		}

		payment := make(map[string]int, 2)
		if used > 0 {
			payment[c.String()] = used
		}
		if totalLoco := forcedLoco + neededLoco; totalLoco > 0 {
			payment[ColorLoco.String()] = totalLoco
		}
		options = append(options, PaymentOption{Payment: payment})
	}
	return options
}
