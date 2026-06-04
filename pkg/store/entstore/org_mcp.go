package entstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	orgent "github.com/schardosin/astonish/ent/org"
	"github.com/schardosin/astonish/ent/org/orgmcpserver"
	"github.com/schardosin/astonish/pkg/store"
)

// orgMCPServerStore implements store.MCPServerStore for org-level MCP servers.
type orgMCPServerStore struct {
	client *orgent.Client
}

var _ store.MCPServerStore = (*orgMCPServerStore)(nil)

func (ms *orgMCPServerStore) List(ctx context.Context) ([]store.MCPServer, error) {
	ents, err := ms.client.OrgMCPServer.Query().
		Order(orgmcpserver.ByName()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	servers := make([]store.MCPServer, len(ents))
	for i, e := range ents {
		servers[i] = entOrgMCPServerToStore(e)
	}
	return servers, nil
}

func (ms *orgMCPServerStore) Get(ctx context.Context, name string) (*store.MCPServer, error) {
	ent, err := ms.client.OrgMCPServer.Query().
		Where(orgmcpserver.NameEQ(name)).
		Only(ctx)
	if err != nil {
		if orgent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	srv := entOrgMCPServerToStore(ent)
	return &srv, nil
}

func (ms *orgMCPServerStore) Save(ctx context.Context, server *store.MCPServer) error {
	// Check if server with this name already exists.
	existing, err := ms.client.OrgMCPServer.Query().
		Where(orgmcpserver.NameEQ(server.Name)).
		Only(ctx)
	if err != nil && !orgent.IsNotFound(err) {
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
			SetUpdatedAt(time.Now())

		if server.Enabled != nil {
			update.SetEnabled(*server.Enabled)
		}
		if server.Command == "" {
			update.ClearCommand()
		}
		if server.URL == "" {
			update.ClearURL()
		}

		return update.Exec(ctx)
	}

	// Create new.
	var createdBy uuid.UUID
	if server.CreatedBy != "" {
		uid, err := uuid.Parse(server.CreatedBy)
		if err == nil {
			createdBy = uid
		}
	}

	create := ms.client.OrgMCPServer.Create().
		SetName(server.Name).
		SetNillableCommand(nilStrPtr(server.Command)).
		SetArgs(server.Args).
		SetEnv(server.Env).
		SetTransport(server.Transport).
		SetNillableURL(nilStrPtr(server.URL)).
		SetCreatedBy(createdBy)

	if server.Enabled != nil {
		create.SetEnabled(*server.Enabled)
	}

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
	server.ID = created.ID.String()
	return nil
}

func (ms *orgMCPServerStore) Delete(ctx context.Context, name string) error {
	_, err := ms.client.OrgMCPServer.Delete().
		Where(orgmcpserver.NameEQ(name)).
		Exec(ctx)
	return err
}

func (ms *orgMCPServerStore) UpdateCachedTools(ctx context.Context, name string, tools json.RawMessage) error {
	if tools == nil {
		_, err := ms.client.OrgMCPServer.Update().
			Where(orgmcpserver.NameEQ(name)).
			ClearCachedTools().
			SetUpdatedAt(time.Now()).
			Save(ctx)
		return err
	}

	var parsed []interface{}
	if err := json.Unmarshal(tools, &parsed); err != nil {
		return fmt.Errorf("invalid cached_tools JSON: %w", err)
	}

	_, err := ms.client.OrgMCPServer.Update().
		Where(orgmcpserver.NameEQ(name)).
		SetCachedTools(parsed).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	return err
}

func entOrgMCPServerToStore(e *orgent.OrgMCPServer) store.MCPServer {
	srv := store.MCPServer{
		ID:        e.ID.String(),
		Name:      e.Name,
		Args:      e.Args,
		Env:       e.Env,
		Transport: e.Transport,
		Enabled:   &e.Enabled,
		CreatedBy: e.CreatedBy.String(),
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
	if e.Command != nil {
		srv.Command = *e.Command
	}
	if e.URL != nil {
		srv.URL = *e.URL
	}
	if e.CachedTools != nil {
		if data, err := json.Marshal(e.CachedTools); err == nil {
			srv.CachedTools = data
		}
	}
	return srv
}
