package ttr

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/uuid"
)

// supportedSchemaVersion is the only schema_version ParseMap accepts.
const supportedSchemaVersion = 1

// Default ticket-deck composition when a map document omits
// rules.long_tickets / rules.regular_tickets: the Europe base-game counts
// (rules §3.2).
const (
	defaultLongTickets    = 6
	defaultRegularTickets = 40
)

// ValidationError is one problem found while parsing a map document, located
// by a JSON-path-like string rooted at "$" (e.g. "$.rules.routes[3].length").
type ValidationError struct {
	Path    string
	Message string
}

// Error renders a ValidationError as "<path>: <message>".
func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// ValidationErrors is every problem found while parsing a map document. It
// implements error so ParseMap can return the whole list through a single
// error return value.
type ValidationErrors []ValidationError

// Error renders the full list of validation problems, one per line.
func (es ValidationErrors) Error() string {
	if len(es) == 0 {
		return "no validation errors"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "%d map validation error(s):", len(es))
	for _, e := range es {
		fmt.Fprintf(&b, "\n  - %s", e.Error())
	}
	return b.String()
}

// ParseMap decodes and fully validates a map document per rules §4 and the
// layout contract (plan Q6). On success it returns a *Map with every derived
// index populated. On any problem it returns nil and a ValidationErrors
// listing every problem found — it never returns only the first.
func ParseMap(doc []byte) (*Map, error) {
	m, err := decodeMapDoc(doc)
	if err != nil {
		return nil, err
	}
	applyRuleDefaults(m)

	var errs ValidationErrors
	if m.SchemaVersion != supportedSchemaVersion {
		errs = append(errs, ValidationError{
			Path:    "$.schema_version",
			Message: fmt.Sprintf("unsupported schema_version %d, expected %d", m.SchemaVersion, supportedSchemaVersion),
		})
	}

	cityIdx, cerrs := validateCities(m)
	errs = append(errs, cerrs...)

	routeIdx, rerrs := validateRoutes(m, cityIdx)
	errs = append(errs, rerrs...)
	errs = append(errs, validatePairs(m, routeIdx)...)

	errs = append(errs, validateTickets(m, cityIdx)...)
	errs = append(errs, validatePlayerBounds(m)...)
	errs = append(errs, validateLayout(m)...)

	if len(errs) > 0 {
		return nil, errs
	}

	buildIndices(m)
	return m, nil
}

// DocSHA256 returns the hex-encoded sha256 digest of the raw document bytes,
// used to detect drift between an embedded seed file and its DB row.
func DocSHA256(doc []byte) string {
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:])
}

// decodeMapDoc JSON-decodes doc into a fresh Map, rejecting unknown fields so
// typos in a hand-authored map document fail loudly instead of being
// silently ignored.
func decodeMapDoc(doc []byte) (*Map, error) {
	var m Map
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, ValidationErrors{{Path: "$", Message: err.Error()}}
	}
	return &m, nil
}

// applyRuleDefaults fills in map-level ticket-count defaults (Europe's 6
// long + 40 regular) when the document doesn't declare its own.
func applyRuleDefaults(m *Map) {
	if m.Rules.LongTickets == 0 {
		m.Rules.LongTickets = defaultLongTickets
	}
	if m.Rules.RegularTickets == 0 {
		m.Rules.RegularTickets = defaultRegularTickets
	}
}

// validateCities checks city id uniqueness and returns an id -> slice-index
// lookup used by route/ticket/layout validation.
func validateCities(m *Map) (map[string]int, ValidationErrors) {
	idx := make(map[string]int, len(m.Rules.Cities))
	var errs ValidationErrors
	for i := range m.Rules.Cities {
		c := &m.Rules.Cities[i]
		if _, dup := idx[c.ID]; dup {
			errs = append(errs, ValidationError{
				Path:    fmt.Sprintf("$.rules.cities[%d].id", i),
				Message: fmt.Sprintf("duplicate city id %q", c.ID),
			})
			continue
		}
		idx[c.ID] = i
	}
	return idx, errs
}

