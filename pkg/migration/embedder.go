package migration

import (
	"context"
	"fmt"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/memory"
)

// migrationEmbedder wraps the memory package's local embedder for use during migration.
type migrationEmbedder struct {
	hugot   *memory.HugotEmbedder
}

// Embed returns the 384-dim embedding for a text chunk.
func (e *migrationEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	fn := e.hugot.EmbeddingFunc()
	return fn(ctx, text)
}

// Close releases the embedder resources.
func (e *migrationEmbedder) Close() error {
	return e.hugot.Close()
}

// newMigrationEmbedder creates a local embedding model for the migration process.
// Uses the same all-MiniLM-L6-v2 model as the regular memory indexer (384-dim vectors).
func newMigrationEmbedder(appCfg *config.AppConfig) (*migrationEmbedder, error) {
	modelsDir, err := config.GetModelsDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve models directory: %w", err)
	}

	hugot, err := memory.NewHugotEmbedder(modelsDir, false)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize embedding model: %w", err)
	}

	return &migrationEmbedder{hugot: hugot}, nil
}
