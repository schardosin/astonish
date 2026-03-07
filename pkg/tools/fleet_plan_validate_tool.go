package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// ValidateFleetPlanArgs are the arguments for the validate_fleet_plan tool.
type ValidateFleetPlanArgs struct {
	// ChannelType is the channel to validate: "github_issues", "jira", "email", "chat"
	ChannelType string `json:"channel_type"`
	// ChannelConfig holds channel-specific settings to validate
	ChannelConfig map[string]any `json:"channel_config,omitempty"`
	// Artifacts maps artifact categories to destinations to validate
	Artifacts map[string]SaveFleetPlanArtifact `json:"artifacts,omitempty"`
}

// ValidateFleetPlanResult is the result of validation.
type ValidateFleetPlanResult struct {
	Status  string            `json:"status"` // "passed", "failed"
	Checks  []ValidationCheck `json:"checks"`
	Message string            `json:"message"`
}

// ValidationCheck is a single validation step result.
type ValidationCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "passed", "failed", "skipped"
	Message string `json:"message"`
}

func validateFleetPlan(_ tool.Context, args ValidateFleetPlanArgs) (ValidateFleetPlanResult, error) {
	channelType := strings.TrimSpace(args.ChannelType)
	if channelType == "" || channelType == "chat" {
		return ValidateFleetPlanResult{
			Status:  "passed",
			Checks:  []ValidationCheck{{Name: "channel_type", Status: "passed", Message: "Chat channel requires no external validation."}},
			Message: "Chat channel validated. No external connections needed.",
		}, nil
	}

	var checks []ValidationCheck

	switch channelType {
	case "github_issues":
		checks = validateGitHubIssues(args.ChannelConfig)
	default:
		return ValidateFleetPlanResult{
			Status:  "failed",
			Checks:  []ValidationCheck{{Name: "channel_type", Status: "failed", Message: fmt.Sprintf("Unsupported channel type %q. Supported: chat, github_issues.", channelType)}},
			Message: fmt.Sprintf("Channel type %q is not yet supported for validation.", channelType),
		}, nil
	}

	// Validate artifacts
	artifactChecks := validateArtifacts(args.Artifacts)
	checks = append(checks, artifactChecks...)

	// Determine overall status
	overallStatus := "passed"
	var failedNames []string
	for _, c := range checks {
		if c.Status == "failed" {
			overallStatus = "failed"
			failedNames = append(failedNames, c.Name)
		}
	}

	message := "All validation checks passed."
	if overallStatus == "failed" {
		message = fmt.Sprintf("Validation failed for: %s. Fix these issues before saving the plan.", strings.Join(failedNames, ", "))
	}

	return ValidateFleetPlanResult{
		Status:  overallStatus,
		Checks:  checks,
		Message: message,
	}, nil
}

