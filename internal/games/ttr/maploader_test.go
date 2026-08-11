package ttr

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// validMapDoc returns a minimal, fully valid map document as a generic
// map[string]any so individual test cases can mutate arbitrary paths
// (including injecting unknown fields) before marshaling and feeding it to
// ParseMap.
//
// Board: 3 cities (a, b, c) and 4 routes:
//
//	1: a-b Red   length 2
//	2: b-c Gray  length 3
//	3: a-c Blue  length 2
//	4: a-b White length 2  (same city pair + length as route 1, so pair
//	                        tests can pair it with route 1 without touching
//	                        anything else)
//
// None of the routes are paired by default, so the base document has no
// double-route constraints active until a test case opts in.
func validMapDoc() map[string]any {
	return map[string]any{
		"schema_version": 1,
		"name":           "Test Map",
		"rules": map[string]any{
			"players":             map[string]any{"min": 2, "max": 5},
			"trains_per_player":   45,
			"stations_per_player": 3,
			"cities": []any{
				map[string]any{"id": "a", "name": "A"},
				map[string]any{"id": "b", "name": "B"},
				map[string]any{"id": "c", "name": "C"},
			},
			"routes": []any{
				map[string]any{"id": 1, "a": "a", "b": "b", "color": "Red", "length": 2, "tunnel": false, "locos": 0, "pair": nil},
				map[string]any{"id": 2, "a": "b", "b": "c", "color": "Gray", "length": 3, "tunnel": false, "locos": 0, "pair": nil},
				map[string]any{"id": 3, "a": "a", "b": "c", "color": "Blue", "length": 2, "tunnel": false, "locos": 0, "pair": nil},
				map[string]any{"id": 4, "a": "a", "b": "b", "color": "White", "length": 2, "tunnel": false, "locos": 0, "pair": nil},
			},
			"tickets": genTickets(6, 40),
		},
		"layout": map[string]any{
			"view_box":   map[string]any{"width": 1000, "height": 800},
			"background": map[string]any{"asset_id": nil, "width": 1000, "height": 800},
			"cities": map[string]any{
				"a": map[string]any{"x": 0.1, "y": 0.1, "label_anchor": "n"},
				"b": map[string]any{"x": 0.2, "y": 0.2, "label_anchor": "n"},
				"c": map[string]any{"x": 0.3, "y": 0.3, "label_anchor": "n"},
			},
			"routes": map[string]any{
				"1": map[string]any{"slots": []any{
					map[string]any{"x": 0.10, "y": 0.10, "angle": 0},
					map[string]any{"x": 0.11, "y": 0.11, "angle": 0},
				}},
				"2": map[string]any{"slots": []any{
					map[string]any{"x": 0.20, "y": 0.20, "angle": 0},
					map[string]any{"x": 0.21, "y": 0.21, "angle": 0},
					map[string]any{"x": 0.22, "y": 0.22, "angle": 0},
				}},
				"3": map[string]any{"slots": []any{
					map[string]any{"x": 0.30, "y": 0.30, "angle": 0},
					map[string]any{"x": 0.31, "y": 0.31, "angle": 0},
				}},
				"4": map[string]any{"slots": []any{
					map[string]any{"x": 0.40, "y": 0.40, "angle": 0},
					map[string]any{"x": 0.41, "y": 0.41, "angle": 0},
				}},
			},
			"slot": map[string]any{"width": 0.02, "height": 0.01, "corner_radius": 0.002},
		},
	}
}

// genTickets builds `long` long tickets followed by `regular` regular
// tickets with sequential ids starting at 1, all referencing cities that
// exist in validMapDoc.
func genTickets(long, regular int) []any {
	tickets := make([]any, 0, long+regular)
	id := 1
	for range long {
		tickets = append(tickets, map[string]any{"id": id, "a": "a", "b": "c", "points": 20, "long": true})
		id++
	}
	for range regular {
		tickets = append(tickets, map[string]any{"id": id, "a": "a", "b": "b", "points": 5, "long": false})
		id++
	}
	return tickets
}

