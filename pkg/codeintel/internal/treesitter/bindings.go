package treesitter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const DefaultLibraryPath = "/usr/lib/astonish/libastonish-treesitter.so"

var ErrLibraryUnavailable = errors.New("tree-sitter library unavailable")

type library struct {
	handle uintptr

	tsParserNew               func() uintptr
	tsParserSetLanguage       func(uintptr, uintptr) bool
	tsParserParseString       func(uintptr, uintptr, *byte, uint32) uintptr
	tsParserDelete            func(uintptr)
	tsTreeRootNode            func(uintptr) Node
	tsTreeDelete              func(uintptr)
	tsNodeType                func(Node) string
	tsNodeStartByte           func(Node) uint32
	tsNodeEndByte             func(Node) uint32
	tsNodeStartPoint          func(Node) Point
	tsNodeEndPoint            func(Node) Point
	tsNodeChildCount          func(Node) uint32
	tsNodeChild               func(Node, uint32) Node
	tsNodeNamedChildCount     func(Node) uint32
	tsNodeNamedChild          func(Node, uint32) Node
	tsNodeParent              func(Node) Node
	tsNodeNextSibling         func(Node) Node
	tsNodePrevSibling         func(Node) Node
	tsNodeIsNull              func(Node) bool
	tsNodeIsNamed             func(Node) bool
	tsQueryNew                func(uintptr, *byte, uint32, *uint32, *uint32) uintptr
	tsQueryDelete             func(uintptr)
	tsQueryPatternCount       func(uintptr) uint32
	tsQueryCaptureCount       func(uintptr) uint32
	tsQueryCaptureNameForID   func(uintptr, uint32, *uint32) unsafe.Pointer
	tsQueryCursorNew          func() uintptr
	tsQueryCursorDelete       func(uintptr)
	tsQueryCursorExec         func(uintptr, uintptr, Node)
	tsQueryCursorNextMatch    func(uintptr, *QueryMatch) bool
	tsQueryCursorNextCapture  func(uintptr, *QueryMatch, *uint32) bool
	tsQueryCursorSetByteRange func(uintptr, uint32, uint32) bool
	tsLanguageSymbolCount     func(uintptr) uint32
	tsLanguageSymbolName      func(uintptr, uint16) string
	tsLanguageFieldCount      func(uintptr) uint32
	tsLanguageFieldNameForID  func(uintptr, uint16) string
	treeSitterGo              func() uintptr
	treeSitterTypescript      func() uintptr
	treeSitterTSX             func() uintptr
	treeSitterJavascript      func() uintptr
	treeSitterPython          func() uintptr
}

// Runtime is the loaded tree-sitter shared library handle. It is exported so
// callers inside pkg/codeintel can pass it back to Node helper methods without
// gaining direct access to C function pointers.
type Runtime = library

var (
	defaultMu      sync.Mutex
	defaultLib     *library
	defaultLibErr  error
	defaultLibPath string
)

func DefaultLibrary() (*library, error) {
	defaultMu.Lock()
	defer defaultMu.Unlock()

	path := os.Getenv("ASTONISH_TREESITTER_LIB")
	if path == "" {
		path = DefaultLibraryPath
	}
	if defaultLib != nil {
		return defaultLib, nil
	}
	if defaultLibErr != nil && defaultLibPath == path {
		return nil, defaultLibErr
	}
	lib, err := Open(path)
	if err != nil {
		defaultLibPath = path
		defaultLibErr = fmt.Errorf("%w: %w", ErrLibraryUnavailable, err)
		return nil, defaultLibErr
	}
	defaultLib = lib
	defaultLibErr = nil
	defaultLibPath = path
	return defaultLib, nil
}

// ResetDefaultLibraryForTest clears the cached default library so tests can
// exercise load failures or alternate ASTONISH_TREESITTER_LIB paths.
func ResetDefaultLibraryForTest() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultLib = nil
	defaultLibErr = nil
	defaultLibPath = ""
}

