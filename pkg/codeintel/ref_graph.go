package codeintel

import "math"

func computeRanks(graphs map[string]*ScopeGraph) map[string]float64 {
	if len(graphs) == 0 {
		return nil
	}

	defFiles := make(map[string]map[string]bool)
	for file, graph := range graphs {
		for _, def := range graph.Defs {
			if defFiles[def.Name] == nil {
				defFiles[def.Name] = make(map[string]bool)
			}
			defFiles[def.Name][file] = true
		}
	}

	edges := make(map[string]map[string]float64)
	n := float64(len(graphs))
	for from, graph := range graphs {
		for _, ref := range graph.Refs {
			files := defFiles[ref.Name]
			if len(files) == 0 {
				continue
			}
			idf := math.Log((n + 1) / (float64(len(files)) + 1))
			if idf <= 0 {
				idf = 0.1
			}
			for to := range files {
				if from == to {
					continue
				}
				if edges[from] == nil {
					edges[from] = make(map[string]float64)
				}
				edges[from][to] += idf
			}
		}
	}

	return pageRank(graphs, edges)
}
