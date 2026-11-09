package dispatch

import (
	"sync"

	"astral-party-server/internal/frame"
	"astral-party-server/internal/session"
)

type Handler func(s *session.Session, f *frame.Frame) error

type Router struct {
	mu       sync.RWMutex
	handlers map[int16]Handler
	fallback Handler
}

func NewRouter() *Router {
	return &Router{handlers: make(map[int16]Handler)}
}

// SetFallback installs a handler invoked for any CMDID without a specific
// handler. Used to reply with an empty S2C so client RPCs complete instead of
// hitting the 15s timeout.
func (r *Router) SetFallback(h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallback = h
}

func (r *Router) Register(cmdID int16, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[cmdID] = h
}

func (r *Router) Lookup(cmdID int16) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if h, ok := r.handlers[cmdID]; ok {
		return h, true
	}
	if r.fallback != nil {
		return r.fallback, true
	}
	return nil, false
}
