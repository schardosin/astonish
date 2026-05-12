package filestore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/store"
	"gopkg.in/yaml.v3"
)

// FleetTemplateStoreWrapper wraps the existing fleet.Registry behind the
// store.FleetTemplateStore interface.
type FleetTemplateStoreWrapper struct {
	inner *fleet.Registry
}

// NewFleetTemplateStore creates a FleetTemplateStore backed by the existing file-based fleet registry.
func NewFleetTemplateStore(r *fleet.Registry) store.FleetTemplateStore {
	return &FleetTemplateStoreWrapper{inner: r}
}

// Inner returns the underlying fleet.Registry for code that still needs
// direct access during the transition period.
func (w *FleetTemplateStoreWrapper) Inner() *fleet.Registry {
	return w.inner
}

func (w *FleetTemplateStoreWrapper) GetFleet(_ context.Context, key string) (any, bool) {
	return w.inner.GetFleet(key)
}

func (w *FleetTemplateStoreWrapper) ListFleets(_ context.Context) []store.FleetTemplateSummary {
	summaries := w.inner.ListFleets()
	result := make([]store.FleetTemplateSummary, len(summaries))
	for i, s := range summaries {
		result[i] = store.FleetTemplateSummary{
			Key:         s.Key,
			Name:        s.Name,
			Description: s.Description,
			AgentCount:  s.AgentCount,
			AgentNames:  s.AgentNames,
		}
	}
	return result
}

func (w *FleetTemplateStoreWrapper) Save(_ context.Context, key string, fleetCfg any) error {
	fc, ok := fleetCfg.(*fleet.FleetConfig)
	if !ok {
		return fmt.Errorf("expected *fleet.FleetConfig, got %T", fleetCfg)
	}
	return w.inner.Save(key, fc)
}

func (w *FleetTemplateStoreWrapper) Delete(_ context.Context, key string) error {
	return w.inner.Delete(key)
}

func (w *FleetTemplateStoreWrapper) Count(_ context.Context) int {
	return w.inner.Count()
}

func (w *FleetTemplateStoreWrapper) Reload(_ context.Context) error {
	return w.inner.Reload()
}

// --- Fleet Plan Store ---

// FleetPlanStoreWrapper wraps the existing fleet.PlanRegistry behind the
// store.FleetPlanStore interface.
type FleetPlanStoreWrapper struct {
	inner *fleet.PlanRegistry
}

// NewFleetPlanStore creates a FleetPlanStore backed by the existing file-based plan registry.
func NewFleetPlanStore(r *fleet.PlanRegistry) store.FleetPlanStore {
	return &FleetPlanStoreWrapper{inner: r}
}

// Inner returns the underlying fleet.PlanRegistry for code that still needs
// direct access during the transition period.
func (w *FleetPlanStoreWrapper) Inner() *fleet.PlanRegistry {
	return w.inner
}

func (w *FleetPlanStoreWrapper) GetPlan(_ context.Context, key string) (any, bool) {
	return w.inner.GetPlan(key)
}

func (w *FleetPlanStoreWrapper) ListPlans(_ context.Context) []store.FleetPlanSummary {
	summaries := w.inner.ListPlans()
	result := make([]store.FleetPlanSummary, len(summaries))
	for i, s := range summaries {
		result[i] = store.FleetPlanSummary{
			Key:         s.Key,
			Name:        s.Name,
			Description: s.Description,
			CreatedFrom: s.CreatedFrom,
			ChannelType: s.ChannelType,
			AgentCount:  s.AgentCount,
			AgentNames:  s.AgentNames,
		}
	}
	return result
}

func (w *FleetPlanStoreWrapper) Save(_ context.Context, plan any) error {
	fp, ok := plan.(*fleet.FleetPlan)
	if !ok {
		return fmt.Errorf("expected *fleet.FleetPlan, got %T", plan)
	}
	return w.inner.Save(fp)
}

func (w *FleetPlanStoreWrapper) Delete(_ context.Context, key string) error {
	return w.inner.Delete(key)
}

func (w *FleetPlanStoreWrapper) Count(_ context.Context) int {
	return w.inner.Count()
}

func (w *FleetPlanStoreWrapper) Reload(_ context.Context) error {
	return w.inner.Reload()
}

func (w *FleetPlanStoreWrapper) GetPlanYAML(_ context.Context, key string) (string, error) {
	dir := w.inner.Dir()
	if dir == "" {
		return "", fmt.Errorf("fleet plan directory not configured")
	}
	yamlPath := filepath.Join(dir, key+".yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return "", fmt.Errorf("fleet plan %q not found: %w", key, err)
	}
	return string(data), nil
}

func (w *FleetPlanStoreWrapper) SavePlanYAML(_ context.Context, key string, yamlContent string) error {
	// Parse the YAML to validate and create a proper FleetPlan
	var plan fleet.FleetPlan
	if err := yaml.Unmarshal([]byte(yamlContent), &plan); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	plan.Key = key
	return w.inner.Save(&plan)
}

// Compile-time checks.
var _ store.FleetTemplateStore = (*FleetTemplateStoreWrapper)(nil)
var _ store.FleetPlanStore = (*FleetPlanStoreWrapper)(nil)
