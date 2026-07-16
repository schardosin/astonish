package entstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	platforment "github.com/SAP/astonish/ent/platform"
	"github.com/SAP/astonish/ent/platform/platformmcpserver"
	"github.com/SAP/astonish/pkg/store"
)

// mcpServerStore implements store.MCPServerStore for platform-level MCP servers.
type mcpServerStore struct {
	client *platforment.Client
}

func (s *Store) PlatformMCPServers() store.MCPServerStore {
	return &mcpServerStore{client: s.platformClient}
}

func (ms *mcpServerStore) List(ctx context.Context) ([]store.MCPServer, error) {
	ents, err := ms.client.PlatformMCPServer.Query().
		Order(platformmcpserver.ByName()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	servers := make([]store.MCPServer, len(ents))
	for i, e := range ents {
		servers[i] = entMCPServerToStore(e)
	}
	return servers, nil
}

func (ms *mcpServerStore) Get(ctx context.Context, name string) (*store.MCPServer, error) {
	ent, err := ms.client.PlatformMCPServer.Query().
		Where(platformmcpserver.NameEQ(name)).
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	srv := entMCPServerToStore(ent)
	return &srv, nil
}

func (ms *mcpServerStore) Save(ctx context.Context, server *store.MCPServer) error {
	// Check if a server with this name already exists.
	existing, err := ms.client.PlatformMCPServer.Query().
		Where(platformmcpserver.NameEQ(server.Name)).
		Only(ctx)
	if err != nil && !platforment.IsNotFound(err) {
		return err
	}

	if existing != nil {
		// Update existing.
		update := existing.Update().
			SetNillableCommand(nilStrPtr(server.Command)).
			SetArgs(server.Args).
			SetEnv(server.Env).
			SetTransport(server.Transport).
			SetNillableURL(nilStrPtr(server.URL)).
			SetNillableEnabled(server.Enabled).
			SetNillableCreatedBy(nilStrPtr(server.CreatedBy)).
			SetUpdatedAt(time.Now())

		if server.Command == "" {
			update.ClearCommand()
		}
		if server.URL == "" {
			update.ClearURL()
		}

		return update.Exec(ctx)
	}

	// Create new.
	create := ms.client.PlatformMCPServer.Create().
		SetName(server.Name).
		SetNillableCommand(nilStrPtr(server.Command)).
		SetArgs(server.Args).
		SetEnv(server.Env).
		SetTransport(server.Transport).
		SetNillableURL(nilStrPtr(server.URL)).
		SetNillableEnabled(server.Enabled).
		SetNillableCreatedBy(nilStrPtr(server.CreatedBy))

	if server.CachedTools != nil {
		var tools []interface{}
		if err := json.Unmarshal(server.CachedTools, &tools); err == nil {
			create.SetCachedTools(tools)
		}
	}

	created, err := create.Save(ctx)
	if err != nil {
		return err
	}

	// Populate the ID back into the caller's struct.
	server.ID = created.ID.String()
	return nil
}

func (ms *mcpServerStore) Delete(ctx context.Context, name string) error {
	_, err := ms.client.PlatformMCPServer.Delete().
		Where(platformmcpserver.NameEQ(name)).
		Exec(ctx)
	return err
}

func (ms *mcpServerStore) UpdateCachedTools(ctx context.Context, name string, tools json.RawMessage) error {
	if tools == nil {
		_, err := ms.client.PlatformMCPServer.Update().
			Where(platformmcpserver.NameEQ(name)).
			ClearCachedTools().
			SetUpdatedAt(time.Now()).
			Save(ctx)
		return err
	}

	var parsed []interface{}
	if err := json.Unmarshal(tools, &parsed); err != nil {
		return fmt.Errorf("invalid cached_tools JSON: %w", err)
	}

	_, err := ms.client.PlatformMCPServer.Update().
		Where(platformmcpserver.NameEQ(name)).
		SetCachedTools(parsed).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	return err
}

func entMCPServerToStore(e *platforment.PlatformMCPServer) store.MCPServer {
	srv := store.MCPServer{
		ID:        e.ID.String(),
		Name:      e.Name,
		Args:      e.Args,
		Env:       e.Env,
		Transport: e.Transport,
		Enabled:   &e.Enabled,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
	if e.Command != nil {
		srv.Command = *e.Command
	}
	if e.URL != nil {
		srv.URL = *e.URL
	}
	if e.CreatedBy != nil {
		srv.CreatedBy = *e.CreatedBy
	}
	if e.CachedTools != nil {
		if data, err := json.Marshal(e.CachedTools); err == nil {
			srv.CachedTools = data
		}
	}
	return srv
}

// Compile-time assertion.
var _ store.MCPServerStore = (*mcpServerStore)(nil)
