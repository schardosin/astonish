package codeintel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/codeintel/internal/treesitter"
)

func TestDefaultLibraryPathMatchesSandboxContract(t *testing.T) {
	const want = "/usr/lib/astonish/libastonish-treesitter.so"
	if treesitter.DefaultLibraryPath != want {
		t.Fatalf("DefaultLibraryPath = %q, want %q", treesitter.DefaultLibraryPath, want)
	}
}

func TestComputeRanks_Empty(t *testing.T) {
	if ranks := computeRanks(nil); ranks != nil {
		t.Fatalf("expected nil ranks, got %v", ranks)
	}
	if ranks := computeRanks(map[string]*ScopeGraph{}); ranks != nil {
		t.Fatalf("expected nil ranks for empty map, got %v", ranks)
	}
}

func TestComputeRanks_CrossFileReferences(t *testing.T) {
	graphs := map[string]*ScopeGraph{
		"a.go": {
			File: "a.go",
			Defs: []Definition{{Name: "Helper", File: "a.go"}},
			Refs: []Reference{},
		},
		"b.go": {
			File: "b.go",
			Defs: []Definition{{Name: "Use", File: "b.go"}},
			Refs: []Reference{{Name: "Helper", File: "b.go"}},
		},
		"c.go": {
			File: "c.go",
			Defs: []Definition{{Name: "Other", File: "c.go"}},
			Refs: []Reference{{Name: "Helper", File: "c.go"}},
		},
	}
	ranks := computeRanks(graphs)
	if len(ranks) != 3 {
		t.Fatalf("expected 3 ranks, got %d", len(ranks))
	}
	// a.go is referenced by others, so should rank highest among the three.
	if ranks["a.go"] <= ranks["b.go"] || ranks["a.go"] <= ranks["c.go"] {
		t.Fatalf("expected a.go highest rank, got %v", ranks)
	}
}

func TestComputeRanks_SelfLoopIgnored(t *testing.T) {
	graphs := map[string]*ScopeGraph{
		"solo.go": {
			File: "solo.go",
			Defs: []Definition{{Name: "F", File: "solo.go"}},
			Refs: []Reference{{Name: "F", File: "solo.go"}},
		},
	}
	ranks := computeRanks(graphs)
	if len(ranks) != 1 {
		t.Fatalf("expected 1 rank, got %d", len(ranks))
	}
	if ranks["solo.go"] <= 0 {
		t.Fatalf("expected positive rank, got %v", ranks["solo.go"])
	}
}

func TestPageRank_DanglingMassConserved(t *testing.T) {
	// All-leaf graph: no resolvable cross-file edges. Without dangling-mass
	// redistribution, total rank collapses below 1.0.
	graphs := map[string]*ScopeGraph{
		"a.go": {File: "a.go", Defs: []Definition{{Name: "A", File: "a.go"}}},
		"b.go": {File: "b.go", Defs: []Definition{{Name: "B", File: "b.go"}}},
		"c.go": {File: "c.go", Defs: []Definition{{Name: "C", File: "c.go"}}},
	}
	ranks := pageRank(graphs, nil)
	var sum float64
	for _, r := range ranks {
		sum += r
	}
	if sum < 0.99 || sum > 1.01 {
		t.Fatalf("expected rank mass ≈ 1.0 for edgeless graph, got %v (ranks=%v)", sum, ranks)
	}

	// Cross-file refs should still prefer the referenced file.
	cross := map[string]*ScopeGraph{
		"a.go": {File: "a.go", Defs: []Definition{{Name: "Helper", File: "a.go"}}},
		"b.go": {File: "b.go", Defs: []Definition{{Name: "Use", File: "b.go"}}, Refs: []Reference{{Name: "Helper", File: "b.go"}}},
	}
	crossRanks := computeRanks(cross)
	if crossRanks["a.go"] <= crossRanks["b.go"] {
		t.Fatalf("expected a.go > b.go after dangling fix, got %v", crossRanks)
	}
}

func TestFilterDefinitionsAndReferences(t *testing.T) {
	defs := []Definition{
		{Name: "A", File: "pkg/a.go", Line: 1},
		{Name: "A", File: "pkg/b.go", Line: 2},
		{Name: "A", File: "other/c.go", Line: 3},
	}
	filtered := filterDefinitions(defs, "pkg/a.go", "")
	if len(filtered) != 1 || filtered[0].File != "pkg/a.go" {
		t.Fatalf("file filter: got %#v", filtered)
	}
	filtered = filterDefinitions(defs, "", "pkg")
	if len(filtered) != 2 {
		t.Fatalf("path filter: got %#v", filtered)
	}

	refs := []Reference{
		{Name: "A", File: "pkg/a.go", Line: 10},
		{Name: "A", File: "pkg/b.go", Line: 11},
	}
	filteredRefs := filterReferences(refs, "pkg/b.go", "")
	if len(filteredRefs) != 1 || filteredRefs[0].File != "pkg/b.go" {
		t.Fatalf("ref file filter: got %#v", filteredRefs)
	}
}

