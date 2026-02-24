package browser

import (
	"fmt"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// RefEntry maps a ref ID back to a DOM node for interaction.
type RefEntry struct {
	BackendDOMNodeID proto.DOMBackendNodeID
	Role             string
	Name             string
	CSSSelector      string // Fallback: CSS selector for DOM-based refs
}

// RefMap maps ref IDs (e.g. "ref1") to accessibility node metadata.
// It is rebuilt on each snapshot call.
type RefMap struct {
	mu      sync.RWMutex
	refs    map[string]RefEntry
	counter int
}

// NewRefMap creates an empty RefMap.
func NewRefMap() *RefMap {
	return &RefMap{
		refs: make(map[string]RefEntry),
	}
}

// Reset clears all refs and resets the counter.
func (rm *RefMap) Reset() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.refs = make(map[string]RefEntry)
	rm.counter = 0
}

// Add registers a new ref and returns its ID (e.g. "ref1").
func (rm *RefMap) Add(entry RefEntry) string {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.counter++
	id := fmt.Sprintf("ref%d", rm.counter)
	rm.refs[id] = entry
	return id
}

// Get looks up a ref by ID.
func (rm *RefMap) Get(ref string) (RefEntry, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	e, ok := rm.refs[ref]
	return e, ok
}

// Count returns the number of refs.
func (rm *RefMap) Count() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return len(rm.refs)
}

// ResolveElement resolves a ref ID to a rod Element on the given page.
// If the ref has a BackendDOMNodeID (from an AX-tree snapshot), it uses
// DOM.resolveNode. Otherwise, it falls back to a CSS selector lookup
// (from a DOM-based snapshot).
func (rm *RefMap) ResolveElement(pg *rod.Page, ref string) (*rod.Element, error) {
	entry, ok := rm.Get(ref)
	if !ok {
		return nil, fmt.Errorf("ref %q not found — take a new browser_snapshot first", ref)
	}

	// Fast path: resolve via BackendDOMNodeID (AX-tree snapshot)
	if entry.BackendDOMNodeID != 0 {
		res, err := proto.DOMResolveNode{
			BackendNodeID: entry.BackendDOMNodeID,
		}.Call(pg)
		if err != nil {
			return nil, fmt.Errorf("ref %q could not be resolved (page may have changed) — take a new browser_snapshot", ref)
		}

		el, err := pg.ElementFromObject(res.Object)
		if err != nil {
			return nil, fmt.Errorf("ref %q could not be converted to element: %w", ref, err)
		}
		return el, nil
	}

	// Fallback: resolve via CSS selector (DOM-based snapshot)
	if entry.CSSSelector != "" {
		el, err := pg.Element(entry.CSSSelector)
		if err != nil {
			return nil, fmt.Errorf("ref %q selector %q not found (page may have changed) — take a new browser_snapshot", ref, entry.CSSSelector)
		}
		return el, nil
	}

	return nil, fmt.Errorf("ref %q has no resolution method — take a new browser_snapshot", ref)
}
