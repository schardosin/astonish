package memory

import (
	"context"
	"log/slog"
	"os"

	chromem "github.com/philippgille/chromem-go"
	"github.com/schardosin/astonish/pkg/config"
)

// DaemonIndexerResult holds the resources created by StartDaemonIndexer.
type DaemonIndexerResult struct {
	Store         *Store
	Indexer       *Indexer
	EmbeddingFunc chromem.EmbeddingFunc
	Cleanup       func() // stops watcher and releases embedder resources
}

// StartDaemonIndexer initializes the memory vector store and indexer as a
// standalone background service. It runs IndexAll() in a goroutine and starts
// the file watcher for live sync. This is intended for the daemon process,
// which must maintain the vector index regardless of whether a ChatAgent is
// created (channels may or may not be enabled).
//
// Returns nil (no error) if memory is disabled or the memory directory cannot
// be resolved — the daemon simply runs without semantic memory indexing.
func StartDaemonIndexer(ctx context.Context, appCfg *config.AppConfig, debugMode bool, getSecret config.SecretGetter) (*DaemonIndexerResult, error) {
	if appCfg == nil || !appCfg.Memory.IsMemoryEnabled() {
		return nil, nil
	}

	memCfg := &appCfg.Memory

	memDir, mdErr := config.GetMemoryDir(memCfg)
	vecDir, vdErr := config.GetVectorDir(memCfg)
	if mdErr != nil || vdErr != nil {
		if debugMode {
			slog.Warn("cannot resolve memory directories", "memDirErr", mdErr, "vecDirErr", vdErr)
		}
		return nil, nil
	}

	if err := os.MkdirAll(memDir, 0755); err != nil {
		if debugMode {
			slog.Warn("failed to create memory directory", "error", err)
		}
		return nil, nil
	}

	embResult, embErr := ResolveEmbeddingFunc(appCfg, memCfg, debugMode, getSecret)
	if embErr != nil {
		if debugMode {
			slog.Warn("semantic memory unavailable", "error", embErr)
		}
		return nil, nil
	}

	storeCfg := DefaultStoreConfig()
	storeCfg.MemoryDir = memDir
	storeCfg.VectorDir = vecDir
	if memCfg.Chunking.MaxChars > 0 {
		storeCfg.ChunkMaxChars = memCfg.Chunking.MaxChars
	}
	if memCfg.Chunking.Overlap > 0 {
		storeCfg.ChunkOverlap = memCfg.Chunking.Overlap
	}
	if memCfg.Search.MaxResults > 0 {
		storeCfg.MaxResults = memCfg.Search.MaxResults
	}
	if memCfg.Search.MinScore > 0 {
		storeCfg.MinScore = memCfg.Search.MinScore
	}

	store, storeErr := NewStore(storeCfg, embResult.EmbeddingFunc)
	if storeErr != nil {
		if debugMode {
			slog.Warn("failed to create memory store", "error", storeErr)
		}
		return nil, nil
	}

	indexer := NewIndexer(store, storeCfg, debugMode)
	store.SetIndexer(indexer)

	var cleanups []func()
	if embResult.Cleanup != nil {
		cleanups = append(cleanups, func() { _ = embResult.Cleanup() })
	}

	// Run initial indexing in background
	go func() {
		if err := indexer.IndexAll(context.Background()); err != nil {
			slog.Warn("memory indexing failed", "component", "daemon-indexer", "error", err)
		} else {
			slog.Info("memory indexing complete", "component", "daemon-indexer", "files", store.Count())
		}
	}()

	// Start file watcher for live sync
	if memCfg.IsWatchEnabled() {
		debounceMs := memCfg.Sync.DebounceMs
		if debounceMs <= 0 {
			debounceMs = 1500
		}
		watchCtx, watchCancel := context.WithCancel(ctx)
		cleanups = append(cleanups, watchCancel)
		slog.Info("starting memory file watcher", "component", "daemon-indexer", "debounce_ms", debounceMs)
		go func() {
			wErr := indexer.WatchAndSync(watchCtx, debounceMs)
			if wErr != nil {
				slog.Warn("memory file watcher exited with error", "component", "daemon-indexer", "error", wErr)
			} else {
				slog.Info("memory file watcher stopped", "component", "daemon-indexer")
			}
		}()
	}

	return &DaemonIndexerResult{
		Store:         store,
		Indexer:       indexer,
		EmbeddingFunc: embResult.EmbeddingFunc,
		Cleanup: func() {
			for _, fn := range cleanups {
				fn()
			}
		},
	}, nil
}
