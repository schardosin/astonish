package codeintel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SAP/astonish/pkg/codeintel/internal/treesitter"
)

type Index struct {
	Root       string
	Graphs     map[string]*ScopeGraph
	DefsByName map[string][]Definition
	RefsByName map[string][]Reference
	Ranks      map[string]float64

	builtAt time.Time
}

type BuildResult struct {
	Index       *Index
	FilesParsed int
	FilesRanked int
	Duration    time.Duration
}

func Build(ctx context.Context, root string) (*BuildResult, error) {
	start := time.Now()
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	files, err := enumerateFiles(ctx, absRoot)
	if err != nil {
		return nil, err
	}
	if cached, ok := loadDiskCache(absRoot, files); ok {
		return &BuildResult{Index: cached, FilesParsed: len(cached.Graphs), FilesRanked: len(cached.Ranks), Duration: time.Since(start)}, nil
	}
	if len(files) > 0 {
		if _, err := treesitter.DefaultLibrary(); err != nil {
			return nil, err
		}
	}

	idx := &Index{
		Root:       absRoot,
		Graphs:     make(map[string]*ScopeGraph),
		DefsByName: make(map[string][]Definition),
		RefsByName: make(map[string][]Reference),
		Ranks:      make(map[string]float64),
		builtAt:    time.Now(),
	}
	if len(files) == 0 {
		_ = saveDiskCache(absRoot, files, idx)
		return &BuildResult{Index: idx, FilesParsed: 0, FilesRanked: 0, Duration: time.Since(start)}, nil
	}

	type job struct {
		path string
		spec languageSpec
	}
	type result struct {
		path  string
		graph *ScopeGraph
		err   error
	}

	jobs := make(chan job)
	results := make(chan result)
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			parsers := make(map[languageSpec]*fileParser)
			defer func() {
				for _, parser := range parsers {
					parser.close()
				}
			}()
			for j := range jobs {
				parser := parsers[j.spec]
				if parser == nil {
					var err error
					parser, err = newFileParser(j.spec)
					if err != nil {
						results <- result{path: j.path, err: err}
						continue
					}
					parsers[j.spec] = parser
				}
				source, err := os.ReadFile(j.path)
				if err != nil {
					results <- result{path: j.path, err: err}
					continue
				}
				graph, err := parser.parse(relTo(absRoot, j.path), source)
				results <- result{path: j.path, graph: graph, err: err}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, file := range files {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if spec, ok := specForPath(file); ok {
				jobs <- job{path: file, spec: spec}
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var parseErrs []error
	parsed := 0
	for res := range results {
		if res.err != nil {
			parseErrs = append(parseErrs, fmt.Errorf("%s: %w", relTo(absRoot, res.path), res.err))
			continue
		}
		if res.graph == nil {
			continue
		}
		idx.Graphs[res.graph.File] = res.graph
		for _, def := range res.graph.Defs {
			idx.DefsByName[def.Name] = append(idx.DefsByName[def.Name], def)
		}
		for _, ref := range res.graph.Refs {
			idx.RefsByName[ref.Name] = append(idx.RefsByName[ref.Name], ref)
		}
		parsed++
	}
	// Do not persist or return a partial index when the build was cancelled.
	// Disk-cache validation only checks the full file set's mtime/size, so a
	// truncated Graphs map would be reloaded as if complete (cache poisoning).
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if parsed == 0 && len(parseErrs) > 0 {
		return nil, errors.Join(parseErrs...)
	}

	idx.Ranks = computeRanks(idx.Graphs)
	_ = saveDiskCache(absRoot, files, idx)
	return &BuildResult{Index: idx, FilesParsed: parsed, FilesRanked: len(idx.Ranks), Duration: time.Since(start)}, nil
}

func (i *Index) Definitions(symbol, fileFilter, pathFilter string) []Definition {
	defs := append([]Definition(nil), i.DefsByName[symbol]...)
	defs = filterDefinitions(defs, fileFilter, pathFilter)
	sort.SliceStable(defs, func(a, b int) bool {
		ra := i.Ranks[defs[a].File]
		rb := i.Ranks[defs[b].File]
		if ra == rb {
			return defs[a].File < defs[b].File || defs[a].Line < defs[b].Line
		}
		return ra > rb
	})
	return defs
}

func (i *Index) References(symbol, fileFilter, pathFilter string) []Reference {
	refs := append([]Reference(nil), i.RefsByName[symbol]...)
	refs = filterReferences(refs, fileFilter, pathFilter)
	sort.SliceStable(refs, func(a, b int) bool {
		if refs[a].File == refs[b].File {
			return refs[a].Line < refs[b].Line
		}
		return refs[a].File < refs[b].File
	})
	return refs
}

func enumerateFiles(ctx context.Context, root string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		cmd := exec.CommandContext(ctx, "git", "ls-files")
		cmd.Dir = root
		out, err := cmd.Output()
		if err == nil {
			var files []string
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if line == "" {
					continue
				}
				path := filepath.Join(root, filepath.FromSlash(line))
				if _, ok := specForPath(path); ok {
					files = append(files, path)
				}
			}
			return files, nil
		}
	}

	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := specForPath(path); ok {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", ".next", ".cache", ".codeintel":
		return true
	default:
		return false
	}
}

func relTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func filterDefinitions(defs []Definition, fileFilter, pathFilter string) []Definition {
	if fileFilter == "" && pathFilter == "" {
		return defs
	}
	out := defs[:0]
	for _, def := range defs {
		if fileFilter != "" && filepath.ToSlash(def.File) != filepath.ToSlash(fileFilter) {
			continue
		}
		if pathFilter != "" && !strings.HasPrefix(filepath.ToSlash(def.File), strings.TrimSuffix(filepath.ToSlash(pathFilter), "/")+"/") && filepath.ToSlash(def.File) != filepath.ToSlash(pathFilter) {
			continue
		}
		out = append(out, def)
	}
	return out
}

func filterReferences(refs []Reference, fileFilter, pathFilter string) []Reference {
	if fileFilter == "" && pathFilter == "" {
		return refs
	}
	out := refs[:0]
	for _, ref := range refs {
		if fileFilter != "" && filepath.ToSlash(ref.File) != filepath.ToSlash(fileFilter) {
			continue
		}
		if pathFilter != "" && !strings.HasPrefix(filepath.ToSlash(ref.File), strings.TrimSuffix(filepath.ToSlash(pathFilter), "/")+"/") && filepath.ToSlash(ref.File) != filepath.ToSlash(pathFilter) {
			continue
		}
		out = append(out, ref)
	}
	return out
}
