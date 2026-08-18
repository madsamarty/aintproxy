package server

import (
	"sync"

	"github.com/mohamed-sameh/aintproxy/internal/rotation"
)

type History struct {
	mu      sync.RWMutex
	entries []rotation.RotateResult
	max     int
}

func NewHistory(max int) *History {
	return &History{
		entries: make([]rotation.RotateResult, 0, max),
		max:     max,
	}
}

func (h *History) Add(entry rotation.RotateResult) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.entries) >= h.max {
		h.entries = h.entries[1:]
	}
	h.entries = append(h.entries, entry)
}

func (h *History) All() []rotation.RotateResult {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]rotation.RotateResult, len(h.entries))
	copy(out, h.entries)
	return out
}

func (h *History) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.entries)
}
