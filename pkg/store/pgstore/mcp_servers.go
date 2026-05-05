package pgstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgMCPServerStore implements store.MCPServerStore for PostgreSQL.
//
// Reused for both org-level ("public"."org_mcp_servers") and
// team-level ("team_<slug>"."mcp_servers") by parameterizing schema and table.
type pgMCPServerStore struct {
	pool   *pgxpool.Pool
	schema string
	table  string
}

func (s *pgMCPServerStore) tableName() string {
	return pgx.Identifier{s.schema, s.table}.Sanitize()
}

func (s *pgMCPServerStore) List() ([]store.MCPServer, error) {
	ctx := context.Background()
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

func (s *pgMCPServerStore) Get(name string) (*store.MCPServer, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT id, name, command, args, env, transport, url, enabled, cached_tools, created_by, created_at, updated_at
		 FROM %s WHERE name = $1`, s.tableName()),
		name,
	)

	return s.scanSingleRow(row)
}

func (s *pgMCPServerStore) Save(server *store.MCPServer) error {
	ctx := context.Background()

	argsJSON, err := json.Marshal(server.Args)
	if err != nil {
		argsJSON = []byte("[]")
	}

	envJSON, err := json.Marshal(server.Env)
	if err != nil {
		envJSON = []byte("{}")
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

func (s *pgMCPServerStore) Delete(name string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE name = $1`, s.tableName()),
		name,
	)
	return err
}

func (s *pgMCPServerStore) UpdateCachedTools(name string, tools json.RawMessage) error {
	ctx := context.Background()
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
		_ = json.Unmarshal(envJSON, &srv.Env)
	}
	if len(cachedToolsJSON) > 0 {
		srv.CachedTools = cachedToolsJSON
	}

	return &srv, nil
}
