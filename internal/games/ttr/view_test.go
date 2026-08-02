package ttr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"

	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// viewJSON builds the redacted view for forUser via ViewFor (bypassing
// e.maps entirely, since the tests below only care about redaction logic
// over a hand-built *pb.GameState, not map resolution) and marshals it to
// raw JSON bytes for byte-level inspection.
func viewJSON(t *testing.T, m *Map, gs *pb.GameState, forUser uuid.UUID) []byte {
	t.Helper()

	e := newTestEngine(m, 1)
	state, err := marshal(gs)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	v, err := e.ViewFor(m, state, forUser)
	if err != nil {
		t.Fatalf("ViewFor: unexpected error: %v", err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	return raw
}

// decodeView unmarshals raw view JSON back into a *TTRView for field-level
// assertions (used alongside the raw-byte greps below — the redaction tests
// are only trustworthy when they check the actual marshalled bytes, per the
// step brief, so this is a convenience on top of that, not a replacement).
func decodeView(t *testing.T, raw []byte) *TTRView {
	t.Helper()
	var v TTRView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal view JSON: %v\n%s", err, raw)
	}
	return &v
}

// --- Test 1: opponent hand/ticket redaction --------------------------------

// TestViewRedactsOpponentHandAndTickets checks that a player's view never
// contains another player's hand composition or ticket ids — asserted
// against the raw marshalled JSON bytes, not just Go struct fields, per the
// step brief ("a struct field that is populated but happens to be json:"-"
// would pass a field-level check while still leaking").
func TestViewRedactsOpponentHandAndTickets(t *testing.T) {
	m := testMap(t)
	gs, ids := newNormalState(m, 2)

	gs.Players[0].Hand = handFromColors(map[Color]int{ColorRed: 2})
	// A distinctive colour/count pair that must never appear in seat 0's
	// view, and a secret ticket id (105, the long "e-b" ticket) likewise.
	gs.Players[1].Hand = handFromColors(map[Color]int{ColorYellow: 9})
	gs.Players[1].TicketIds = []int32{105}

	raw := viewJSON(t, m, gs, ids[0])

	if bytes.Contains(raw, []byte(`"Yellow":9`)) {
		t.Errorf("view leaks opponent's hand composition (Yellow:9): %s", raw)
	}
	if bytes.Contains(raw, []byte(`"id":105`)) {
		t.Errorf("view leaks opponent's secret ticket id 105: %s", raw)
	}

	// Sanity: the viewer's own hand DOES appear, so the absence above is a
	// redaction, not a test that merely finds nothing at all.
	if !bytes.Contains(raw, []byte(`"Red":2`)) {
		t.Errorf("view is missing the viewer's own hand composition: %s", raw)
	}
}

// --- Test 2: pile contents never leak, only counts -------------------------

// TestViewRedactsPileContents checks that draw_pile/discard_pile/ticket_deck
// contents are never present on the wire (only their counts), by grepping
// for the raw-content key names (with a trailing colon, so "draw_pile_count"
// can't spuriously match) and separately confirming the count keys/values.
func TestViewRedactsPileContents(t *testing.T) {
	m := testMap(t)
	gs, ids := newNormalState(m, 2)

	gs.DrawPile = colorInts(ColorRed, ColorBlue, ColorGreen)
	gs.DiscardPile = colorInts(ColorBlack, ColorWhite)
	gs.FaceUp = stableFaceUp()
	gs.TicketDeck = []int32{7, 8, 9}

	raw := viewJSON(t, m, gs, ids[0])

	for _, key := range []string{`"draw_pile":`, `"discard_pile":`, `"ticket_deck":`} {
		if bytes.Contains(raw, []byte(key)) {
			t.Errorf("view leaks raw pile contents via key %s: %s", key, raw)
		}
	}

	for marker, want := range map[string]int{
		`"draw_pile_count":`:    len(gs.DrawPile),
		`"discard_pile_count":`: len(gs.DiscardPile),
		`"ticket_deck_count":`:  len(gs.TicketDeck),
	} {
		wantBytes := fmt.Appendf(nil, "%s%d", marker, want)
		if !bytes.Contains(raw, wantBytes) {
			t.Errorf("view missing expected count %s in %s", wantBytes, raw)
		}
	}
}

// --- Test 3: spectator view --------------------------------------------

// TestViewSpectator checks that a viewer occupying no seat gets your_seat ==
// -1 and none of your_hand/your_tickets/pending/legal, at both the struct
// and raw-JSON level.
func TestViewSpectator(t *testing.T) {
	m := testMap(t)
	gs, _ := newNormalState(m, 2)
	stranger := uuid.New()

	raw := viewJSON(t, m, gs, stranger)
	view := decodeView(t, raw)

	if view.YourSeat != -1 {
		t.Errorf("your_seat = %d, want -1 for a spectator", view.YourSeat)
	}
	if view.YourHand != nil {
		t.Errorf("your_hand = %v, want absent for a spectator", view.YourHand)
	}
	if view.YourTickets != nil {
		t.Errorf("your_tickets = %v, want absent for a spectator", view.YourTickets)
	}
	if view.Pending != nil {
		t.Errorf("pending = %+v, want absent for a spectator", view.Pending)
	}
	if view.Legal != nil {
		t.Errorf("legal = %+v, want absent for a spectator", view.Legal)
	}

	for _, key := range []string{`"your_hand"`, `"your_tickets"`, `"pending"`, `"legal"`} {
		if bytes.Contains(raw, []byte(key)) {
			t.Errorf("spectator's raw view JSON contains key %s: %s", key, raw)
		}
	}
}

// --- Test 4: pending.revealed only for the deciding player ------------------

// TestViewPendingRevealedOnlyForDecidingPlayer checks the single most
// sensitive field in the wire protocol: a pending tunnel's revealed cards
// tell you what the deciding player must pay, and must never reach anyone
// else (rules §8.4).
func TestViewPendingRevealedOnlyForDecidingPlayer(t *testing.T) {
	m := testMap(t)
	gs, ids := newNormalState(m, 2)
	gs.Phase = pb.Phase_PHASE_AWAITING_TUNNEL
	gs.CurrentSeat = 0
	gs.PendingTunnel = &pb.PendingTunnel{
		RouteId:      3,
		BasePayment:  map[int32]int32{int32(ColorBlue): 4},
		PaymentColor: pb.Color(ColorBlue),
		Revealed:     colorInts(ColorRed, ColorWhite, ColorLoco),
		Surcharge:    2,
	}

	deciderRaw := viewJSON(t, m, gs, ids[0])
	decider := decodeView(t, deciderRaw)
	if decider.Pending == nil || decider.Pending.Kind != DecisionKindTunnel {
		t.Fatalf("deciding player: pending = %+v, want a tunnel decision", decider.Pending)
	}
	wantRevealed := []string{"Red", "White", "Locomotive"}
	if !reflect.DeepEqual(decider.Pending.Revealed, wantRevealed) {
		t.Errorf("deciding player: pending.revealed = %v, want %v", decider.Pending.Revealed, wantRevealed)
	}
	if decider.Pending.Surcharge != 2 {
		t.Errorf("deciding player: pending.surcharge = %d, want 2", decider.Pending.Surcharge)
	}

	otherRaw := viewJSON(t, m, gs, ids[1])
	other := decodeView(t, otherRaw)
	if other.Pending != nil {
		t.Errorf("non-deciding player: pending = %+v, want absent", other.Pending)
	}
	for _, key := range []string{`"revealed"`, `"surcharge"`, `"pending"`} {
		if bytes.Contains(otherRaw, []byte(key)) {
			t.Errorf("non-deciding player's raw view leaks tunnel secrets via key %s: %s", key, otherRaw)
		}
	}
}

// --- Test 5: final_turns_left null vs number --------------------------------

// TestViewFinalTurnsLeft checks that final_turns_left marshals as JSON null
// before the §11 trigger (FinalTurnsLeft < 0) and as a number afterward.
func TestViewFinalTurnsLeft(t *testing.T) {
	m := testMap(t)
	gs, ids := newNormalState(m, 2)

	raw := viewJSON(t, m, gs, ids[0])
	if !bytes.Contains(raw, []byte(`"final_turns_left":null`)) {
		t.Errorf("final_turns_left should be null before the trigger: %s", raw)
	}

	gs.FinalTurnsLeft = 2
	raw = viewJSON(t, m, gs, ids[0])
	if !bytes.Contains(raw, []byte(`"final_turns_left":2`)) {
		t.Errorf("final_turns_left should be 2 after the trigger: %s", raw)
	}
}

// --- Test 6: empty collections marshal as []/{}, never null ----------------

// TestViewEmptyCollectionsMarshalAsArraysNotNull checks that every
// unconditionally-present collection in the view — closed_routes,
// route_owner, station_owner, face_up, claimed_route_ids — renders as an
// empty array/object rather than JSON null when its underlying protobuf
// field is a nil map/slice (the "a proto3 map/slice with zero entries never
// round-trips" gotcha this package's own comments call out elsewhere). The
// one legitimate null in the whole payload is final_turns_left before the
// §11 trigger, so this also cross-checks that "null" appears exactly once.
func TestViewEmptyCollectionsMarshalAsArraysNotNull(t *testing.T) {
	m := testMap(t)
	gs, ids := newNormalState(m, 2)
	// newNormalState leaves RouteOwner/StationOwner/ClosedRoutes/FaceUp all
	// nil and every player's ClaimedRouteIds nil — exactly the shapes this
	// test needs, with nothing further to set up.

	raw := viewJSON(t, m, gs, ids[0])

	for _, want := range []string{
		`"closed_routes":[]`,
		`"route_owner":{}`,
		`"station_owner":{}`,
		`"face_up":[]`,
		`"claimed_route_ids":[]`,
	} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("view is missing %s (nil collection may have serialized as null instead): %s", want, raw)
		}
	}

	if n := bytes.Count(raw, []byte("null")); n != 1 {
		t.Errorf(`view JSON contains %d occurrence(s) of "null", want exactly 1 (final_turns_left only): %s`, n, raw)
	}
}

