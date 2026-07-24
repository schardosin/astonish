package entstore

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/SAP/astonish/pkg/backup"
	"github.com/SAP/astonish/pkg/credentials"
)

const (
	restoreCredentialStatusOK              = "ok"
	restoreCredentialStatusMissingKey      = "missing-key"
	restoreCredentialStatusWrongKey        = "wrong-key"
	restoreCredentialStatusPlaintextLegacy = "legacy/plaintext"
	restoreCredentialStatusUnknown         = "unknown"
)

type restoreCredentialCheck struct {
	OrgSlug        string
	Status         string
	CredentialRows int64
	Message        string
}

type restoreCredentialOrgState struct {
	orgSlug             string
	keyData             []byte
	credentialRows      int64
	sampleCredentialRow []byte
}

func inspectRestoreCredentialKeys(_ context.Context, archivePath string, summary backup.Summary, opts PlatformRestoreOptions) ([]restoreCredentialCheck, error) {
	states := make(map[string]*restoreCredentialOrgState)
	entriesByPath := make(map[string]backup.Entry, len(summary.Manifest.Entries))
	for _, entry := range summary.Manifest.Entries {
		if entry.Entity != "org_encryption_keys" && entry.Entity != "credentials" {
			continue
		}
		entriesByPath[entry.Path] = entry
		orgSlug := mappedRestoreScope(entry.Scope, opts).OrgSlug
		if orgSlug == "" {
			continue
		}
		state := states[orgSlug]
		if state == nil {
			state = &restoreCredentialOrgState{orgSlug: orgSlug}
			states[orgSlug] = state
		}
		if entry.Entity == "credentials" {
			state.credentialRows += entry.Records
		}
	}
	if len(states) == 0 {
		return nil, nil
	}

	reader, err := backup.OpenReader(archivePath, backup.ReaderOptions{Passphrase: opts.Passphrase})
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	if err := reader.ForEachFile(func(path string, r io.Reader) error {
		entry, ok := entriesByPath[path]
		if !ok {
			return nil
		}
		orgSlug := mappedRestoreScope(entry.Scope, opts).OrgSlug
		state := states[orgSlug]
		if state == nil {
			return nil
		}
		switch entry.Entity {
		case "org_encryption_keys":
			return inspectRestoreOrgCredentialKey(entry, r, state)
		case "credentials":
			return inspectRestoreCredentialRows(entry, r, state)
		default:
			return nil
		}
	}); err != nil {
		return nil, err
	}

	masterKey := loadMasterKey()
	checks := make([]restoreCredentialCheck, 0, len(states))
	for _, state := range states {
		check := evaluateRestoreCredentialState(state, masterKey)
		checks = append(checks, check)
	}
	return checks, nil
}

func inspectRestoreOrgCredentialKey(entry backup.Entry, r io.Reader, state *restoreCredentialOrgState) error {
	scanner := backup.NewRecordScanner(r, entry.Entity)
	for scanner.Next() {
		record, err := scanner.Record()
		if err != nil {
			return err
		}
		row, err := backup.DecodeRecordData(record)
		if err != nil {
			return err
		}
		if fmt.Sprint(row["key_name"]) != "credential_key" {
			continue
		}
		keyData, ok := restoreBytesFromValue(row["key_data"])
		if !ok || len(keyData) == 0 {
			continue
		}
		state.keyData = keyData
		break
	}
	return scanner.Err()
}

func inspectRestoreCredentialRows(entry backup.Entry, r io.Reader, state *restoreCredentialOrgState) error {
	if state.sampleCredentialRow != nil {
		return nil
	}
	scanner := backup.NewRecordScanner(r, entry.Entity)
	for scanner.Next() {
		record, err := scanner.Record()
		if err != nil {
			return err
		}
		row, err := backup.DecodeRecordData(record)
		if err != nil {
			return err
		}
		encrypted, ok := restoreBytesFromValue(row["encrypted"])
		if !ok || len(encrypted) == 0 {
			continue
		}
		state.sampleCredentialRow = encrypted
		break
	}
	return scanner.Err()
}

