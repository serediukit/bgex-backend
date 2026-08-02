package ttr

import "errors"

// Domain sentinel errors for the ttr package. Handlers/services map these to
// HTTP responses via errors.Is without needing to know package internals.
var (
	// ErrInvalidRouteLength is returned when a route length has no defined
	// point value (rules §12 — fail loudly on lengths other than
	// 1, 2, 3, 4, 6, 8).
	ErrInvalidRouteLength = errors.New("invalid route length")
	// ErrMapNotFound is returned when a referenced map id does not exist.
	ErrMapNotFound = errors.New("map not found")
	// ErrMapVersionNotFound is returned when a referenced (map, version)
	// pair does not exist.
	ErrMapVersionNotFound = errors.New("map version not found")
	// ErrAssetNotFound is returned when a referenced map asset id does not
	// exist.
	ErrAssetNotFound = errors.New("map asset not found")
	// ErrAssetTooLarge is returned when an uploaded background asset
	// exceeds the maximum allowed size.
	ErrAssetTooLarge = errors.New("map asset exceeds maximum size")
	// ErrUnsupportedMime is returned when an uploaded background asset is
	// not one of the supported image mime types.
	ErrUnsupportedMime = errors.New("unsupported map asset mime type")
	// ErrVersionPublished is returned when an attempt is made to mutate a
	// map version that has already been published.
	ErrVersionPublished = errors.New("map version is already published")
	// ErrInvalidMapDoc is returned when a map document fails validation;
	// callers that need the itemized problems should use ParseMap directly
	// and inspect the returned ValidationErrors.
	ErrInvalidMapDoc = errors.New("invalid map document")
	// ErrInvalidConfig is returned when the lobby config InitState receives
	// is missing or malformed (e.g. no "map_id"/"map_version"). Note this is
	// distinct from lobby.ErrInvalidConfig: the lobby's ConfigValidator seam
	// normalizes config before InitState ever sees it, so this sentinel only
	// fires on a genuinely malformed call (defensive, not user-facing).
	ErrInvalidConfig = errors.New("invalid ttr game configuration")
)
