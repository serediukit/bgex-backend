// Europe v2 (Step 7): 15 route colours corrected against the official board
// scan, a 9th double route added (Budapest-Wien, ids 45 White / 99 Red,
// mutually paired), and every slot angle regenerated with the corrected
// pixel-space formula (see mapmodel.go's Slot doc comment and Step 2's
// slotAngleDeg). Produced entirely through the real admin editor UI against
// published v1 — see agents-workspace/plan/ttr-map-editor/europe-v2-notes.md
// for the full authoring record.
//
// europe.v1.json and europe_test.go are byte-untouched by this file: v1
// remains published, immutable, and stuck on the old normalized-space angle
// convention forever, by design.
package mapdata_test

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/serediukit/bgex-backend/internal/games/ttr"
	"github.com/serediukit/bgex-backend/internal/games/ttr/mapdata"
)

// TestEuropeV2Parses asserts the embedded v2 document passes every ParseMap
// validation (rules §4, plan Q6 layout contract) with no error.
func TestEuropeV2Parses(t *testing.T) {
	m, err := ttr.ParseMap(mapdata.EuropeV2)
	if err != nil {
		t.Fatalf("ParseMap(mapdata.EuropeV2) error:\n%v", err)
	}
	if m == nil {
		t.Fatal("ParseMap(mapdata.EuropeV2) returned nil map with nil error")
	}
}

// TestEuropeV2Counts asserts the corrected document's cardinalities: the
// well-known 47 cities and 46 tickets are unchanged from v1, but routes grow
// from 98 to 99 with the confirmed 9th double route (Budapest-Wien).
func TestEuropeV2Counts(t *testing.T) {
	m := mustParseEuropeV2(t)

	if got, want := len(m.CityByID), 47; got != want {
		t.Errorf("len(CityByID) = %d, want %d", got, want)
	}
	if got, want := len(m.RouteByID), 99; got != want {
		t.Errorf("len(RouteByID) = %d, want %d", got, want)
	}
	if got, want := len(m.TicketByID), 46; got != want {
		t.Errorf("len(TicketByID) = %d, want %d", got, want)
	}
}

// TestEuropeV2ColorCorrections asserts exactly the 15 route colour
// corrections recorded in the plan's Step 7 table (and
// europe-v2-notes.md) were applied, by route id, and nothing else on these
// routes drifted (length, tunnel, locos, city pair all still match v1).
func TestEuropeV2ColorCorrections(t *testing.T) {
	v1 := mustParseEurope(t)
	v2 := mustParseEuropeV2(t)

	corrections := map[int32]ttr.Color{
		9:  ttr.ColorPurple, // Paris-Dieppe: White -> Purple
		22: ttr.ColorGray,   // Pamplona-Barcelona: White -> Gray
		43: ttr.ColorGreen,  // Berlin-Wien: Red -> Green
		50: ttr.ColorBlue,   // Warszawa-Wien: Green -> Blue
		62: ttr.ColorGray,   // Bucuresti-Kyiv: White -> Gray
		63: ttr.ColorGray,   // Kyiv-Warszawa: Blue -> Gray
		64: ttr.ColorGreen,  // Sarajevo-Athina: Red -> Green
		65: ttr.ColorGray,   // Sarajevo-Sofia: Green -> Gray
		69: ttr.ColorPurple, // Athina-Sofia: Yellow -> Purple
		71: ttr.ColorWhite,  // Sevastopol-Bucuresti: Red -> White
		75: ttr.ColorGray,   // Angora-Erzurum: Black -> Gray
		85: ttr.ColorOrange, // Moskva-Smolensk: Yellow -> Orange
		86: ttr.ColorWhite,  // Moskva-Petrograd: Blue -> White
		87: ttr.ColorBlue,   // Petrograd-Wilno: Gray -> Blue
		89: ttr.ColorGray,   // Petrograd-Stockholm: White -> Gray
	}

	for id, wantColor := range corrections {
		v1r, ok := v1.RouteByID[id]
		if !ok {
			t.Fatalf("route %d not found in v1", id)
		}
		v2r, ok := v2.RouteByID[id]
		if !ok {
			t.Fatalf("route %d not found in v2", id)
		}
		if !sameUnorderedPair(v1r.A, v1r.B, v2r.A, v2r.B) {
			t.Errorf("route %d: city pair drifted between v1 and v2 (%s-%s vs %s-%s)", id, v1r.A, v1r.B, v2r.A, v2r.B)
		}
		if v2r.Color != wantColor {
			t.Errorf("route %d (%s-%s): color = %s, want %s", id, v2r.A, v2r.B, v2r.Color, wantColor)
		}
		if v1r.Color == wantColor {
			t.Errorf("route %d (%s-%s): v1 color was already %s — this id is not actually a correction", id, v1r.A, v1r.B, wantColor)
		}
		if v2r.Length != v1r.Length {
			t.Errorf("route %d: length drifted (v1 %d, v2 %d)", id, v1r.Length, v2r.Length)
		}
		if v2r.Tunnel != v1r.Tunnel {
			t.Errorf("route %d: tunnel flag drifted (v1 %v, v2 %v)", id, v1r.Tunnel, v2r.Tunnel)
		}
		if v2r.Locos != v1r.Locos {
			t.Errorf("route %d: locos drifted (v1 %d, v2 %d)", id, v1r.Locos, v2r.Locos)
		}
	}

	// Pin the count so an accidental extra/missing correction is caught.
	if want := 15; len(corrections) != want {
		t.Errorf("test table lists %d corrections, want %d", len(corrections), want)
	}
}

