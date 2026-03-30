package astonish

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
)

func handleFleetCommand(args []string) error {
	if len(args) == 0 {
		printFleetUsage()
		return fmt.Errorf("no fleet subcommand provided")
	}

	switch args[0] {
	case "list", "ls":
		return handleFleetList()
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish fleet show <key>")
		}
		return handleFleetShow(strings.Join(args[1:], " "))
	case "activate":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish fleet activate <key>")
		}
		return handleFleetActivate(strings.Join(args[1:], " "))
	case "deactivate":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish fleet deactivate <key>")
		}
		return handleFleetDeactivate(strings.Join(args[1:], " "))
	case "status":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish fleet status <key>")
		}
		return handleFleetStatus(strings.Join(args[1:], " "))
	case "delete", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish fleet delete <key>")
		}
		return handleFleetDelete(strings.Join(args[1:], " "))
	case "templates":
		return handleFleetTemplates()
	default:
		printFleetUsage()
		return fmt.Errorf("unknown fleet subcommand: %s", args[0])
	}
}

func printFleetUsage() {
	fmt.Println("usage: astonish fleet {list,show,activate,deactivate,status,delete,templates}")
	fmt.Println("")
	fmt.Println("Manage fleet plans and autonomous agent teams.")
	fmt.Println("")
	fmt.Println("subcommands:")
	fmt.Println("  list (ls)            List all fleet plans")
	fmt.Println("  show <key>           Show fleet plan details")
	fmt.Println("  activate <key>       Activate a plan (start polling for new items)")
	fmt.Println("  deactivate <key>     Deactivate a plan (stop polling)")
	fmt.Println("  status <key>         Show activation status and poll details")
	fmt.Println("  delete (rm) <key>    Delete a fleet plan")
	fmt.Println("  templates            List available fleet templates")
	fmt.Println("")
	fmt.Println("To create a new fleet plan, use Studio UI: astonish studio")
	fmt.Println("Then type /fleet-plan in the chat, or use the Fleet tab.")
}

// --- API types ---

type fleetPlanListItemAPI struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	CreatedFrom string   `json:"created_from,omitempty"`
	ChannelType string   `json:"channel_type"`
	AgentCount  int      `json:"agent_count"`
	AgentNames  []string `json:"agent_names"`
	Activated   bool     `json:"activated"`
}

type fleetPlanDetailAPI struct {
	Name          string                          `json:"name"`
	Key           string                          `json:"key"`
	Description   string                          `json:"description,omitempty"`
	CreatedFrom   string                          `json:"created_from,omitempty"`
	Credentials   map[string]string               `json:"credentials,omitempty"`
	Channel       fleetPlanChannelAPI             `json:"channel"`
	Artifacts     map[string]fleetPlanArtifactAPI `json:"artifacts,omitempty"`
	Agents        map[string]json.RawMessage      `json:"agents,omitempty"`
	Communication *fleetPlanCommAPI               `json:"communication,omitempty"`
	Validation    fleetPlanValidationAPI          `json:"validation,omitempty"`
	Activation    fleetPlanActivationAPI          `json:"activation,omitempty"`
	CreatedAt     string                          `json:"created_at,omitempty"`
	UpdatedAt     string                          `json:"updated_at,omitempty"`
}

type fleetPlanChannelAPI struct {
	Type     string         `json:"type"`
	Config   map[string]any `json:"config,omitempty"`
	Schedule string         `json:"schedule,omitempty"`
}

type fleetPlanArtifactAPI struct {
	Type          string `json:"type"`
	Path          string `json:"path,omitempty"`
	Repo          string `json:"repo,omitempty"`
	BranchPattern string `json:"branch_pattern,omitempty"`
	SubPath       string `json:"sub_path,omitempty"`
	AutoPR        *bool  `json:"auto_pr,omitempty"`
}

type fleetPlanCommAPI struct {
	Flow []fleetPlanCommNodeAPI `json:"flow"`
}

type fleetPlanCommNodeAPI struct {
	Role       string   `json:"role"`
	TalksTo    []string `json:"talks_to"`
	EntryPoint bool     `json:"entry_point,omitempty"`
}

type fleetPlanValidationAPI struct {
	Status string `json:"status,omitempty"`
}