// cloneDoc deep-copies doc via a JSON round trip so each test case mutates
// its own independent copy of the base document.
func cloneDoc(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal base doc: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal base doc: %v", err)
	}
	return out
}

// must asserts v is of type T, panicking (failing the test) otherwise. It
// exists so test fixture navigation reads as plain field access instead of
// repeating checked type assertions everywhere.
func must[T any](v any) T {
	t, ok := v.(T)
	if !ok {
		panic(fmt.Sprintf("type assertion failed: got %T, want %T", v, *new(T)))
	}
	return t
}

func rulesOf(doc map[string]any) map[string]any  { return must[map[string]any](doc["rules"]) }
func layoutOf(doc map[string]any) map[string]any { return must[map[string]any](doc["layout"]) }
func citiesOf(doc map[string]any) []any          { return must[[]any](rulesOf(doc)["cities"]) }
func routesOf(doc map[string]any) []any          { return must[[]any](rulesOf(doc)["routes"]) }
func ticketsOf(doc map[string]any) []any         { return must[[]any](rulesOf(doc)["tickets"]) }

// routeAt returns the i'th route in doc as a mutable map.
func routeAt(doc map[string]any, i int) map[string]any { return must[map[string]any](routesOf(doc)[i]) }

// ticketAt returns the i'th ticket in doc as a mutable map.
func ticketAt(doc map[string]any, i int) map[string]any {
	return must[map[string]any](ticketsOf(doc)[i])
}

func hasErrorAtPath(errs ValidationErrors, path string) bool {
	for _, e := range errs {
		if e.Path == path {
			return true
		}
	}
	return false
}

