package drill

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/store"
)

// memFlowStore is a minimal in-memory FlowStore for LoadSuiteFromStore tests.
type memFlowStore struct {
	flows map[string]string
	meta  map[string]store.FlowSummary
}

func newMemFlowStore() *memFlowStore {
	return &memFlowStore{
		flows: make(map[string]string),
		meta:  make(map[string]store.FlowSummary),
	}
}

func (m *memFlowStore) ListAllFlows(_ context.Context) []store.FlowSummary {
	out := make([]store.FlowSummary, 0, len(m.meta))
	for _, s := range m.meta {
		out = append(out, s)
	}
	return out
}

func (m *memFlowStore) ListFlowsByType(_ context.Context, types []string) []store.FlowSummary {
	want := make(map[string]bool, len(types))
	for _, t := range types {
		want[t] = true
	}
	out := make([]store.FlowSummary, 0)
	for _, s := range m.meta {
		if want[s.Type] {
			out = append(out, s)
		}
	}
	return out
}

func (m *memFlowStore) GetFlow(_ context.Context, name string) (string, error) {
	yamlContent, ok := m.flows[name]
	if !ok {
		return "", fmt.Errorf("flow %q not found", name)
	}
	return yamlContent, nil
}

func (m *memFlowStore) SaveFlow(_ context.Context, name string, yamlContent string) error {
	m.flows[name] = yamlContent
	typ := "flow"
	suite := ""
	desc := ""
	if strings.Contains(yamlContent, "type: drill_suite") || strings.Contains(yamlContent, "type: test_suite") {
		typ = "drill_suite"
	} else if strings.Contains(yamlContent, "type: drill") || strings.Contains(yamlContent, "type: test") {
		typ = "drill"
	}
	for _, line := range strings.Split(yamlContent, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "suite:") {
			suite = strings.TrimSpace(strings.TrimPrefix(line, "suite:"))
			suite = strings.Trim(suite, `"'`)
		}
		if strings.HasPrefix(line, "description:") {
			desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			desc = strings.Trim(desc, `"'`)
		}
	}
	m.meta[name] = store.FlowSummary{Name: name, Type: typ, Suite: suite, Description: desc}
	return nil
}

func (m *memFlowStore) DeleteFlow(_ context.Context, name string) error {
	delete(m.flows, name)
	delete(m.meta, name)
	return nil
}

func (m *memFlowStore) GetTaps(_ context.Context) []store.FlowTap { return nil }
func (m *memFlowStore) AddTap(_ context.Context, _ string, _ string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
func (m *memFlowStore) RemoveTap(_ context.Context, _ string) error { return nil }
func (m *memFlowStore) GetStoreDir(_ context.Context) string       { return "" }

func TestLoadSuiteFromStore(t *testing.T) {
	fs := newMemFlowStore()
	ctx := context.Background()

	suiteYAML := "description: Juicytrade drills\ntype: drill_suite\nsuite_config:\n  template: juicytrade\n  setup: []\n"
	drillYAML := "type: drill\nsuite: juicytrade\ndescription: health check\nnodes:\n  - name: ping\n    type: tool\n    args:\n      tool: shell_command\n      command: echo ok\n"
	if err := fs.SaveFlow(ctx, "juicytrade", suiteYAML); err != nil {
		t.Fatal(err)
	}
	if err := fs.SaveFlow(ctx, "health-check", drillYAML); err != nil {
		t.Fatal(err)
	}

	suite, err := LoadSuiteFromStore(fs, ctx, "juicytrade")
	if err != nil {
		t.Fatalf("LoadSuiteFromStore: %v", err)
	}
	if suite.Name != "juicytrade" {
		t.Errorf("Name = %q, want juicytrade", suite.Name)
	}
	if suite.Config == nil || suite.Config.Description != "Juicytrade drills" {
		t.Errorf("unexpected config: %+v", suite.Config)
	}
	if suite.Config.SuiteConfig == nil || suite.Config.SuiteConfig.Template != "juicytrade" {
		t.Errorf("SuiteConfig.Template = %+v", suite.Config.SuiteConfig)
	}
	if len(suite.Tests) != 1 || suite.Tests[0].Name != "health-check" {
		t.Fatalf("Tests = %+v, want one health-check", suite.Tests)
	}

	ctxStr := BuildSuiteContext(suite)
	if !strings.Contains(ctxStr, "Suite: juicytrade") {
		t.Errorf("BuildSuiteContext missing suite name: %s", ctxStr)
	}
	if !strings.Contains(ctxStr, "health-check") {
		t.Errorf("BuildSuiteContext missing drill: %s", ctxStr)
	}
	if !strings.Contains(ctxStr, "Template: juicytrade") {
		t.Errorf("BuildSuiteContext missing template: %s", ctxStr)
	}
}

func TestLoadSuiteFromStore_NotFound(t *testing.T) {
	fs := newMemFlowStore()
	_, err := LoadSuiteFromStore(fs, context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for missing suite")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want not found", err)
	}
}

func TestLoadSuiteFromStore_NilStore(t *testing.T) {
	_, err := LoadSuiteFromStore(nil, context.Background(), "juicytrade")
	if err == nil {
		t.Fatal("expected error for nil store")
	}
}

func TestLoadSuiteFromStore_WrongType(t *testing.T) {
	fs := newMemFlowStore()
	ctx := context.Background()
	if err := fs.SaveFlow(ctx, "not-a-suite", "description: x\ntype: drill\nsuite: other\nnodes: []\n"); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSuiteFromStore(fs, ctx, "not-a-suite")
	if err == nil || !strings.Contains(err.Error(), "expected drill_suite") {
		t.Fatalf("error = %v, want expected drill_suite", err)
	}
}
