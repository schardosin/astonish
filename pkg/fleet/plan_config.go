package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FleetPlan is a fully configured, reusable fleet definition that includes
// both the team composition (snapshotted from a base fleet) and environment-specific
// settings like communication channel and artifact destinations.
//
// Fleet plans are created through an LLM-guided conversation (/fleet-plan command)
// and stored as YAML files in ~/.config/astonish/fleet_plans/.
// They can be launched exactly like regular fleets.
type FleetPlan struct {
	Name        string `yaml:"name" json:"name"`
	Key         string `yaml:"key" json:"key"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	CreatedFrom string `yaml:"created_from,omitempty" json:"created_from,omitempty"` // base fleet key (informational)

	// Full fleet config snapshot (agents, communication, settings).
	// Note: uses yaml:"-" because MarshalYAML/UnmarshalYAML handle serialization
	// manually to avoid duplicate 'name' and 'description' keys with the FleetPlan
	// top-level fields. Go struct embedding still promotes Agents, Communication, etc.
	FleetConfig `yaml:"-"`

	// Environment configuration
	Channel   PlanChannelConfig             `yaml:"channel" json:"channel"`
	Artifacts map[string]PlanArtifactConfig `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`

	// Validation state
	Validation PlanValidationState `yaml:"validation,omitempty" json:"validation,omitempty"`

	// Activation state
	Activation PlanActivationState `yaml:"activation,omitempty" json:"activation,omitempty"`

	// Creation metadata
	CreatedAt time.Time `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	UpdatedAt time.Time `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`

	// WorkspaceDir is the resolved workspace directory path for this plan.
	// Set at runtime (not persisted). Derived from artifact repo config using
	// the standard ~/astonish_projects/<repo-name> convention.
	WorkspaceDir string `yaml:"-" json:"-"`
}

// fleetPlanYAML is the on-disk YAML representation of FleetPlan.
// It flattens the FleetConfig fields (communication, agents, settings)
// alongside FleetPlan fields, avoiding the duplicate 'name'/'description'
// keys that yaml:",inline" would produce.
type fleetPlanYAML struct {
	Name        string `yaml:"name"`
	Key         string `yaml:"key"`
	Description string `yaml:"description,omitempty"`
	CreatedFrom string `yaml:"created_from,omitempty"`

	// FleetConfig fields (excluding Name and Description which are already above)
	Communication *CommunicationConfig        `yaml:"communication,omitempty"`
	Agents        map[string]FleetAgentConfig `yaml:"agents"`
	Settings      FleetSettings               `yaml:"settings,omitempty"`

	// FleetPlan-specific fields
	Channel    PlanChannelConfig             `yaml:"channel"`
	Artifacts  map[string]PlanArtifactConfig `yaml:"artifacts,omitempty"`
	Validation PlanValidationState           `yaml:"validation,omitempty"`
	Activation PlanActivationState           `yaml:"activation,omitempty"`
	CreatedAt  time.Time                     `yaml:"created_at,omitempty"`
	UpdatedAt  time.Time                     `yaml:"updated_at,omitempty"`
}

// MarshalYAML implements custom YAML marshalling to avoid duplicate keys
// from the embedded FleetConfig.
func (p *FleetPlan) MarshalYAML() (interface{}, error) {
	return &fleetPlanYAML{
		Name:          p.Name,
		Key:           p.Key,
		Description:   p.Description,
		CreatedFrom:   p.CreatedFrom,
		Communication: p.FleetConfig.Communication,
		Agents:        p.FleetConfig.Agents,
		Settings:      p.FleetConfig.Settings,
		Channel:       p.Channel,
		Artifacts:     p.Artifacts,
		Validation:    p.Validation,
		Activation:    p.Activation,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}, nil
}

// UnmarshalYAML implements custom YAML unmarshalling to populate the
// embedded FleetConfig from the flattened YAML structure.
func (p *FleetPlan) UnmarshalYAML(value *yaml.Node) error {
	var raw fleetPlanYAML
	if err := value.Decode(&raw); err != nil {
		return err
	}

	p.Name = raw.Name
	p.Key = raw.Key
	p.Description = raw.Description
	p.CreatedFrom = raw.CreatedFrom
	p.FleetConfig = FleetConfig{
		Name:          raw.Name, // Use the plan name as the fleet name
		Description:   raw.Description,
		Communication: raw.Communication,
		Agents:        raw.Agents,
		Settings:      raw.Settings,
	}
	p.Channel = raw.Channel
	p.Artifacts = raw.Artifacts
	p.Validation = raw.Validation
	p.Activation = raw.Activation
	p.CreatedAt = raw.CreatedAt
	p.UpdatedAt = raw.UpdatedAt

	return nil
}