func TestParseMap(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(doc map[string]any)
		wantErrPath string // asserted against ParseMap's ValidationErrors when wantOK is false
		wantOK      bool
	}{
		{
			name:   "valid minimal doc",
			mutate: func(map[string]any) {},
			wantOK: true,
		},
		{
			name: "schema_version 2",
			mutate: func(doc map[string]any) {
				doc["schema_version"] = 2
			},
			wantErrPath: "$.schema_version",
		},
		{
			name: "duplicate city id",
			mutate: func(doc map[string]any) {
				must[map[string]any](citiesOf(doc)[1])["id"] = "a"
			},
			wantErrPath: "$.rules.cities[1].id",
		},
		{
			name: "duplicate route id",
			mutate: func(doc map[string]any) {
				routeAt(doc, 1)["id"] = 1
			},
			wantErrPath: "$.rules.routes[1].id",
		},
		{
			// M1 in the scoring/redaction review: route id 0 collides with
			// scoring.go's noBorrow sentinel ("this station option borrows
			// nothing"), which would silently turn a real route into a
			// no-op in bestStationAssignment's optimizer.
			name: "route id 0 collides with the noBorrow sentinel",
			mutate: func(doc map[string]any) {
				routeAt(doc, 0)["id"] = 0
			},
			wantErrPath: "$.rules.routes[0].id",
		},
		{
			name: "duplicate ticket id",
			mutate: func(doc map[string]any) {
				ticketAt(doc, 1)["id"] = ticketAt(doc, 0)["id"]
			},
			wantErrPath: "$.rules.tickets[1].id",
		},
		{
			name: "route referencing an unknown city",
			mutate: func(doc map[string]any) {
				routeAt(doc, 0)["a"] = "nowhere"
			},
			wantErrPath: "$.rules.routes[0].a",
		},
		{
			name: "route a == b",
			mutate: func(doc map[string]any) {
				r := routeAt(doc, 0)
				r["b"] = r["a"]
			},
			wantErrPath: "$.rules.routes[0].b",
		},
		{
			name: "route color Locomotive",
			mutate: func(doc map[string]any) {
				routeAt(doc, 0)["color"] = "Locomotive"
			},
			wantErrPath: "$.rules.routes[0].color",
		},
		{
			name: "route length 5",
			mutate: func(doc map[string]any) {
				routeAt(doc, 0)["length"] = 5
			},
			wantErrPath: "$.rules.routes[0].length",
		},
		{
			name: "route length 7",
			mutate: func(doc map[string]any) {
				routeAt(doc, 1)["length"] = 7
			},
			wantErrPath: "$.rules.routes[1].length",
		},
		{
			name: "locos > length",
			mutate: func(doc map[string]any) {
				routeAt(doc, 0)["locos"] = 5
			},
			wantErrPath: "$.rules.routes[0].locos",
		},
		{
			name: `locos 2 with color "Black" is a non-gray ferry`,
			mutate: func(doc map[string]any) {
				r := routeAt(doc, 0)
				r["locos"] = 2
				r["color"] = "Black"
			},
			wantErrPath: "$.rules.routes[0].color",
		},
		{
			name: "asymmetric pair",
			mutate: func(doc map[string]any) {
				routeAt(doc, 0)["pair"] = 4
			},
			wantErrPath: "$.rules.routes[0].pair",
		},
		{
			name: "pair with unequal length",
			mutate: func(doc map[string]any) {
				routeAt(doc, 0)["pair"] = 4
				r4 := routeAt(doc, 3)
				r4["pair"] = 1
				r4["length"] = 3
			},
			wantErrPath: "$.rules.routes[0].pair",
		},
		{
			name: "pair pointing to a route with a different city pair",
			mutate: func(doc map[string]any) {
				routeAt(doc, 2)["pair"] = 4
				routeAt(doc, 3)["pair"] = 3
			},
			wantErrPath: "$.rules.routes[2].pair",
		},
		{
			name: "pair == id",
			mutate: func(doc map[string]any) {
				routeAt(doc, 0)["pair"] = 1
			},
			wantErrPath: "$.rules.routes[0].pair",
		},
		{
			name: "5 long tickets (not 6)",
			mutate: func(doc map[string]any) {
				rulesOf(doc)["tickets"] = genTickets(5, 40)
			},
			wantErrPath: "$.rules.tickets",
		},
		{
			name: "39 regular tickets",
			mutate: func(doc map[string]any) {
				rulesOf(doc)["tickets"] = genTickets(6, 39)
			},
			wantErrPath: "$.rules.tickets",
		},
		{
			name: "45 tickets total (uneven split)",
			mutate: func(doc map[string]any) {
				rulesOf(doc)["tickets"] = genTickets(7, 38)
			},
			wantErrPath: "$.rules.tickets",
		},
		{
			name: "players.min 1",
			mutate: func(doc map[string]any) {
				must[map[string]any](rulesOf(doc)["players"])["min"] = 1
			},
			wantErrPath: "$.rules.players.min",
		},
		{
			name: "players.max 6",
			mutate: func(doc map[string]any) {
				must[map[string]any](rulesOf(doc)["players"])["max"] = 6
			},
			wantErrPath: "$.rules.players.max",
		},
		{
			// C1 in the scoring/redaction review: trains_per_player has no
			// upper bound anywhere else, but longestTrail's DFS (scoring.go)
			// memoizes on a uint64 used-edge bitmask once a player owns more
			// than 20 routes, which only has 64 bits. A player can never
			// claim more routes than trains, so this loader bound is what
			// keeps that bitmask sound for every legitimately-parsed map.
			name: "trains_per_player 65 (exceeds the 64-bit used-edge bitmask)",
			mutate: func(doc map[string]any) {
				rulesOf(doc)["trains_per_player"] = 65
			},
			wantErrPath: "$.rules.trains_per_player",
		},
		{
			name: "trains_per_player 0",
			mutate: func(doc map[string]any) {
				rulesOf(doc)["trains_per_player"] = 0
			},
			wantErrPath: "$.rules.trains_per_player",
		},
		{
			name: "trains_per_player 64 is the allowed ceiling",
			mutate: func(doc map[string]any) {
				rulesOf(doc)["trains_per_player"] = 64
			},
			wantOK: true,
		},
		{
			name: "layout.cities missing a city",
			mutate: func(doc map[string]any) {
				delete(must[map[string]any](layoutOf(doc)["cities"]), "c")
			},
			wantErrPath: "$.layout.cities.c",
		},
		{
			name: "layout.routes missing a route",
			mutate: func(doc map[string]any) {
				delete(must[map[string]any](layoutOf(doc)["routes"]), "2")
			},
			wantErrPath: "$.layout.routes.2",
		},
		{
			name: "len(slots) != length",
			mutate: func(doc map[string]any) {
				routes := must[map[string]any](layoutOf(doc)["routes"])
				route1 := must[map[string]any](routes["1"])
				route1["slots"] = []any{map[string]any{"x": 0.1, "y": 0.1, "angle": 0}}
			},
			wantErrPath: "$.layout.routes.1.slots",
		},
		{
			name: "x 1.5",
			mutate: func(doc map[string]any) {
				cities := must[map[string]any](layoutOf(doc)["cities"])
				must[map[string]any](cities["a"])["x"] = 1.5
			},
			wantErrPath: "$.layout.cities.a.x",
		},
		{
			name: "unknown JSON field",
			mutate: func(doc map[string]any) {
				doc["unexpected_field"] = "boom"
			},
			wantErrPath: "$",
		},
		{
			name: "route offset 0.006 accepted",
			mutate: func(doc map[string]any) {
				route1 := must[map[string]any](must[map[string]any](layoutOf(doc)["routes"])["1"])
				route1["offset"] = 0.006
			},
			wantOK: true,
		},
		{
			name: "route bend 0.1 accepted",
			mutate: func(doc map[string]any) {
				route1 := must[map[string]any](must[map[string]any](layoutOf(doc)["routes"])["1"])
				route1["bend"] = 0.1
			},
			wantOK: true,
		},
		{
			name: "route offset 0.5 rejected",
			mutate: func(doc map[string]any) {
				route1 := must[map[string]any](must[map[string]any](layoutOf(doc)["routes"])["1"])
				route1["offset"] = 0.5
			},
			wantErrPath: "$.layout.routes.1.offset",
		},
		{
			name: "route bend -1 rejected",
			mutate: func(doc map[string]any) {
				route1 := must[map[string]any](must[map[string]any](layoutOf(doc)["routes"])["1"])
				route1["bend"] = -1
			},
			wantErrPath: "$.layout.routes.1.bend",
		},
		// The plan's literal ticket-count bullet ("exactly 6 long + 40
		// regular") is Europe-specific, not a property of every map: a
		// custom map's ticket deck is data the map author supplies, the
		// same way its cities and routes are. rules.long_tickets /
		// rules.regular_tickets make the expected split map-driven,
		// defaulting to Europe's 6/40 (see applyRuleDefaults) so
		// europe.v1.json (Step 5) validates unchanged. These two cases
		// cover the override explicitly.
		{
			name: "ticket count override satisfied",
			mutate: func(doc map[string]any) {
				rulesOf(doc)["long_tickets"] = 5
				rulesOf(doc)["regular_tickets"] = 35
				rulesOf(doc)["tickets"] = genTickets(5, 35)
			},
			wantOK: true,
		},
		{
			name: "ticket count override still enforced",
			mutate: func(doc map[string]any) {
				rulesOf(doc)["long_tickets"] = 3
				// tickets left at the default 6 long / 40 regular split,
				// which no longer satisfies the overridden count.
			},
			wantErrPath: "$.rules.tickets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := cloneDoc(t, validMapDoc())
			tt.mutate(doc)
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatalf("marshal mutated doc: %v", err)
			}

			m, err := ParseMap(raw)
			if tt.wantOK {
				if err != nil {
					t.Fatalf("ParseMap() error = %v, want nil", err)
				}
				if m == nil {
					t.Fatal("ParseMap() returned nil map with nil error")
				}
				return
			}

			if err == nil {
				t.Fatalf("ParseMap() error = nil, want error at path %q", tt.wantErrPath)
			}
			var verrs ValidationErrors
			if !errors.As(err, &verrs) {
				t.Fatalf("ParseMap() error type = %T, want ValidationErrors", err)
			}
			if !hasErrorAtPath(verrs, tt.wantErrPath) {
				t.Fatalf("ParseMap() errors = %v, want one at path %q", verrs, tt.wantErrPath)
			}
		})
	}
}