// --- Test 7: LegalMoves ------------------------------------------------
//
// Each aspect of LegalMoves gets its own top-level Test function (rather
// than one Test function full of t.Run subtests) so that no single function
// accumulates enough branches to trip gocyclo.

func TestLegalMovesCanDrawCardFalseWithEmptyPiles(t *testing.T) {
	m := testMap(t)
	gs, _ := newNormalState(m, 2)
	// DrawPile, DiscardPile and FaceUp are all left nil/empty.
	legal := LegalMoves(m, gs, gs.Players[0])
	if legal.CanDrawCard {
		t.Errorf("can_draw_card = true, want false with every pile empty")
	}
}

func TestLegalMovesCanDrawTicketsFalseWithEmptyDeck(t *testing.T) {
	m := testMap(t)
	gs, _ := newNormalState(m, 2)
	gs.TicketDeck = nil
	legal := LegalMoves(m, gs, gs.Players[0])
	if legal.CanDrawTickets {
		t.Errorf("can_draw_tickets = true, want false with an empty ticket deck")
	}
}

// TestLegalMovesClaimableRoutesExcludesIllegalAndUnaffordable checks every
// exclusion bullet 7 of the step brief lists: owned, closed, sibling-owned,
// unaffordable and too-long routes are all filtered out, while a genuinely
// claimable route remains.
func TestLegalMovesClaimableRoutesExcludesIllegalAndUnaffordable(t *testing.T) {
	m := testMap(t)
	gs, _ := newNormalState(m, 2)
	p0 := gs.Players[0]
	p0.Hand = handFromColors(map[Color]int{ColorRed: 10, ColorBlue: 10})
	p0.TrainsLeft = 5
	// Route 1 (a-b, Red, len 2): p0 already owns it -> excluded (owned).
	// Route 9 (a-h, Purple, len 3, paired with 10): p0 owns it too, so its
	// sibling route 10 must be excluded on the "sibling-owned" branch
	// specifically (deliberately NOT adding 10 to ClosedRoutes, to isolate
	// this from the "closed" exclusion below).
	p0.ClaimedRouteIds = []int32{1, 9}
	gs.RouteOwner = map[int32]int32{1: 0, 9: 0}
	// Route 8 (g-h, White, len 2) is marked closed outright, unrelated to
	// any pair — claimability's closed-check is a plain membership test, so
	// this exercises "closed" independently of "sibling-owned".
	gs.ClosedRoutes = []int32{8}

	legal := LegalMoves(m, gs, p0)
	claimable := make(map[int32]bool, len(legal.ClaimableRoutes))
	for _, cr := range legal.ClaimableRoutes {
		claimable[cr.RouteID] = true
	}

	cases := []struct {
		routeID int32
		want    bool
		reason  string
	}{
		{1, false, "already owned by the viewer"},
		{9, false, "already owned by the viewer"},
		{10, false, "sibling of an owned route"},
		{8, false, "closed"},
		{14, false, "too long for trains_left (8 > 5)"},
		{7, false, "unaffordable (no Black or Locomotive in hand)"},
		{2, true, "affordable Gray route (b-c, len 3, hand has Red+Blue)"},
	}
	for _, c := range cases {
		if got := claimable[c.routeID]; got != c.want {
			t.Errorf("route %d claimable = %v, want %v (%s)", c.routeID, got, c.want, c.reason)
		}
	}
}

