// embedder_core.go is intentionally NOT build-tagged so it can be used by
// both the e2e test harness AND the standalone tools/e2e-inspector binary.
package e2eboot

import (
	"context"
	"os"
	"path/filepath"

	"github.com/schardosin/astonish/pkg/memory"
	"github.com/schardosin/astonish/pkg/store"
)

// initEmbedFuncCore initializes the local Hugot embedding function and sets it
// on the platform backend. This is the non-test version (no *testing.T) used by
// both BootstrapPlatformCore (inspector) and the test wrappers.
//
// If initialization fails (model download fails, etc.), a warning is logged and
// the backend continues with keyword-only search.
//
// The embedder is intentionally NOT closed during test cleanup. The PlatformReflector
// goroutine is detached (context.Background()) and may outlive server shutdown,
// calling the embed func to generate vectors for memory saves. Closing the Hugot
// session while those goroutines are in-flight causes nil pointer panics (the
// underlying GoMLX model is destroyed). Since E2E tests run as a single process
// and exit cleanly, the ~23MB model memory is reclaimed at process exit.
func initEmbedFuncCore(log CoreLogger, backend store.PlatformBackend) {
	modelsDir := stableModelsDir()

	embedder, err := memory.NewHugotEmbedder(modelsDir, false)
	if err != nil {
		log.Logf("[e2eboot] Warning: embedding model unavailable (keyword-only search): %v", err)
		return
	}

	// Cache the embedding function once (avoid repeated closure allocation).
	embedFn := embedder.EmbeddingFunc()

	backend.SetEmbedFunc(func(ctx context.Context, text string) ([]float32, error) {
		return embedFn(ctx, text)
	})
	log.Logf("[e2eboot] Memory stores: hybrid vector+keyword search enabled")
}

// stableModelsDir returns a stable models directory that persists across runs.
// Uses ~/.config/astonish/models/ (the real user's config, not any temporary
// XDG_CONFIG_HOME). Falls back to /tmp/astonish-e2e-models/ if the user's home
// directory cannot be determined.
func stableModelsDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "astonish", "models")
	}
	return "/tmp/astonish-e2e-models"
}
