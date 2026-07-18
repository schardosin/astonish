package codeintel

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/SAP/astonish/pkg/codeintel/internal/treesitter"
)

type fileParser struct {
	language *treesitter.Language
	query    *treesitter.Query
	parser   *treesitter.Parser
}

func newFileParser(spec languageSpec) (*fileParser, error) {
	lang, err := treesitter.GetLanguage(spec.Name)
	if err != nil {
		return nil, err
	}
	querySource, err := loadQuery(spec)
	if err != nil {
		return nil, err
	}
	query, err := treesitter.NewQuery(lang, querySource)
	if err != nil {
		return nil, err
	}
	parser, err := treesitter.NewParser(lang)
	if err != nil {
		query.Close()
		return nil, err
	}
	return &fileParser{language: lang, query: query, parser: parser}, nil
}

func (p *fileParser) close() {
	if p == nil {
		return
	}
	if p.parser != nil {
		p.parser.Close()
	}
	if p.query != nil {
		p.query.Close()
	}
}

func (p *fileParser) parse(path string, source []byte) (*ScopeGraph, error) {
	tree, err := p.parser.Parse(source)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	runtime := p.language.Runtime()
	cursor := treesitter.NewQueryCursor(runtime)
	defer cursor.Close()
	cursor.Exec(p.query, root)

	graph := &ScopeGraph{File: path}
	seenDefs := make(map[string]bool)
	seenRefs := make(map[string]bool)

	for {
		match, captureIndex, ok := cursor.NextCapture()
		if !ok {
			break
		}
		captures := match.CaptureSlice()
		if int(captureIndex) >= len(captures) {
			continue
		}
		capture := captures[captureIndex]
		captureName := p.query.CaptureName(capture.Index)
		name := strings.TrimSpace(capture.Node.Text(runtime, source))
		if name == "" || !looksLikeSymbol(name) {
			continue
		}
		point := capture.Node.StartPoint(runtime)
		line := int(point.Row) + 1
		column := int(point.Column)
		switch {
		case strings.HasPrefix(captureName, "definition."):
			kind := strings.TrimPrefix(captureName, "definition.")
			key := fmt.Sprintf("%s:%s:%d:%d", kind, name, line, column)
			if seenDefs[key] {
				continue
			}
			seenDefs[key] = true
			graph.Defs = append(graph.Defs, Definition{
				Name:      name,
				Kind:      kind,
				File:      path,
				Line:      line,
				Column:    column,
				Signature: contextLine(source, line),
			})
		case captureName == "reference":
			key := fmt.Sprintf("%s:%d:%d", name, line, column)
			if seenRefs[key] {
				continue
			}
			seenRefs[key] = true
			graph.Refs = append(graph.Refs, Reference{
				Name:        name,
				File:        path,
				Line:        line,
				Column:      column,
				ContextLine: contextLine(source, line),
			})
		}
	}

	return graph, nil
}

func looksLikeSymbol(s string) bool {
	if strings.ContainsAny(s, " \t\n\r(){}[];,:+-*/=%!&|<>\"'`") {
		return false
	}
	return true
}

func contextLine(source []byte, line int) string {
	if line <= 0 {
		return ""
	}
	lines := bytes.Split(source, []byte("\n"))
	if line > len(lines) {
		return ""
	}
	return string(bytes.TrimSpace(lines[line-1]))
}