// TestEuropeV2BudapestWienPair asserts the confirmed 9th double route: route
// 45 (the original, White) and the new sibling route 99 (Red) both connect
// Budapest-Wien, are length 1, non-tunnel, non-ferry, and are mutually
// paired.
func TestEuropeV2BudapestWienPair(t *testing.T) {
	m := mustParseEuropeV2(t)

	r45, ok := m.RouteByID[45]
	if !ok {
		t.Fatal("route 45 (Budapest-Wien) not found")
	}
	r99, ok := m.RouteByID[99]
	if !ok {
		t.Fatal("route 99 (Budapest-Wien sibling) not found")
	}

	if !sameUnorderedPair(r45.A, r45.B, r99.A, r99.B) {
		t.Errorf("route 45 <-> 99: expected the same city pair, got %s-%s vs %s-%s", r45.A, r45.B, r99.A, r99.B)
	}
	if !sameUnorderedPair(r45.A, r45.B, "budapest", "wien") {
		t.Errorf("route 45: expected Budapest-Wien, got %s-%s", r45.A, r45.B)
	}

	if r45.Color != ttr.ColorWhite {
		t.Errorf("route 45: color = %s, want White", r45.Color)
	}
	if r99.Color != ttr.ColorRed {
		t.Errorf("route 99: color = %s, want Red", r99.Color)
	}
	if r45.Length != 1 {
		t.Errorf("route 45: length = %d, want 1", r45.Length)
	}
	if r99.Length != 1 {
		t.Errorf("route 99: length = %d, want 1", r99.Length)
	}

	if r45.Pair == nil || *r45.Pair != 99 {
		t.Errorf("route 45: pair = %v, want 99", r45.Pair)
	}
	if r99.Pair == nil || *r99.Pair != 45 {
		t.Errorf("route 99: pair = %v, want 45", r99.Pair)
	}
}

