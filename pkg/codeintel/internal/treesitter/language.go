package treesitter

import "fmt"

type Language struct {
	lib *library
	ptr uintptr
}

func GetLanguage(name string) (*Language, error) {
	lib, err := DefaultLibrary()
	if err != nil {
		return nil, err
	}

	var ptr uintptr
	switch name {
	case "go":
		ptr = lib.treeSitterGo()
	case "typescript":
		ptr = lib.treeSitterTypescript()
	case "tsx":
		ptr = lib.treeSitterTSX()
	case "javascript":
		ptr = lib.treeSitterJavascript()
	case "python":
		ptr = lib.treeSitterPython()
	default:
		return nil, fmt.Errorf("unsupported tree-sitter language %q", name)
	}
	if ptr == 0 {
		return nil, fmt.Errorf("tree-sitter language %q returned nil", name)
	}
	return &Language{lib: lib, ptr: ptr}, nil
}

func (l *Language) SymbolCount() uint32 {
	return l.lib.tsLanguageSymbolCount(l.ptr)
}

func (l *Language) Runtime() *Runtime {
	return l.lib
}

func (l *Language) SymbolName(id uint16) string {
	return l.lib.tsLanguageSymbolName(l.ptr, id)
}

func (l *Language) FieldCount() uint32 {
	return l.lib.tsLanguageFieldCount(l.ptr)
}

func (l *Language) FieldName(id uint16) string {
	return l.lib.tsLanguageFieldNameForID(l.ptr, id)
}
