package daemon

import (
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/credentials"
)

// daemonSecretGetter returns a SecretGetter function from the platform backend.
// In platform mode it reads from the platform_secrets table; falls back to
// the file-based credential store if available.
// fileStore may be nil (e.g. when the encrypted store could not be opened after upgrade).
func daemonSecretGetter(backend platformDB, fileStore *credentials.Store) config.SecretGetter {
	if backend != nil {
		dbGetter := backend.SecretGetter()
		if fileStore != nil {
			// Prefer DB, fall back to file store
			return func(key string) string {
				if val := dbGetter(key); val != "" {
					return val
				}
				return fileStore.GetSecret(key)
			}
		}
		return dbGetter
	}
	if fileStore != nil {
		return fileStore.GetSecret
	}
	return nil
}

// resolveDaemonSecret resolves a single secret key using the backend + fallback.
func resolveDaemonSecret(backend platformDB, fileStore *credentials.Store, key string) string {
	getter := daemonSecretGetter(backend, fileStore)
	if getter == nil {
		return ""
	}
	return getter(key)
}
