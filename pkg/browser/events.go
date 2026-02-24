package browser

import (
	"sync"
	"time"
)

// ConsoleMessage represents a browser console message.
type ConsoleMessage struct {
	Level     string    `json:"level"` // "log", "warning", "error", "info", "debug"
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

// NetworkRequest represents a captured network request.
type NetworkRequest struct {
	Method       string    `json:"method"`
	URL          string    `json:"url"`
	ResourceType string    `json:"resourceType,omitempty"`
	Status       int       `json:"status,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// PageError represents a captured page error.
type PageError struct {
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// PageState tracks per-page state including event buffers.
type PageState struct {
	Console *RingBuffer[ConsoleMessage]
	Network *RingBuffer[NetworkRequest]
	Errors  *RingBuffer[PageError]
}

// NewPageState creates a PageState with default buffer sizes.
func NewPageState() *PageState {
	return &PageState{
		Console: NewRingBuffer[ConsoleMessage](500),
		Network: NewRingBuffer[NetworkRequest](500),
		Errors:  NewRingBuffer[PageError](200),
	}
}

// RingBuffer is a thread-safe ring buffer of fixed capacity.
type RingBuffer[T any] struct {
	mu    sync.Mutex
	items []T
	cap   int
	start int // index of oldest item
	count int // number of items
}

// NewRingBuffer creates a RingBuffer with the given capacity.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	return &RingBuffer[T]{
		items: make([]T, capacity),
		cap:   capacity,
	}
}

// Add appends an item, evicting the oldest if at capacity.
func (rb *RingBuffer[T]) Add(item T) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	idx := (rb.start + rb.count) % rb.cap
	rb.items[idx] = item
	if rb.count == rb.cap {
		rb.start = (rb.start + 1) % rb.cap
	} else {
		rb.count++
	}
}

// Items returns all items in order (oldest first).
func (rb *RingBuffer[T]) Items() []T {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	result := make([]T, rb.count)
	for i := 0; i < rb.count; i++ {
		result[i] = rb.items[(rb.start+i)%rb.cap]
	}
	return result
}

// Clear removes all items.
func (rb *RingBuffer[T]) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.start = 0
	rb.count = 0
}

// Len returns the current number of items.
func (rb *RingBuffer[T]) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}

// UpdateLast finds the last item matching the predicate and calls the update
// function on it. Returns true if an item was updated.
func (rb *RingBuffer[T]) UpdateLast(update func(*T) bool) bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for i := rb.count - 1; i >= 0; i-- {
		idx := (rb.start + i) % rb.cap
		if update(&rb.items[idx]) {
			return true
		}
	}
	return false
}
