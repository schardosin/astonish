package fleet

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/schardosin/astonish/pkg/sandbox/openshell"
	"github.com/schardosin/astonish/pkg/store"
)

// OpenShellProviderBinding tracks providers attached to a fleet sandbox session.
type OpenShellProviderBinding struct {
	gateway       openshell.GatewayClient
	sandboxName   string
	providerNames []string
	planKey       string
	sessionID     string
}

// AttachOpenShellProviders creates and attaches OpenShell providers for plan credentials.
// Env injection entries for each logical credential become provider credential keys.
func AttachOpenShellProviders(
	ctx context.Context,
	gateway openshell.GatewayClient,
	sandboxName, sessionID string,
	plan *FleetPlan,
	cs store.CredentialStore,
) (*OpenShellProviderBinding, error) {
	if gateway == nil || plan == nil || sandboxName == "" {
		return nil, nil
	}
	if len(plan.Credentials) == 0 {
		return nil, nil
	}

	binding := &OpenShellProviderBinding{
		gateway:     gateway,
		sandboxName: sandboxName,
		planKey:     plan.Key,
		sessionID:   sessionID,
	}

	inj := plan.EffectiveCredentialInjection()
	attached := make(map[string]bool)

	for logicalName := range plan.Credentials {
		creds := make(map[string]string)
		for _, spec := range inj.Env {
			if spec.Credential != logicalName || spec.Var == "" {
				continue
			}
			storeName := plan.Credentials[logicalName]
			val, err := extractCredentialField(ctx, cs, storeName, spec.Field, nil)
			if err != nil || val == "" {
				continue
			}
			creds[spec.Var] = val
		}
		if len(creds) == 0 {
			continue
		}

		providerName := fmt.Sprintf("fleet-%s-%s", truncateSessionID(sessionID), sanitizeProviderSuffix(logicalName))
		if err := gateway.CreateProvider(ctx, providerName, "generic", creds); err != nil {
			binding.DetachAll(context.Background()) //nolint:errcheck
			return nil, fmt.Errorf("CreateProvider(%s): %w", providerName, err)
		}
		if err := gateway.AttachSandboxProvider(ctx, sandboxName, providerName); err != nil {
			_ = gateway.DeleteProvider(ctx, providerName)
			binding.DetachAll(context.Background()) //nolint:errcheck
			return nil, fmt.Errorf("AttachSandboxProvider(%s): %w", providerName, err)
		}
		binding.providerNames = append(binding.providerNames, providerName)
		attached[logicalName] = true
		LogInjectionAudit(plan.Key, sessionID, logicalName, "provider", providerName, "")
	}

	if len(binding.providerNames) == 0 {
		return nil, nil
	}
	slog.Info("openshell providers attached for fleet session",
		"component", "fleet-injection",
		"session_id", sessionID,
		"plan", plan.Key,
		"count", len(binding.providerNames),
	)
	return binding, nil
}

// DetachAll detaches and deletes all providers created for the session.
func (b *OpenShellProviderBinding) DetachAll(ctx context.Context) {
	if b == nil || b.gateway == nil {
		return
	}
	for _, name := range b.providerNames {
		if err := b.gateway.DetachSandboxProvider(ctx, b.sandboxName, name); err != nil {
			slog.Warn("failed to detach openshell provider", "component", "fleet-injection", "provider", name, "error", err)
		}
		if err := b.gateway.DeleteProvider(ctx, name); err != nil {
			slog.Warn("failed to delete openshell provider", "component", "fleet-injection", "provider", name, "error", err)
		}
	}
	b.providerNames = nil
}

func truncateSessionID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func sanitizeProviderSuffix(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer(" ", "-", "_", "-", ".", "-").Replace(s)
	if s == "" {
		return "cred"
	}
	return s
}
