package engine

// Registry maps a game key to its Engine. The lobby and realtime layers depend
// only on Registry + Engine, never on a concrete game, so adding a new game is
// a matter of registering it here at wire-up time.
type Registry struct {
	engines map[string]Engine
}

// NewRegistry builds a Registry from the given engines, keyed by GameKey.
func NewRegistry(engines ...Engine) *Registry {
	m := make(map[string]Engine, len(engines))
	for _, e := range engines {
		m[e.GameKey()] = e
	}
	return &Registry{engines: m}
}

// Get returns the engine for a game key and whether it exists.
func (r *Registry) Get(gameKey string) (Engine, bool) {
	e, ok := r.engines[gameKey]
	return e, ok
}

// Has reports whether a game key is registered.
func (r *Registry) Has(gameKey string) bool {
	_, ok := r.engines[gameKey]
	return ok
}
