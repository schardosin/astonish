package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

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
	if s.secrets != nil && len(envJSON) > 2 { // > 2 = not empty "{}"
		encrypted, encErr := s.secrets.encrypt(envJSON)
		if encErr != nil {
			return fmt.Errorf("encrypt mcp env: %w", encErr)
		}
		envJSON = encrypted
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
// Falls back to returning raw data if decryption fails (unencrypted legacy data).
func (s *pgMCPServerStore) decryptEnv(data []byte) []byte {
	if s.secrets == nil {
		return data
	}
	plaintext, err := s.secrets.decrypt(data)
	if err != nil {
		// Likely unencrypted (stored before encryption was enabled)
		slog.Debug("mcp env decrypt fallback to raw", "error", err)
		return data
	}
	return plaintext
}
