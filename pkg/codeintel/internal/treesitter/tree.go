package treesitter

type Tree struct {
	lib *library
	ptr uintptr
}

func (t *Tree) RootNode() Node {
	if t == nil || t.ptr == 0 {
		return Node{}
	}
	return t.lib.tsTreeRootNode(t.ptr)
}

func (t *Tree) Close() {
	if t != nil && t.ptr != 0 {
		t.lib.tsTreeDelete(t.ptr)
		t.ptr = 0
	}
}
