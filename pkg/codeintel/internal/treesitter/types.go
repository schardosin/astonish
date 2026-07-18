package treesitter

import "unsafe"

// Point mirrors TSPoint from tree_sitter/api.h.
type Point struct {
	Row    uint32
	Column uint32
}

// Node mirrors TSNode from tree_sitter/api.h. Keep this layout exact.
type Node struct {
	Context [4]uint32
	ID      uintptr
	Tree    uintptr
}

// QueryCapture mirrors TSQueryCapture. The explicit padding preserves the
// 40-byte array stride on 64-bit platforms.
type QueryCapture struct {
	Node  Node
	Index uint32
	_     [4]byte
}

// QueryMatch mirrors TSQueryMatch.
type QueryMatch struct {
	ID           uint32
	PatternIndex uint16
	CaptureCount uint16
	Captures     unsafe.Pointer
}

func init() {
	if unsafe.Sizeof(Point{}) != 8 {
		panic("treesitter: Point layout mismatch")
	}
	if unsafe.Sizeof(Node{}) != 32 {
		panic("treesitter: Node layout mismatch")
	}
	if unsafe.Sizeof(QueryCapture{}) != 40 {
		panic("treesitter: QueryCapture layout mismatch")
	}
	if unsafe.Sizeof(QueryMatch{}) != 16 {
		panic("treesitter: QueryMatch layout mismatch")
	}
}

func (n Node) IsZero() bool {
	return n.ID == 0 && n.Tree == 0
}