// validateRoutes checks route id uniqueness, city references, and per-route
// attribute constraints (color/length/locos/ferry). It sets Route.Color as a
// side effect and returns an id -> slice-index lookup for pair validation.
func validateRoutes(m *Map, cityIdx map[string]int) (map[int32]int, ValidationErrors) {
	idx := make(map[int32]int, len(m.Rules.Routes))
	var errs ValidationErrors
	for i := range m.Rules.Routes {
		r := &m.Rules.Routes[i]
		path := fmt.Sprintf("$.rules.routes[%d]", i)
		errs = append(errs, validateRouteIdentity(r, i, path, idx, cityIdx)...)
		errs = append(errs, validateRouteAttributes(r, path)...)
	}
	return idx, errs
}

func validateRouteIdentity(r *Route, i int, path string, idx map[int32]int, cityIdx map[string]int) ValidationErrors {
	var errs ValidationErrors
	// Route id 0 is reserved: scoring.go's noBorrow uses it to mean "this
	// station option borrows nothing" (bestStationAssignment/
	// evaluateAssignment). A route legitimately carrying id 0 would silently
	// be treated as a no-op borrow candidate instead of a real, claimable
	// route (M1 in the scoring/redaction review) — reject it here rather
	// than let it reach scoring at all.
	if r.ID < 1 {
		errs = append(errs, ValidationError{Path: path + ".id", Message: "route id must be >= 1 (0 is reserved)"})
	}
	if _, dup := idx[r.ID]; dup {
		errs = append(errs, ValidationError{Path: path + ".id", Message: fmt.Sprintf("duplicate route id %d", r.ID)})
	} else {
		idx[r.ID] = i
	}
	if r.A == r.B {
		errs = append(errs, ValidationError{Path: path + ".b", Message: "must reference a different city than a"})
	}
	if _, ok := cityIdx[r.A]; !ok {
		errs = append(errs, ValidationError{Path: path + ".a", Message: fmt.Sprintf("unknown city id %q", r.A)})
	}
	if _, ok := cityIdx[r.B]; !ok {
		errs = append(errs, ValidationError{Path: path + ".b", Message: fmt.Sprintf("unknown city id %q", r.B)})
	}
	return errs
}

func validateRouteAttributes(r *Route, path string) ValidationErrors {
	var errs ValidationErrors

	color, ok := ParseColor(r.ColorName)
	if !ok || !color.IsRouteColor() {
		errs = append(errs, ValidationError{Path: path + ".color", Message: fmt.Sprintf("invalid route color %q", r.ColorName)})
	} else {
		r.Color = color
	}

	if _, validLength := RoutePoints[r.Length]; !validLength {
		errs = append(errs, ValidationError{Path: path + ".length", Message: fmt.Sprintf("length %d has no defined point value", r.Length)})
	}

	if r.Locos < 0 || r.Locos > r.Length {
		errs = append(errs, ValidationError{Path: path + ".locos", Message: "must be between 0 and length"})
	}

	if ok && r.Locos >= 1 && color != ColorGray {
		errs = append(errs, ValidationError{Path: path + ".color", Message: "ferries (locos >= 1) must be Gray"})
	}

	return errs
}

// validatePairs checks double-route symmetry (rules §8.5): routes[pair].pair
// must point back, both routes must have equal length, and both must connect
// the same unordered city pair.
func validatePairs(m *Map, routeIdx map[int32]int) ValidationErrors {
	var errs ValidationErrors
	for i := range m.Rules.Routes {
		r := &m.Rules.Routes[i]
		if r.Pair == nil {
			continue
		}
		path := fmt.Sprintf("$.rules.routes[%d].pair", i)
		pid := *r.Pair

		if pid == r.ID {
			errs = append(errs, ValidationError{Path: path, Message: "cannot pair a route with itself"})
			continue
		}
		j, ok := routeIdx[pid]
		if !ok {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("references unknown route id %d", pid)})
			continue
		}
		other := &m.Rules.Routes[j]
		if other.Pair == nil || *other.Pair != r.ID {
			errs = append(errs, ValidationError{Path: path, Message: "pair is not symmetric"})
			continue
		}
		if other.Length != r.Length {
			errs = append(errs, ValidationError{Path: path, Message: "paired routes must have equal length"})
		}
		if !sameCityPair(r.A, r.B, other.A, other.B) {
			errs = append(errs, ValidationError{Path: path, Message: "paired routes must connect the same city pair"})
		}
	}
	return errs
}

func sameCityPair(a1, b1, a2, b2 string) bool {
	return (a1 == a2 && b1 == b2) || (a1 == b2 && b1 == a2)
}

