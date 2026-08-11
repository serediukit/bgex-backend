// Package mapdata holds the official board documents shipped with the binary.
package mapdata

import _ "embed"

//go:embed europe.v1.json
var EuropeV1 []byte

// EuropeV2 is the colour-corrected, pixel-space-angle Europe layout (Step
// 7): 15 route colours corrected against the official board scan, a 9th
// double route added (Budapest-Wien, ids 45/99), and every slot angle
// regenerated with the corrected pixel-space formula. Seeded by migration
// 0010. EuropeV1 remains immutable and byte-untouched by this addition.
//
//go:embed europe.v2.json
var EuropeV2 []byte

// EuropeMapID is the fixed uuid seeded by migration 0008.
const EuropeMapID = "00000000-0000-0000-0000-0000000000e0"

// EuropeVersion is the map_versions.version seeded by migration 0008.
const EuropeVersion = 1
