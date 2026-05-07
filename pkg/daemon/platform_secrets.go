package daemon

import (
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// resolvePlatformSecret retrieves a secret from the platform_secrets table in
// the platform database. In platform mode, instance-wide secrets (bot tokens,
// passwords, API keys) live here — not in the file-based credential store and
// not in any org/team-scoped store.
func resolvePlatformSecret(pgStore *pgstore.PGStore, key string) string {
	return pgStore.PlatformSecrets().GetSecret(key)
}

// resolveDaemonSecret resolves a secret key using the appropriate store for the
// current mode. In platform mode (pgStore != nil), it reads from the platform
// secrets table in the DB. In personal mode, it reads from the file-based
// credential store.
func resolveDaemonSecret(pgStore *pgstore.PGStore, _ *config.AppConfig, fileStore secretGetter, key string) string {
	if pgStore != nil {
		// Platform mode: platform_secrets table is the source of truth
		return resolvePlatformSecret(pgStore, key)
	}

	// Personal mode: file-based credential store
	if fileStore != nil {
		return fileStore.GetSecret(key)
	}

	return ""
}

// daemonSecretGetter returns a SecretGetter function appropriate for the
// current mode. In platform mode it reads from the platform_secrets table;
// in personal mode it reads from the file-based credential store.
func daemonSecretGetter(pgStore *pgstore.PGStore, _ *config.AppConfig, fileStore secretGetter) config.SecretGetter {
	if pgStore != nil {
		return func(key string) string {
			return resolvePlatformSecret(pgStore, key)
		}
	}
	if fileStore != nil {
		return fileStore.GetSecret
	}
	return nil
}

// secretGetter is satisfied by *credentials.Store.
type secretGetter interface {
	GetSecret(key string) string
}