// PlanChannelConfig defines the communication channel for a fleet plan.
// The channel determines how work items reach the fleet and how the fleet
// reports progress.
type PlanChannelConfig struct {
	// Type is the channel type: "chat", "github_issues", "jira", "email"
	Type string `yaml:"type" json:"type"`
	// Config holds channel-type-specific settings (e.g., repo, labels, board).
	Config map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
	// Schedule is a cron expression for polling external channels.
	// Only relevant for non-chat channels (e.g., GitHub Issues polling).
	Schedule string `yaml:"schedule,omitempty" json:"schedule,omitempty"`
}

// PlanArtifactConfig defines where a category of artifacts should be stored.
// Each fleet plan can have multiple named artifact destinations (e.g., "code", "docs", "helm").
type PlanArtifactConfig struct {
	// Type is the storage type: "local", "git_repo"
	Type string `yaml:"type" json:"type"`
	// Path is the local filesystem path (for type "local")
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	// Repo is the git repository (for type "git_repo"), e.g., "owner/repo"
	Repo string `yaml:"repo,omitempty" json:"repo,omitempty"`
	// BranchPattern is the branch naming pattern for git repos, e.g., "fleet/{task-id}"
	BranchPattern string `yaml:"branch_pattern,omitempty" json:"branch_pattern,omitempty"`
	// SubPath is a subdirectory within the repo to target
	SubPath string `yaml:"sub_path,omitempty" json:"sub_path,omitempty"`
	// AutoPR creates a pull request when the artifact is ready
	AutoPR bool `yaml:"auto_pr,omitempty" json:"auto_pr,omitempty"`
}

// PlanValidationState tracks whether the fleet plan's tool dependencies
// have been validated.
type PlanValidationState struct {
	ToolsRequired []string  `yaml:"tools_required,omitempty" json:"tools_required,omitempty"`
	LastValidated time.Time `yaml:"last_validated,omitempty" json:"last_validated,omitempty"`
	Status        string    `yaml:"status,omitempty" json:"status,omitempty"` // "passed", "failed", "pending"
}

// IsChat returns true if the plan uses the default chat channel.
func (c *PlanChannelConfig) IsChat() bool {
	return c.Type == "" || c.Type == "chat"
}

// PlanActivationState tracks whether a fleet plan is actively monitoring
// its configured channel (e.g., polling GitHub Issues on a schedule).
type PlanActivationState struct {
	// Activated is true when the plan has an active scheduler job polling for work.
	Activated bool `yaml:"activated,omitempty" json:"activated,omitempty"`
	// SchedulerJobID is the ID of the scheduler job created when the plan was activated.
	SchedulerJobID string `yaml:"scheduler_job_id,omitempty" json:"scheduler_job_id,omitempty"`
	// ActivatedAt is when the plan was last activated.
	ActivatedAt time.Time `yaml:"activated_at,omitempty" json:"activated_at,omitempty"`
	// LastPollAt is when the scheduler last polled the channel.
	LastPollAt time.Time `yaml:"last_poll_at,omitempty" json:"last_poll_at,omitempty"`
	// LastPollStatus is the result of the last poll ("success", "failed", "no_new_items").
	LastPollStatus string `yaml:"last_poll_status,omitempty" json:"last_poll_status,omitempty"`
	// LastPollError is the error message from the last failed poll.
	LastPollError string `yaml:"last_poll_error,omitempty" json:"last_poll_error,omitempty"`
	// SessionsStarted is the total number of fleet sessions triggered by this plan.
	SessionsStarted int `yaml:"sessions_started,omitempty" json:"sessions_started,omitempty"`
}

// IsActivated returns true if the plan is actively monitoring its channel.
func (p *FleetPlan) IsActivated() bool {
	return p.Activation.Activated
}

// ResolveWorkspaceDir derives the workspace directory from the plan's artifact
// config and sets the WorkspaceDir field. The convention is:
//
//	~/astonish_projects/<repo-name>
//
// where <repo-name> is the last segment of the first git_repo artifact's repo
// field (e.g., "schardosin/atari-astonish" → "atari-astonish"). If no git_repo
// artifacts exist, falls back to ~/astonish_projects/<plan-key>.
func (p *FleetPlan) ResolveWorkspaceDir() string {
	if p.WorkspaceDir != "" {
		return p.WorkspaceDir
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/root"
	}
	baseDir := filepath.Join(homeDir, "astonish_projects")

	// Try to derive name from the first git_repo artifact
	for _, artifact := range p.Artifacts {
		if artifact.Type == "git_repo" && artifact.Repo != "" {
			parts := strings.Split(artifact.Repo, "/")
			repoName := parts[len(parts)-1]
			p.WorkspaceDir = filepath.Join(baseDir, repoName)
			return p.WorkspaceDir
		}
	}

	// Fallback to plan key
	p.WorkspaceDir = filepath.Join(baseDir, p.Key)
	return p.WorkspaceDir
}