func TestParseMapBuildsIndices(t *testing.T) {
	raw, err := json.Marshal(validMapDoc())
	if err != nil {
		t.Fatalf("marshal base doc: %v", err)
	}
	m, err := ParseMap(raw)
	if err != nil {
		t.Fatalf("ParseMap() error = %v", err)
	}

	checkIndexSizes(t, m)
	checkRoutesByCity(t, m)

	if got := m.RouteByID[1].Color; got != ColorRed {
		t.Errorf("RouteByID[1].Color = %v, want %v", got, ColorRed)
	}
	if m.RouteByID[2].IsFerry() {
		t.Errorf("RouteByID[2].IsFerry() = true, want false (locos == 0)")
	}
}

func checkIndexSizes(t *testing.T, m *Map) {
	t.Helper()
	if got, want := len(m.CityByID), 3; got != want {
		t.Errorf("len(CityByID) = %d, want %d", got, want)
	}
	if got, want := len(m.RouteByID), 4; got != want {
		t.Errorf("len(RouteByID) = %d, want %d", got, want)
	}
	if got, want := len(m.TicketByID), 46; got != want {
		t.Errorf("len(TicketByID) = %d, want %d", got, want)
	}
	if got, want := len(m.LongTicketIDs), 6; got != want {
		t.Errorf("len(LongTicketIDs) = %d, want %d", got, want)
	}
	if got, want := len(m.RegularTicketIDs), 40; got != want {
		t.Errorf("len(RegularTicketIDs) = %d, want %d", got, want)
	}
	if got, want := m.Rules.LongTickets, 6; got != want {
		t.Errorf("Rules.LongTickets = %d, want %d (default)", got, want)
	}
	if got, want := m.Rules.RegularTickets, 40; got != want {
		t.Errorf("Rules.RegularTickets = %d, want %d (default)", got, want)
	}
}

