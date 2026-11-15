package hook

import "time"

// SetNow replaces the hook's clock so tests can move time without sleeping.
func (h *Hook) SetNow(now func() time.Time) { h.now = now }

// SeenLen reports the number of entries in the replay cache.
func (h *Hook) SeenLen() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.seen)
}