// validateTickets checks ticket id uniqueness, city references, and the
// long/regular ticket counts declared by rules.long_tickets /
// rules.regular_tickets (defaulted to 6/40 by applyRuleDefaults).
func validateTickets(m *Map, cityIdx map[string]int) ValidationErrors {
	var errs ValidationErrors
	idx := make(map[int32]int, len(m.Rules.Tickets))
	var longCount, regularCount int

	for i := range m.Rules.Tickets {
		t := &m.Rules.Tickets[i]
		path := fmt.Sprintf("$.rules.tickets[%d]", i)
		if _, dup := idx[t.ID]; dup {
			errs = append(errs, ValidationError{Path: path + ".id", Message: fmt.Sprintf("duplicate ticket id %d", t.ID)})
		} else {
			idx[t.ID] = i
		}
		if _, ok := cityIdx[t.A]; !ok {
			errs = append(errs, ValidationError{Path: path + ".a", Message: fmt.Sprintf("unknown city id %q", t.A)})
		}
		if _, ok := cityIdx[t.B]; !ok {
			errs = append(errs, ValidationError{Path: path + ".b", Message: fmt.Sprintf("unknown city id %q", t.B)})
		}
		if t.Long {
			longCount++
		} else {
			regularCount++
		}
	}

	if longCount != m.Rules.LongTickets {
		errs = append(errs, ValidationError{
			Path:    "$.rules.tickets",
			Message: fmt.Sprintf("expected %d long tickets, got %d", m.Rules.LongTickets, longCount),
		})
	}
	if regularCount != m.Rules.RegularTickets {
		errs = append(errs, ValidationError{
			Path:    "$.rules.tickets",
			Message: fmt.Sprintf("expected %d regular tickets, got %d", m.Rules.RegularTickets, regularCount),
		})
	}
	total := m.Rules.LongTickets + m.Rules.RegularTickets
	if len(m.Rules.Tickets) != total {
		errs = append(errs, ValidationError{
			Path:    "$.rules.tickets",
			Message: fmt.Sprintf("expected %d tickets total, got %d", total, len(m.Rules.Tickets)),
		})
	}

	return errs
}

// maxTrainsPerPlayer bounds rules.trains_per_player at 64. This is not a
// rules constraint (the physical Europe game ships 45) but a correctness
// requirement of longestTrail's DFS (scoring.go): once a player owns more
// than 20 routes it memoizes on a uint64 used-edge bitmask, which only has
// 64 bits. A player can never claim more routes than they have trains for,
// so capping trains_per_player here caps every claimed-route subgraph
// longestTrail ever has to walk, independent of buildTrailGraph's own
// (unbounded-but-correct) traversal. See the C1 finding in the scoring/
// redaction review: an admin-authored map with trains_per_player > 64 could
// otherwise crash the whole server process with a fatal stack overflow.
const maxTrainsPerPlayer = 64

// validatePlayerBounds enforces the digital engine's supported seat range:
// no fewer than 2, no more than 5 (rules §3.1); and trains_per_player must
// be a positive count no greater than maxTrainsPerPlayer.
func validatePlayerBounds(m *Map) ValidationErrors {
	var errs ValidationErrors
	if m.Rules.Players.Min < 2 {
		errs = append(errs, ValidationError{Path: "$.rules.players.min", Message: "must be >= 2"})
	}
	if m.Rules.Players.Max > 5 {
		errs = append(errs, ValidationError{Path: "$.rules.players.max", Message: "must be <= 5"})
	}
	if m.Rules.TrainsPerPlayer <= 0 || m.Rules.TrainsPerPlayer > maxTrainsPerPlayer {
		errs = append(errs, ValidationError{
			Path:    "$.rules.trains_per_player",
			Message: fmt.Sprintf("must be > 0 and <= %d", maxTrainsPerPlayer),
		})
	}
	return errs
}

// validateLayout checks that the layout half of the document fully covers
// every city and route declared in rules, with in-bounds coordinates.
func validateLayout(m *Map) ValidationErrors {
	var errs ValidationErrors
	errs = append(errs, validateLayoutCities(m)...)
	errs = append(errs, validateLayoutRoutes(m)...)
	errs = append(errs, validateLayoutBackground(m)...)
	return errs
}

