package treesitter

import "unsafe"

type QueryCursor struct {
	lib *library
	ptr uintptr
}

func NewQueryCursor(lib *library) *QueryCursor {
	return &QueryCursor{lib: lib, ptr: lib.tsQueryCursorNew()}
}

func (c *QueryCursor) Exec(query *Query, node Node) {
	c.lib.tsQueryCursorExec(c.ptr, query.ptr, node)
}

func (c *QueryCursor) SetByteRange(start, end uint32) bool {
	return c.lib.tsQueryCursorSetByteRange(c.ptr, start, end)
}

func (c *QueryCursor) NextMatch() (QueryMatch, bool) {
	var match QueryMatch
	if c == nil || c.ptr == 0 {
		return match, false
	}
	return match, c.lib.tsQueryCursorNextMatch(c.ptr, &match)
}

func (c *QueryCursor) NextCapture() (QueryMatch, uint32, bool) {
	var match QueryMatch
	var captureIndex uint32
	if c == nil || c.ptr == 0 {
		return match, captureIndex, false
	}
	ok := c.lib.tsQueryCursorNextCapture(c.ptr, &match, &captureIndex)
	return match, captureIndex, ok
}

func (m QueryMatch) CaptureSlice() []QueryCapture {
	if m.CaptureCount == 0 || m.Captures == nil {
		return nil
	}
	return unsafe.Slice((*QueryCapture)(m.Captures), int(m.CaptureCount)) // #nosec G103 -- tree-sitter owns this match buffer for cursor iteration.
}

func (c *QueryCursor) Close() {
	if c != nil && c.ptr != 0 {
		c.lib.tsQueryCursorDelete(c.ptr)
		c.ptr = 0
	}
}
