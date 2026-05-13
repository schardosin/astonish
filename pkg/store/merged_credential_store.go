package store

import "context"

// MergedCredentialStore combines a personal and team credential store,
// implementing the CredentialStore interface with personal-first resolution.
//
// Read operations (Get, Resolve, GetSecret) check personal store first,
// then fall back to the team store. List operations merge results from both
// stores, with personal credentials taking precedence on name conflicts.
//
// Write operations always go to the personal store. This ensures that
// credentials saved from chat (by the LLM) are private by default.
// Team credential management is done explicitly through the Settings UI.
//
// For fleet/headless sessions (no user context), personal is nil and
// only team credentials are available.
type MergedCredentialStore struct {
	Personal CredentialStore // may be nil (headless/fleet sessions)
	Team     CredentialStore // may be nil (personal-only mode)
}

// NewMergedCredentialStore creates a merged credential store.
// Either store can be nil; the other will be used exclusively.
func NewMergedCredentialStore(personal, team CredentialStore) *MergedCredentialStore {
	return &MergedCredentialStore{Personal: personal, Team: team}
}

func (m *MergedCredentialStore) Get(ctx context.Context, name string) *Credential {
	if m.Personal != nil {
		if c := m.Personal.Get(ctx, name); c != nil {
			return c
		}
	}
	if m.Team != nil {
		return m.Team.Get(ctx, name)
	}
	return nil
}

func (m *MergedCredentialStore) Set(ctx context.Context, name string, cred *Credential) error {
	// Writes always go to personal store (private-first).
	// If no personal store (fleet/headless), write to team.
	if m.Personal != nil {
		return m.Personal.Set(ctx, name, cred)
	}
	if m.Team != nil {
		return m.Team.Set(ctx, name, cred)
	}
	return nil
}

func (m *MergedCredentialStore) Remove(ctx context.Context, name string) error {
	// Remove from personal store only (from chat/LLM tools).
	// Team credential removal is admin-only via Settings UI.
	if m.Personal != nil {
		return m.Personal.Remove(ctx, name)
	}
	if m.Team != nil {
		return m.Team.Remove(ctx, name)
	}
	return nil
}

func (m *MergedCredentialStore) List(ctx context.Context) map[string]CredentialType {
	result := make(map[string]CredentialType)
	// Team first, then personal overwrites (personal wins on conflict)
	if m.Team != nil {
		for k, v := range m.Team.List(ctx) {
			result[k] = v
		}
	}
	if m.Personal != nil {
		for k, v := range m.Personal.List(ctx) {
			result[k] = v
		}
	}
	return result
}

func (m *MergedCredentialStore) Count(ctx context.Context) int {
	// Approximate: merged count without dedup
	n := 0
	if m.Personal != nil {
		n += m.Personal.Count(ctx)
	}
	if m.Team != nil {
		n += m.Team.Count(ctx)
	}
	return n
}

func (m *MergedCredentialStore) Resolve(ctx context.Context, name string) (headerKey, headerValue string, err error) {
	if m.Personal != nil {
		if c := m.Personal.Get(ctx, name); c != nil {
			return m.Personal.Resolve(ctx, name)
		}
	}
	if m.Team != nil {
		return m.Team.Resolve(ctx, name)
	}
	return "", "", nil
}

func (m *MergedCredentialStore) SetSecret(ctx context.Context, key, value string) error {
	if m.Personal != nil {
		return m.Personal.SetSecret(ctx, key, value)
	}
	if m.Team != nil {
		return m.Team.SetSecret(ctx, key, value)
	}
	return nil
}

func (m *MergedCredentialStore) SetSecretBatch(ctx context.Context, secrets map[string]string) error {
	if m.Personal != nil {
		return m.Personal.SetSecretBatch(ctx, secrets)
	}
	if m.Team != nil {
		return m.Team.SetSecretBatch(ctx, secrets)
	}
	return nil
}

func (m *MergedCredentialStore) GetSecret(ctx context.Context, key string) string {
	if m.Personal != nil {
		if v := m.Personal.GetSecret(ctx, key); v != "" {
			return v
		}
	}
	if m.Team != nil {
		return m.Team.GetSecret(ctx, key)
	}
	return ""
}

func (m *MergedCredentialStore) RemoveSecret(ctx context.Context, key string) error {
	if m.Personal != nil {
		return m.Personal.RemoveSecret(ctx, key)
	}
	if m.Team != nil {
		return m.Team.RemoveSecret(ctx, key)
	}
	return nil
}

func (m *MergedCredentialStore) HasSecrets(ctx context.Context) bool {
	if m.Personal != nil && m.Personal.HasSecrets(ctx) {
		return true
	}
	if m.Team != nil && m.Team.HasSecrets(ctx) {
		return true
	}
	return false
}

func (m *MergedCredentialStore) SecretCount(ctx context.Context) int {
	n := 0
	if m.Personal != nil {
		n += m.Personal.SecretCount(ctx)
	}
	if m.Team != nil {
		n += m.Team.SecretCount(ctx)
	}
	return n
}

func (m *MergedCredentialStore) ListSecrets(ctx context.Context) []string {
	seen := make(map[string]bool)
	var result []string
	// Personal first (takes precedence)
	if m.Personal != nil {
		for _, s := range m.Personal.ListSecrets(ctx) {
			if !seen[s] {
				seen[s] = true
				result = append(result, s)
			}
		}
	}
	if m.Team != nil {
		for _, s := range m.Team.ListSecrets(ctx) {
			if !seen[s] {
				seen[s] = true
				result = append(result, s)
			}
		}
	}
	return result
}

func (m *MergedCredentialStore) Reload(ctx context.Context) error {
	if m.Personal != nil {
		if err := m.Personal.Reload(ctx); err != nil {
			return err
		}
	}
	if m.Team != nil {
		return m.Team.Reload(ctx)
	}
	return nil
}

// Compile-time check.
var _ CredentialStore = (*MergedCredentialStore)(nil)
