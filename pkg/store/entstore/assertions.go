package entstore

import "github.com/schardosin/astonish/pkg/store"

// Compile-time assertions for top-level store interfaces.
// These will cause build failures if any method is missing.
var (
	_ store.PlatformStore   = (*Store)(nil)
	_ store.TenantRouter    = (*Store)(nil)
	_ store.PlatformBackend = (*Store)(nil)

	_ store.OrgDataStore      = (*orgDataStore)(nil)
	_ store.TeamDataStore     = (*teamDataStore)(nil)
	_ store.PersonalDataStore = (*personalDataStore)(nil)
)