func Open(path string) (*library, error) {
	if path == "" {
		path = DefaultLibraryPath
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err == nil {
			path = abs
		}
	}

	handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("load tree-sitter library %q: %w", path, err)
	}

	lib := &library{handle: handle}
	registrations := []struct {
		fn   any
		name string
	}{
		{&lib.tsParserNew, "ts_parser_new"},
		{&lib.tsParserSetLanguage, "ts_parser_set_language"},
		{&lib.tsParserParseString, "ts_parser_parse_string"},
		{&lib.tsParserDelete, "ts_parser_delete"},
		{&lib.tsTreeRootNode, "ts_tree_root_node"},
		{&lib.tsTreeDelete, "ts_tree_delete"},
		{&lib.tsNodeType, "ts_node_type"},
		{&lib.tsNodeStartByte, "ts_node_start_byte"},
		{&lib.tsNodeEndByte, "ts_node_end_byte"},
		{&lib.tsNodeStartPoint, "ts_node_start_point"},
		{&lib.tsNodeEndPoint, "ts_node_end_point"},
		{&lib.tsNodeChildCount, "ts_node_child_count"},
		{&lib.tsNodeChild, "ts_node_child"},
		{&lib.tsNodeNamedChildCount, "ts_node_named_child_count"},
		{&lib.tsNodeNamedChild, "ts_node_named_child"},
		{&lib.tsNodeParent, "ts_node_parent"},
		{&lib.tsNodeNextSibling, "ts_node_next_sibling"},
		{&lib.tsNodePrevSibling, "ts_node_prev_sibling"},
		{&lib.tsNodeIsNull, "ts_node_is_null"},
		{&lib.tsNodeIsNamed, "ts_node_is_named"},
		{&lib.tsQueryNew, "ts_query_new"},
		{&lib.tsQueryDelete, "ts_query_delete"},
		{&lib.tsQueryPatternCount, "ts_query_pattern_count"},
		{&lib.tsQueryCaptureCount, "ts_query_capture_count"},
		{&lib.tsQueryCaptureNameForID, "ts_query_capture_name_for_id"},
		{&lib.tsQueryCursorNew, "ts_query_cursor_new"},
		{&lib.tsQueryCursorDelete, "ts_query_cursor_delete"},
		{&lib.tsQueryCursorExec, "ts_query_cursor_exec"},
		{&lib.tsQueryCursorNextMatch, "ts_query_cursor_next_match"},
		{&lib.tsQueryCursorNextCapture, "ts_query_cursor_next_capture"},
		{&lib.tsQueryCursorSetByteRange, "ts_query_cursor_set_byte_range"},
		{&lib.tsLanguageSymbolCount, "ts_language_symbol_count"},
		{&lib.tsLanguageSymbolName, "ts_language_symbol_name"},
		{&lib.tsLanguageFieldCount, "ts_language_field_count"},
		{&lib.tsLanguageFieldNameForID, "ts_language_field_name_for_id"},
		{&lib.treeSitterGo, "tree_sitter_go"},
		{&lib.treeSitterTypescript, "tree_sitter_typescript"},
		{&lib.treeSitterTSX, "tree_sitter_tsx"},
		{&lib.treeSitterJavascript, "tree_sitter_javascript"},
		{&lib.treeSitterPython, "tree_sitter_python"},
	}

	for _, reg := range registrations {
		purego.RegisterLibFunc(reg.fn, handle, reg.name)
	}
	return lib, nil
}

func bytesPtr(b []byte) *byte {
	if len(b) == 0 {
		return nil
	}
	return (*byte)(unsafe.Pointer(unsafe.SliceData(b))) // #nosec G103 -- required FFI pointer conversion; slice lifetime is kept by caller.
}

func keepBytesAlive(b []byte) {
	runtime.KeepAlive(b)
}
