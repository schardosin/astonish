package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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

// NewHugotEmbedder creates a local embedding pipeline using Hugot's pure Go backend.
// modelsDir is where ONNX models are stored (e.g., ~/.config/astonish/models/).
// The model is downloaded from HuggingFace on first use (~23 MB).
func NewHugotEmbedder(modelsDir string, debugMode bool) (*HugotEmbedder, error) {
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
			fmt.Printf("[Hugot] Model: %s\n", DefaultEmbeddingModel)
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
		fmt.Printf("[Hugot] Using cached model at %s\n", modelPath)
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

// EmbeddingFunc returns a chromem.EmbeddingFunc that uses the local Hugot pipeline.
// This satisfies the chromem-go interface: func(ctx, text) ([]float32, error).
func (h *HugotEmbedder) EmbeddingFunc() chromem.EmbeddingFunc {
	return func(ctx context.Context, text string) ([]float32, error) {
		h.mu.Lock()
		defer h.mu.Unlock()

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
