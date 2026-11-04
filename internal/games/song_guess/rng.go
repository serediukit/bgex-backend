package song_guess

import "math/rand/v2"

// Shuffler draws random bounded integers. This game never shuffles a
// sequence — a cell pick draws exactly one song at random from the unplayed
// songs of that intersection (rules §5.1.1) — so, unlike ttr.Shuffler, only
// IntN is needed. Injectable so tests are deterministic — production code
// uses DefaultShuffler, tests use NewSeededShuffler.
type Shuffler interface {
	// IntN returns a pseudo-random int in [0, n).
	IntN(n int) int
}

// DefaultShuffler uses math/rand/v2's global source (OS-seeded at startup) —
// no cryptographic secrecy requirement, this is game shuffling.
type DefaultShuffler struct{}

// IntN returns a random int in [0, n) via math/rand/v2's global source.
func (DefaultShuffler) IntN(n int) int { return rand.IntN(n) } // #nosec G404 -- no cryptographic secrecy requirement, see DefaultShuffler doc

// NewSeededShuffler returns a deterministic Shuffler for tests: the same
// seed always produces the same sequence of draws. *rand.Rand already
// implements Shuffler (its IntN signature matches exactly), so no wrapper
// type is needed.
func NewSeededShuffler(seed uint64) Shuffler {
	// Two independent 64-bit halves feed PCG's 128-bit state; XOR-ing in a
	// fixed odd constant avoids the low-entropy case of seeding both halves
	// identically.
	return rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) // #nosec G404 -- deterministic by design, for tests only
}
