package ttr

import (
	"errors"
	"fmt"
	"slices"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// tunnelRevealCount is the number of cards revealed to resolve a tunnel
// surcharge (rules §3.1 TUNNEL_REVEAL_COUNT, §8.4.2).
const tunnelRevealCount = 3

// errTunnelSurchargeUnaffordable marks validateTunnelSurcharge's one failure
// mode that is a genuine §8.4.5 "cannot pay" — the player's hand does not
// hold enough of a legal surcharge colour — as opposed to a malformed or
// arithmetically wrong request (an unparseable payload, an unknown or
// non-card colour, a colour that is neither the declared payment colour nor
// a locomotive, or a total that does not equal the surcharge). resolveTunnel
// checks for this specific sentinel to decide whether to fall back to the
// refund path or surface the error instead (M2 in the Step 11 review).
var errTunnelSurchargeUnaffordable = errors.New("hand cannot cover the tunnel surcharge")

// canonicalDiscardOrder is every real card colour (the 8 payable colours
// plus Locomotive) in a fixed, deterministic order. Several call sites
// (discardPay below, and the resign/tunnel-refund paths in turnflow.go)
// discard an entire hand/payment map's cards into gs.DiscardPile in one
// pass; ranging over the map directly would make the resulting discard-pile
// order vary run to run (Go's map iteration order is randomized), and
// refillDrawPile later shuffles that same pile back into the draw pile — so
// a seeded engine would stop being reproducible the moment a claim or
// resignation precedes a reshuffle (m1 in the Step 11 review). Iterating
// this fixed slice instead of the source map keeps every such site
// deterministic for a given sequence of actions.
var canonicalDiscardOrder = append(append([]Color{}, CardColors[:]...), ColorLoco)

// applyClaimRoute implements rules §8 end to end for one claim_route action:
// resolve the route and validate legality/payment, then either commit
// immediately (a non-tunnel route) or begin the §8.4 tunnel reveal.
func (e *Engine) applyClaimRoute(gs *pb.GameState, p *pb.PlayerState, pl ClaimRoutePayload, ev *[]engine.Event) error {
	m, err := e.resolveMap(gs)
	if err != nil {
		return err
	}
	r := m.RouteByID[pl.RouteID]

	if err := claimability(m, gs, p, r); err != nil {
		return err
	}

	pay, err := paymentToColors(pl.Payment)
	if err != nil {
		return err
	}
	paymentColor, err := validatePayment(r, p.Hand, pay)
	if err != nil {
		return err
	}

	if !r.Tunnel {
		if err := commitClaim(m, gs, p, r, pay, ev); err != nil {
			return err
		}
		return e.endTurn(m, gs, p, ev)
	}

	return e.beginTunnelClaim(m, gs, p, r, pay, paymentColor, ev)
}

// claimability reports whether p may claim route r (rules §8.1): it exists,
// has no owner, is not closed, p does not already own its double-route
// sibling (at any player count), and p has enough trains left. Payment
// legality is validatePayment's concern, not this function's. m is accepted
// for parity with the rules-doc signature; route legality here never needs
// anything beyond r itself.
func claimability(_ *Map, gs *pb.GameState, p *pb.PlayerState, r *Route) error {
	if r == nil {
		return fmt.Errorf("%w: unknown route", engine.ErrIllegalAction)
	}
	if _, owned := gs.RouteOwner[r.ID]; owned {
		return fmt.Errorf("%w: route %d is already claimed", engine.ErrIllegalAction, r.ID)
	}
	if slices.Contains(gs.ClosedRoutes, r.ID) {
		return fmt.Errorf("%w: route %d is closed", engine.ErrIllegalAction, r.ID)
	}
	if r.Pair != nil && slices.Contains(p.ClaimedRouteIds, *r.Pair) {
		return fmt.Errorf("%w: you already own route %d, the double-route sibling of %d", engine.ErrIllegalAction, *r.Pair, r.ID)
	}
	if p.TrainsLeft < int32(r.Length) { // #nosec G115 -- r.Length is a small map-authored constant
		return fmt.Errorf("%w: only %d trains left, route %d needs %d", engine.ErrIllegalAction, p.TrainsLeft, r.ID, r.Length)
	}
	return nil
}

// validateSingleColourPayment checks the payment shape shared by every §8.2
// colour/gray route cost and every §10.2 station cost: each entry must be
// affordable from hand and positive, the total must equal want exactly, and
// at most one non-locomotive colour may be used. It returns the declared
// payment colour that §8.4.1/§16.1 key the tunnel surcharge match on and
// that a route-colour check builds on: the single non-locomotive colour
// used, or ColorLoco if pay is entirely locomotives. Ferry floors and
// required-colour matching are route-specific and layered on by the caller
// (validatePayment); a station cost has no colour requirement beyond this.
func validateSingleColourPayment(hand map[int32]int32, pay map[Color]int, want int) (Color, error) {
	total := 0
	paymentColor := ColorLoco
	nonLocoSeen := false

	for c, n := range pay {
		if n <= 0 {
			return ColorUnspecified, fmt.Errorf("%w: payment count for %s must be positive, got %d", engine.ErrIllegalAction, c, n)
		}
		if hand[int32(c)] < int32(n) { // #nosec G115 -- n is a small validated payment count
			return ColorUnspecified, fmt.Errorf("%w: hand only has %d %s, payment needs %d", engine.ErrIllegalAction, hand[int32(c)], c, n)
		}
		total += n
		if c == ColorLoco {
			continue
		}
		if nonLocoSeen {
			return ColorUnspecified, fmt.Errorf("%w: payment uses more than one non-locomotive colour", engine.ErrIllegalAction)
		}
		nonLocoSeen = true
		paymentColor = c
	}

	if total != want {
		return ColorUnspecified, fmt.Errorf("%w: payment totals %d, need %d", engine.ErrIllegalAction, total, want)
	}
	return paymentColor, nil
}

// validatePayment checks pay's composition against r's cost (rules §8.2
// colours/gray substitution, §8.3 ferries) on top of
// validateSingleColourPayment's shared shape check, and, on success, returns
// the declared payment colour (see validateSingleColourPayment).
func validatePayment(r *Route, hand map[int32]int32, pay map[Color]int) (Color, error) {
	paymentColor, err := validateSingleColourPayment(hand, pay, r.Length)
	if err != nil {
		return ColorUnspecified, err
	}

	switch {
	case r.IsFerry():
		if pay[ColorLoco] < r.Locos {
			return ColorUnspecified, fmt.Errorf("%w: ferry route %d requires at least %d locomotives, paid %d", engine.ErrIllegalAction, r.ID, r.Locos, pay[ColorLoco])
		}
	case r.Color != ColorGray && paymentColor != ColorLoco && paymentColor != r.Color:
		return ColorUnspecified, fmt.Errorf("%w: route %d requires %s, paid %s", engine.ErrIllegalAction, r.ID, r.Color, paymentColor)
	}

	return paymentColor, nil
}

// debitHandCount removes n copies of colour c from p's hand, deleting the
// map entry once it reaches zero rather than leaving a zero-valued one. This
// mirrors addToHand's nil-map guard on the credit side (draw.go): a hand
// fully emptied by a claim must still be safe to credit from later, and
// reading a possibly-nil map is always safe in Go, so only the write branch
// needs care — and it is only reached when the entry already held >= n,
// which means the map was already non-nil.
func debitHandCount(p *pb.PlayerState, c Color, n int) {
	if n <= 0 {
		return
	}
	key := int32(c)
	remaining := p.Hand[key] - int32(n) // #nosec G115 -- n is a small validated payment count
	if remaining <= 0 {
		delete(p.Hand, key)
		return
	}
	p.Hand[key] = remaining
}

// discardPay appends every card in pay to the discard pile, n copies of each
// colour, in canonicalDiscardOrder rather than pay's own (randomized) map
// iteration order — see canonicalDiscardOrder's doc comment (m1 in the Step
// 11 review).
func discardPay(gs *pb.GameState, pay map[Color]int) {
	for _, c := range canonicalDiscardOrder {
		for range pay[c] {
			gs.DiscardPile = append(gs.DiscardPile, int32(c))
		}
	}
}

// colorCountsFromProto converts a PendingTunnel.BasePayment-shaped map
// (Color(int32) -> count) into a Color -> int map, for combining with a
// map[Color]int extra payment.
func colorCountsFromProto(m map[int32]int32) map[Color]int {
	out := make(map[Color]int, len(m))
	for k, v := range m {
		out[Color(k)] = int(v) // #nosec G115 -- k is always one of the 10 Color enum values written by this package
	}
	return out
}

// finishClaim performs the accounting shared by a direct §8.6 claim and a
// resolved §8.4.6 tunnel accept, given pay already known to be out of p's
// hand (either just debited, in the non-tunnel case, or held in a
// PendingTunnel that is about to be cleared): discard pay's cards, deduct
// trains, score the route (a PointsForLength error surfaces rather than
// being swallowed, per §12), and record ownership, closing the double-route
// sibling in 2-3 player games (§8.5).
func finishClaim(gs *pb.GameState, p *pb.PlayerState, r *Route, pay map[Color]int, ev *[]engine.Event) error {
	discardPay(gs, pay)
	p.TrainsLeft -= int32(r.Length) // #nosec G115 -- r.Length is a small map-authored constant

	pts, err := PointsForLength(r.Length)
	if err != nil {
		return err
	}
	p.RouteScore += int32(pts) // #nosec G115 -- pts is a small route-scoring constant

	if gs.RouteOwner == nil {
		gs.RouteOwner = make(map[int32]int32, 1)
	}
	gs.RouteOwner[r.ID] = p.SeatIndex
	p.ClaimedRouteIds = append(p.ClaimedRouteIds, r.ID)

	// "2-3 player games" (rules §8.5) is a fixed property of the seated game,
	// decided once at InitState — it must not track the live non-resigned
	// count, which a resignation can shrink mid-game. Keying this on
	// activePlayers(gs) instead would let a resignation turn on sibling
	// closure in an already-running 4-5 player game, stripping legal moves
	// from survivors and even flagging an already-claimed route as closed
	// once its sibling is claimed (M1 in the Step 11 review). This matches
	// assertClosedSiblings' len(gs.Players) check in invariants_test.go.
	if len(gs.Players) <= 3 && r.Pair != nil {
		gs.ClosedRoutes = append(gs.ClosedRoutes, *r.Pair)
	}

	if ev != nil {
		*ev = append(*ev, engine.Event{
			Type: "route_claimed",
			Data: map[string]any{"seat": p.SeatIndex, "route_id": r.ID, "length": r.Length, "points": pts},
		})
	}
	return nil
}

// commitClaim implements rules §8.6 for a non-tunnel claim: pay is still in
// p's hand, so it is debited first and then handed to finishClaim. m is
// accepted for parity with the rules-doc signature; nothing here needs it
// beyond r, which already came from m.
func commitClaim(_ *Map, gs *pb.GameState, p *pb.PlayerState, r *Route, pay map[Color]int, ev *[]engine.Event) error {
	for c, n := range pay {
		debitHandCount(p, c, n)
	}
	return finishClaim(gs, p, r, pay, ev)
}

// revealTunnel implements rules §8.4.2-§8.4.3: pop up to tunnelRevealCount
// cards from the draw pile (reshuffling the discard pile in mid-reveal if
// needed; fewer than 3 available reveals only what exists; none available
// reveals nothing), then count matches — a revealed locomotive always
// matches, and a revealed card of paymentColor also matches (which, when
// paymentColor is itself ColorLoco because the base payment was
// all-locomotive, collapses to "only locomotives match", rules §16.1). ev
// still collects a "deck_reshuffled" event if a reveal-triggered reshuffle
// happens (pile counts are public, unlike the reveals themselves): only the
// revealed cards' identities stay secret from the other players until
// resolution, which is why they are never themselves added to ev (m4 in the
// Step 11 review — this used to always pass nil, silently dropping that
// event even though callers had a real ev to append to).
func (e *Engine) revealTunnel(gs *pb.GameState, paymentColor Color, ev *[]engine.Event) (revealed []Color, matches int) {
	for range tunnelRevealCount {
		card, ok := e.popDrawPile(gs, ev)
		if !ok {
			break
		}
		revealed = append(revealed, Color(card)) // #nosec G115 -- card is always one of the 10 Color enum values written by this package
	}
	for _, c := range revealed {
		if c == ColorLoco || c == paymentColor {
			matches++
		}
	}
	return revealed, matches
}

// beginTunnelClaim implements the entry half of rules §8.4: pay is debited
// from p's hand into a held base payment (not yet discarded, so a decline
// can refund it exactly), then revealTunnel determines the surcharge. A
// surcharge of 0 (including the "nothing left to reveal" case, §8.4.2)
// resolves immediately: the reveals are discarded and the claim commits like
// any non-tunnel route. A nonzero surcharge parks the decision in
// PHASE_AWAITING_TUNNEL for resolveTunnel to finish.
func (e *Engine) beginTunnelClaim(m *Map, gs *pb.GameState, p *pb.PlayerState, r *Route, pay map[Color]int, paymentColor Color, ev *[]engine.Event) error {
	for c, n := range pay {
		debitHandCount(p, c, n)
	}
	base := make(map[int32]int32, len(pay))
	for c, n := range pay {
		base[int32(c)] = int32(n) // #nosec G115 -- n is a small validated payment count
	}

	revealed, matches := e.revealTunnel(gs, paymentColor, ev)
	revealedInts := make([]int32, len(revealed))
	for i, c := range revealed {
		revealedInts[i] = int32(c)
	}

	if matches == 0 {
		gs.DiscardPile = append(gs.DiscardPile, revealedInts...)
		if err := finishClaim(gs, p, r, pay, ev); err != nil {
			return err
		}
		return e.endTurn(m, gs, p, ev)
	}

	gs.PendingTunnel = &pb.PendingTunnel{
		RouteId:      r.ID,
		BasePayment:  base,
		PaymentColor: pb.Color(paymentColor),
		Revealed:     revealedInts,
		Surcharge:    int32(matches), // #nosec G115 -- matches is at most tunnelRevealCount (3)
	}
	gs.Phase = pb.Phase_PHASE_AWAITING_TUNNEL

	if ev != nil {
		// Deliberately omits "surcharge" (the match count) from the
		// broadcast payload: broadcastEvents (realtime/handler.go) pushes
		// every event to every subscriber, ignoring which seat it's for, so
		// anything placed in Data here reaches every opponent's socket
		// before the redacted view even arrives. view.go's own doc comment
		// says the tunnel reveal "must never reach anyone but the player
		// who must pay the surcharge" — the match count is exactly that
		// reveal, one step removed (M2 in the scoring/redaction review: it
		// leaks how many of the burned cards matched the payment colour,
		// which view.go's pendingView already withholds from everyone else).
		// The deciding player still learns the real surcharge through
		// pending.surcharge (view.go); this event exists only so every
		// viewer's log can read "seat X attempted a tunnel on route Y"
		// without the count. (The alternative — tunnel reveals are public
		// in the physical game, so broadcasting the count to everyone would
		// also be a defensible, consistent choice — was not taken here: it
		// would change the wire contract while a frontend worker is
		// mid-flight, whereas dropping a field is a no-op for any consumer
		// that wasn't already relying on it.)
		*ev = append(*ev, engine.Event{
			Type: "tunnel_surcharge",
			Data: map[string]any{"seat": p.SeatIndex, "route_id": r.ID},
		})
	}
	return nil
}

// resolveTunnel implements the rules §8.4.4-§8.4.6 accept/decline outcome
// for a pending tunnel surcharge. Only a genuine §8.4.5 "cannot or chooses
// not to pay" takes the refund path: an explicit decline (accept omitted or
// false), or an accept whose hand genuinely cannot cover the required
// surcharge (errTunnelSurchargeUnaffordable) — the base payment returns to
// the hand exactly, the reveals are discarded, and the turn ends with
// nothing gained. A malformed or arithmetically wrong extra_payment (fails
// to decode, an unknown/non-card colour, a colour that is neither the
// declared payment colour nor a locomotive, or a total that does not equal
// the surcharge) is NOT the same thing and must not silently forfeit the
// claim: it is returned as an error instead, which Apply's caller discards
// state on, leaving the tunnel decision pending so the client can correct
// and retry (M2 in the Step 11 review — this used to conflate "unpayable"
// with "malformed", both routed to the refund path with a success
// response).
func (e *Engine) resolveTunnel(m *Map, gs *pb.GameState, p *pb.PlayerState, pl ResolveDecisionPayload, ev *[]engine.Event) error {
	pt := gs.PendingTunnel
	if pt == nil {
		return fmt.Errorf("%w: no tunnel decision is pending", engine.ErrIllegalAction)
	}

	if pl.Accept == nil || !*pl.Accept {
		return e.refundTunnel(m, gs, p, pt, ev)
	}

	extra, err := paymentToColors(pl.ExtraPayment)
	if err != nil {
		return err
	}
	if err := validateTunnelSurcharge(pt, p.Hand, extra); err != nil {
		if errors.Is(err, errTunnelSurchargeUnaffordable) {
			return e.refundTunnel(m, gs, p, pt, ev)
		}
		return err
	}

	return e.commitTunnelAccept(m, gs, p, pt, extra, ev)
}

// validateTunnelSurcharge checks extra against pt's required composition
// (rules §8.4.4): it must total exactly pt.Surcharge, every colour must be
// pt.PaymentColor or a locomotive (which, when PaymentColor is itself
// ColorLoco, means only locomotives are accepted — §16.1), and every count
// must be within hand. The wrong-colour and wrong-total failures are
// malformed/arithmetically wrong requests (plain engine.ErrIllegalAction);
// only the hand-insufficient failure is the genuine §8.4.5 "cannot pay"
// case, additionally wrapping errTunnelSurchargeUnaffordable so resolveTunnel
// can route it to the refund path instead of surfacing it as an error.
func validateTunnelSurcharge(pt *pb.PendingTunnel, hand map[int32]int32, extra map[Color]int) error {
	paymentColor := Color(pt.PaymentColor) // #nosec G115 -- pt.PaymentColor is always one of the 10 Color enum values written by this package
	total := 0
	for c, n := range extra {
		if c != paymentColor && c != ColorLoco {
			return fmt.Errorf("%w: surcharge card must be %s or Locomotive, got %s", engine.ErrIllegalAction, paymentColor, c)
		}
		if hand[int32(c)] < int32(n) { // #nosec G115 -- n is a small validated payment count
			return fmt.Errorf("%w: %w: hand only has %d %s, surcharge needs %d", engine.ErrIllegalAction, errTunnelSurchargeUnaffordable, hand[int32(c)], c, n)
		}
		total += n
	}
	if total != int(pt.Surcharge) {
		return fmt.Errorf("%w: surcharge payment totals %d, need exactly %d", engine.ErrIllegalAction, total, pt.Surcharge)
	}
	return nil
}

// commitTunnelAccept implements rules §8.4.6: the extra payment is debited
// from the hand, combined with the already-held base payment, and the whole
// claim commits through finishClaim exactly like a non-tunnel route — the
// only difference is where the spent cards came from.
func (e *Engine) commitTunnelAccept(m *Map, gs *pb.GameState, p *pb.PlayerState, pt *pb.PendingTunnel, extra map[Color]int, ev *[]engine.Event) error {
	r := m.RouteByID[pt.RouteId]
	if r == nil {
		return fmt.Errorf("%w: pending tunnel references unknown route %d", engine.ErrIllegalAction, pt.RouteId)
	}

	for c, n := range extra {
		debitHandCount(p, c, n)
	}
	total := colorCountsFromProto(pt.BasePayment)
	for c, n := range extra {
		total[c] += n
	}

	gs.DiscardPile = append(gs.DiscardPile, pt.Revealed...)
	gs.PendingTunnel = nil

	if err := finishClaim(gs, p, r, total, ev); err != nil {
		return err
	}
	return e.endTurn(m, gs, p, ev)
}

// refundTunnel implements rules §8.4.5: the held base payment returns to p's
// hand card for card, the 3 revealed cards go to the discard pile, and the
// turn ends with the route unclaimed — no substitute action.
func (e *Engine) refundTunnel(m *Map, gs *pb.GameState, p *pb.PlayerState, pt *pb.PendingTunnel, ev *[]engine.Event) error {
	for c, n := range pt.BasePayment {
		for range n {
			addToHand(p, c)
		}
	}
	gs.DiscardPile = append(gs.DiscardPile, pt.Revealed...)
	gs.PendingTunnel = nil

	if ev != nil {
		*ev = append(*ev, engine.Event{
			Type: "tunnel_declined",
			Data: map[string]any{"seat": p.SeatIndex, "route_id": pt.RouteId},
		})
	}
	return e.endTurn(m, gs, p, ev)
}
