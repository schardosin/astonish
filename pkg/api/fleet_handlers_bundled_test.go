package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/store"
)

type memFleetTemplateStore struct {
	custom map[string]*fleet.FleetConfig
}

func (m *memFleetTemplateStore) GetFleet(_ context.Context, key string) (any, bool) {
	if fleet.IsBundledKey(key) {
		bundled, err := fleet.LoadBundledConfigs()
		if err != nil {
			return nil, false
		}
		f, ok := bundled[key]
		return f, ok
	}
	f, ok := m.custom[key]
	return f, ok
}

func (m *memFleetTemplateStore) ListFleets(_ context.Context) []store.FleetTemplateSummary {
	var out []store.FleetTemplateSummary
	if bundled, err := fleet.LoadBundledConfigs(); err == nil {
		for key, cfg := range bundled {
			out = append(out, store.FleetTemplateSummary{
				Key: key, Name: cfg.Name, AgentCount: len(cfg.Agents), Source: "bundled",
			})
		}
	}
	for key, cfg := range m.custom {
		if fleet.IsBundledKey(key) {
			continue
		}
		out = append(out, store.FleetTemplateSummary{
			Key: key, Name: cfg.Name, AgentCount: len(cfg.Agents), Source: "custom",
		})
	}
	return out
}

func (m *memFleetTemplateStore) Save(_ context.Context, key string, f any) error {
	if fleet.IsBundledKey(key) {
		return store.ErrBundledTemplateImmutable
	}
	cfg, ok := f.(*fleet.FleetConfig)
	if !ok {
		return nil
	}
	if m.custom == nil {
		m.custom = map[string]*fleet.FleetConfig{}
	}
	m.custom[key] = cfg
	return nil
}

func (m *memFleetTemplateStore) Delete(_ context.Context, key string) error {
	if fleet.IsBundledKey(key) {
		return store.ErrBundledTemplateImmutable
	}
	delete(m.custom, key)
	return nil
}

func (m *memFleetTemplateStore) Count(_ context.Context) int { return len(m.custom) }
func (m *memFleetTemplateStore) Reload(_ context.Context) error { return nil }

func withFleetTemplates(r *http.Request, ts store.FleetTemplateStore) *http.Request {
	svc := &store.Services{Mode: store.ModePlatform, FleetTemplates: ts}
	ctx := store.WithServices(r.Context(), svc)
	return r.WithContext(ctx)
}

func TestSaveFleetHandler_RejectsBundledKey(t *testing.T) {
	body := `{"name":"x","agents":{"a":{"name":"A","identity":"i","behaviors":"b","tools":true}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/fleets/software-dev", bytes.NewBufferString(body))
	req = mux.SetURLVars(req, map[string]string{"key": "software-dev"})
	req = withFleetTemplates(req, &memFleetTemplateStore{})
	rr := httptest.NewRecorder()
	SaveFleetHandler(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteFleetHandler_RejectsBundledKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/fleets/software-dev", nil)
	req = mux.SetURLVars(req, map[string]string{"key": "software-dev"})
	req = withFleetTemplates(req, &memFleetTemplateStore{})
	rr := httptest.NewRecorder()
	DeleteFleetHandler(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
}

func TestListFleetsHandler_BundledSource(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/fleets", nil)
	req = withFleetTemplates(req, &memFleetTemplateStore{
		custom: map[string]*fleet.FleetConfig{
			"software-dev": {Name: "Orphan Override"}, // ignored orphan
			"my-fleet": {
				Name: "My Fleet",
				Agents: map[string]fleet.FleetAgentConfig{
					"a": {Name: "A", Identity: "i", Behaviors: "b", Tools: fleet.ToolsConfig{All: true}},
				},
			},
		},
	})
	rr := httptest.NewRecorder()
	ListFleetsHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Fleets []FleetListItem `json:"fleets"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	var sawBundled, sawCustom, sawOrphanName bool
	for _, f := range payload.Fleets {
		if f.Key == "software-dev" {
			if f.Source != "bundled" {
				t.Fatalf("software-dev source = %q, want bundled", f.Source)
			}
			if f.Name == "Orphan Override" {
				sawOrphanName = true
			}
			sawBundled = true
		}
		if f.Key == "my-fleet" {
			if f.Source != "custom" {
				t.Fatalf("my-fleet source = %q, want custom", f.Source)
			}
			sawCustom = true
		}
	}
	if !sawBundled || !sawCustom {
		t.Fatalf("missing expected fleets: bundled=%v custom=%v payload=%+v", sawBundled, sawCustom, payload.Fleets)
	}
	if sawOrphanName {
		t.Fatal("bundled key should not show orphan DB name")
	}
}

func TestCloneFleetHandler_CreatesCustomCopy(t *testing.T) {
	mem := &memFleetTemplateStore{custom: map[string]*fleet.FleetConfig{}}
	body := `{"new_key":"software-dev-copy","name":"Software Dev Copy"}`
	req := httptest.NewRequest(http.MethodPost, "/api/fleets/software-dev/clone", bytes.NewBufferString(body))
	req = mux.SetURLVars(req, map[string]string{"key": "software-dev"})
	req = withFleetTemplates(req, mem)
	rr := httptest.NewRecorder()
	CloneFleetHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if _, ok := mem.custom["software-dev-copy"]; !ok {
		t.Fatal("expected cloned template in custom store")
	}
	if fleet.IsBundledKey("software-dev-copy") {
		t.Fatal("clone key must not be bundled")
	}
}

func TestGetFleetHandler_BundledWins(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/fleets/software-dev", nil)
	req = mux.SetURLVars(req, map[string]string{"key": "software-dev"})
	req = withFleetTemplates(req, &memFleetTemplateStore{
		custom: map[string]*fleet.FleetConfig{
			"software-dev": {Name: "Orphan Override"},
		},
	})
	rr := httptest.NewRecorder()
	GetFleetHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Source string         `json:"source"`
		Fleet  map[string]any `json:"fleet"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Source != "bundled" {
		t.Fatalf("source = %q, want bundled", payload.Source)
	}
	if name, _ := payload.Fleet["name"].(string); name == "Orphan Override" {
		t.Fatal("GET should return embedded bundled content, not orphan DB row")
	}
}
