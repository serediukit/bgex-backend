// Package mapdata_test validates the embedded Europe map document (Step 5)
// against the loader built in Step 4, and guards against drift between the
// embedded JSON and the SQL seed migration that inlines it.
//
// REVIEW — uncertainty list for human spot-check against a physical board
// -------------------------------------------------------------------------
// The full city list (47), route topology (which cities each route
// connects), lengths, tunnel flags and ferry/locomotive counts were
// cross-checked against a scan of the official board plus a structural
// community dataset (github.com/leonsi7/ticket-to-ride-europe) and are
// high-confidence. The 46 destination tickets (city pairs, points, and the
// long/regular split) were cross-checked against two independent
// transcriptions (a BoardGameGeek thread and a strategy-wiki listing) that
// agreed exactly, and are high-confidence.
//
// The one topology correction made: the community CSV's "Palermo-Athina"
// length-6/2-locomotive ferry was replaced with "Palermo-Smyrna", matching
// both the official rulebook's worked ferry-cost example ("the Ferry Route
// from Smyrna to Palermo requires four Train cards of any one color and two
// Locomotives") and the plan document's own worked example for this exact
// route. High-confidence.
//
// Route COLORS were legible on the board scan for roughly two-thirds of the
// 90 city-pairs (all colors on the following are directly board-confirmed:
// Edinburgh-London, London-Amsterdam, London-Dieppe (double), Brest-Dieppe,
// Brest-Paris, Brest-Pamplona, Paris-Zurich, Paris-Pamplona (double),
// Madrid-Pamplona (double), Bruxelles-Dieppe, Bruxelles-Paris (double),
// Bruxelles-Frankfurt, Paris-Frankfurt, Amsterdam-Essen, Frankfurt-Essen,
// Essen-Berlin, Kobenhavn-Essen, Frankfurt-Berlin (double), Frankfurt-
// Munchen, Zurich-Venezia, Zurich-Marseille, Marseille-Roma, Venezia-Roma,
// Venezia-Zagrab, Roma-Brindisi, Roma-Palermo, Brindisi-Palermo, Brindisi-
// Athina, Palermo-Smyrna, Zagrab-Sarajevo, Zagrab-Budapest, Budapest-
// Sarajevo, Budapest-Wien, Budapest-Kyiv, Bucuresti-Budapest, Athina-Smyrna,
// Constantinople-Smyrna, Constantinople-Angora, Smyrna-Angora, Sevastopol-
// Constantinople, Sevastopol-Erzurum, Sevastopol-Sochi, Sevastopol-Rostov,
// Riga-Danzig, Danzig-Warszawa, Warszawa-Wilno, Wilno-Riga, Moskva-
// Smolensk, Moskva-Petrograd, Petrograd-Riga, Berlin-Danzig, Berlin-
// Warszawa (double), Stockholm-Kobenhavn (double), Kharkov-Kyiv, Munchen-
// Zurich, Munchen-Venezia, Munchen-Wien.
//
// The following routes' EXACT printed colour could not be read with
// confidence at board-scan resolution (topology, length, tunnel and
// ferry/locomotive flags for these are still board/rulebook-confirmed or
// directly inherited from the structural dataset — only the specific colour
// is a best-effort guess chosen to keep the palette self-consistent and the
// document validation-passing):
//
//	Lisboa-Cadiz, Madrid-Lisboa, Madrid-Cadiz, Madrid-Barcelona,
//	Marseille-Pamplona, Marseille-Barcelona, Pamplona-Barcelona,
//	Paris-Dieppe, Berlin-Wien, Wien-Zagrab, Warszawa-Wien,
//	Bucuresti-Kyiv, Kyiv-Warszawa, Sarajevo-Athina, Bucuresti-
//	Constantinople, Athina-Sofia, Sevastopol-Bucuresti, Angora-Erzurum,
//	Sochi-Erzurum, Sochi-Rostov, Rostov-Kharkov, Kharkov-Moskva,
//	Petrograd-Wilno, Petrograd-Stockholm (also unconfirmed: tunnel vs.
//	plain-color rendering — encoded here as a tunnel per the structural
//	dataset), Wilno-Smolensk, Wilno-Kyiv, Smolensk-Kyiv.
//
// Double-route completeness: 8 double routes were identified by directly
// observing two parallel tracks between the same city pair on the board
// scan (Edinburgh-London, Madrid-Pamplona, Paris-Pamplona, Bruxelles-Paris,
// London-Dieppe, Frankfurt-Berlin, Berlin-Warszawa, Stockholm-Kobenhavn). A
// secondary source described the Europe map as having "9" double routes
// in total; a 9th pair may exist elsewhere on the board that this
// inspection did not catch. A human should specifically check for a missed
// double route before treating the double-route list as final.
package mapdata_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/serediukit/bgex-backend/internal/games/ttr"
	"github.com/serediukit/bgex-backend/internal/games/ttr/mapdata"
)

