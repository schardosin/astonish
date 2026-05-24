package pgstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// encryptedEnvEnvelope is the JSON shape used to store AES-GCM-encrypted MCP
// env values inside the JSONB `env` column.
//
// JSONB enforces UTF-8 validation, so raw ciphertext bytes (which may contain
// 0x00-0x7F-violating bytes such as 0x8e) cannot be written directly. We wrap
// the ciphertext as base64 inside a single-key object: {"_encrypted":"<b64>"}.
//
// On read, decryptEnv detects the envelope, base64-decodes, and decrypts.
// Plaintext rows (legacy/unencrypted, or written when masterKey was nil) lack
// the envelope and pass through unchanged for backward compatibility.
const encryptedEnvKey = "_encrypted"

// pgMCPServerStore implements store.MCPServerStore for PostgreSQL.
//
// Reused for org-level ("public"."org_mcp_servers"), team-level
// ("team_<slug>"."mcp_servers"), and platform-level ("public"."platform_mcp_servers")
// by parameterizing schema, table, and optional encryption.
type pgMCPServerStore struct {
	pool    *pgxpool.Pool
	schema  string
	table   string
	secrets *PlatformSecretStore // optional; when set, Env values are encrypted at rest
}

func (s *pgMCPServerStore) tableName() string {
	return pgx.Identifier{s.schema, s.table}.Sanitize()
}

func (s *pgMCPServerStore) List(ctx context.Context) ([]store.MCPServer, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT id, name, command, args, env, transport, url, enabled, cached_tools, created_by, created_at, updated_at
		 FROM %s ORDER BY name`, s.tableName()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.MCPServer
	for rows.Next() {
		srv, err := s.scanRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *srv)
	}
	return result, rows.Err()
}

func (s *pgMCPServerStore) Get(ctx context.Context, name string) (*store.MCPServer, error) {
	row := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT id, name, command, args, env, transport, url, enabled, cached_tools, created_by, created_at, updated_at
		 FROM %s WHERE name = $1`, s.tableName()),
		name,
	)

	return s.scanSingleRow(row)
}

func (s *pgMCPServerStore) Save(ctx context.Context, server *store.MCPServer) error {
	argsJSON, err := json.Marshal(server.Args)
	if err != nil {
		argsJSON = []byte("[]")
	}

	envJSON, err := json.Marshal(server.Env)
	if err != nil {
		envJSON = []byte("{}")
	}

	// Encrypt env JSON at rest when secrets store is configured (platform tier).
	// JSONB column requires valid UTF-8, so we base64-wrap the ciphertext inside
	// a small JSON envelope: {"_encrypted":"<base64>"}. See encryptedEnvEnvelope.
	if s.secrets != nil && len(envJSON) > 2 { // > 2 = not empty "{}"
		encrypted, encErr := s.secrets.encrypt(envJSON)
		if encErr != nil {
			return fmt.Errorf("encrypt mcp env: %w", encErr)
		}
		// If masterKey is unset, encrypt() returns plaintext unchanged. In that
		// case skip the envelope; plaintext JSON is already valid for JSONB.
		if !bytesEqual(encrypted, envJSON) {
			envelope, mErr := json.Marshal(map[string]string{
				encryptedEnvKey: base64.StdEncoding.EncodeToString(encrypted),
			})
			if mErr != nil {
				return fmt.Errorf("marshal encrypted env envelope: %w", mErr)
			}
			envJSON = envelope
		}
	}

	enabled := true
	if server.Enabled != nil {
		enabled = *server.Enabled
	}

	transport := server.Transport
	if transport == "" {
		transport = "stdio"
	}

	_, err = s.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (name, command, args, env, transport, url, enabled, cached_tools, created_by, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		 ON CONFLICT (name) DO UPDATE SET
		     command = EXCLUDED.command,
		     args = EXCLUDED.args,
		     env = EXCLUDED.env,
		     transport = EXCLUDED.transport,
		     url = EXCLUDED.url,
		     enabled = EXCLUDED.enabled,
		     cached_tools = COALESCE(EXCLUDED.cached_tools, %s.cached_tools),
		     updated_at = now()`,
		s.tableName(), s.tableName()),
		server.Name,
		server.Command,
		argsJSON,
		envJSON,
		transport,
		server.URL,
		enabled,
		server.CachedTools,
		server.CreatedBy,
	)
	return err
}

func (s *pgMCPServerStore) Delete(ctx context.Context, name string) error {
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE name = $1`, s.tableName()),
		name,
	)
	return err
}