// validateGitHubIssues runs validation checks for GitHub Issues channel config.
func validateGitHubIssues(config map[string]any) []ValidationCheck {
	var checks []ValidationCheck

	// Check 1: gh CLI is installed
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		checks = append(checks, ValidationCheck{
			Name:    "gh_cli_installed",
			Status:  "failed",
			Message: "GitHub CLI (gh) is not installed or not in PATH. Install it from https://cli.github.com/",
		})
		// Cannot continue without gh CLI
		return checks
	}
	checks = append(checks, ValidationCheck{
		Name:    "gh_cli_installed",
		Status:  "passed",
		Message: fmt.Sprintf("GitHub CLI found at %s.", ghPath),
	})

	// Check 2: gh is authenticated
	authOut, authErr := runCommand("gh", "auth", "status")
	if authErr != nil {
		checks = append(checks, ValidationCheck{
			Name:    "gh_authenticated",
			Status:  "failed",
			Message: fmt.Sprintf("GitHub CLI is not authenticated. Run 'gh auth login' first. Error: %s", strings.TrimSpace(authOut)),
		})
		return checks
	}
	checks = append(checks, ValidationCheck{
		Name:    "gh_authenticated",
		Status:  "passed",
		Message: "GitHub CLI is authenticated.",
	})

	// Check 3: repo is specified in config
	repo := getStringFromConfig(config, "repo")
	if repo == "" {
		checks = append(checks, ValidationCheck{
			Name:    "repo_specified",
			Status:  "failed",
			Message: "No 'repo' specified in channel_config. Provide the repository as 'owner/repo' (e.g., 'myorg/myproject').",
		})
		return checks
	}
	checks = append(checks, ValidationCheck{
		Name:    "repo_specified",
		Status:  "passed",
		Message: fmt.Sprintf("Repository: %s.", repo),
	})

	// Check 4: repo exists and is accessible
	repoOut, repoErr := runCommand("gh", "repo", "view", repo, "--json", "name,owner,isPrivate,viewerPermission")
	if repoErr != nil {
		checks = append(checks, ValidationCheck{
			Name:    "repo_accessible",
			Status:  "failed",
			Message: fmt.Sprintf("Cannot access repository %q. Check that the repo exists and your credentials have access. Error: %s", repo, strings.TrimSpace(repoOut)),
		})
		return checks
	}

	// Parse repo info
	var repoInfo struct {
		Name             string                 `json:"name"`
		Owner            struct{ Login string } `json:"owner"`
		IsPrivate        bool                   `json:"isPrivate"`
		ViewerPermission string                 `json:"viewerPermission"`
	}
	if jsonErr := json.Unmarshal([]byte(repoOut), &repoInfo); jsonErr == nil {
		visibility := "public"
		if repoInfo.IsPrivate {
			visibility = "private"
		}
		checks = append(checks, ValidationCheck{
			Name:    "repo_accessible",
			Status:  "passed",
			Message: fmt.Sprintf("Repository %s/%s is accessible (%s). Your permission: %s.", repoInfo.Owner.Login, repoInfo.Name, visibility, repoInfo.ViewerPermission),
		})
	} else {
		checks = append(checks, ValidationCheck{
			Name:    "repo_accessible",
			Status:  "passed",
			Message: fmt.Sprintf("Repository %q is accessible.", repo),
		})
	}

	// Check 5: write access (need WRITE or ADMIN to post comments)
	if repoInfo.ViewerPermission != "" {
		perm := strings.ToUpper(repoInfo.ViewerPermission)
		if perm == "WRITE" || perm == "ADMIN" || perm == "MAINTAIN" {
			checks = append(checks, ValidationCheck{
				Name:    "write_access",
				Status:  "passed",
				Message: fmt.Sprintf("Write access confirmed (permission: %s). Fleet agents can post comments to issues.", repoInfo.ViewerPermission),
			})
		} else {
			checks = append(checks, ValidationCheck{
				Name:    "write_access",
				Status:  "failed",
				Message: fmt.Sprintf("Insufficient permissions (%s). Need WRITE or ADMIN access to post comments to issues.", repoInfo.ViewerPermission),
			})
		}
	} else {
		checks = append(checks, ValidationCheck{
			Name:    "write_access",
			Status:  "skipped",
			Message: "Could not determine write access. Verify manually that your credentials can post issue comments.",
		})
	}

	// Check 6: can list issues (verifies issue tracker is enabled)
	issueOut, issueErr := runCommand("gh", "issue", "list", "--repo", repo, "--limit", "1", "--json", "number")
	if issueErr != nil {
		checks = append(checks, ValidationCheck{
			Name:    "issues_accessible",
			Status:  "failed",
			Message: fmt.Sprintf("Cannot list issues on %q. The issue tracker may be disabled. Error: %s", repo, strings.TrimSpace(issueOut)),
		})
	} else {
		checks = append(checks, ValidationCheck{
			Name:    "issues_accessible",
			Status:  "passed",
			Message: "Issue tracker is accessible and enabled.",
		})
	}

	// Check 7: validate filter labels (if specified)
	labels := getStringSliceFromConfig(config, "labels")
	if len(labels) > 0 {
		labelOut, labelErr := runCommand("gh", "label", "list", "--repo", repo, "--json", "name", "--limit", "200")
		if labelErr != nil {
			checks = append(checks, ValidationCheck{
				Name:    "labels_exist",
				Status:  "failed",
				Message: fmt.Sprintf("Cannot list labels on %q. Error: %s", repo, strings.TrimSpace(labelOut)),
			})
		} else {
			var repoLabels []struct {
				Name string `json:"name"`
			}
			if jsonErr := json.Unmarshal([]byte(labelOut), &repoLabels); jsonErr == nil {
				labelMap := make(map[string]bool, len(repoLabels))
				for _, l := range repoLabels {
					labelMap[strings.ToLower(l.Name)] = true
				}
				var missing []string
				var found []string
				for _, l := range labels {
					if labelMap[strings.ToLower(l)] {
						found = append(found, l)
					} else {
						missing = append(missing, l)
					}
				}
				if len(missing) > 0 {
					checks = append(checks, ValidationCheck{
						Name:    "labels_exist",
						Status:  "failed",
						Message: fmt.Sprintf("Labels not found in repo: %s. Found: %s. Create the missing labels or update the filter.", strings.Join(missing, ", "), strings.Join(found, ", ")),
					})
				} else {
					checks = append(checks, ValidationCheck{
						Name:    "labels_exist",
						Status:  "passed",
						Message: fmt.Sprintf("All filter labels exist: %s.", strings.Join(found, ", ")),
					})
				}
			}
		}
	}

	return checks
}

