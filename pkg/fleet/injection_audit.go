package fleet

import "log/slog"

// LogInjectionAudit records a fleet credential injection event without secret values.
func LogInjectionAudit(planKey, sessionID, logicalCred, injectedAs, target, format string) {
	slog.Info("fleet credential injected",
		"component", "fleet-injection-audit",
		"plan_key", planKey,
		"session_id", sessionID,
		"logical_cred", logicalCred,
		"injected_as", injectedAs,
		"target", target,
		"format", format,
	)
}