func (s *pgMCPServerStore) UpdateCachedTools(ctx context.Context, name string, tools json.RawMessage) error {
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE %s SET cached_tools = $1, updated_at = now() WHERE name = $2`, s.tableName()),
		tools, name,
	)
	return err
}

// scanRow scans a row from a Query result.
func (s *pgMCPServerStore) scanRow(rows pgx.Rows) (*store.MCPServer, error) {
	var srv store.MCPServer
	var argsJSON, envJSON, cachedToolsJSON []byte
	var enabled bool

	err := rows.Scan(
		&srv.ID,
		&srv.Name,
		&srv.Command,
		&argsJSON,
		&envJSON,
		&srv.Transport,
		&srv.URL,
		&enabled,
		&cachedToolsJSON,
		&srv.CreatedBy,
		&srv.CreatedAt,
		&srv.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	srv.Enabled = &enabled

	if len(argsJSON) > 0 {
		_ = json.Unmarshal(argsJSON, &srv.Args)
	}
	if len(envJSON) > 0 {
		envJSON = s.decryptEnv(envJSON)
		_ = json.Unmarshal(envJSON, &srv.Env)
	}
	if len(cachedToolsJSON) > 0 {
		srv.CachedTools = cachedToolsJSON
	}

	return &srv, nil
}

// scanSingleRow scans a QueryRow result.
func (s *pgMCPServerStore) scanSingleRow(row pgx.Row) (*store.MCPServer, error) {
	var srv store.MCPServer
	var argsJSON, envJSON, cachedToolsJSON []byte
	var enabled bool

	err := row.Scan(
		&srv.ID,
		&srv.Name,
		&srv.Command,
		&argsJSON,
		&envJSON,
		&srv.Transport,
		&srv.URL,
		&enabled,
		&cachedToolsJSON,
		&srv.CreatedBy,
		&srv.CreatedAt,
		&srv.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("mcp server not found: %w", err)
	}

	srv.Enabled = &enabled

	if len(argsJSON) > 0 {
		_ = json.Unmarshal(argsJSON, &srv.Args)
	}
	if len(envJSON) > 0 {
		envJSON = s.decryptEnv(envJSON)
		_ = json.Unmarshal(envJSON, &srv.Env)
	}
	if len(cachedToolsJSON) > 0 {
		srv.CachedTools = cachedToolsJSON
	}

	return &srv, nil
}

// decryptEnv decrypts env JSON if encryption is configured.
//
// Three input shapes are tolerated:
//  1. {"_encrypted":"<base64>"} envelope — base64-decode, then AES-GCM decrypt.
//     This is the canonical shape written by Save() when secrets!=nil and
//     masterKey is set.
//  2. Plain JSON object — pass through unchanged. Covers rows written when
//     masterKey was unset (encrypt() returns plaintext) and any legacy rows.
//  3. Raw ciphertext bytes (legacy, pre-envelope) — best-effort decrypt; if
//     that fails too, return as-is for forward debug visibility.
//
// Falls back to returning raw data if all decrypt attempts fail.
func (s *pgMCPServerStore) decryptEnv(data []byte) []byte {
	if s.secrets == nil || len(data) == 0 {
		return data
	}

	// Shape 1: envelope detection. Cheap probe before full unmarshal.
	if isEncryptedEnvelope(data) {
		var env map[string]string
		if err := json.Unmarshal(data, &env); err == nil {
			if b64, ok := env[encryptedEnvKey]; ok {
				cipherBytes, dErr := base64.StdEncoding.DecodeString(b64)
				if dErr != nil {
					slog.Warn("mcp env envelope: invalid base64", "error", dErr)
					return data
				}
				plaintext, decErr := s.secrets.decrypt(cipherBytes)
				if decErr != nil {
					slog.Warn("mcp env envelope: decrypt failed", "error", decErr)
					return data
				}
				return plaintext
			}
		}
	}

	// Shape 2 & 3: try secrets.decrypt directly (handles legacy unencrypted
	// plaintext via its isPlainText fallback).
	plaintext, err := s.secrets.decrypt(data)
	if err != nil {
		slog.Debug("mcp env decrypt fallback to raw", "error", err)
		return data
	}
	return plaintext
}

// isEncryptedEnvelope returns true if data appears to be a JSON object whose
// only / first key is `_encrypted`. Keeps the probe O(prefix) so legacy
// plaintext-JSON rows aren't unmarshaled twice.
func isEncryptedEnvelope(data []byte) bool {
	// Trim leading whitespace.
	i := 0
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\n', '\r':
			i++
			continue
		}
		break
	}
	if i >= len(data) || data[i] != '{' {
		return false
	}
	// Cheap textual check; full validation happens via json.Unmarshal in caller.
	const needle = `"` + encryptedEnvKey + `"`
	if len(data)-i < len(needle)+1 {
		return false
	}
	// Search for `"_encrypted"` shortly after `{`.
	window := data[i:min(i+len(needle)+8, len(data))]
	for j := 0; j+len(needle) <= len(window); j++ {
		match := true
		for k := 0; k < len(needle); k++ {
			if window[j+k] != needle[k] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// bytesEqual is a tiny helper to avoid importing "bytes" just for Equal in this
// hot path. Kept package-private and inline-friendly.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