func evaluateRestoreCredentialState(state *restoreCredentialOrgState, masterKey []byte) restoreCredentialCheck {
	check := restoreCredentialCheck{
		OrgSlug:        state.orgSlug,
		CredentialRows: state.credentialRows,
	}
	if state.credentialRows == 0 {
		check.Status = restoreCredentialStatusOK
		check.Message = fmt.Sprintf("org %s has no credential rows", state.orgSlug)
		return check
	}
	if len(state.keyData) == 0 {
		return evaluateRestoreCredentialStateWithoutOrgKey(state, masterKey, check)
	}
	if len(masterKey) == 0 {
		check.Status = restoreCredentialStatusMissingKey
		check.Message = fmt.Sprintf("org %s contains an encrypted credential key, but no ASTONISH_MASTER_KEY or ~/.config/astonish/.store_key is configured", state.orgSlug)
		return check
	}
	dek, err := credentials.Decrypt(state.keyData, masterKey)
	if err != nil {
		check.Status = restoreCredentialStatusWrongKey
		check.Message = fmt.Sprintf("org %s contains encrypted credentials, but the current ASTONISH_MASTER_KEY or ~/.config/astonish/.store_key cannot decrypt its credential key", state.orgSlug)
		return check
	}
	return evaluateRestoreCredentialSample(state, dek, masterKey, check)
}

func evaluateRestoreCredentialStateWithoutOrgKey(state *restoreCredentialOrgState, masterKey []byte, check restoreCredentialCheck) restoreCredentialCheck {
	if state.credentialRows == 0 {
		check.Status = restoreCredentialStatusOK
		check.Message = fmt.Sprintf("org %s has no credential rows", state.orgSlug)
		return check
	}
	if len(state.sampleCredentialRow) == 0 {
		check.Status = restoreCredentialStatusUnknown
		check.Message = fmt.Sprintf("org %s contains credential rows but no credential key was found in the backup", state.orgSlug)
		return check
	}
	return evaluateRestoreCredentialSample(state, nil, masterKey, check)
}

func evaluateRestoreCredentialSample(state *restoreCredentialOrgState, dek, masterKey []byte, check restoreCredentialCheck) restoreCredentialCheck {
	if state.credentialRows == 0 || len(state.sampleCredentialRow) == 0 {
		check.Status = restoreCredentialStatusOK
		check.Message = fmt.Sprintf("org %s credential encryption key is valid", state.orgSlug)
		return check
	}
	if jsonLooksValid(state.sampleCredentialRow) {
		check.Status = restoreCredentialStatusPlaintextLegacy
		check.Message = fmt.Sprintf("org %s contains legacy plaintext credential rows", state.orgSlug)
		return check
	}
	if len(dek) > 0 {
		if plaintext, err := credentials.Decrypt(state.sampleCredentialRow, dek); err == nil && jsonLooksValid(plaintext) {
			check.Status = restoreCredentialStatusOK
			check.Message = fmt.Sprintf("org %s encrypted credentials can be decrypted with the current key", state.orgSlug)
			return check
		}
	}
	if len(masterKey) > 0 {
		if plaintext, err := credentials.Decrypt(state.sampleCredentialRow, masterKey); err == nil && jsonLooksValid(plaintext) {
			check.Status = restoreCredentialStatusOK
			check.Message = fmt.Sprintf("org %s credentials use legacy direct master-key encryption and can be decrypted", state.orgSlug)
			return check
		}
	}
	if isPlainText(state.sampleCredentialRow) {
		check.Status = restoreCredentialStatusPlaintextLegacy
		check.Message = fmt.Sprintf("org %s contains legacy plaintext credential rows", state.orgSlug)
		return check
	}
	if len(masterKey) == 0 && len(dek) == 0 {
		check.Status = restoreCredentialStatusMissingKey
		check.Message = fmt.Sprintf("org %s contains encrypted credential rows, but no ASTONISH_MASTER_KEY or ~/.config/astonish/.store_key is configured", state.orgSlug)
		return check
	}
	check.Status = restoreCredentialStatusWrongKey
	check.Message = fmt.Sprintf("org %s contains encrypted credential rows that cannot be decrypted with the current key", state.orgSlug)
	return check
}

func restoreBytesFromValue(value any) ([]byte, bool) {
	decoded := normalizeRestoreValue(value)
	switch v := decoded.(type) {
	case []byte:
		return v, true
	case string:
		if v == "" || strings.EqualFold(v, "[REDACTED]") {
			return nil, false
		}
		return []byte(v), true
	default:
		return nil, false
	}
}

func jsonLooksValid(data []byte) bool {
	trimmed := strings.TrimSpace(string(data))
	return strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")
}

func addCredentialRestoreCheckToPlan(plan *backup.RestorePlan, checks []restoreCredentialCheck) {
	for _, check := range checks {
		switch check.Status {
		case restoreCredentialStatusOK:
			continue
		case restoreCredentialStatusMissingKey, restoreCredentialStatusWrongKey:
			plan.Blockers = append(plan.Blockers, check.Message+"; copy the source installation key before restore")
		case restoreCredentialStatusPlaintextLegacy:
			plan.Warnings = append(plan.Warnings, check.Message)
		case restoreCredentialStatusUnknown:
			plan.Warnings = append(plan.Warnings, check.Message)
		}
	}
}