func TestLegalMovesStationCitiesExcludesStationedCities(t *testing.T) {
	m := testMap(t)
	gs, _ := newNormalState(m, 2)
	p0 := gs.Players[0]
	gs.StationOwner = map[string]int32{"a": 0}
	p0.StationCities = []string{"a"}
	p0.StationsLeft = 2

	legal := LegalMoves(m, gs, p0)
	for _, c := range legal.StationCities {
		if c == "a" {
			t.Errorf("station_cities = %v, must not include already-stationed city %q", legal.StationCities, c)
		}
	}
	if legal.StationCost != 2 {
		t.Errorf("station_cost = %d, want 2 (2nd station)", legal.StationCost)
	}
}

func TestLegalMovesStationCitiesAndCostEmptyAtZeroStationsLeft(t *testing.T) {
	m := testMap(t)
	gs, _ := newNormalState(m, 2)
	p0 := gs.Players[0]
	p0.StationsLeft = 0

	legal := LegalMoves(m, gs, p0)
	if len(legal.StationCities) != 0 {
		t.Errorf("station_cities = %v, want empty at stations_left == 0", legal.StationCities)
	}
	if legal.StationCost != 0 {
		t.Errorf("station_cost = %d, want 0 at stations_left == 0", legal.StationCost)
	}
}