// TestEuropeMapParses asserts the embedded Europe document passes every
// ParseMap validation (rules §4, plan Q6 layout contract) with no error.
func TestEuropeMapParses(t *testing.T) {
	m, err := ttr.ParseMap(mapdata.EuropeV1)
	if err != nil {
		t.Fatalf("ParseMap(mapdata.EuropeV1) error:\n%v", err)
	}
	if m == nil {
		t.Fatal("ParseMap(mapdata.EuropeV1) returned nil map with nil error")
	}
}

// TestEuropeTicketCounts asserts the 46/6/40 ticket split required by rules
// §3.2.
func TestEuropeTicketCounts(t *testing.T) {
	m := mustParseEurope(t)

	if got, want := len(m.TicketByID), 46; got != want {
		t.Errorf("len(TicketByID) = %d, want %d", got, want)
	}
	if got, want := len(m.LongTicketIDs), 6; got != want {
		t.Errorf("len(LongTicketIDs) = %d, want %d", got, want)
	}
	if got, want := len(m.RegularTicketIDs), 40; got != want {
		t.Errorf("len(RegularTicketIDs) = %d, want %d", got, want)
	}
}

// TestEuropeCityCount asserts the Europe board's well-known 47 cities.
func TestEuropeCityCount(t *testing.T) {
	m := mustParseEurope(t)
	if got, want := len(m.CityByID), 47; got != want {
		t.Errorf("len(CityByID) = %d, want %d", got, want)
	}
}

// TestEuropeRouteLengths asserts every route length is one of the values
// with a defined point value (rules §12 — fail loudly on 5/7, never emit
// them in map data).
func TestEuropeRouteLengths(t *testing.T) {
	m := mustParseEurope(t)
	valid := map[int]bool{1: true, 2: true, 3: true, 4: true, 6: true, 8: true}
	for id, r := range m.RouteByID {
		if !valid[r.Length] {
			t.Errorf("route %d (%s-%s): length %d is not in {1,2,3,4,6,8}", id, r.A, r.B, r.Length)
		}
	}
}

// TestEuropeFerriesAreGray asserts every ferry (locos >= 1) is a Gray route
// (rules §4 ferry assertion). ParseMap already enforces this fatally, so
// this test is a documentation-level double-check against the parsed
// result.
func TestEuropeFerriesAreGray(t *testing.T) {
	m := mustParseEurope(t)
	for id, r := range m.RouteByID {
		if r.IsFerry() && r.Color != ttr.ColorGray {
			t.Errorf("route %d (%s-%s): ferry (locos=%d) but color=%s, want Gray", id, r.A, r.B, r.Locos, r.Color)
		}
	}
}

