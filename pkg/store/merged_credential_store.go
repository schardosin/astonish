package store

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

func (m *MergedCredentialStore) Get(name string) *Credential {
	if m.Personal != nil {
		if c := m.Personal.Get(name); c != nil {
			return c
		}
	}
	if m.Team != nil {
		return m.Team.Get(name)
	}
	return nil
}

func (m *MergedCredentialStore) Set(name string, cred *Credential) error {
	// Writes always go to personal store (private-first).
	// If no personal store (fleet/headless), write to team.
	if m.Personal != nil {
		return m.Personal.Set(name, cred)
	}
	if m.Team != nil {
		return m.Team.Set(name, cred)
	}
	return nil
}

func (m *MergedCredentialStore) Remove(name string) error {
	// Remove from personal store only (from chat/LLM tools).
	// Team credential removal is admin-only via Settings UI.
	if m.Personal != nil {
		return m.Personal.Remove(name)
	}
	if m.Team != nil {
		return m.Team.Remove(name)
	}
	return nil
}

func (m *MergedCredentialStore) List() map[string]CredentialType {
	result := make(map[string]CredentialType)
	// Team first, then personal overwrites (personal wins on conflict)
	if m.Team != nil {
		for k, v := range m.Team.List() {
			result[k] = v
		}
	}
	if m.Personal != nil {
		for k, v := range m.Personal.List() {
			result[k] = v
		}
	}
	return result
}

func (m *MergedCredentialStore) Count() int {
	// Approximate: merged count without dedup
	n := 0
	if m.Personal != nil {
		n += m.Personal.Count()
	}
	if m.Team != nil {
		n += m.Team.Count()
	}
	return n
}

func (m *MergedCredentialStore) Resolve(name string) (headerKey, headerValue string, err error) {
	if m.Personal != nil {
		if c := m.Personal.Get(name); c != nil {
			return m.Personal.Resolve(name)
		}
	}
	if m.Team != nil {
		return m.Team.Resolve(name)
	}
	return "", "", nil
}

func (m *MergedCredentialStore) SetSecret(key, value string) error {
	if m.Personal != nil {
		return m.Personal.SetSecret(key, value)
	}
	if m.Team != nil {
		return m.Team.SetSecret(key, value)
	}
	return nil
}

func (m *MergedCredentialStore) SetSecretBatch(secrets map[string]string) error {
	if m.Personal != nil {
		return m.Personal.SetSecretBatch(secrets)
	}
	if m.Team != nil {
		return m.Team.SetSecretBatch(secrets)
	}
	return nil
}

func (m *MergedCredentialStore) GetSecret(key string) string {
	if m.Personal != nil {
		if v := m.Personal.GetSecret(key); v != "" {
			return v
		}
	}
	if m.Team != nil {
		return m.Team.GetSecret(key)
	}
	return ""
}

func (m *MergedCredentialStore) RemoveSecret(key string) error {
	if m.Personal != nil {
		return m.Personal.RemoveSecret(key)
	}
	if m.Team != nil {
		return m.Team.RemoveSecret(key)
	}
	return nil
}

func (m *MergedCredentialStore) HasSecrets() bool {
	if m.Personal != nil && m.Personal.HasSecrets() {
		return true
	}
	if m.Team != nil && m.Team.HasSecrets() {
		return true
	}
	return false
}

func (m *MergedCredentialStore) SecretCount() int {
	n := 0
	if m.Personal != nil {
		n += m.Personal.SecretCount()
	}
	if m.Team != nil {
		n += m.Team.SecretCount()
	}
	return n
}

func (m *MergedCredentialStore) ListSecrets() []string {
	seen := make(map[string]bool)
	var result []string
	// Personal first (takes precedence)
	if m.Personal != nil {
		for _, s := range m.Personal.ListSecrets() {
			if !seen[s] {
				seen[s] = true
				result = append(result, s)
			}
		}
	}
	if m.Team != nil {
		for _, s := range m.Team.ListSecrets() {
			if !seen[s] {
				seen[s] = true
				result = append(result, s)
			}
		}
	}
	return result
}

func (m *MergedCredentialStore) Reload() error {
	if m.Personal != nil {
		if err := m.Personal.Reload(); err != nil {
			return err
		}
	}
	if m.Team != nil {
		return m.Team.Reload()
	}
	return nil
}

// Compile-time check.
var _ CredentialStore = (*MergedCredentialStore)(nil)