func TestLegalMovesStationCostByBuildCount(t *testing.T) {
	m := testMap(t)
	gs, _ := newNormalState(m, 2)
	p0 := gs.Players[0]
	for stationsLeft, wantCost := range map[int32]int{3: 1, 2: 2, 1: 3} {
		p0.StationsLeft = stationsLeft
		legal := LegalMoves(m, gs, p0)
		if legal.StationCost != wantCost {
			t.Errorf("stations_left=%d: station_cost = %d, want %d", stationsLeft, legal.StationCost, wantCost)
		}
	}
}

func TestLegalMovesEmptyFormForNonCurrentPlayer(t *testing.T) {
	m := testMap(t)
	gs, _ := newNormalState(m, 2)
	p1 := gs.Players[1] // CurrentSeat is 0, so seat 1 is not current.

	legal := LegalMoves(m, gs, p1)
	if legal.CanDrawCard || legal.CanDrawTickets || len(legal.ClaimableRoutes) != 0 ||
		len(legal.StationCities) != 0 || legal.StationCost != 0 || legal.PendingKind != "" {
		t.Errorf("non-current player: legal = %+v, want the fully empty/false form", legal)
	}
}

func TestLegalMovesEmptyFormWithPendingKindForAwaitingPhases(t *testing.T) {
	m := testMap(t)
	gs, _ := newNormalState(m, 2)
	gs.Phase = pb.Phase_PHASE_AWAITING_TUNNEL
	gs.CurrentSeat = 0

	legal := LegalMoves(m, gs, gs.Players[0])
	if legal.CanDrawCard || legal.CanDrawTickets || len(legal.ClaimableRoutes) != 0 || len(legal.StationCities) != 0 {
		t.Errorf("deciding player in AWAITING_TUNNEL: legal = %+v, want the four turn actions all empty/false", legal)
	}
	if legal.PendingKind != DecisionKindTunnel {
		t.Errorf("deciding player in AWAITING_TUNNEL: pending_kind = %q, want %q", legal.PendingKind, DecisionKindTunnel)
	}
}

