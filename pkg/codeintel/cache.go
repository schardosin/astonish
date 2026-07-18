package codeintel

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type diskCache struct {
	Files  map[string]fileCacheEntry `json:"files"`
	Graphs map[string]*ScopeGraph    `json:"graphs"`
	Ranks  map[string]float64        `json:"ranks"`
}

type fileCacheEntry struct {
	Size    int64 `json:"size"`
	ModUnix int64 `json:"mod_unix"`
}

func loadDiskCache(root string, files []string) (*Index, bool) {
	data, err := os.ReadFile(cachePath(root))
	if err != nil {
		return nil, false
	}
	var cached diskCache
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}
	current, ok := fileEntries(root, files)
	if !ok || len(current) != len(cached.Files) {
		return nil, false
	}
	for file, entry := range current {
		if cached.Files[file] != entry {
			return nil, false
		}
	}
	idx := &Index{
		Root:       root,
		Graphs:     cached.Graphs,
		DefsByName: make(map[string][]Definition),
		RefsByName: make(map[string][]Reference),
		Ranks:      cached.Ranks,
	}
	for _, graph := range idx.Graphs {
		for _, def := range graph.Defs {
			idx.DefsByName[def.Name] = append(idx.DefsByName[def.Name], def)
		}
		for _, ref := range graph.Refs {
			idx.RefsByName[ref.Name] = append(idx.RefsByName[ref.Name], ref)
		}
	}
	return idx, true
}

func saveDiskCache(root string, files []string, idx *Index) error {
	entries, ok := fileEntries(root, files)
	if !ok {
		return nil
	}
	cache := diskCache{Files: entries, Graphs: idx.Graphs, Ranks: idx.Ranks}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	path := cachePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func fileEntries(root string, files []string) (map[string]fileCacheEntry, bool) {
	entries := make(map[string]fileCacheEntry, len(files))
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return nil, false
		}
		entries[relTo(root, file)] = fileCacheEntry{Size: info.Size(), ModUnix: info.ModTime().UnixNano()}
	}
	return entries, true
}

func cachePath(root string) string {
	return filepath.Join(root, ".codeintel", "index.json")
}