type fleetPlanActivationAPI struct {
	Activated       bool   `json:"activated"`
	SchedulerJobID  string `json:"scheduler_job_id,omitempty"`
	ActivatedAt     string `json:"activated_at,omitempty"`
	LastPollAt      string `json:"last_poll_at,omitempty"`
	LastPollStatus  string `json:"last_poll_status,omitempty"`
	LastPollError   string `json:"last_poll_error,omitempty"`
	SessionsStarted int    `json:"sessions_started,omitempty"`
}

type fleetPlanStatusAPI struct {
	Activated       bool   `json:"activated"`
	SchedulerJobID  string `json:"scheduler_job_id,omitempty"`
	ActivatedAt     string `json:"activated_at,omitempty"`
	LastPollAt      string `json:"last_poll_at,omitempty"`
	LastPollStatus  string `json:"last_poll_status,omitempty"`
	LastPollError   string `json:"last_poll_error,omitempty"`
	SessionsStarted int    `json:"sessions_started,omitempty"`
}

type fleetTemplateListItemAPI struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	AgentCount int      `json:"agent_count"`
	AgentNames []string `json:"agent_names"`
}

// --- Subcommand handlers ---

func handleFleetList() error {
	plans, err := fetchFleetPlans()
	if err != nil {
		return err
	}

	if len(plans) == 0 {
		fmt.Println("No fleet plans.")
		fmt.Println("\nCreate plans through Studio UI (astonish studio) using the /fleet-plan command.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "KEY\tNAME\tCHANNEL\tACTIVATED\tBASE\tAGENTS\n")
	fmt.Fprintf(w, "---\t----\t-------\t---------\t----\t------\n")

	for _, p := range plans {
		activated := "no"
		if p.Activated {
			activated = "yes"
		}
		agents := strings.Join(p.AgentNames, ", ")
		if len(agents) > 40 {
			agents = agents[:37] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			p.Key, p.Name, p.ChannelType, activated, p.CreatedFrom, agents)
	}
	w.Flush()

	fmt.Printf("\n%d plan(s) total\n", len(plans))
	return nil
}

func handleFleetShow(keyOrPrefix string) error {
	plan, err := findFleetPlanByKey(keyOrPrefix)
	if err != nil {
		return err
	}

	key := plan.Key

	// Fetch the full plan detail
	baseURL := getDaemonBaseURL()
	resp, err := http.Get(baseURL + "/api/fleet-plans/" + key)
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			body = []byte(fmt.Sprintf("<unreadable: %v>", readErr))
		}
		return fmt.Errorf("daemon returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Plan fleetPlanDetailAPI `json:"plan"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("invalid response from daemon")
	}

	p := result.Plan

	// Header
	fmt.Printf("Fleet Plan: %s\n", p.Name)
	fmt.Printf("Key:        %s\n", p.Key)
	if p.Description != "" {
		fmt.Printf("Description: %s\n", p.Description)
	}
	if p.CreatedFrom != "" {
		fmt.Printf("Base:       %s\n", p.CreatedFrom)
	}
	if p.CreatedAt != "" {
		created := p.CreatedAt
		if len(created) > 16 {
			created = created[:16]
		}
		fmt.Printf("Created:    %s\n", created)
	}

	// Channel
	fmt.Println()
	fmt.Println("Channel")
	fmt.Println("-------")
	fmt.Printf("  Type:     %s\n", p.Channel.Type)
	if p.Channel.Schedule != "" {
		fmt.Printf("  Schedule: %s\n", p.Channel.Schedule)
	}
	if p.Channel.Config != nil {
		for k, v := range p.Channel.Config {
			fmt.Printf("  %s: %v\n", k, v)
		}
	}

	// Communication Flow
	if p.Communication != nil && len(p.Communication.Flow) > 0 {
		fmt.Println()
		fmt.Println("Communication Flow")
		fmt.Println("------------------")
		roles := make([]string, len(p.Communication.Flow))
		for i, node := range p.Communication.Flow {
			roles[i] = node.Role
		}
		fmt.Printf("  %s\n", strings.Join(roles, " -> "))
		fmt.Println()
		for _, node := range p.Communication.Flow {
			prefix := "  "
			if node.EntryPoint {
				prefix = "* "
			}
			fmt.Printf("  %s%s talks to: %s\n", prefix, node.Role, strings.Join(node.TalksTo, ", "))
		}
	}

	// Agents
	if len(p.Agents) > 0 {
		fmt.Println()
		fmt.Println("Agents")
		fmt.Println("------")
		for name := range p.Agents {
			fmt.Printf("  - %s\n", name)
		}
	}

	// Artifacts
	if len(p.Artifacts) > 0 {
		fmt.Println()
		fmt.Println("Artifacts")
		fmt.Println("---------")
		for name, a := range p.Artifacts {
			switch a.Type {
			case "git_repo":
				autoPR := "no"
				if a.AutoPR != nil && *a.AutoPR {
					autoPR = "yes"
				}
				fmt.Printf("  %s: git_repo %s%s (branch: %s, auto-PR: %s)\n",
					name, a.Repo, a.SubPath, a.BranchPattern, autoPR)
			case "local":
				fmt.Printf("  %s: local %s\n", name, a.Path)
			default:
				fmt.Printf("  %s: %s\n", name, a.Type)
			}
		}
	}

	// Credentials
	if len(p.Credentials) > 0 {
		fmt.Println()
		fmt.Println("Credentials")
		fmt.Println("-----------")
		for logical, store := range p.Credentials {
			fmt.Printf("  %s -> %s\n", logical, store)
		}
	}

	// Activation status
	fmt.Println()
	fmt.Println("Activation")
	fmt.Println("----------")
	if p.Channel.Type == "chat" {
		fmt.Println("  Chat-based plan (launched manually, no activation needed)")
	} else if p.Activation.Activated {
		fmt.Println("  Status: ACTIVE")
		if p.Activation.ActivatedAt != "" {
			at := p.Activation.ActivatedAt
			if len(at) > 16 {
				at = at[:16]
			}
			fmt.Printf("  Since:  %s\n", at)
		}
		if p.Activation.LastPollAt != "" {
			poll := p.Activation.LastPollAt
			if len(poll) > 16 {
				poll = poll[:16]
			}
			fmt.Printf("  Last poll: %s (%s)\n", poll, p.Activation.LastPollStatus)
		}
		if p.Activation.LastPollError != "" {
			fmt.Printf("  Last error: %s\n", p.Activation.LastPollError)
		}
		fmt.Printf("  Sessions started: %d\n", p.Activation.SessionsStarted)
	} else {
		fmt.Println("  Status: NOT ACTIVE")
		fmt.Printf("  Run: astonish fleet activate %s\n", p.Key)
	}

	return nil
}

func handleFleetActivate(keyOrPrefix string) error {
	plan, err := findFleetPlanByKey(keyOrPrefix)
	if err != nil {
		return err
	}

	baseURL := getDaemonBaseURL()
	resp, err := http.Post(baseURL+"/api/fleet-plans/"+plan.Key+"/activate", "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			body = []byte(fmt.Sprintf("<unreadable: %v>", readErr))
		}
		return fmt.Errorf("activation failed: %s", strings.TrimSpace(string(body)))
	}

	fmt.Printf("Plan %q activated.\n", plan.Name)
	fmt.Printf("The scheduler will now poll for new items on the configured channel.\n")
	fmt.Printf("Run: astonish fleet status %s  to check polling status.\n", plan.Key)
	return nil
}