// TestLegalMovesAndPendingEmptyForResignedPlayerDuringSetup is the
// regression test for m3 in the scoring/redaction review: applyResign never
// clears SetupTicketOffer nor sets SetupDone (turnflow.go), so a player who
// resigns during PHASE_SETUP_TICKETS is left with a permanently non-empty,
// unanswered offer. Pre-fix, LegalMoves/pendingView only checked
// !p.SetupDone (never p.Resigned), so that player was handed
// pending_kind == "setup_tickets" / pending.kind == "setup_tickets" forever
// — an unsubmittable dialog (checkPhaseGate already rejects any attempt to
// actually answer it, but the UI had no way to know that). Both must now
// report the empty/nil form for a resigned player regardless of
// SetupDone/SetupTicketOffer.
func TestLegalMovesAndPendingEmptyForResignedPlayerDuringSetup(t *testing.T) {
	m := testMap(t)
	gs, _ := newNormalState(m, 2)
	gs.Phase = pb.Phase_PHASE_SETUP_TICKETS
	p0 := gs.Players[0]
	p0.SetupTicketOffer = []int32{1, 2, 3, 4} // deliberately left non-empty, as applyResign leaves it
	p0.SetupDone = false                      // deliberately left false, as applyResign leaves it
	p0.Resigned = true

	legal := LegalMoves(m, gs, p0)
	if legal.PendingKind != "" {
		t.Errorf("resigned player during setup: pending_kind = %q, want \"\" (an unsubmittable dialog)", legal.PendingKind)
	}

	if pv := pendingView(m, gs, p0); pv != nil {
		t.Errorf("resigned player during setup: pending = %+v, want nil", pv)
	}
}

// --- Test 8: PaymentOptions --------------------------------------------

// paymentOptionSet renders each PaymentOption's Payment map to a canonical
// string (fmt sorts map keys when formatting, so this is stable regardless
// of map iteration order) for order-independent set comparison.
func paymentOptionSet(opts []PaymentOption) map[string]bool {
	set := make(map[string]bool, len(opts))
	for _, o := range opts {
		set[fmt.Sprint(o.Payment)] = true
	}
	return set
}

// TestPaymentOptionsGrayRoute covers the step brief's worked example: a gray
// length-3 route, one base colour fully sufficient, one needing a
// locomotive top-up, and a purely-locomotive option.
//
// NOTE: the step brief describes this scenario against hand {Red:3, Blue:2,
// Loco:2}, but that hand is arithmetically inconsistent for a length-3
// route: an all-locomotive payment needs 3 locomotives, and that hand only
// holds 2, so "Red-only, Blue+2Loco, and all-Loco" cannot all be valid at
// once (and the minimal-locomotive rule would make the Blue option
// Blue:2+Locomotive:1, not +2, since only 1 more card is needed to reach
// length 3). This test uses a corrected hand — {Red:3, Blue:1, Loco:3} —
// that realizes the same three named options exactly as described,
// preserving the spirit of the example. See the Step 13 report.
func TestPaymentOptionsGrayRoute(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[2] // b-c, Gray, length 3

	hand := handFromColors(map[Color]int{ColorRed: 3, ColorBlue: 1, ColorLoco: 3})
	got := paymentOptionSet(PaymentOptions(r, hand))

	want := map[string]bool{
		fmt.Sprint(map[string]int{"Red": 3}):                   true,
		fmt.Sprint(map[string]int{"Blue": 1, "Locomotive": 2}): true,
		fmt.Sprint(map[string]int{"Locomotive": 3}):            true,
	}
	if len(got) != len(want) {
		t.Fatalf("PaymentOptions = %v, want exactly %d distinct options: %v", got, len(want), want)
	}
	for opt := range want {
		if !got[opt] {
			t.Errorf("PaymentOptions missing expected option %s; got %v", opt, got)
		}
	}
}

// TestPaymentOptionsColoredRouteHasOneCandidate checks that a genuinely
// coloured (non-gray, non-ferry) route only ever considers its own colour as
// a base, regardless of what else the hand holds.
func TestPaymentOptionsColoredRouteHasOneCandidate(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[1] // a-b, Red, length 2

	hand := handFromColors(map[Color]int{ColorRed: 1, ColorLoco: 1, ColorBlue: 5})
	got := PaymentOptions(r, hand)
	if len(got) != 1 {
		t.Fatalf("colored route: PaymentOptions = %v, want exactly 1 option", got)
	}
	want := fmt.Sprint(map[string]int{"Red": 1, "Locomotive": 1})
	if fmt.Sprint(got[0].Payment) != want {
		t.Errorf("colored route: option = %v, want %s", got[0].Payment, want)
	}
}