// TestEuropePairsSymmetric asserts every double-route pair (rules §8.5) is
// symmetric, equal length, connects the same unordered city pair, and never
// pairs a route with itself. ParseMap already enforces this fatally; this
// test re-derives it from the parsed indices as a belt-and-suspenders
// check and documents the double-route count.
func TestEuropePairsSymmetric(t *testing.T) {
	m := mustParseEurope(t)

	seen := map[int32]bool{}
	pairs := 0
	for id, r := range m.RouteByID {
		if r.Pair == nil {
			continue
		}
		if seen[id] {
			continue
		}
		other, ok := m.RouteByID[*r.Pair]
		if !ok {
			t.Fatalf("route %d: pair %d does not exist", id, *r.Pair)
		}
		if other.Pair == nil || *other.Pair != id {
			t.Errorf("route %d <-> %d: pair is not symmetric", id, *r.Pair)
		}
		if other.Length != r.Length {
			t.Errorf("route %d <-> %d: pair lengths differ (%d vs %d)", id, *r.Pair, r.Length, other.Length)
		}
		if !sameUnorderedPair(r.A, r.B, other.A, other.B) {
			t.Errorf("route %d <-> %d: pair connects different city pairs (%s-%s vs %s-%s)", id, *r.Pair, r.A, r.B, other.A, other.B)
		}
		if *r.Pair == id {
			t.Errorf("route %d: paired with itself", id)
		}
		seen[id] = true
		seen[*r.Pair] = true
		pairs++
	}
	// 8 double routes were positively identified on the board scan (see the
	// REVIEW block above) — this pins the count so an accidental drop/add
	// while editing the map is caught.
	if want := 8; pairs != want {
		t.Errorf("found %d double-route pairs, want %d (see REVIEW block for the known double-route list)", pairs, want)
	}
}

func sameUnorderedPair(a1, b1, a2, b2 string) bool {
	return (a1 == a2 && b1 == b2) || (a1 == b2 && b1 == a2)
}

// TestEuropeTicketAndRouteCitiesResolve asserts every route and ticket city
// reference resolves to a declared city. ParseMap already enforces this
// fatally (a dangling reference is a validation error, not a nil pointer),
// so this test documents the guarantee by walking the parsed indices.
func TestEuropeTicketAndRouteCitiesResolve(t *testing.T) {
	m := mustParseEurope(t)

	for id, r := range m.RouteByID {
		if _, ok := m.CityByID[r.A]; !ok {
			t.Errorf("route %d: city %q does not exist", id, r.A)
		}
		if _, ok := m.CityByID[r.B]; !ok {
			t.Errorf("route %d: city %q does not exist", id, r.B)
		}
	}
	for id, tk := range m.TicketByID {
		if _, ok := m.CityByID[tk.A]; !ok {
			t.Errorf("ticket %d: city %q does not exist", id, tk.A)
		}
		if _, ok := m.CityByID[tk.B]; !ok {
			t.Errorf("ticket %d: city %q does not exist", id, tk.B)
		}
	}
}

// TestEuropeGraphConnected asserts every city is reachable from every other
// city via routes — a prerequisite for every destination ticket being
// completable in principle, and a strong signal that no city or route was
// dropped while authoring the document.
func TestEuropeGraphConnected(t *testing.T) {
	m := mustParseEurope(t)

	adj := make(map[string][]string, len(m.CityByID))
	for _, r := range m.RouteByID {
		adj[r.A] = append(adj[r.A], r.B)
		adj[r.B] = append(adj[r.B], r.A)
	}

	var start string
	for id := range m.CityByID {
		start = id
		break
	}

	visited := map[string]bool{start: true}
	stack := []string{start}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, n := range adj[cur] {
			if !visited[n] {
				visited[n] = true
				stack = append(stack, n)
			}
		}
	}

	if len(visited) != len(m.CityByID) {
		var unreached []string
		for id := range m.CityByID {
			if !visited[id] {
				unreached = append(unreached, id)
			}
		}
		t.Errorf("graph is not connected: %d of %d cities reachable from %q, unreached: %v",
			len(visited), len(m.CityByID), start, unreached)
	}
}

