package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/fleet"
	"gopkg.in/yaml.v3"
)

// FleetPlanListItem is a single item in the fleet plans list response.
type FleetPlanListItem struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	CreatedFrom string   `json:"created_from,omitempty"`
	ChannelType string   `json:"channel_type"`
	AgentCount  int      `json:"agent_count"`
	AgentNames  []string `json:"agent_names"`
	Activated   bool     `json:"activated"`
}

// ListFleetPlansHandler handles GET /api/fleet-plans.
func ListFleetPlansHandler(w http.ResponseWriter, r *http.Request) {
	if fleetPlanRegistryVar == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"plans": []FleetPlanListItem{},
		})
		return
	}

	summaries := fleetPlanRegistryVar.ListPlans()
	items := make([]FleetPlanListItem, len(summaries))
	for i, s := range summaries {
		activated := false
		if plan, ok := fleetPlanRegistryVar.GetPlan(s.Key); ok {
			activated = plan.IsActivated()
		}
		items[i] = FleetPlanListItem{
			Key:         s.Key,
			Name:        s.Name,
			Description: s.Description,
			CreatedFrom: s.CreatedFrom,
			ChannelType: s.ChannelType,
			AgentCount:  s.AgentCount,
			AgentNames:  s.AgentNames,
			Activated:   activated,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"plans": items,
	})
}

