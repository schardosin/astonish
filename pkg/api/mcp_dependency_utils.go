package api

import "github.com/SAP/astonish/pkg/config"

func mcpDependenciesEqual(a, b []config.MCPDependency) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Server != b[i].Server || a[i].Source != b[i].Source || a[i].StoreID != b[i].StoreID {
			return false
		}
		if len(a[i].Tools) != len(b[i].Tools) {
			return false
		}
		for j := range a[i].Tools {
			if a[i].Tools[j] != b[i].Tools[j] {
				return false
			}
		}
	}
	return true
}