// validateArtifacts validates artifact destination configs.
func validateArtifacts(artifacts map[string]SaveFleetPlanArtifact) []ValidationCheck {
	var checks []ValidationCheck

	for name, a := range artifacts {
		switch a.Type {
		case "git_repo":
			if a.Repo == "" {
				checks = append(checks, ValidationCheck{
					Name:    fmt.Sprintf("artifact_%s_repo", name),
					Status:  "failed",
					Message: fmt.Sprintf("Artifact %q has type 'git_repo' but no 'repo' specified.", name),
				})
				continue
			}
			// Verify repo access via git ls-remote
			lsOut, lsErr := runCommand("git", "ls-remote", "--exit-code", fmt.Sprintf("https://github.com/%s.git", a.Repo), "HEAD")
			if lsErr != nil {
				// Try SSH
				lsOut, lsErr = runCommand("git", "ls-remote", "--exit-code", fmt.Sprintf("git@github.com:%s.git", a.Repo), "HEAD")
			}
			if lsErr != nil {
				checks = append(checks, ValidationCheck{
					Name:    fmt.Sprintf("artifact_%s_repo", name),
					Status:  "failed",
					Message: fmt.Sprintf("Cannot access artifact repo %q. Error: %s", a.Repo, strings.TrimSpace(lsOut)),
				})
			} else {
				checks = append(checks, ValidationCheck{
					Name:    fmt.Sprintf("artifact_%s_repo", name),
					Status:  "passed",
					Message: fmt.Sprintf("Artifact repo %q is accessible.", a.Repo),
				})
			}

		case "local":
			if a.Path == "" {
				checks = append(checks, ValidationCheck{
					Name:    fmt.Sprintf("artifact_%s_path", name),
					Status:  "failed",
					Message: fmt.Sprintf("Artifact %q has type 'local' but no 'path' specified.", name),
				})
			} else {
				checks = append(checks, ValidationCheck{
					Name:    fmt.Sprintf("artifact_%s_path", name),
					Status:  "passed",
					Message: fmt.Sprintf("Local artifact path: %s (will be created if needed at runtime).", a.Path),
				})
			}

		case "":
			checks = append(checks, ValidationCheck{
				Name:    fmt.Sprintf("artifact_%s_type", name),
				Status:  "failed",
				Message: fmt.Sprintf("Artifact %q has no type specified. Use 'local' or 'git_repo'.", name),
			})

		default:
			checks = append(checks, ValidationCheck{
				Name:    fmt.Sprintf("artifact_%s_type", name),
				Status:  "failed",
				Message: fmt.Sprintf("Artifact %q has unsupported type %q. Use 'local' or 'git_repo'.", name, a.Type),
			})
		}
	}

	return checks
}

// runCommand executes a command and returns combined output and error.
func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// getStringFromConfig extracts a string value from a config map.
func getStringFromConfig(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	v, ok := config[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// getStringSliceFromConfig extracts a string slice from a config map.
// Handles both []string and []any (from JSON unmarshalling).
func getStringSliceFromConfig(config map[string]any, key string) []string {
	if config == nil {
		return nil
	}
	v, ok := config[key]
	if !ok {
		return nil
	}

	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case string:
		// Allow comma-separated string
		if val == "" {
			return nil
		}
		parts := strings.Split(val, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, p)
			}
		}
		return result
	}
	return nil
}

// GetFleetPlanValidateTools returns the fleet plan validation tool.
func GetFleetPlanValidateTools() ([]tool.Tool, error) {
	t, err := functiontool.New(functiontool.Config{
		Name: "validate_fleet_plan",
		Description: "Validate a fleet plan's external connections before saving. " +
			"Tests that the configured communication channel (e.g., GitHub repo) is accessible, " +
			"credentials are valid, required permissions exist, and artifact destinations are reachable. " +
			"ALWAYS call this tool before save_fleet_plan to verify the configuration works. " +
			"If validation fails, show the user what failed and help them fix it before retrying.",
	}, validateFleetPlan)
	if err != nil {
		return nil, err
	}
	return []tool.Tool{t}, nil
}
