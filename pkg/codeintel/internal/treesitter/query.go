package treesitter

import (
	"fmt"
	"unsafe"
)

type Query struct {
	lib          *library
	ptr          uintptr
	captureNames []string
}

func NewQuery(lang *Language, source []byte) (*Query, error) {
	var offset uint32
	var errType uint32
	ptr := lang.lib.tsQueryNew(lang.ptr, bytesPtr(source), uint32(len(source)), &offset, &errType)
	keepBytesAlive(source)
	if ptr == 0 {
		return nil, fmt.Errorf("compile tree-sitter query failed at byte %d (type %d)", offset, errType)
	}

	q := &Query{lib: lang.lib, ptr: ptr}
	count := q.lib.tsQueryCaptureCount(ptr)
	q.captureNames = make([]string, count)
	for i := uint32(0); i < count; i++ {
		var length uint32
		namePtr := q.lib.tsQueryCaptureNameForID(ptr, i, &length)
		if namePtr != nil && length > 0 {
			q.captureNames[i] = string(unsafe.Slice((*byte)(namePtr), int(length))) // #nosec G103 -- tree-sitter returns a length-bounded capture-name buffer.
		}
	}
	return q, nil
}

func (q *Query) CaptureName(index uint32) string {
	if q == nil || int(index) >= len(q.captureNames) {
		return ""
	}
	return q.captureNames[index]
}

func (q *Query) CaptureNames() []string {
	if q == nil {
		return nil
	}
	names := make([]string, len(q.captureNames))
	copy(names, q.captureNames)
	return names
}

func (q *Query) PatternCount() uint32 {
	if q == nil || q.ptr == 0 {
		return 0
	}
	return q.lib.tsQueryPatternCount(q.ptr)
}

func (q *Query) Close() {
	if q != nil && q.ptr != 0 {
		q.lib.tsQueryDelete(q.ptr)
		q.ptr = 0
	}
}