// TestPaymentOptionsFerryRequiresMandatoryLocomotives checks rules §8.3: a
// ferry's route.Locos locomotives are mandatory and not substitutable, on
// top of the usual per-colour remainder.
func TestPaymentOptionsFerryRequiresMandatoryLocomotives(t *testing.T) {
	m := testMap(t)
	r := m.RouteByID[6] // e-f, Gray, length 6, ferry, 2 locomotives

	// Not enough locomotives to cover even the mandatory 2.
	insufficient := handFromColors(map[Color]int{ColorRed: 6, ColorLoco: 1})
	if got := PaymentOptions(r, insufficient); len(got) != 0 {
		t.Errorf("ferry with insufficient locomotives: PaymentOptions = %v, want none", got)
	}

	// Exactly enough: the mandatory 2, plus 4 of one colour for the rest.
	sufficient := handFromColors(map[Color]int{ColorRed: 4, ColorLoco: 2})
	got := PaymentOptions(r, sufficient)
	if len(got) != 1 {
		t.Fatalf("ferry: PaymentOptions = %v, want exactly 1 option", got)
	}
	want := fmt.Sprint(map[string]int{"Red": 4, "Locomotive": 2})
	if fmt.Sprint(got[0].Payment) != want {
		t.Errorf("ferry: option = %v, want %s", got[0].Payment, want)
	}
}

// --- Sanity: View delegates to ViewFor after resolving the map -------------

// TestViewDelegatesToViewForAfterResolvingMap checks that Engine.View (which
// resolves the pinned map via e.maps) produces byte-identical output to
// Engine.ViewFor given that same map directly — the split the step brief
// calls for so Step 14's Session.View can resolve the map itself and avoid
// View's context.Background() cache lookup.
func TestViewDelegatesToViewForAfterResolvingMap(t *testing.T) {
	m := testMap(t)
	e := newTestEngine(m, 1)
	gs, ids := newNormalState(m, 2)
	state, err := marshal(gs)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}

	got, err := e.View(state, ids[0])
	if err != nil {
		t.Fatalf("View: unexpected error: %v", err)
	}
	want, err := e.ViewFor(m, state, ids[0])
	if err != nil {
		t.Fatalf("ViewFor: unexpected error: %v", err)
	}

	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal View result: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal ViewFor result: %v", err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("View(...) = %s,\nwant %s (ViewFor over the same map)", gotJSON, wantJSON)
	}
}

// --- Sentinel sweep: representation-independent redaction coverage --------
//
// Every test above is a per-VALUE byte grep (`"Yellow":9`, `"id":105`, the
// literal "surcharge"/"revealed" key names, ...): each one only catches a
// leak someone already thought to look for, and every one is coupled to the
// current rendering. If Color.String() ever emitted enum numerics instead
// of names, `"Yellow":9` would simply stop matching and
// TestViewRedactsOpponentHandAndTickets would go green while the leak still
// existed (m7 in the scoring/redaction review).
//
// ticketIDJSON renders the wire-exact substring a kept/offered ticket's id
// appears as (TTRTicketView.ID, json tag "id" — the only struct field in
// this whole package's wire vocabulary tagged bare "id"), so a match is
// unambiguously "this specific ticket id was rendered somewhere," never an
// incidental numeric coincidence with an unrelated small field (seat index,
// turn number, ...) elsewhere in the payload.
func ticketIDJSON(id int32) []byte {
	return fmt.Appendf(nil, `"id":%d`, id)
}

// sentinelViewer is one (viewer, seat-or-spectator) pair a sentinel-sweep
// scenario checks a built *pb.GameState against.
type sentinelViewer struct {
	seat int32 // -1 for a spectator (occupies no seat)
	user uuid.UUID
}

