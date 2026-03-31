package memory

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/gomlx/gomlx/backends"
	"github.com/gomlx/gomlx/backends/simplego"
	chromem "github.com/philippgille/chromem-go"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
	"github.com/schardosin/astonish/pkg/config"
)

const (
	// DefaultEmbeddingModel is the HuggingFace model used for local embeddings.
	// all-MiniLM-L6-v2 produces 384-dim vectors, is fast on CPU, and is the
	// same model ChromaDB uses by default.
	DefaultEmbeddingModel = "sentence-transformers/all-MiniLM-L6-v2"

	// embeddingPipelineName is the internal name for the Hugot pipeline.
	embeddingPipelineName = "astonish_embedding"
)

// HugotEmbedder wraps a Hugot FeatureExtractionPipeline to provide
// in-process, pure-Go sentence embeddings with zero external dependencies.
type HugotEmbedder struct {
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
	mu       sync.Mutex // serializes embedding calls for safety
}

// patchGoMLXBackendForLowCPU works around a deadlock in GoMLX's simplego backend
// that occurs when runtime.NumCPU() is low (typically 1-2 CPUs in containers).
//
// The simplego backend's parallel execution mode uses a worker pool sized by
// runtime.NumCPU(). The DotGeneral operation (matrix multiplication) internally
// calls WaitToStart() to spawn sub-tasks from within an already-running worker
// goroutine. With a small worker pool, this causes a deadlock: all worker slots
// are occupied by goroutines waiting to spawn sub-tasks, but no slots are free.
//
// The fix: override the "go" backend constructor to always use ops_sequential
// when CPU count is low. Sequential execution has negligible performance impact
// for sentence embedding workloads (~140ms per embedding on a single core).
func patchGoMLXBackendForLowCPU() {
	if runtime.NumCPU() > 2 {
		return
	}
	backends.Register(simplego.BackendName, func(config string) (backends.Backend, error) {
		if config == "" {
			config = "ops_sequential"
		} else {
			config += ",ops_sequential"
		}
		return simplego.New(config)
	})
}

// NewHugotEmbedder creates a local embedding pipeline using Hugot's pure Go backend.
// modelsDir is where ONNX models are stored (e.g., ~/.config/astonish/models/).
// The model is downloaded from HuggingFace on first use (~23 MB).
func NewHugotEmbedder(modelsDir string, debugMode bool) (*HugotEmbedder, error) {
	// Patch GoMLX backend before creating the session to avoid deadlock on low-CPU systems.
	patchGoMLXBackendForLowCPU()

	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create models directory: %w", err)
	}

	// Determine model path on disk.
	// hugot.DownloadModel stores at: modelsDir/sentence-transformers_all-MiniLM-L6-v2/
	modelDirName := "sentence-transformers_all-MiniLM-L6-v2"
	modelPath := filepath.Join(modelsDir, modelDirName)

	// Download if not present
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		fmt.Printf("Downloading embedding model (one-time, ~23 MB)...\n")
		if debugMode {
			slog.Debug("hugot downloading embedding model", "model", DefaultEmbeddingModel)
		}
		opts := hugot.NewDownloadOptions()
		opts.Verbose = debugMode
		opts.OnnxFilePath = "onnx/model.onnx" // Select the standard (non-quantized) ONNX model
		downloadedPath, dlErr := hugot.DownloadModel(DefaultEmbeddingModel, modelsDir, opts)
		if dlErr != nil {
			return nil, fmt.Errorf("failed to download embedding model: %w", dlErr)
		}
		modelPath = downloadedPath
		fmt.Printf("Embedding model ready.\n")
	} else if debugMode {
		slog.Debug("hugot using cached model", "path", modelPath)
	}

	// Create Hugot session with pure Go backend (GoMLX simplego)
	session, err := hugot.NewGoSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create Hugot Go session: %w", err)
	}

	// Create FeatureExtraction pipeline with L2 normalization
	pipelineCfg := hugot.FeatureExtractionConfig{
		ModelPath:    modelPath,
		Name:         embeddingPipelineName,
		OnnxFilename: "model.onnx",
		Options: []hugot.FeatureExtractionOption{
			pipelines.WithNormalization(),
		},
	}

	pipeline, err := hugot.NewPipeline(session, pipelineCfg)
	if err != nil {
		_ = session.Destroy()
		return nil, fmt.Errorf("failed to create embedding pipeline: %w", err)
	}

	return &HugotEmbedder{
		session:  session,
		pipeline: pipeline,
	}, nil
}

// embeddingMaxChars is the maximum input text length (in characters) passed to
// the embedding model. The all-MiniLM-L6-v2 model has a 512-token positional
// embedding limit. With dense technical text (numbers, URLs, special chars),
// the token-to-character ratio can be as low as ~3 chars/token. 1400 chars
// yields ~467 tokens in the worst case, staying safely under 512.
//
// This is a defensive fallback — the primary defense is the chunk size limit
// (ChunkMaxChars = 1200). This constant only fires for unusually dense text
// or search queries that bypass the chunker.
const embeddingMaxChars = 1400

// EmbeddingFunc returns a chromem.EmbeddingFunc that uses the local Hugot pipeline.
// This satisfies the chromem-go interface: func(ctx, text) ([]float32, error).
func (h *HugotEmbedder) EmbeddingFunc() chromem.EmbeddingFunc {
	return func(ctx context.Context, text string) ([]float32, error) {
		h.mu.Lock()
		defer h.mu.Unlock()

		// Truncate to avoid exceeding the model's max_position_embeddings (512
		// tokens). Hugot's Go tokenizer (v0.7.0) does not truncate automatically
		// unlike the Rust tokenizer, causing a panic when token count exceeds 512.
		if len(text) > embeddingMaxChars {
			text = text[:embeddingMaxChars]
		}

		output, err := h.pipeline.RunPipeline([]string{text})
		if err != nil {
			return nil, fmt.Errorf("embedding failed: %w", err)
		}
		if output == nil || len(output.Embeddings) == 0 {
			return nil, fmt.Errorf("embedding returned no results")
		}
		return output.Embeddings[0], nil
	}
}

// Close destroys the Hugot session and frees resources.
func (h *HugotEmbedder) Close() error {
	if h.session != nil {
		return h.session.Destroy()
	}
	return nil
}

// resolveLocalEmbedder creates a local embedding function using the Hugot library.
// This runs entirely in-process using the GoMLX pure-Go backend.
func resolveLocalEmbedder(debugMode bool) (*EmbedderResult, error) {
	modelsDir, err := config.GetModelsDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve models directory: %w", err)
	}

	embedder, err := NewHugotEmbedder(modelsDir, debugMode)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize local embedder: %w", err)
	}

	return &EmbedderResult{
		EmbeddingFunc: embedder.EmbeddingFunc(),
		Cleanup:       embedder.Close,
	}, nil
}
