package fleet

import (
	"context"
	"fmt"

	"github.com/SAP/astonish/pkg/store"
)

// PlanBoundCredentialStore wraps a team CredentialStore and only exposes
// credentials declared in a fleet plan's credentials map (store names).
// Used for fleet session runtime — regular Chat keeps the full merged store.
type PlanBoundCredentialStore struct {
	inner   store.CredentialStore
	allowed map[string]struct{} // store names from plan.Credentials values
}

// NewPlanBoundCredentialStore returns a plan-scoped credential store.
// Returns nil if inner is nil or plan has no credentials (caller may use inner directly).
func NewPlanBoundCredentialStore(inner store.CredentialStore, plan *FleetPlan) store.CredentialStore {
	if inner == nil {
		return nil
	}
	if plan == nil || len(plan.Credentials) == 0 {
		return inner
	}
	allowed := make(map[string]struct{}, len(plan.Credentials))
	for _, storeName := range plan.Credentials {
		if storeName != "" {
			allowed[storeName] = struct{}{}
		}
	}
	return &PlanBoundCredentialStore{inner: inner, allowed: allowed}
}

func (s *PlanBoundCredentialStore) allowedName(name string) bool {
	if s == nil || len(s.allowed) == 0 {
		return false
	}
	_, ok := s.allowed[name]
	return ok
}

func (s *PlanBoundCredentialStore) Get(ctx context.Context, name string) *store.Credential {
	if !s.allowedName(name) {
		return nil
	}
	return s.inner.Get(ctx, name)
}

func (s *PlanBoundCredentialStore) Set(ctx context.Context, name string, cred *store.Credential) error {
	return fmt.Errorf("fleet plan-bound credential store: cannot save credentials during fleet session")
}

func (s *PlanBoundCredentialStore) Remove(ctx context.Context, name string) error {
	return fmt.Errorf("fleet plan-bound credential store: cannot remove credentials during fleet session")
}

func (s *PlanBoundCredentialStore) List(ctx context.Context) map[string]store.CredentialType {
	all := s.inner.List(ctx)
	if len(all) == 0 {
		return nil
	}
	out := make(map[string]store.CredentialType, len(s.allowed))
	for name, typ := range all {
		if s.allowedName(name) {
			out[name] = typ
		}
	}
	return out
}

func (s *PlanBoundCredentialStore) Count(ctx context.Context) int {
	return len(s.List(ctx))
}

func (s *PlanBoundCredentialStore) Resolve(ctx context.Context, name string) (string, string, error) {
	if !s.allowedName(name) {
		return "", "", fmt.Errorf("credential %q is not declared in this fleet plan", name)
	}
	return s.inner.Resolve(ctx, name)
}

func (s *PlanBoundCredentialStore) InvalidateToken(ctx context.Context, name string) {
	if s.allowedName(name) {
		s.inner.InvalidateToken(ctx, name)
	}
}

func (s *PlanBoundCredentialStore) SetSecret(ctx context.Context, key, value string) error {
	return fmt.Errorf("fleet plan-bound credential store: cannot save secrets during fleet session")
}

func (s *PlanBoundCredentialStore) SetSecretBatch(ctx context.Context, secrets map[string]string) error {
	return fmt.Errorf("fleet plan-bound credential store: cannot save secrets during fleet session")
}

func (s *PlanBoundCredentialStore) GetSecret(ctx context.Context, key string) string {
	if !s.allowedName(key) {
		return ""
	}
	return s.inner.GetSecret(ctx, key)
}

func (s *PlanBoundCredentialStore) RemoveSecret(ctx context.Context, key string) error {
	return fmt.Errorf("fleet plan-bound credential store: cannot remove secrets during fleet session")
}

func (s *PlanBoundCredentialStore) HasSecrets(ctx context.Context) bool {
	for key := range s.allowed {
		if s.inner.GetSecret(ctx, key) != "" {
			return true
		}
	}
	return false
}

func (s *PlanBoundCredentialStore) SecretCount(ctx context.Context) int {
	n := 0
	for key := range s.allowed {
		if s.inner.GetSecret(ctx, key) != "" {
			n++
		}
	}
	return n
}

func (s *PlanBoundCredentialStore) ListSecrets(ctx context.Context) []string {
	var keys []string
	for key := range s.allowed {
		if s.inner.GetSecret(ctx, key) != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func (s *PlanBoundCredentialStore) Reload(ctx context.Context) error {
	return s.inner.Reload(ctx)
}

// AllowedStoreNames returns the set of store credential names allowed by the plan.
func (s *PlanBoundCredentialStore) AllowedStoreNames() map[string]struct{} {
	if s == nil {
		return nil
	}
	return s.allowed
}