func validateLayoutCities(m *Map) ValidationErrors {
	var errs ValidationErrors
	for _, c := range m.Rules.Cities {
		lc, ok := m.Layout.Cities[c.ID]
		if !ok {
			errs = append(errs, ValidationError{
				Path:    "$.layout.cities." + c.ID,
				Message: "missing layout entry for city",
			})
			continue
		}
		errs = append(errs, validateUnitInterval(fmt.Sprintf("$.layout.cities.%s.x", c.ID), lc.X)...)
		errs = append(errs, validateUnitInterval(fmt.Sprintf("$.layout.cities.%s.y", c.ID), lc.Y)...)
	}
	return errs
}

func validateLayoutRoutes(m *Map) ValidationErrors {
	var errs ValidationErrors
	for _, r := range m.Rules.Routes {
		key := strconv.Itoa(int(r.ID))
		lr, ok := m.Layout.Routes[key]
		if !ok {
			errs = append(errs, ValidationError{
				Path:    "$.layout.routes." + key,
				Message: "missing layout entry for route",
			})
			continue
		}
		if len(lr.Slots) != r.Length {
			errs = append(errs, ValidationError{
				Path:    fmt.Sprintf("$.layout.routes.%s.slots", key),
				Message: fmt.Sprintf("expected %d slots (route length), got %d", r.Length, len(lr.Slots)),
			})
		}
		for si, s := range lr.Slots {
			errs = append(errs, validateUnitInterval(fmt.Sprintf("$.layout.routes.%s.slots[%d].x", key, si), s.X)...)
			errs = append(errs, validateUnitInterval(fmt.Sprintf("$.layout.routes.%s.slots[%d].y", key, si), s.Y)...)
		}
		if lr.Offset < -0.2 || lr.Offset > 0.2 {
			errs = append(errs, ValidationError{
				Path:    fmt.Sprintf("$.layout.routes.%s.offset", key),
				Message: fmt.Sprintf("must be in [-0.2, 0.2], got %v", lr.Offset),
			})
		}
		if lr.Bend < -0.5 || lr.Bend > 0.5 {
			errs = append(errs, ValidationError{
				Path:    fmt.Sprintf("$.layout.routes.%s.bend", key),
				Message: fmt.Sprintf("must be in [-0.5, 0.5], got %v", lr.Bend),
			})
		}
	}
	return errs
}

func validateLayoutBackground(m *Map) ValidationErrors {
	var errs ValidationErrors
	if m.Layout.Background.AssetID != nil {
		if _, err := uuid.Parse(*m.Layout.Background.AssetID); err != nil {
			errs = append(errs, ValidationError{Path: "$.layout.background.asset_id", Message: "must be a valid UUID"})
		}
	}
	return errs
}

// validateUnitInterval checks that v is within the normalized [0,1]
// coordinate space every layout position is expressed in.
func validateUnitInterval(path string, v float64) ValidationErrors {
	if v < 0 || v > 1 {
		return ValidationErrors{{Path: path, Message: fmt.Sprintf("must be in [0,1], got %v", v)}}
	}
	return nil
}

// buildIndices populates every derived index on m once the document has
// passed full validation.
func buildIndices(m *Map) {
	m.CityByID = make(map[string]*City, len(m.Rules.Cities))
	for i := range m.Rules.Cities {
		c := &m.Rules.Cities[i]
		m.CityByID[c.ID] = c
	}

	m.RouteByID = make(map[int32]*Route, len(m.Rules.Routes))
	m.RoutesByCity = make(map[string][]int32, len(m.Rules.Cities))
	for i := range m.Rules.Routes {
		r := &m.Rules.Routes[i]
		m.RouteByID[r.ID] = r
		m.RoutesByCity[r.A] = append(m.RoutesByCity[r.A], r.ID)
		m.RoutesByCity[r.B] = append(m.RoutesByCity[r.B], r.ID)
	}

	m.TicketByID = make(map[int32]*Ticket, len(m.Rules.Tickets))
	for i := range m.Rules.Tickets {
		t := &m.Rules.Tickets[i]
		m.TicketByID[t.ID] = t
		if t.Long {
			m.LongTicketIDs = append(m.LongTicketIDs, t.ID)
		} else {
			m.RegularTicketIDs = append(m.RegularTicketIDs, t.ID)
		}
	}
}
