package api

import (
	"fmt"
	"os"
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
