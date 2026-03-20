package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

	// Credentials maps logical names to credential store entries.
	// Each key is a logical name visible to agents (e.g., "github", "jira"),
	// and each value is the name of a credential in the encrypted credential store.
	// At runtime, the system resolves the actual tokens/passwords from the store
	// and injects them into the environment (e.g., GH_TOKEN for "github").
	// Agents see the logical names in their prompt but never the actual secrets.
	Credentials map[string]string `yaml:"credentials,omitempty" json:"credentials,omitempty"`

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

	// WorkspaceBaseDir overrides the base directory for the project workspace.
	// Deprecated: Per-session workspaces are now used instead. This field is
	// kept for backward compatibility with old plans created before per-session
	// isolation was introduced. New plans should not set this.
	WorkspaceBaseDir string `yaml:"workspace_base_dir,omitempty" json:"workspace_base_dir,omitempty"`

	// WorkspaceDir is the fully resolved workspace directory path for this plan.
	// Deprecated: Use FleetSession.WorkspaceDir instead. This is now only used
	// as a fallback for old plans during the transition to per-session workspaces.
	// Not persisted.
	WorkspaceDir string `yaml:"-" json:"-"`

	// ProjectSource describes where the project code lives so each session
	// can create its own isolated workspace by cloning or copying.
	ProjectSource *ProjectSourceConfig `yaml:"project_source,omitempty" json:"project_source,omitempty"`
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
	Communication        *CommunicationConfig        `yaml:"communication,omitempty"`
	Agents               map[string]FleetAgentConfig `yaml:"agents"`
	Settings             FleetSettings               `yaml:"settings,omitempty"`
	ProjectContext       *ProjectContextConfig       `yaml:"project_context,omitempty"`
	TemplateWorkspaceDir string                      `yaml:"workspace_base_dir_template,omitempty"` // template-level default

	// FleetPlan-specific fields
	Credentials      map[string]string             `yaml:"credentials,omitempty"`
	Channel          PlanChannelConfig             `yaml:"channel"`
	Artifacts        map[string]PlanArtifactConfig `yaml:"artifacts,omitempty"`
	ProjectSource    *ProjectSourceConfig          `yaml:"project_source,omitempty"`
	WorkspaceBaseDir string                        `yaml:"workspace_base_dir,omitempty"` // plan-level override (deprecated)
	Validation       PlanValidationState           `yaml:"validation,omitempty"`
	Activation       PlanActivationState           `yaml:"activation,omitempty"`
	CreatedAt        time.Time                     `yaml:"created_at,omitempty"`
	UpdatedAt        time.Time                     `yaml:"updated_at,omitempty"`
}