// GetFleetPlanHandler handles GET /api/fleet-plans/{key}.
func GetFleetPlanHandler(w http.ResponseWriter, r *http.Request) {
	if fleetPlanRegistryVar == nil {
		http.Error(w, "Fleet plan system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]
	plan, ok := fleetPlanRegistryVar.GetPlan(key)
	if !ok {
		http.Error(w, "Fleet plan not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key":  key,
		"plan": plan,
	})
}

// SaveFleetPlanHandler handles PUT /api/fleet-plans/{key}.
func SaveFleetPlanHandler(w http.ResponseWriter, r *http.Request) {
	if fleetPlanRegistryVar == nil {
		http.Error(w, "Fleet plan system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]

	var plan fleet.FleetPlan
	if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	plan.Key = key
	if err := fleetPlanRegistryVar.Save(&plan); err != nil {
		http.Error(w, "Failed to save fleet plan: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "saved",
		"key":    key,
	})
}

// DeleteFleetPlanHandler handles DELETE /api/fleet-plans/{key}.
func DeleteFleetPlanHandler(w http.ResponseWriter, r *http.Request) {
	if fleetPlanRegistryVar == nil {
		http.Error(w, "Fleet plan system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]

	// Clean up associated resources (scheduler job, monitor, state file)
	// before removing the plan from the registry. This prevents orphaned
	// scheduler jobs that poll forever for a deleted plan.
	if planActivatorVar != nil {
		planActivatorVar.ForceCleanup(key)
	}

	if err := fleetPlanRegistryVar.Delete(key); err != nil {
		http.Error(w, "Failed to delete fleet plan: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "deleted",
		"key":    key,
	})
}

// DuplicateFleetPlanHandler handles POST /api/fleet-plans/{key}/duplicate.
// Creates a deep copy of an existing fleet plan with a new unique key.
func DuplicateFleetPlanHandler(w http.ResponseWriter, r *http.Request) {
	if fleetPlanRegistryVar == nil {
		http.Error(w, "Fleet plan system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]
	original, ok := fleetPlanRegistryVar.GetPlan(key)
	if !ok {
		http.Error(w, "Fleet plan not found", http.StatusNotFound)
		return
	}

	// Generate a unique key for the copy
	newKey := key + "-copy"
	for i := 2; ; i++ {
		if _, exists := fleetPlanRegistryVar.GetPlan(newKey); !exists {
			break
		}
		newKey = fmt.Sprintf("%s-copy-%d", key, i)
	}

	// Deep copy the plan
	duplicate := deepCopyFleetPlan(original)
	duplicate.Key = newKey
	duplicate.Name = original.Name + " (copy)"

	// Reset activation and validation state
	duplicate.Validation = fleet.PlanValidationState{}
	duplicate.Activation = fleet.PlanActivationState{}

	// Reset timestamps so Save() sets them fresh
	duplicate.CreatedAt = original.CreatedAt // preserve original creation time in provenance
	duplicate.UpdatedAt = original.UpdatedAt

	if err := fleetPlanRegistryVar.Save(duplicate); err != nil {
		http.Error(w, "Failed to save duplicate: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "duplicated",
		"key":    newKey,
	})
}

// deepCopyFleetPlan creates a deep copy of a FleetPlan, ensuring all maps
// and slices are independent copies.
func deepCopyFleetPlan(src *fleet.FleetPlan) *fleet.FleetPlan {
	dst := &fleet.FleetPlan{
		Name:        src.Name,
		Key:         src.Key,
		Description: src.Description,
		CreatedFrom: src.CreatedFrom,
		FleetConfig: fleet.FleetConfig{
			Name:        src.FleetConfig.Name,
			Description: src.FleetConfig.Description,
			Settings:    src.FleetConfig.Settings,
		},
		Channel: fleet.PlanChannelConfig{
			Type:     src.Channel.Type,
			Schedule: src.Channel.Schedule,
		},
		CreatedAt: src.CreatedAt,
		UpdatedAt: src.UpdatedAt,
	}

	// Deep copy Communication
	if src.FleetConfig.Communication != nil {
		comm := &fleet.CommunicationConfig{}
		for _, node := range src.FleetConfig.Communication.Flow {
			talksTo := make([]string, len(node.TalksTo))
			copy(talksTo, node.TalksTo)
			comm.Flow = append(comm.Flow, fleet.CommunicationNode{
				Role:       node.Role,
				TalksTo:    talksTo,
				EntryPoint: node.EntryPoint,
			})
		}
		dst.FleetConfig.Communication = comm
	}

	// Deep copy Agents map
	if src.FleetConfig.Agents != nil {
		dst.FleetConfig.Agents = make(map[string]fleet.FleetAgentConfig, len(src.FleetConfig.Agents))
		for k, agent := range src.FleetConfig.Agents {
			agentCopy := fleet.FleetAgentConfig{
				Name:        agent.Name,
				Description: agent.Description,
				Identity:    agent.Identity,
				Mode:        agent.Mode,
				Behaviors:   agent.Behaviors,
				Tools: fleet.ToolsConfig{
					All: agent.Tools.All,
				},
			}
			if agent.Tools.Names != nil {
				agentCopy.Tools.Names = make([]string, len(agent.Tools.Names))
				copy(agentCopy.Tools.Names, agent.Tools.Names)
			}
			if agent.Delegate != nil {
				delegateCopy := &fleet.DelegateConfig{
					Tool:        agent.Delegate.Tool,
					Description: agent.Delegate.Description,
				}
				if agent.Delegate.Params != nil {
					delegateCopy.Params = make(map[string]any, len(agent.Delegate.Params))
					for pk, pv := range agent.Delegate.Params {
						delegateCopy.Params[pk] = pv
					}
				}
				if agent.Delegate.Env != nil {
					delegateCopy.Env = make([]string, len(agent.Delegate.Env))
					copy(delegateCopy.Env, agent.Delegate.Env)
				}
				agentCopy.Delegate = delegateCopy
			}
			dst.FleetConfig.Agents[k] = agentCopy
		}
	}

	// Deep copy Channel.Config map
	if src.Channel.Config != nil {
		dst.Channel.Config = make(map[string]any, len(src.Channel.Config))
		for k, v := range src.Channel.Config {
			dst.Channel.Config[k] = v
		}
	}

	// Deep copy Artifacts map
	if src.Artifacts != nil {
		dst.Artifacts = make(map[string]fleet.PlanArtifactConfig, len(src.Artifacts))
		for k, v := range src.Artifacts {
			dst.Artifacts[k] = v
		}
	}

	return dst
}

// GetFleetPlanYAMLHandler handles GET /api/fleet-plans/{key}/yaml.
// Returns the raw YAML content of a fleet plan for the YAML editor.
func GetFleetPlanYAMLHandler(w http.ResponseWriter, r *http.Request) {
	if fleetPlanRegistryVar == nil {
		http.Error(w, "Fleet plan system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]
	yamlPath := filepath.Join(fleetPlanRegistryVar.Dir(), key+".yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Fleet plan not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to read plan file: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write(data)
}

// SaveFleetPlanYAMLHandler handles PUT /api/fleet-plans/{key}/yaml.
// Accepts raw YAML content, parses it, validates, and saves via the registry.
func SaveFleetPlanYAMLHandler(w http.ResponseWriter, r *http.Request) {
	if fleetPlanRegistryVar == nil {
		http.Error(w, "Fleet plan system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var plan fleet.FleetPlan
	if err := yaml.Unmarshal(body, &plan); err != nil {
		http.Error(w, "Invalid YAML: "+err.Error(), http.StatusBadRequest)
		return
	}

	plan.Key = key
	if err := fleetPlanRegistryVar.Save(&plan); err != nil {
		http.Error(w, "Failed to save fleet plan: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "saved",
		"key":    key,
	})
}
