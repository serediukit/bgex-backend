package ttr

import "fmt"

// Map is a fully parsed and validated TTR board definition: the physical
// components (Rules) plus the visual layout used to render them (Layout).
// Construct one via ParseMap — the zero value has no derived indices.
type Map struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	Rules         Rules  `json:"rules"`
	Layout        Layout `json:"layout"`

	// Derived indices built by ParseMap. Not serialized.
	CityByID     map[string]*City   `json:"-"`
	RouteByID    map[int32]*Route   `json:"-"`
	TicketByID   map[int32]*Ticket  `json:"-"`
	RoutesByCity map[string][]int32 `json:"-"`
	// LongTicketIDs / RegularTicketIDs partition Rules.Tickets by the Long
	// flag, in document order.
	LongTicketIDs    []int32 `json:"-"`
	RegularTicketIDs []int32 `json:"-"`
}

// PlayerBounds is the inclusive [Min, Max] seat count a map supports.
type PlayerBounds struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Rules is the "rules" half of a map document (§4): the physical components
// of the board, independent of how they are drawn.
type Rules struct {
	Players           PlayerBounds `json:"players"`
	TrainsPerPlayer   int          `json:"trains_per_player"`
	StationsPerPlayer int          `json:"stations_per_player"`
	// LongTickets / RegularTickets declare how many of Tickets must have
	// Long == true / false respectively. Both are map-specific data, not an
	// engine constant: they describe this map's physical ticket deck, the
	// same way Cities/Routes/Tickets themselves do. Zero (i.e. omitted from
	// the document) defaults to the Europe base-game counts of 6 long + 40
	// regular (rules §3.2) so existing Europe-shaped documents don't need
	// to specify them.
	LongTickets    int      `json:"long_tickets,omitempty"`
	RegularTickets int      `json:"regular_tickets,omitempty"`
	Cities         []City   `json:"cities"`
	Routes         []Route  `json:"routes"`
	Tickets        []Ticket `json:"tickets"`
}

// City is a single node on the board graph.
type City struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Route is a single claimable edge between two cities (rules §3.3, §8).
type Route struct {
	ID        int32  `json:"id"`
	A         string `json:"a"`
	B         string `json:"b"`
	ColorName string `json:"color"`
	Length    int    `json:"length"`
	Tunnel    bool   `json:"tunnel"`
	Locos     int    `json:"locos"`
	// Pair is the route id of this route's double-route sibling (rules
	// §8.5), or nil if this route has no sibling.
	Pair *int32 `json:"pair"`

	// Color is ColorName parsed to a Color, set by ParseMap. Not
	// serialized — ColorName is the wire representation.
	Color Color `json:"-"`
}

// IsFerry reports whether r is a ferry route (rules §8.3): a gray route
// with one or more locomotive symbols printed on it.
func (r *Route) IsFerry() bool { return r.Locos > 0 }

// Ticket is a destination ticket (rules §9, §13.2).
type Ticket struct {
	ID     int32  `json:"id"`
	A      string `json:"a"`
	B      string `json:"b"`
	Points int    `json:"points"`
	Long   bool   `json:"long"`
}

// RoutePoints is the route scoring table (rules §12): points awarded per
// route length. Lengths absent from this table (5, 7, …) have no defined
// value — PointsForLength fails loudly instead of interpolating.
var RoutePoints = map[int]int{
	1: 1,
	2: 2,
	3: 4,
	4: 7,
	6: 15,
	8: 21,
}

// PointsForLength returns the route points for a claimed route of length l.
// It returns ErrInvalidRouteLength for any length not in RoutePoints (rules
// §12 — "fail loudly rather than interpolating").
func PointsForLength(l int) (int, error) {
	pts, ok := RoutePoints[l]
	if !ok {
		return 0, fmt.Errorf("%w: %d", ErrInvalidRouteLength, l)
	}
	return pts, nil
}

// ViewBox is the SVG-style coordinate space the layout's normalized (0..1)
// coordinates are drawn against.
type ViewBox struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Background describes the board's background image, if any.
type Background struct {
	// AssetID is a UUID string referencing a ttr.map_assets row, or nil for
	// no background image. Existence is checked by the handler that serves
	// it, not by ParseMap.
	AssetID *string `json:"asset_id"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
}

// LayoutCity is a city's normalized (0..1) position on the board.
type LayoutCity struct {
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	LabelAnchor string  `json:"label_anchor,omitempty"`
}

// Slot is one normalized (0..1) train-car space drawn along a route.
type Slot struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Angle float64 `json:"angle"`
}

// LayoutRoute is a route's drawn train-car slots. len(Slots) must equal the
// corresponding Route.Length.
type LayoutRoute struct {
	Slots []Slot `json:"slots"`
}

// SlotStyle is the shared normalized size/shape used to draw every slot.
type SlotStyle struct {
	Width        float64 `json:"width"`
	Height       float64 `json:"height"`
	CornerRadius float64 `json:"corner_radius"`
}

// Layout is the "layout" half of a map document (plan Q6): everything
// needed to render the board, keyed to Rules by city id / route id.
type Layout struct {
	ViewBox    ViewBox    `json:"view_box"`
	Background Background `json:"background"`
	// Cities is keyed by City.ID.
	Cities map[string]LayoutCity `json:"cities"`
	// Routes is keyed by the decimal string form of Route.ID (JSON object
	// keys must be strings).
	Routes map[string]LayoutRoute `json:"routes"`
	Slot   SlotStyle              `json:"slot"`
}
