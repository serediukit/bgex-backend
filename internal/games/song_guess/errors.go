package song_guess

import "errors"

// Domain sentinel errors for the song_guess package. Handlers/services map
// these to HTTP responses via errors.Is without needing to know package
// internals. Turn/phase/actor errors are deliberately NOT duplicated here —
// see internal/games/engine.ErrNotYourTurn / ErrIllegalAction / ErrWrongPhase
// / ErrGameOver / ErrNotSeated / ErrNotEnoughPlayers, which this package
// reuses directly.
var (
	// ErrInvalidConfig is returned when the lobby config InitState receives
	// is missing or malformed (e.g. no "pack_id"/"pack_version"). The lobby's
	// ConfigValidator seam normalizes config before InitState ever sees it,
	// so this sentinel only fires on a genuinely malformed call (defensive,
	// not user-facing).
	ErrInvalidConfig = errors.New("invalid song_guess game configuration")
	// ErrPackNotFound is returned when a referenced pack id/slug does not
	// exist.
	ErrPackNotFound = errors.New("pack not found")
	// ErrPackVersionNotFound is returned when a referenced (pack, version)
	// pair does not exist.
	ErrPackVersionNotFound = errors.New("pack version not found")
	// ErrInvalidPackDoc is returned when a pack document fails validation;
	// callers that need the itemized problems should use the pack parser
	// directly and inspect the returned validation errors.
	ErrInvalidPackDoc = errors.New("invalid pack document")
)
