package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/store"
)

// sqliteMCPServerStore implements store.MCPServerStore.
type sqliteMCPServerStore struct {
	db    *sql.DB
	table string // "mcp_servers", "org_mcp_servers", or "platform_mcp_servers"
}

func (s *sqliteMCPServerStore) List(ctx context.Context) ([]store.MCPServer, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, name, command, args, env, transport, url, enabled, cached_tools, created_by, created_at, updated_at
		 FROM %s ORDER BY name`, s.table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []store.MCPServer
	for rows.Next() {
		srv, err := scanMCPRow(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, *srv)
	}
	return servers, rows.Err()
}

func (s *sqliteMCPServerStore) Get(ctx context.Context, name string) (*store.MCPServer, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, name, command, args, env, transport, url, enabled, cached_tools, created_by, created_at, updated_at
		 FROM %s WHERE name = ?`, s.table), name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}
	return scanMCPRow(rows)
}

func (s *sqliteMCPServerStore) Save(ctx context.Context, server *store.MCPServer) error {
	if server.ID == "" {
		server.ID = uuid.New().String()
	}

	argsJSON, _ := json.Marshal(server.Args)
	envJSON, _ := json.Marshal(server.Env)
	now := formatTime(time.Now())

	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (id, name, command, args, env, transport, url, enabled, cached_tools, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET command = excluded.command, args = excluded.args, env = excluded.env,
		 transport = excluded.transport, url = excluded.url, enabled = excluded.enabled, updated_at = excluded.updated_at`, s.table),
		server.ID, server.Name, nilStr(server.Command), string(argsJSON), string(envJSON),
		nilStr(server.Transport), nilStr(server.URL), boolPtrToInt(server.Enabled),
		nilStr(string(server.CachedTools)), nilStr(server.CreatedBy), now, now)
	return err
}

func (s *sqliteMCPServerStore) Delete(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE name = ?`, s.table), name)
	return err
}

func (s *sqliteMCPServerStore) UpdateCachedTools(ctx context.Context, name string, tools json.RawMessage) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET cached_tools = ?, updated_at = ? WHERE name = ?`, s.table),
		string(tools), formatTime(time.Now()), name)
	return err
}

func scanMCPRow(rows *sql.Rows) (*store.MCPServer, error) {
	srv := &store.MCPServer{}
	var command, argsStr, envStr, transport, url, cachedTools, createdBy sql.NullString
	var createdAt, updatedAt string
	var enabled int

	err := rows.Scan(&srv.ID, &srv.Name, &command, &argsStr, &envStr, &transport, &url,
		&enabled, &cachedTools, &createdBy, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	srv.Command = command.String
	srv.Transport = transport.String
	srv.URL = url.String
	e := enabled != 0
	srv.Enabled = &e
	srv.CreatedBy = createdBy.String

	if argsStr.Valid && argsStr.String != "" {
		_ = json.Unmarshal([]byte(argsStr.String), &srv.Args)
	}
	if envStr.Valid && envStr.String != "" {
		_ = json.Unmarshal([]byte(envStr.String), &srv.Env)
	}
	if cachedTools.Valid && cachedTools.String != "" {
		srv.CachedTools = json.RawMessage(cachedTools.String)
	}

	return srv, nil
}

func boolPtrToInt(b *bool) int {
	if b == nil || *b {
		return 1
	}
	return 0
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