func TestIndexDefinitionsAndReferences(t *testing.T) {
	idx := &Index{
		DefsByName: map[string][]Definition{
			"Add": {
				{Name: "Add", File: "low.go", Line: 1},
				{Name: "Add", File: "high.go", Line: 1},
			},
		},
		RefsByName: map[string][]Reference{
			"Add": {
				{Name: "Add", File: "b.go", Line: 2},
				{Name: "Add", File: "a.go", Line: 1},
			},
		},
		Ranks: map[string]float64{"high.go": 2, "low.go": 1},
	}
	defs := idx.Definitions("Add", "", "")
	if len(defs) != 2 || defs[0].File != "high.go" {
		t.Fatalf("expected high.go first, got %#v", defs)
	}
	refs := idx.References("Add", "", "")
	if len(refs) != 2 || refs[0].File != "a.go" {
		t.Fatalf("expected a.go first by path sort, got %#v", refs)
	}
}

func TestRepoMap_TokenBudgetAndFocus(t *testing.T) {
	idx := &Index{
		Graphs: map[string]*ScopeGraph{
			"focus.go": {
				File: "focus.go",
				Defs: []Definition{{Name: "Important", Kind: "function", Signature: "func Important()"}},
			},
			"other.go": {
				File: "other.go",
				Defs: []Definition{{Name: "Other", Kind: "function", Signature: "func Other()"}},
			},
		},
		Ranks: map[string]float64{"focus.go": 0.1, "other.go": 0.9},
	}

	text, ranked, shown := idx.RepoMap(RepoMapOptions{MaxTokens: 4096, FocusFiles: []string{"focus.go"}})
	if ranked != 2 || shown != 2 {
		t.Fatalf("ranked=%d shown=%d", ranked, shown)
	}
	if !strings.Contains(text, "focus.go") || !strings.Contains(text, "Important") {
		t.Fatalf("expected focus file in map:\n%s", text)
	}
	// Focus boost should put focus.go first despite lower base rank.
	if !strings.HasPrefix(strings.TrimSpace(text), "focus.go") {
		t.Fatalf("expected focus.go first:\n%s", text)
	}

	tiny, _, shownTiny := idx.RepoMap(RepoMapOptions{MaxTokens: 1})
	if shownTiny != 1 {
		t.Fatalf("expected budget to show 1 file, got %d (text=%q)", shownTiny, tiny)
	}
}

func TestDiskCache_HitMissAndStale(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files := []string{file}

	idx := &Index{
		Root: root,
		Graphs: map[string]*ScopeGraph{
			"main.go": {File: "main.go", Defs: []Definition{{Name: "main", File: "main.go"}}},
		},
		Ranks: map[string]float64{"main.go": 1},
	}
	if err := saveDiskCache(root, files, idx); err != nil {
		t.Fatalf("saveDiskCache: %v", err)
	}
	loaded, ok := loadDiskCache(root, files)
	if !ok || loaded == nil {
		t.Fatal("expected cache hit")
	}
	if len(loaded.Graphs) != 1 || len(loaded.DefsByName["main"]) != 1 {
		t.Fatalf("unexpected loaded index: %#v", loaded)
	}

	// mtime/size change → miss
	time.Sleep(5 * time.Millisecond)
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadDiskCache(root, files); ok {
		t.Fatal("expected cache miss after content change")
	}

	// Restore matching content/mtime set by re-save, then add a file → length mismatch miss
	if err := saveDiskCache(root, files, idx); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(root, "extra.go")
	if err := os.WriteFile(extra, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadDiskCache(root, []string{file, extra}); ok {
		t.Fatal("expected cache miss when file set grows")
	}
}

func TestSpecForPath(t *testing.T) {
	cases := map[string]string{
		"a.go":   "go",
		"a.ts":   "typescript",
		"a.tsx":  "tsx",
		"a.js":   "javascript",
		"a.jsx":  "javascript",
		"a.mjs":  "javascript",
		"a.cjs":  "javascript",
		"a.py":   "python",
		"a.txt":  "",
		"Makefile": "",
	}
	for path, want := range cases {
		spec, ok := specForPath(path)
		if want == "" {
			if ok {
				t.Errorf("%s: expected no spec, got %#v", path, spec)
			}
			continue
		}
		if !ok || spec.Name != want {
			t.Errorf("%s: got ok=%v name=%q, want %q", path, ok, spec.Name, want)
		}
	}
}
