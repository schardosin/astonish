package browser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AccountStatus represents the state of a portal account.
type AccountStatus string

const (
	AccountPending   AccountStatus = "pending"   // Registration started but not completed
	AccountVerifying AccountStatus = "verifying" // Waiting for email verification
	AccountActive    AccountStatus = "active"    // Successfully registered and verified
	AccountSuspended AccountStatus = "suspended" // Account suspended or banned
	AccountFailed    AccountStatus = "failed"    // Registration failed
)

// Account represents a registered account on a web portal.
type Account struct {
	Portal         string        `json:"portal"`                    // Portal domain (e.g. "reddit.com")
	Username       string        `json:"username"`                  // Actual username used
	Email          string        `json:"email,omitempty"`           // Email used for registration
	CredentialName string        `json:"credential_name,omitempty"` // Reference to credential store entry
	Status         AccountStatus `json:"status"`                    // Current account status
	ProfileURL     string        `json:"profile_url,omitempty"`     // URL to account profile
	Notes          string        `json:"notes,omitempty"`           // Free-form notes (e.g. "email verified")
	RegisteredAt   time.Time     `json:"registered_at"`             // When the account was created
	UpdatedAt      time.Time     `json:"updated_at"`                // Last status update
}

// AccountStore manages persisted account state in a JSON file.
// It is safe for concurrent use.
type AccountStore struct {
	mu       sync.RWMutex
	filePath string
	accounts map[string]*Account // keyed by "portal:username"
}

// NewAccountStore creates or loads an account store from the given directory.
// The store file is created automatically on first Save.
func NewAccountStore(configDir string) (*AccountStore, error) {
	fp := filepath.Join(configDir, "accounts.json")
	s := &AccountStore{
		filePath: fp,
		accounts: make(map[string]*Account),
	}

	// Load existing data if file exists
	data, err := os.ReadFile(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("failed to read accounts file: %w", err)
	}

	var accounts []*Account
	if err := json.Unmarshal(data, &accounts); err != nil {
		return nil, fmt.Errorf("failed to parse accounts file: %w", err)
	}
	for _, a := range accounts {
		s.accounts[accountKey(a.Portal, a.Username)] = a
	}
	return s, nil
}

// accountKey creates a unique key for a portal+username combination.
func accountKey(portal, username string) string {
	return portal + ":" + username
}

// Save persists an account (insert or update) and writes to disk.
func (s *AccountStore) Save(account *Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	account.UpdatedAt = time.Now()
	if account.RegisteredAt.IsZero() {
		account.RegisteredAt = account.UpdatedAt
	}
	s.accounts[accountKey(account.Portal, account.Username)] = account
	return s.writeLocked()
}

// Get retrieves an account by portal and username.
// Returns nil if not found.
func (s *AccountStore) Get(portal, username string) *Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accounts[accountKey(portal, username)]
}

// GetByPortal returns all accounts for a given portal domain.
func (s *AccountStore) GetByPortal(portal string) []*Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Account
	for _, a := range s.accounts {
		if a.Portal == portal {
			result = append(result, a)
		}
	}
	return result
}

// List returns all tracked accounts.
func (s *AccountStore) List() []*Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		result = append(result, a)
	}
	return result
}

// UpdateStatus changes the status of an existing account.
func (s *AccountStore) UpdateStatus(portal, username string, status AccountStatus, notes string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := accountKey(portal, username)
	a, ok := s.accounts[key]
	if !ok {
		return fmt.Errorf("account not found: %s@%s", username, portal)
	}

	a.Status = status
	if notes != "" {
		a.Notes = notes
	}
	a.UpdatedAt = time.Now()
	return s.writeLocked()
}

// Delete removes an account from the store.
func (s *AccountStore) Delete(portal, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := accountKey(portal, username)
	if _, ok := s.accounts[key]; !ok {
		return fmt.Errorf("account not found: %s@%s", username, portal)
	}

	delete(s.accounts, key)
	return s.writeLocked()
}

// writeLocked writes the current state to disk. Must be called with mu held.
func (s *AccountStore) writeLocked() error {
	accounts := make([]*Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		accounts = append(accounts, a)
	}

	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal accounts: %w", err)
	}

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create accounts directory: %w", err)
	}

	return os.WriteFile(s.filePath, data, 0644)
}