// MarshalYAML implements custom YAML marshalling to avoid duplicate keys
// from the embedded FleetConfig.
func (p *FleetPlan) MarshalYAML() (interface{}, error) {
	return &fleetPlanYAML{
		Name:                 p.Name,
		Key:                  p.Key,
		Description:          p.Description,
		CreatedFrom:          p.CreatedFrom,
		Communication:        p.FleetConfig.Communication,
		Agents:               p.FleetConfig.Agents,
		Settings:             p.FleetConfig.Settings,
		ProjectContext:       p.FleetConfig.ProjectContext,
		TemplateWorkspaceDir: p.FleetConfig.WorkspaceBaseDir,
		Credentials:          p.Credentials,
		Channel:              p.Channel,
		Artifacts:            p.Artifacts,
		ProjectSource:        p.ProjectSource,
		WorkspaceBaseDir:     p.WorkspaceBaseDir,
		Validation:           p.Validation,
		Activation:           p.Activation,
		CreatedAt:            p.CreatedAt,
		UpdatedAt:            p.UpdatedAt,
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
		Name:             raw.Name, // Use the plan name as the fleet name
		Description:      raw.Description,
		Communication:    raw.Communication,
		Agents:           raw.Agents,
		Settings:         raw.Settings,
		ProjectContext:   raw.ProjectContext,
		WorkspaceBaseDir: raw.TemplateWorkspaceDir,
	}
	p.Credentials = raw.Credentials
	p.Channel = raw.Channel
	p.Artifacts = raw.Artifacts
	p.ProjectSource = raw.ProjectSource
	p.WorkspaceBaseDir = raw.WorkspaceBaseDir
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
	// Type is the channel type: "chat", "github_issues"
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

// ProjectSourceConfig describes where the project code lives so each session
// can create its own isolated workspace clone or copy.
type ProjectSourceConfig struct {
	// Type is the source type: "git_repo" or "local"
	Type string `yaml:"type" json:"type"`
	// Repo is the git repository in owner/repo or full URL format (for type "git_repo")
	Repo string `yaml:"repo,omitempty" json:"repo,omitempty"`
	// Path is the local filesystem path (for type "local")
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
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

// GetSchedule returns the polling schedule for this channel.
// It checks the top-level Schedule field first, then falls back to
// config["poll_schedule"] (which the LLM sometimes uses instead),
// and finally defaults to every 5 minutes.
func (c *PlanChannelConfig) GetSchedule() string {
	if c.Schedule != "" {
		return c.Schedule
	}
	// Fallback: check the config map for poll_schedule
	if s, ok := c.Config["poll_schedule"]; ok {
		if str, ok := s.(string); ok && str != "" {
			return str
		}
	}
	return "*/5 * * * *"
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

// ResolveSessionWorkspaceDir returns the per-session workspace directory path.
// Each session gets its own isolated workspace under the sessions base directory:
//
//	<sessionsDir>/workspaces/<containerName>/
//
// The container name is the task slug (e.g., "issue-6-payoff-chart") if available,
// otherwise the first 8 characters of the session ID.
func ResolveSessionWorkspaceDir(sessionsBaseDir, sessionID, taskSlug string) string {
	containerName := taskSlug
	if containerName == "" {
		// Use first 8 chars of session ID as fallback
		containerName = sessionID
		if len(containerName) > 8 {
			containerName = containerName[:8]
		}
	}
	return filepath.Join(sessionsBaseDir, "workspaces", containerName)
}

// ResolveProjectSource derives a ProjectSourceConfig from the plan.
// If the plan has an explicit ProjectSource, that is returned.
// Otherwise, it infers the source from the artifact configuration
// (backward compatibility with old plans).
func (p *FleetPlan) ResolveProjectSource() *ProjectSourceConfig {
	if p.ProjectSource != nil {
		return p.ProjectSource
	}

	// Backward compat: derive from artifact config
	for _, artifact := range p.Artifacts {
		if artifact.Type == "git_repo" && artifact.Repo != "" {
			return &ProjectSourceConfig{
				Type: "git_repo",
				Repo: artifact.Repo,
			}
		}
		if artifact.Type == "local" && artifact.Path != "" {
			return &ProjectSourceConfig{
				Type: "local",
				Path: artifact.Path,
			}
		}
	}

	return nil
}

// ResolveWorkspaceDir derives the workspace directory for this plan.
// The base directory is resolved in priority order:
//
//  1. Plan-level WorkspaceBaseDir (set by the user during plan creation)
//  2. Template-level FleetConfig.WorkspaceBaseDir (defined in the fleet template YAML)
//  3. Default: ~/astonish_projects
//
// The final path is <base_dir>/<project-name>, where project-name comes from
// the first git_repo artifact's repo field (last segment), or the plan key.
func (p *FleetPlan) ResolveWorkspaceDir() string {
	if p.WorkspaceDir != "" {
		return p.WorkspaceDir
	}

	// Determine base directory: plan override → template default → hardcoded fallback
	baseDir := p.WorkspaceBaseDir
	if baseDir == "" {
		baseDir = p.FleetConfig.WorkspaceBaseDir
	}
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = "/root"
		}
		baseDir = filepath.Join(homeDir, "astonish_projects")
	}

	// Expand ~ to home directory
	baseDir = expandHome(baseDir)

	// Try to derive project name from the first git_repo artifact
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

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/root"
	}
	if path == "~" {
		return homeDir
	}
	// Handle ~/something
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// slugUnsafeRe matches characters that are not safe in branch names or URL slugs.
var slugUnsafeRe = regexp.MustCompile(`[^a-z0-9]+`)

// TaskSlugFromIssue derives a short, branch-safe slug from a GitHub issue
// number and title. Example: issue 6 "Improve Payoff Chart to show the Today
// Line" → "issue-6-improve-payoff-chart-to-show-the-today-line".
// The slug is capped at 60 characters (trimmed to last full word boundary).
func TaskSlugFromIssue(number int, title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = slugUnsafeRe.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")

	prefix := fmt.Sprintf("issue-%d", number)
	slug = prefix + "-" + slug

	// Cap at 60 characters on a word boundary
	if len(slug) > 60 {
		slug = slug[:60]
		if idx := strings.LastIndex(slug, "-"); idx > len(prefix) {
			slug = slug[:idx]
		}
	}
	return slug
}

// ResolveBranchPattern replaces {task} (or <task> after escaping) in a branch
// pattern with the given task slug. Returns the resolved branch name.
func ResolveBranchPattern(pattern, taskSlug string) string {
	if taskSlug == "" {
		return pattern
	}
	// Handle both the raw {task} and the escaped <task> forms
	result := strings.ReplaceAll(pattern, "{task}", taskSlug)
	result = strings.ReplaceAll(result, "<task>", taskSlug)
	return result
}
