package store

import "time"

// FlowSummary is a summary of a flow from the flow store.
type FlowSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	TapName     string   `json:"tap_name"`
	Installed   bool     `json:"installed"`
	LocalPath   string   `json:"local_path"`
}

// FlowStore manages flow/agent YAML definitions and the tap registry.
//
// In personal mode, this wraps the existing flowstore.Store.
// In platform mode, flows are stored in the team's schema.
type FlowStore interface {
	// ListAllFlows returns all flows from all taps.
	ListAllFlows() []FlowSummary

	// GetTaps returns the list of configured taps.
	GetTaps() []FlowTap

	// AddTap registers a new tap by URL or shorthand.
	AddTap(urlOrShorthand string, alias string) (string, error)

	// RemoveTap removes a tap by name.
	RemoveTap(name string) error

	// GetStoreDir returns the base directory for flow stores.
	GetStoreDir() string
}

// FlowTap represents a tap (remote flow repository).
type FlowTap struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Branch string `json:"branch"`
}

// ScheduledJob represents a scheduled automation job.
type ScheduledJob struct {
	ID                  string       `json:"id"`
	Name                string       `json:"name"`
	Mode                string       `json:"mode"` // routine, adaptive, fleet_poll
	Schedule            JobSchedule  `json:"schedule"`
	Payload             JobPayload   `json:"payload"`
	Delivery            JobDelivery  `json:"delivery"`
	Enabled             bool         `json:"enabled"`
	CreatedAt           time.Time    `json:"created_at"`
	LastRun             *time.Time   `json:"last_run,omitempty"`
	LastStatus          string       `json:"last_status"`
	LastError           string       `json:"last_error,omitempty"`
	NextRun             *time.Time   `json:"next_run,omitempty"`
	ConsecutiveFailures int          `json:"consecutive_failures"`
}

// JobSchedule defines when a job runs.
type JobSchedule struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone,omitempty"`
}

// JobPayload defines what a job executes.
type JobPayload struct {
	Flow         string            `json:"flow,omitempty"`
	Params       map[string]string `json:"params,omitempty"`
	Instructions string            `json:"instructions,omitempty"`
}

// JobDelivery defines where job results are delivered.
type JobDelivery struct {
	Channel string `json:"channel"`
	Target  string `json:"target"`
}

// SchedulerStore manages scheduled job persistence.
//
// In personal mode, this wraps the existing scheduler.Store.
// In platform mode, jobs are stored in the team's schema.
type SchedulerStore interface {
	List() []*ScheduledJob
	Get(id string) *ScheduledJob
	GetByName(name string) *ScheduledJob
	Add(job *ScheduledJob) error
	Update(job *ScheduledJob) error
	Remove(id string) error
}
