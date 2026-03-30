package agent

import (
	"context"
	"fmt"
	"testing"

	chromem "github.com/philippgille/chromem-go"
	"google.golang.org/adk/tool"
)

// generateBenchTools creates N tools with realistic names and descriptions.
func generateBenchTools(n int) []tool.Tool {
	categories := []string{"file", "git", "web", "shell", "search", "memory", "database", "api"}
	actions := []string{"read", "write", "create", "delete", "list", "search", "update", "query"}

	tools := make([]tool.Tool, n)
	for i := 0; i < n; i++ {
		cat := categories[i%len(categories)]
		act := actions[i%len(actions)]
		tools[i] = mockTool{
			name: fmt.Sprintf("%s_%s_%d", cat, act, i),
		}
	}
	return tools
}

// BenchmarkTokenize measures the tokenizer used in BM25 indexing.
func BenchmarkTokenize(b *testing.B) {
	inputs := []struct {
		name string
		text string
	}{
		{"short", "read_file"},
		{"medium", "Search for files matching a glob pattern in the workspace"},
		{"long", "This tool reads a file from the local filesystem and returns its contents. It supports filtering by line range using offset and limit parameters. Binary files are detected and returned as a summary instead of raw bytes."},
	}

	for _, input := range inputs {
		b.Run(input.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = tokenize(input.text)
			}
		})
	}
}

// BenchmarkHybridSearch measures the full hybrid (vector + BM25) search.
func BenchmarkHybridSearch(b *testing.B) {
	for _, numTools := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("tools=%d", numTools), func(b *testing.B) {
			ctx := context.Background()

			db := chromem.NewDB()
			idx, err := NewToolIndex(db, testEmbeddingFunc())
			if err != nil {
				b.Fatal(err)
			}

			tools := generateBenchTools(numTools)
			if err := idx.SyncTools(ctx, tools, nil); err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = idx.SearchHybrid(ctx, "file read search query", 10, 0.1)
			}
		})
	}
}

// BenchmarkVectorSearch measures vector-only search for comparison.
func BenchmarkVectorSearch(b *testing.B) {
	for _, numTools := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("tools=%d", numTools), func(b *testing.B) {
			ctx := context.Background()

			db := chromem.NewDB()
			idx, err := NewToolIndex(db, testEmbeddingFunc())
			if err != nil {
				b.Fatal(err)
			}

			tools := generateBenchTools(numTools)
			if err := idx.SyncTools(ctx, tools, nil); err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = idx.Search(ctx, "file read search query", 10, 0.1)
			}
		})
	}
}
