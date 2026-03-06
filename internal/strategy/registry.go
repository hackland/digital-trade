package strategy

import (
	"fmt"
	"sync"
)

// Registry manages available strategies and allows lookup by name.
type Registry struct {
	mu         sync.RWMutex
	factories  map[string]func() Strategy
	strategies map[string]Strategy
}

// NewRegistry creates an empty strategy registry.
func NewRegistry() *Registry {
	return &Registry{
		factories:  make(map[string]func() Strategy),
		strategies: make(map[string]Strategy),
	}
}

// Register adds a strategy factory to the registry.
func (r *Registry) Register(name string, factory func() Strategy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

// Create instantiates a strategy by name and initializes it with config.
func (r *Registry) Create(name string, cfg map[string]interface{}) (Strategy, error) {
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown strategy: %s, available: %v", name, r.Available())
	}

	s := factory()
	if err := s.Init(cfg); err != nil {
		return nil, fmt.Errorf("init strategy %s: %w", name, err)
	}

	r.mu.Lock()
	r.strategies[name] = s
	r.mu.Unlock()

	return s, nil
}

// Get returns a previously created strategy by name.
func (r *Registry) Get(name string) (Strategy, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.strategies[name]
	return s, ok
}

// Available returns names of all registered strategy factories.
func (r *Registry) Available() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

// All returns all instantiated strategies.
func (r *Registry) All() []Strategy {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Strategy, 0, len(r.strategies))
	for _, s := range r.strategies {
		result = append(result, s)
	}
	return result
}
