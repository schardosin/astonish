package codeintel

import (
	"fmt"
	"sort"
	"strings"
)

type RepoMapOptions struct {
	MaxTokens    int
	FocusFiles   []string
	FocusSymbols []string
}

func (i *Index) RepoMap(opts RepoMapOptions) (string, int, int) {
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	if maxTokens > 16384 {
		maxTokens = 16384
	}

	type rankedFile struct {
		file  string
		rank  float64
		graph *ScopeGraph
	}

	focusFiles := make(map[string]bool)
	for _, file := range opts.FocusFiles {
		focusFiles[file] = true
	}
	focusSymbols := make(map[string]bool)
	for _, symbol := range opts.FocusSymbols {
		focusSymbols[symbol] = true
	}

	files := make([]rankedFile, 0, len(i.Graphs))
	for file, graph := range i.Graphs {
		rank := i.Ranks[file]
		if focusFiles[file] {
			rank += 10
		}
		for _, def := range graph.Defs {
			if focusSymbols[def.Name] {
				rank += 5
			}
		}
		files = append(files, rankedFile{file: file, rank: rank, graph: graph})
	}
	sort.SliceStable(files, func(a, b int) bool {
		if files[a].rank == files[b].rank {
			return files[a].file < files[b].file
		}
		return files[a].rank > files[b].rank
	})

	var b strings.Builder
	shown := 0
	for _, file := range files {
		section := renderFile(file.graph)
		if approxTokens(b.Len()+len(section)) > maxTokens && shown > 0 {
			break
		}
		b.WriteString(section)
		shown++
	}
	return strings.TrimSpace(b.String()), len(files), shown
}

func renderFile(graph *ScopeGraph) string {
	var b strings.Builder
	b.WriteString(graph.File)
	b.WriteByte('\n')
	limit := len(graph.Defs)
	if limit > 20 {
		limit = 20
	}
	for _, def := range graph.Defs[:limit] {
		line := def.Signature
		if line == "" {
			line = fmt.Sprintf("%s %s", def.Kind, def.Name)
		}
		b.WriteString("| ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if len(graph.Defs) > limit {
		b.WriteString(fmt.Sprintf("| ... %d more definitions\n", len(graph.Defs)-limit))
	}
	b.WriteByte('\n')
	return b.String()
}

func approxTokens(chars int) int {
	return (chars + 3) / 4
}
