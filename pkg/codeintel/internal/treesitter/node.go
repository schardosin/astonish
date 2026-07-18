package treesitter

func (n Node) Kind(lib *library) string {
	if n.IsZero() || lib == nil || lib.tsNodeIsNull(n) {
		return ""
	}
	return lib.tsNodeType(n)
}

func (n Node) StartByte(lib *library) uint32 {
	return lib.tsNodeStartByte(n)
}

func (n Node) EndByte(lib *library) uint32 {
	return lib.tsNodeEndByte(n)
}

func (n Node) StartPoint(lib *library) Point {
	return lib.tsNodeStartPoint(n)
}

func (n Node) EndPoint(lib *library) Point {
	return lib.tsNodeEndPoint(n)
}

func (n Node) ChildCount(lib *library) uint32 {
	return lib.tsNodeChildCount(n)
}

func (n Node) Child(lib *library, i uint32) Node {
	return lib.tsNodeChild(n, i)
}

func (n Node) NamedChildCount(lib *library) uint32 {
	return lib.tsNodeNamedChildCount(n)
}

func (n Node) NamedChild(lib *library, i uint32) Node {
	return lib.tsNodeNamedChild(n, i)
}

func (n Node) Parent(lib *library) Node {
	return lib.tsNodeParent(n)
}

func (n Node) NextSibling(lib *library) Node {
	return lib.tsNodeNextSibling(n)
}

func (n Node) PrevSibling(lib *library) Node {
	return lib.tsNodePrevSibling(n)
}

func (n Node) IsNull(lib *library) bool {
	return lib == nil || lib.tsNodeIsNull(n)
}

func (n Node) IsNamed(lib *library) bool {
	return lib != nil && lib.tsNodeIsNamed(n)
}

func (n Node) Text(lib *library, source []byte) string {
	start := int(n.StartByte(lib))
	end := int(n.EndByte(lib))
	if start < 0 || end < start || end > len(source) {
		return ""
	}
	return string(source[start:end])
}