// sentinelViewers returns every seated viewer in ids (seat == index) plus
// one spectator, the full set of vantage points a sentinel-sweep scenario
// checks.
func sentinelViewers(ids []uuid.UUID) []sentinelViewer {
	out := make([]sentinelViewer, 0, len(ids)+1)
	for i, id := range ids {
		out = append(out, sentinelViewer{seat: int32(i), user: id}) // #nosec G115 -- small test player count
	}
	return append(out, sentinelViewer{seat: -1, user: uuid.New()})
}

// TestViewSentinelSweep assigns each seat a unique, otherwise-unused ticket
// id as its secret (a setup offer or an in-game ticket-draw offer,
// depending on the scenario) and asserts — for every (scenario, viewer)
// pair — that a viewer's marshalled JSON contains a seat's sentinel if and
// only if that scenario says the viewer is entitled to see it. Unlike the
// per-value tests above, this is representation-independent: it does not
// know or care HOW a ticket id is rendered, only THAT it must not appear at
// all outside its rightful owner's view, so it stays valid even if
// TTRTicketView's shape changes, and the same table grows to cover any
// future secret by adding one more scenario.
//
// Covers the previously-untested vectors named in the m7 finding: another
// player's setup_ticket_offer during PHASE_SETUP_TICKETS, the ticket_keep
// pending during PHASE_AWAITING_TICKET_KEEP, and (embedded in that same
// scenario) a seated-but-not-current viewer's view during
// PHASE_AWAITING_TICKET_KEEP.
func TestViewSentinelSweep(t *testing.T) {
	m := testMap(t)

	// ticketSentinels: one real, distinct long-ticket id per seat (0, 1, 2)
	// — real because an unknown id is silently skipped by
	// offeredTicketViews/ticketsView (defensive, see their doc comments) and
	// would never be rendered at all, which would make the "not present"
	// half of every assertion trivially true for the wrong reason. 101-103
	// are otherwise unused by any scenario below, and testMap()'s other
	// numeric fields (seat indices, turn_no, trains/stations left, map
	// version) never reach into the 100s, so a match is unambiguous.
	ticketSentinels := []int32{101, 102, 103}

	scenarios := []struct {
		name  string
		build func() (*pb.GameState, []uuid.UUID)
		// wantVisibleTo maps a viewer's seat (-1 for spectator) to the one
		// ticket sentinel that viewer's own view is entitled to show, or 0
		// if none.
		wantVisibleTo func(viewerSeat int32) int32
	}{
		{
			name: "another player's setup_ticket_offer during PHASE_SETUP_TICKETS",
			build: func() (*pb.GameState, []uuid.UUID) {
				gs, ids := newNormalState(m, 3)
				gs.Phase = pb.Phase_PHASE_SETUP_TICKETS
				for i, p := range gs.Players {
					p.SetupTicketOffer = []int32{ticketSentinels[i]}
				}
				return gs, ids
			},
			wantVisibleTo: func(viewerSeat int32) int32 {
				if viewerSeat < 0 {
					return 0 // spectator: setup_tickets pending is never populated
				}
				return ticketSentinels[viewerSeat] // every seat sees only its own offer
			},
		},
		{
			name: "ticket_keep pending during PHASE_AWAITING_TICKET_KEEP",
			build: func() (*pb.GameState, []uuid.UUID) {
				gs, ids := newNormalState(m, 3)
				gs.Phase = pb.Phase_PHASE_AWAITING_TICKET_KEEP
				gs.CurrentSeat = 0
				gs.PendingTicketDraw = &pb.PendingTicketDraw{OfferedTicketIds: []int32{ticketSentinels[0]}}
				return gs, ids
			},
			wantVisibleTo: func(viewerSeat int32) int32 {
				if viewerSeat == 0 {
					return ticketSentinels[0] // the deciding (current) player
				}
				return 0 // seat 1/2 (seated, not current) and the spectator: none
			},
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			gs, ids := sc.build()

			for _, v := range sentinelViewers(ids) {
				raw := viewJSON(t, m, gs, v.user)
				wantVisible := sc.wantVisibleTo(v.seat)

				for _, sentinel := range ticketSentinels {
					present := bytes.Contains(raw, ticketIDJSON(sentinel))
					wantPresent := sentinel == wantVisible
					if present != wantPresent {
						t.Errorf("viewer seat %d: ticket sentinel %d present = %v, want %v: %s",
							v.seat, sentinel, present, wantPresent, raw)
					}
				}
			}
		})
	}
}