func handleFleetDeactivate(keyOrPrefix string) error {
	plan, err := findFleetPlanByKey(keyOrPrefix)
	if err != nil {
		return err
	}

	baseURL := getDaemonBaseURL()
	resp, err := http.Post(baseURL+"/api/fleet-plans/"+plan.Key+"/deactivate", "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			body = []byte(fmt.Sprintf("<unreadable: %v>", readErr))
		}
		return fmt.Errorf("deactivation failed: %s", strings.TrimSpace(string(body)))
	}

	fmt.Printf("Plan %q deactivated. Polling has stopped.\n", plan.Name)
	return nil
}

func handleFleetStatus(keyOrPrefix string) error {
	plan, err := findFleetPlanByKey(keyOrPrefix)
	if err != nil {
		return err
	}

	if plan.ChannelType == "chat" {
		fmt.Printf("Plan %q is a chat-based plan (launched manually, no activation needed).\n", plan.Name)
		return nil
	}

	baseURL := getDaemonBaseURL()
	resp, err := http.Get(baseURL + "/api/fleet-plans/" + plan.Key + "/status")
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			body = []byte(fmt.Sprintf("<unreadable: %v>", readErr))
		}
		return fmt.Errorf("failed to get status: %s", strings.TrimSpace(string(body)))
	}

	var status fleetPlanStatusAPI
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return fmt.Errorf("invalid response from daemon")
	}

	fmt.Printf("Fleet Plan: %s (%s)\n", plan.Name, plan.Key)
	fmt.Printf("Channel:    %s\n", plan.ChannelType)
	fmt.Println()

	if status.Activated {
		fmt.Println("Status: ACTIVE")
		if status.ActivatedAt != "" {
			at := status.ActivatedAt
			if len(at) > 16 {
				at = at[:16]
			}
			fmt.Printf("Activated:       %s\n", at)
		}
		if status.LastPollAt != "" {
			poll := status.LastPollAt
			if len(poll) > 16 {
				poll = poll[:16]
			}
			fmt.Printf("Last poll:       %s\n", poll)
			fmt.Printf("Poll status:     %s\n", status.LastPollStatus)
		} else {
			fmt.Println("Last poll:       never")
		}
		if status.LastPollError != "" {
			fmt.Printf("Last error:      %s\n", status.LastPollError)
		}
		fmt.Printf("Sessions started: %d\n", status.SessionsStarted)
		fmt.Printf("\nTo stop: astonish fleet deactivate %s\n", plan.Key)
	} else {
		fmt.Println("Status: NOT ACTIVE")
		fmt.Printf("\nTo start: astonish fleet activate %s\n", plan.Key)
	}

	return nil
}

