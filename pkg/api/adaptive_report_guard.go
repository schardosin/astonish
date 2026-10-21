package api

import (
	"fmt"
	"net/http"
	"os"

	"github.com/SAP/astonish/pkg/store"
)

// guardedHostReadSessionFile builds the ReadSessionFile callback used by the
// scheduler executor for adaptive-report delivery. The report path originates
// from the model's astonish-report fence, so a job prompt or prompt injection
// can name an arbitrary host path (e.g. /proc/self/environ) and exfiltrate the
// server's secrets into the job result.
//
// SECURITY (ART containment): in multi-tenant platform mode the server process
// is shared across tenants and a host read is never legitimate — captured
// write_file args already supply report bodies there — so the callback denies
// the read. This gates on platform mode (postgres OR sqlite), NOT on the
// Postgres backend alone, because a SQLite platform deployment is equally
// multi-user. Trusted single-tenant (file backend / local Code) deployments
// retain the host fallback.
//
// Both the daemon scheduler (pkg/daemon) and the chat schedule_job test-run
// path (StudioChatHandler) must construct the callback this way; neither may
// hand the executor a bare os.ReadFile closure.
func guardedHostReadSessionFile(platformMode bool) func(string, string) ([]byte, error) {
	return func(_, path string) ([]byte, error) {
		if platformMode {
			return nil, fmt.Errorf("adaptive report host-file read is disabled in platform mode")
		}
		return os.ReadFile(path)
	}
}

// requestIsPlatformMode reports whether the request is served in multi-tenant
// platform mode, decided from the request's resolved store (the authoritative
// in-memory mode) rather than re-reading config.yaml from disk.
//
// This matters for older installs: the daemon may switch to a SQLite
// auth-enabled multi-user store in memory while config.yaml still records an
// empty/file backend. A config-file check (appCfg.Storage.IsPlatformMode)
// would read "not platform" there and wrongly allow the host read. The
// scheduler test-run path must use THIS predicate, not the config file.
//
// It is fail-closed: a resolved platform store OR the presence of an
// authenticated platform user both count as platform mode. The host read is
// only permitted when neither is true (trusted single-tenant / local Code).
func requestIsPlatformMode(r *http.Request) bool {
	if svc := store.FromRequest(r); svc != nil && svc.Mode == store.ModePlatform {
		return true
	}
	if GetPlatformUser(r) != nil {
		return true
	}
	return false
}
