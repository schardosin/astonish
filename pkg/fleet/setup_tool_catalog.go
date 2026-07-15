package fleet

// SetupToolRef describes a tool available during fleet plan setup.
type SetupToolRef struct {
	Name        string `json:"name"`
	Group       string `json:"group"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// SetupToolCatalog returns all known setup tools for UI pills and editor pickers.
func SetupToolCatalog() []SetupToolRef {
	return []SetupToolRef{
		{Name: "list_credentials", Group: "credentials", Label: "List credentials", Description: "List stored credentials"},
		{Name: "save_credential", Group: "credentials", Label: "Save credential", Description: "Store a new credential"},
		{Name: "test_credential", Group: "credentials", Label: "Test credential", Description: "Verify a credential works"},
		{Name: "validate_fleet_plan", Group: "fleet", Label: "Validate plan", Description: "Validate channel and artifact configuration"},
		{Name: "save_fleet_plan", Group: "fleet", Label: "Save plan", Description: "Persist the fleet plan"},
		{Name: "update_setup_draft", Group: "fleet", Label: "Update draft", Description: "Save step values to the setup draft"},
		{Name: "get_setup_profile", Group: "fleet", Label: "Get profile", Description: "Load setup profile and completion status"},
		{Name: "save_sandbox_template", Group: "sandbox_templates", Label: "Save sandbox template", Description: "Snapshot container as reusable template"},
		{Name: "list_sandbox_templates", Group: "sandbox_templates", Label: "List templates", Description: "List available sandbox templates"},
		{Name: "use_sandbox_template", Group: "sandbox_templates", Label: "Use template", Description: "Activate a sandbox template in session"},
		{Name: "run_drill", Group: "drill", Label: "Run drill", Description: "Execute a drill test suite"},
		{Name: "inject_drill_credentials", Group: "drill", Label: "Inject drill credentials", Description: "Materialize suite credentials before start-services"},
		{Name: "shell_command", Group: "core", Label: "Shell", Description: "Run shell commands in sandbox"},
	}
}

// SetupToolGroupsForNames resolves tool group names from explicit tool names.
func SetupToolGroupsForNames(toolNames []string) []string {
	catalog := SetupToolCatalog()
	byName := map[string]string{}
	for _, ref := range catalog {
		byName[ref.Name] = ref.Group
	}
	seen := map[string]struct{}{}
	var out []string
	for _, name := range toolNames {
		if g, ok := byName[name]; ok {
			if _, dup := seen[g]; !dup {
				seen[g] = struct{}{}
				out = append(out, g)
			}
		}
	}
	return out
}