func handleFleetDelete(keyOrPrefix string) error {
	plan, err := findFleetPlanByKey(keyOrPrefix)
	if err != nil {
		return err
	}

	// Confirmation
	fmt.Printf("Delete fleet plan %q (%s)? This cannot be undone. [y/N] ", plan.Name, plan.Key)
	var answer string
	fmt.Scanln(&answer)
	if answer != "y" && answer != "Y" {
		fmt.Println("Cancelled.")
		return nil
	}

	baseURL := getDaemonBaseURL()
	req, err := http.NewRequest(http.MethodDelete, baseURL+"/api/fleet-plans/"+plan.Key, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			body = []byte(fmt.Sprintf("<unreadable: %v>", readErr))
		}
		return fmt.Errorf("deletion failed: %s", strings.TrimSpace(string(body)))
	}

	fmt.Printf("Fleet plan %q deleted.\n", plan.Name)
	return nil
}

func handleFleetTemplates() error {
	baseURL := getDaemonBaseURL()
	resp, err := http.Get(baseURL + "/api/fleets")
	if err != nil {
		return fmt.Errorf("failed to contact daemon (is it running?): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Fleets []fleetTemplateListItemAPI `json:"fleets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("invalid response from daemon")
	}

	if len(result.Fleets) == 0 {
		fmt.Println("No fleet templates available.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "KEY\tNAME\tAGENTS\n")
	fmt.Fprintf(w, "---\t----\t------\n")

	for _, t := range result.Fleets {
		agents := strings.Join(t.AgentNames, ", ")
		fmt.Fprintf(w, "%s\t%s\t%s\n", t.Key, t.Name, agents)
	}
	w.Flush()

	fmt.Printf("\n%d template(s) available\n", len(result.Fleets))
	fmt.Println("\nTo create a plan from a template, use Studio UI: astonish studio")
	return nil
}

// --- Helpers ---

// fetchFleetPlans fetches fleet plans from the daemon API.
func fetchFleetPlans() ([]fleetPlanListItemAPI, error) {
	baseURL := getDaemonBaseURL()
	resp, err := http.Get(baseURL + "/api/fleet-plans")
	if err != nil {
		return nil, fmt.Errorf("failed to contact daemon (is it running?): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result struct {
		Plans []fleetPlanListItemAPI `json:"plans"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid response from daemon")
	}
	return result.Plans, nil
}

// findFleetPlanByKey looks up a fleet plan by key or key prefix.
func findFleetPlanByKey(keyOrPrefix string) (*fleetPlanListItemAPI, error) {
	plans, err := fetchFleetPlans()
	if err != nil {
		return nil, err
	}

	// Exact key match
	for _, p := range plans {
		if p.Key == keyOrPrefix {
			return &p, nil
		}
	}

	// Prefix match
	var matches []fleetPlanListItemAPI
	for _, p := range plans {
		if strings.HasPrefix(p.Key, keyOrPrefix) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) > 1 {
		keys := make([]string, len(matches))
		for i, m := range matches {
			keys[i] = m.Key
		}
		return nil, fmt.Errorf("ambiguous key prefix %q matches %d plans: %s", keyOrPrefix, len(matches), strings.Join(keys, ", "))
	}

	// Case-insensitive name match
	for _, p := range plans {
		if strings.EqualFold(p.Name, keyOrPrefix) {
			return &p, nil
		}
	}

	return nil, fmt.Errorf("no fleet plan found matching %q", keyOrPrefix)
}
