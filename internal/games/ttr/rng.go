package ttr

import "math/rand/v2"

// Shuffler permutes a slice in place and draws random bounded integers.
// Injectable so tests are deterministic — production code uses
// DefaultShuffler, tests use NewSeededShuffler.
type Shuffler interface {
	// Shuffle pseudo-randomly permutes a sequence of n elements using swap
	// (same contract as math/rand/v2.Shuffle).
	Shuffle(n int, swap func(i, j int))
	// IntN returns a pseudo-random int in [0, n).
	IntN(n int) int
}

// DefaultShuffler uses math/rand/v2's global source (OS-seeded at startup),
// matching engine.ShuffledDeck — no cryptographic secrecy requirement.
type DefaultShuffler struct{}

// Shuffle permutes n elements via math/rand/v2's global source.
func (DefaultShuffler) Shuffle(n int, swap func(i, j int)) { rand.Shuffle(n, swap) }

// IntN returns a random int in [0, n) via math/rand/v2's global source.
func (DefaultShuffler) IntN(n int) int { return rand.IntN(n) } // #nosec G404 -- no cryptographic secrecy requirement, see DefaultShuffler doc

// NewSeededShuffler returns a deterministic Shuffler for tests: the same
// seed always produces the same sequence of shuffles/draws. *rand.Rand
// already implements Shuffler (its Shuffle/IntN signatures match exactly),
// so no wrapper type is needed.
func NewSeededShuffler(seed uint64) Shuffler {
	// Two independent 64-bit halves feed PCG's 128-bit state; XOR-ing in a
	// fixed odd constant avoids the low-entropy case of seeding both halves
	// identically.
	return rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) // #nosec G404 -- deterministic by design, for tests only
}