func checkRoutesByCity(t *testing.T, m *Map) {
	t.Helper()
	wantByCity := map[string][]int32{
		"a": {1, 3, 4},
		"b": {1, 2, 4},
		"c": {2, 3},
	}
	for city, want := range wantByCity {
		got := m.RoutesByCity[city]
		if len(got) != len(want) {
			t.Errorf("RoutesByCity[%q] = %v, want %v", city, got, want)
			continue
		}
		seen := make(map[int32]bool, len(got))
		for _, id := range got {
			seen[id] = true
		}
		for _, id := range want {
			if !seen[id] {
				t.Errorf("RoutesByCity[%q] = %v, missing route %d", city, got, id)
			}
		}
	}
}

func TestPointsForLength(t *testing.T) {
	tests := []struct {
		length  int
		want    int
		wantErr bool
	}{
		{length: 1, want: 1},
		{length: 2, want: 2},
		{length: 3, want: 4},
		{length: 4, want: 7},
		{length: 6, want: 15},
		{length: 8, want: 21},
		{length: 5, wantErr: true},
		{length: 7, wantErr: true},
	}

	for _, tt := range tests {
		got, err := PointsForLength(tt.length)
		if tt.wantErr {
			if !errors.Is(err, ErrInvalidRouteLength) {
				t.Errorf("PointsForLength(%d) error = %v, want ErrInvalidRouteLength", tt.length, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("PointsForLength(%d) unexpected error: %v", tt.length, err)
		}
		if got != tt.want {
			t.Errorf("PointsForLength(%d) = %d, want %d", tt.length, got, tt.want)
		}
	}
}