// TestEuropeLayoutCoversEverything asserts the layout half of the document
// has an entry for every city and every route, with slot counts matching
// route length. ParseMap already enforces this fatally; this test
// documents the guarantee explicitly against the parsed Map.
func TestEuropeLayoutCoversEverything(t *testing.T) {
	m := mustParseEurope(t)

	for id := range m.CityByID {
		if _, ok := m.Layout.Cities[id]; !ok {
			t.Errorf("layout.cities missing entry for city %q", id)
		}
	}
	for id, r := range m.RouteByID {
		key := routeKey(id)
		lr, ok := m.Layout.Routes[key]
		if !ok {
			t.Errorf("layout.routes missing entry for route %d", id)
			continue
		}
		if len(lr.Slots) != r.Length {
			t.Errorf("layout.routes[%d]: len(slots) = %d, want %d (route length)", id, len(lr.Slots), r.Length)
		}
	}
}

// routeKey renders a route id the same way ParseMap looks it up in
// layout.routes: the decimal string form of the id (JSON object keys must
// be strings).
func routeKey(id int32) string {
	return strconv.Itoa(int(id))
}

func mustParseEurope(t *testing.T) *ttr.Map {
	t.Helper()
	m, err := ttr.ParseMap(mapdata.EuropeV1)
	if err != nil {
		t.Fatalf("ParseMap(mapdata.EuropeV1) error:\n%v", err)
	}
	return m
}

// TestSeedMigrationMatchesEmbeddedJSON reads
// migrations/0008_seed_ttr_europe_map.up.sql from disk, extracts the JSON
// body between the $ttrjson$ dollar-quote delimiters, and asserts it is
// byte-identical to mapdata.EuropeV1 and that the doc_sha256 literal in the
// SQL equals ttr.DocSHA256(mapdata.EuropeV1). There is no database in this
// environment to run the migration against — this test is the only guard
// against the embedded file and the seed migration drifting apart (e.g. a
// truncated copy-paste, or an edit to one without the other).
func TestSeedMigrationMatchesEmbeddedJSON(t *testing.T) {
	sqlPath := migrationPath(t, "0008_seed_ttr_europe_map.up.sql")
	sql, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("read %s: %v", sqlPath, err)
	}

	const tag = "$ttrjson$"
	body := string(sql)
	first := indexAfter(t, body, tag, 0)
	// The heredoc that authored this file places a newline immediately
	// after the opening tag; skip it so the extracted body starts exactly
	// like the embedded file (which starts with "{").
	if first < len(body) && body[first] == '\n' {
		first++
	}
	second := indexAfter(t, body, tag, first)
	extracted := body[first : second-len(tag)]

	want := string(mapdata.EuropeV1)
	if extracted != want {
		t.Fatalf("migration JSON body does not match mapdata.EuropeV1 byte-for-byte (extracted %d bytes, want %d bytes)",
			len(extracted), len(want))
	}

	wantSHA := ttr.DocSHA256(mapdata.EuropeV1)
	if !containsLiteral(body, wantSHA) {
		t.Errorf("migration does not contain doc_sha256 literal %q (ttr.DocSHA256(mapdata.EuropeV1))", wantSHA)
	}
}

// indexAfter returns the index immediately after the next occurrence of tag
// in s at or after from, failing the test if tag is not found.
func indexAfter(t *testing.T, s, tag string, from int) int {
	t.Helper()
	i := indexFrom(s, tag, from)
	if i < 0 {
		t.Fatalf("tag %q not found in migration starting at offset %d", tag, from)
	}
	return i + len(tag)
}

func indexFrom(s, substr string, from int) int {
	if from > len(s) {
		return -1
	}
	i := indexOf(s[from:], substr)
	if i < 0 {
		return -1
	}
	return i + from
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func containsLiteral(s, substr string) bool {
	return indexOf(s, substr) >= 0
}

// migrationPath resolves the absolute path to a file under the repo's
// migrations/ directory, relative to this test file's own location
// (internal/games/ttr/mapdata/), so it works regardless of the test
// runner's working directory.
func migrationPath(t *testing.T, filename string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// this file:      internal/games/ttr/mapdata/europe_test.go
	// repo root:      ../../../../
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	path := filepath.Join(root, "migrations", filename)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("resolved migration path %s does not exist: %v", path, err)
	}
	return path
}
