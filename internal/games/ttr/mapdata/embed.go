// Package mapdata holds the official board documents shipped with the binary.
package mapdata

import _ "embed"

//go:embed europe.v1.json
var EuropeV1 []byte

// EuropeMapID is the fixed uuid seeded by migration 0008.
const EuropeMapID = "00000000-0000-0000-0000-0000000000e0"

// EuropeVersion is the map_versions.version seeded by migration 0008.
const EuropeVersion = 1
