package codeintel

import (
	"context"
	"os"
	"sync"
	"time"
)

var cache sync.Map

type cachedIndex struct {
	idx     *Index
	builtAt time.Time
}

func GetIndex(ctx context.Context, root string) (*BuildResult, error) {
	if value, ok := cache.Load(root); ok {
		cached := value.(cachedIndex)
		if time.Since(cached.builtAt) < 30*time.Second {
			return &BuildResult{Index: cached.idx, FilesRanked: len(cached.idx.Ranks)}, nil
		}
	}
	result, err := Build(ctx, root)
	if err != nil {
		return nil, err
	}
	cache.Store(root, cachedIndex{idx: result.Index, builtAt: time.Now()})
	return result, nil
}

func Invalidate(root string) {
	cache.Delete(root)
	if root != "" {
		_ = os.Remove(cachePath(root))
	}
}
