package codeintel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/adk/tool"
)

type RepoMapArgs struct {
	Path         string   `json:"path,omitempty" jsonschema:"Root directory to map (default: current workspace)"`
	MaxTokens    int      `json:"max_tokens,omitempty" jsonschema:"Token budget for output (default 4096, max 16384)"`
	FocusFiles   []string `json:"focus_files,omitempty" jsonschema:"Files to boost in ranking"`
	FocusSymbols []string `json:"focus_symbols,omitempty" jsonschema:"Symbols to boost in ranking"`
}

type RepoMapResult struct {
	Map         string `json:"map"`
	FilesRanked int    `json:"files_ranked"`
	FilesShown  int    `json:"files_shown"`
	TokensUsed  int    `json:"tokens_used"`
	IndexTimeMs int64  `json:"index_time_ms"`
}

type CodeDefinitionArgs struct {
	Symbol string `json:"symbol" jsonschema:"Name of the symbol to find the definition of"`
	File   string `json:"file,omitempty" jsonschema:"Limit search to definitions in this file"`
	Path   string `json:"path,omitempty" jsonschema:"Limit search to this directory subtree"`
}

type CodeDefinitionResult struct {
	Definitions []Definition `json:"definitions"`
	Total       int          `json:"total"`
	IndexTimeMs int64        `json:"index_time_ms,omitempty"`
}

type CodeReferencesArgs struct {
	Symbol string `json:"symbol" jsonschema:"Name of the symbol to find references for"`
	File   string `json:"file,omitempty" jsonschema:"Only references in this file"`
	Path   string `json:"path,omitempty" jsonschema:"Limit search to this directory subtree"`
}

type CodeReferencesResult struct {
	References  []Reference `json:"references"`
	Total       int         `json:"total"`
	IndexTimeMs int64       `json:"index_time_ms,omitempty"`
}

func RepoMap(ctx tool.Context, args RepoMapArgs) (RepoMapResult, error) {
	root, err := resolveRoot(args.Path)
	if err != nil {
		return RepoMapResult{}, err
	}
	start := time.Now()
	result, err := GetIndex(context.Background(), root)
	if err != nil {
		return RepoMapResult{}, codeintelUnavailable(err)
	}
	text, ranked, shown := result.Index.RepoMap(RepoMapOptions{
		MaxTokens:    args.MaxTokens,
		FocusFiles:   args.FocusFiles,
		FocusSymbols: args.FocusSymbols,
	})
	return RepoMapResult{Map: text, FilesRanked: ranked, FilesShown: shown, TokensUsed: approxTokens(len(text)), IndexTimeMs: time.Since(start).Milliseconds()}, nil
}

func CodeDefinition(ctx tool.Context, args CodeDefinitionArgs) (CodeDefinitionResult, error) {
	root, pathFilter, err := rootAndPath(args.Path)
	if err != nil {
		return CodeDefinitionResult{}, err
	}
	start := time.Now()
	result, err := GetIndex(context.Background(), root)
	if err != nil {
		return CodeDefinitionResult{}, codeintelUnavailable(err)
	}
	defs := result.Index.Definitions(args.Symbol, args.File, pathFilter)
	return CodeDefinitionResult{Definitions: defs, Total: len(defs), IndexTimeMs: time.Since(start).Milliseconds()}, nil
}

func CodeReferences(ctx tool.Context, args CodeReferencesArgs) (CodeReferencesResult, error) {
	root, pathFilter, err := rootAndPath(args.Path)
	if err != nil {
		return CodeReferencesResult{}, err
	}
	start := time.Now()
	result, err := GetIndex(context.Background(), root)
	if err != nil {
		return CodeReferencesResult{}, codeintelUnavailable(err)
	}
	refs := result.Index.References(args.Symbol, args.File, pathFilter)
	return CodeReferencesResult{References: refs, Total: len(refs), IndexTimeMs: time.Since(start).Milliseconds()}, nil
}

func resolveRoot(path string) (string, error) {
	if path == "" {
		return os.Getwd()
	}
	return filepath.Abs(path)
}

func rootAndPath(path string) (string, string, error) {
	if path == "" {
		root, err := os.Getwd()
		return root, "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(abs)
	if err == nil && info.IsDir() {
		return abs, "", nil
	}
	root := filepath.Dir(abs)
	return root, filepath.Base(abs), nil
}

func codeintelUnavailable(err error) error {
	return fmt.Errorf("code intelligence unavailable: %w; ensure libastonish-treesitter.so is installed or use grep_search/find_files", err)
}
