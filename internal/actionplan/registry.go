package actionplan

import "sync"

type InMemoryRegistry struct {
	mu   sync.RWMutex
	defs map[ActionType]ActionDefinition
}

func NewRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{defs: make(map[ActionType]ActionDefinition)}
}

func (r *InMemoryRegistry) Register(def ActionDefinition) {
	if def.Type == "" {
		panic("actionplan: action definition type is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defs[def.Type] = def
}

func (r *InMemoryRegistry) Get(actionType ActionType) (ActionDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.defs[actionType]
	return def, ok
}
