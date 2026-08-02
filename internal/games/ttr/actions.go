package ttr

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/serediukit/bgex-backend/internal/games/engine"
)

// Action type names — the value of engine.Action.Type for TTR.
const (
	ActionDrawCard        = "draw_card"
	ActionClaimRoute      = "claim_route"
	ActionBuildStation    = "build_station"
	ActionDrawTickets     = "draw_tickets"
	ActionResolveDecision = "resolve_decision"
	ActionResign          = "resign"
)

// resolve_decision "kind" values — see ResolveDecisionPayload.
const (
	// DecisionKindSetupTickets resolves the §5.7 simultaneous setup keep.
	DecisionKindSetupTickets = "setup_tickets"
	// DecisionKindTicketKeep resolves the §9 in-game ticket-draw keep.
	DecisionKindTicketKeep = "ticket_keep"
	// DecisionKindTunnel resolves the §8.4 tunnel surcharge decision.
	DecisionKindTunnel = "tunnel"
	// DecisionKindEndDraw resolves whether a §7.1 second card draw follows
	// the first.
	DecisionKindEndDraw = "end_draw"
)

// DrawCardPayload is the payload for ActionDrawCard (rules §7): Source is
// "face_up" or "deck"; Slot indexes the face-up row (0..4) and is ignored
// for a deck draw.
type DrawCardPayload struct {
	Source string `json:"source"`
	Slot   int    `json:"slot"`
}

// ClaimRoutePayload is the payload for ActionClaimRoute (rules §8). Payment
// maps a canonical color name (ParseColor) to the count spent from hand.
type ClaimRoutePayload struct {
	RouteID int32          `json:"route_id"`
	Payment map[string]int `json:"payment"`
}

// BuildStationPayload is the payload for ActionBuildStation (rules §10).
// Payment maps a canonical color name to the count spent from hand.
type BuildStationPayload struct {
	CityID  string         `json:"city_id"`
	Payment map[string]int `json:"payment"`
}

// ResolveDecisionPayload is the payload for ActionResolveDecision. Kind
// selects which pending decision it answers and which of the other fields
// apply:
//   - DecisionKindSetupTickets / DecisionKindTicketKeep: KeepTicketIDs.
//   - DecisionKindTunnel: Accept, and ExtraPayment when Accept is true.
//   - DecisionKindEndDraw: Accept (whether to take a second card).
type ResolveDecisionPayload struct {
	Kind          string         `json:"kind"`
	KeepTicketIDs []int32        `json:"keep_ticket_ids,omitempty"`
	Accept        *bool          `json:"accept,omitempty"`
	ExtraPayment  map[string]int `json:"extra_payment,omitempty"`
}

// decodePayload strictly decodes raw into a T, rejecting unknown fields so a
// malformed client payload fails loudly instead of silently ignoring typos.
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

// paymentToColors validates and converts a wire payment map (color name ->
// count) into a Color -> count map, rejecting unknown color names,
// non-card colors (Gray), and non-positive counts.
func paymentToColors(p map[string]int) (map[Color]int, error) {
	out := make(map[Color]int, len(p))
	for name, n := range p {
		if n <= 0 {
			return nil, fmt.Errorf("%w: payment count for %q must be positive, got %d", engine.ErrIllegalAction, name, n)
		}
		c, ok := ParseColor(name)
		if !ok || !c.IsCardColor() {
			return nil, fmt.Errorf("%w: %q is not a valid train card color", engine.ErrIllegalAction, name)
		}
		out[c] = n
	}
	return out, nil
}
