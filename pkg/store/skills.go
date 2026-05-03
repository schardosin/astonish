package store

// Skill represents an operational knowledge skill.
type Skill struct {
	Name        string      `json:"name" yaml:"name"`
	Description string      `json:"description" yaml:"description"`
	OS          []string    `json:"os,omitempty" yaml:"os,omitempty"`
	RequireBins []string    `json:"require_bins,omitempty" yaml:"require_bins,omitempty"`
	RequireEnv  []string    `json:"require_env,omitempty" yaml:"require_env,omitempty"`
	Metadata    interface{} `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Content     string      `json:"-" yaml:"-"`
	FilePath    string      `json:"-" yaml:"-"`
	Source      string      `json:"-" yaml:"-"`
	Directory   string      `json:"-" yaml:"-"`
}

// SkillStore manages skill definitions.
//
// In personal mode, this wraps the existing skills.LoadSkills function.
// In platform mode, skills can be org-level or team-level.
type SkillStore interface {
	// LoadAll loads all available skills from all sources.
	LoadAll() ([]Skill, error)

	// Get retrieves a skill by name.
	Get(name string) (*Skill, error)

	// Save persists a custom skill.
	Save(skill *Skill) error

	// Delete removes a custom skill.
	Delete(name string) error

	// List returns all skill names and their sources.
	List() ([]Skill, error)
}
