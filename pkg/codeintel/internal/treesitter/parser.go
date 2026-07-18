package treesitter

import "fmt"

type Parser struct {
	lib *library
	ptr uintptr
}

func NewParser(lang *Language) (*Parser, error) {
	ptr := lang.lib.tsParserNew()
	if ptr == 0 {
		return nil, fmt.Errorf("tree-sitter parser allocation failed")
	}
	if !lang.lib.tsParserSetLanguage(ptr, lang.ptr) {
		lang.lib.tsParserDelete(ptr)
		return nil, fmt.Errorf("tree-sitter parser rejected language")
	}
	return &Parser{lib: lang.lib, ptr: ptr}, nil
}

func (p *Parser) Parse(source []byte) (*Tree, error) {
	if p == nil || p.ptr == 0 {
		return nil, fmt.Errorf("tree-sitter parser is closed")
	}
	tree := p.lib.tsParserParseString(p.ptr, 0, bytesPtr(source), uint32(len(source)))
	keepBytesAlive(source)
	if tree == 0 {
		return nil, fmt.Errorf("tree-sitter parse failed")
	}
	return &Tree{lib: p.lib, ptr: tree}, nil
}

func (p *Parser) Close() {
	if p != nil && p.ptr != 0 {
		p.lib.tsParserDelete(p.ptr)
		p.ptr = 0
	}
}
