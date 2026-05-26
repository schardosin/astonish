package daemon

import (
	"github.com/schardosin/astonish/pkg/config"
)

// daemonSecretGetter returns a SecretGetter function from the platform backend.
// In platform mode it reads from the platform_secrets table; falls back to
// the file-based credential store if available.
func daemonSecretGetter(backend platformDB, fileStore secretGetter) config.SecretGetter {
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
func resolveDaemonSecret(backend platformDB, fileStore secretGetter, key string) string {
	getter := daemonSecretGetter(backend, fileStore)
	if getter == nil {
		return ""
	}
	return getter(key)
}

// secretGetter is satisfied by *credentials.Store.
type secretGetter interface {
	GetSecret(key string) string
}