// TestEuropeV2SlotAnglesArePixelSpace is the permanent guard against the
// slot-angle bug (Step 2) regressing: every slot's stored angle must agree,
// within 0.2 degrees, with the true pixel-space direction of its route
// (atan2(dy*view_box.height, dx*view_box.width), where dx/dy come from the
// two endpoint cities' normalized coordinates). This assertion is
// deliberately only made against v2 — v1's angles are in the old
// normalized-space convention (see mapmodel.go's Slot doc comment) and v1
// is immutable, so the same assertion over v1 would (correctly) fail.
func TestEuropeV2SlotAnglesArePixelSpace(t *testing.T) {
	m := mustParseEuropeV2(t)

	vbW := float64(m.Layout.ViewBox.Width)
	vbH := float64(m.Layout.ViewBox.Height)

	const maxDeviationDeg = 0.2

	checked := 0
	for id, r := range m.RouteByID {
		a, ok := m.Layout.Cities[r.A]
		if !ok {
			t.Fatalf("route %d: layout.cities missing %q", id, r.A)
		}
		b, ok := m.Layout.Cities[r.B]
		if !ok {
			t.Fatalf("route %d: layout.cities missing %q", id, r.B)
		}

		dx := (b.X - a.X) * vbW
		dy := (b.Y - a.Y) * vbH
		wantAngle := math.Atan2(dy, dx) * 180 / math.Pi

		lr, ok := m.Layout.Routes[routeKey(id)]
		if !ok {
			t.Fatalf("route %d: layout.routes missing entry", id)
		}
		for i, slot := range lr.Slots {
			diff := angleDiffDeg(slot.Angle, wantAngle)
			if diff > maxDeviationDeg {
				t.Errorf("route %d (%s-%s) slot %d: angle = %v, want %v (pixel-space direction), diff %.4f > %v",
					id, r.A, r.B, i, slot.Angle, wantAngle, diff, maxDeviationDeg)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no slots were checked — test is vacuous")
	}
}

// angleDiffDeg returns the smallest positive angular distance between a and
// b in degrees, both interpreted mod 360, so e.g. 179 and -179 are 2 degrees
// apart rather than 358.
func angleDiffDeg(a, b float64) float64 {
	d := math.Mod(a-b+540, 360) - 180
	return math.Abs(d)
}

// TestEuropeV2SeedMigrationMatchesEmbeddedJSON reads
// migrations/0010_seed_ttr_europe_map_v2.up.sql from disk, extracts the JSON
// body between the $ttrjson$ dollar-quote delimiters, and asserts it is
// *semantically* identical (parsed-structure comparison) to mapdata.EuropeV2
// and that the doc_sha256 literal in the SQL equals
// ttr.DocSHA256(mapdata.EuropeV2). Unlike the v1 seed-drift test
// (TestSeedMigrationMatchesEmbeddedJSON), this deliberately does NOT compare
// bytes: JSONB does not preserve byte-for-byte formatting on a real
// round-trip through Postgres, so a byte comparison here would be the wrong
// tool even though these two particular files happen to be
// byte-identical today.
func TestEuropeV2SeedMigrationMatchesEmbeddedJSON(t *testing.T) {
	sqlPath := migrationPath(t, "0010_seed_ttr_europe_map_v2.up.sql")
	sql, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("read %s: %v", sqlPath, err)
	}

	const tag = "$ttrjson$"
	body := string(sql)
	first := indexAfter(t, body, tag, 0)
	if first < len(body) && body[first] == '\n' {
		first++
	}
	second := indexAfter(t, body, tag, first)
	extracted := body[first : second-len(tag)]

	var fromMigration any
	if err := json.Unmarshal([]byte(extracted), &fromMigration); err != nil {
		t.Fatalf("json.Unmarshal(extracted migration JSON): %v", err)
	}
	var fromEmbedded any
	if err := json.Unmarshal(mapdata.EuropeV2, &fromEmbedded); err != nil {
		t.Fatalf("json.Unmarshal(mapdata.EuropeV2): %v", err)
	}
	if !reflect.DeepEqual(fromMigration, fromEmbedded) {
		t.Fatal("migration 0010's JSON body is not semantically identical to mapdata.EuropeV2 (parsed-structure comparison)")
	}

	// The hash literal appears twice in this migration: once in the
	// $seed_check$ DO block's published-row comparison, and once in the
	// INSERT ... VALUES list. A bare containsLiteral would stay green if
	// only the INSERT's copy were updated after regenerating the map,
	// leaving the DO block comparing against a stale hash -- which would
	// then RAISE EXCEPTION against a legitimately-identical republish.
	// Asserting the count catches that half-updated state.
	wantSHA := ttr.DocSHA256(mapdata.EuropeV2)
	if got, want := countLiteral(body, wantSHA), 2; got != want {
		t.Errorf("migration contains doc_sha256 literal %q (ttr.DocSHA256(mapdata.EuropeV2)) %d times, want %d (the $seed_check$ DO block comparison and the INSERT literal)", wantSHA, got, want)
	}
}

func mustParseEuropeV2(t *testing.T) *ttr.Map {
	t.Helper()
	m, err := ttr.ParseMap(mapdata.EuropeV2)
	if err != nil {
		t.Fatalf("ParseMap(mapdata.EuropeV2) error:\n%v", err)
	}
	return m
}
