// Package packdata holds the official song pack documents shipped with the
// binary, mirroring internal/games/ttr/mapdata's go:embed pattern.
package packdata

import _ "embed"

// exampleV1 is the "Mixed Hits (example)" pack document (pack format v2,
// rules Appendix A), seeded by migration 0012. It is built from Appendix
// A's own two example songs (the "Song Title"/"Band" placeholder and Queen's
// "Bohemian Rhapsody") padded with additional songs so every (language x
// bucket) cell used here holds at least 2 songs and the total song count is
// even (Req 5, Req 38 parity validator).
//
//go:embed example.v1.json
var exampleV1 []byte

// ExampleV1ID is the fixed uuid seeded by migration 0012.
const ExampleV1ID = "00000000-0000-0000-0000-0000000000f0"

// ExampleV1Version is the pack_versions.version seeded by migration 0012.
const ExampleV1Version = 1

// ExampleV1Slug is the song_guess.packs.slug seeded by migration 0012.
const ExampleV1Slug = "example-hits"

// ExampleV1 returns the embedded example pack document. Callers must not
// mutate the returned slice — it is the same backing array used by every
// caller and by migration 0012's seeded row.
func ExampleV1() []byte {
	return exampleV1
}
