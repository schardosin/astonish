package codeintel

func pageRank(graphs map[string]*ScopeGraph, edges map[string]map[string]float64) map[string]float64 {
	n := len(graphs)
	if n == 0 {
		return nil
	}

	ranks := make(map[string]float64, n)
	base := 1.0 / float64(n)
	for file := range graphs {
		ranks[file] = base
	}

	const damping = 0.85
	for range 20 {
		next := make(map[string]float64, n)
		for file := range graphs {
			next[file] = (1 - damping) / float64(n)
		}
		for from, tos := range edges {
			var total float64
			for _, weight := range tos {
				total += weight
			}
			if total == 0 {
				continue
			}
			for to, weight := range tos {
				next[to] += damping * ranks[from] * (weight / total)
			}
		}
		ranks = next
	}
	return ranks
}
