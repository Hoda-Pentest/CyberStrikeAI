package c2

import (
	"strings"
	"sync"

	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

// Listener is the listener abstraction: each transport (TCP/HTTP/HTTPS/WS/DNS) implements this interface;
// Manager is unaware of implementation details and creates them via the ListenerRegistry factory.
type Listener interface {
	// Type returns the current listener's type string (such as "tcp_reverse")
	Type() string
	// Start starts listening; it should return ErrPortInUse if the port is in use
	Start() error
	// Stop stops listening and releases all related goroutines (should not panic)
	Stop() error
}

// ListenerCreationCtx is the context the factory receives when initializing a listener
type ListenerCreationCtx struct {
	Listener *database.C2Listener
	Config   *ListenerConfig
	Manager  *Manager
	Logger   *zap.Logger
}

// ListenerFactory is the factory that creates a listener instance; the returned instance has not been Started
type ListenerFactory func(ctx ListenerCreationCtx) (Listener, error)

// ListenerRegistry is a type -> factory registry; internal/app registers concrete implementations at startup,
// and tests can inject a mock factory to override them.
type ListenerRegistry struct {
	mu        sync.RWMutex
	factories map[string]ListenerFactory
}

// NewListenerRegistry creates an empty registry
func NewListenerRegistry() *ListenerRegistry {
	return &ListenerRegistry{factories: make(map[string]ListenerFactory)}
}

// Register registers a listener factory
func (r *ListenerRegistry) Register(typeName string, f ListenerFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[strings.ToLower(strings.TrimSpace(typeName))] = f
}

// Get retrieves a factory; nil means not registered
func (r *ListenerRegistry) Get(typeName string) ListenerFactory {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.factories[strings.ToLower(strings.TrimSpace(typeName))]
}

// RegisteredTypes lists the registered types, for frontend enumeration
func (r *ListenerRegistry) RegisteredTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.factories))
	for k := range r.factories {
		out = append(out, k)
	}
	return out
}
