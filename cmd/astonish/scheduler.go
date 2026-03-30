package astonish

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/schardosin/astonish/pkg/config"
)

func handleSchedulerCommand(args []string) error {
	if len(args) == 0 {
		printSchedulerUsage()
		return fmt.Errorf("no scheduler subcommand provided")
	}

	switch args[0] {
	case "list", "ls":
		return handleSchedulerList()
	case "enable":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish scheduler enable <name>")
		}
		return handleSchedulerToggle(strings.Join(args[1:], " "), true)
	case "disable":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish scheduler disable <name>")
		}
		return handleSchedulerToggle(strings.Join(args[1:], " "), false)
	case "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish scheduler remove <name>")
		}
		return handleSchedulerRemove(strings.Join(args[1:], " "))
	case "run":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish scheduler run <name>")
		}
		return handleSchedulerRun(strings.Join(args[1:], " "))
	case "status":
		return handleSchedulerStatus()
	default:
		printSchedulerUsage()
		return fmt.Errorf("unknown scheduler subcommand: %s", args[0])
	}
}

func printSchedulerUsage() {
	fmt.Println("usage: astonish scheduler {list,enable,disable,remove,run,status}")
	fmt.Println("")
	fmt.Println("Manage scheduled jobs. Jobs are created through chat (talk to the AI).")
	fmt.Println("")
	fmt.Println("subcommands:")
	fmt.Println("  list (ls)            List all scheduled jobs")
	fmt.Println("  enable <name>        Enable a scheduled job")
	fmt.Println("  disable <name>       Disable a scheduled job")
	fmt.Println("  remove (rm) <name>   Remove a scheduled job")
	fmt.Println("  run <name>           Trigger immediate execution")
	fmt.Println("  status               Show scheduler status")
}

// schedulerJobAPI is the JSON shape returned by the daemon API.
type schedulerJobAPI struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Mode       string         `json:"mode"`
	Schedule   jobScheduleAPI `json:"schedule"`
	Payload    jobPayloadAPI  `json:"payload"`
	Delivery   jobDeliveryAPI `json:"delivery"`
	Enabled    bool           `json:"enabled"`
	LastRun    *string        `json:"last_run,omitempty"`
	LastStatus string         `json:"last_status"`
	LastError  string         `json:"last_error,omitempty"`
	NextRun    *string        `json:"next_run,omitempty"`
	Failures   int            `json:"consecutive_failures"`
	CreatedAt  string         `json:"created_at"`
}

type jobScheduleAPI struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone,omitempty"`
}

type jobPayloadAPI struct {
	Flow         string            `json:"flow,omitempty"`
	Params       map[string]string `json:"params,omitempty"`
	Instructions string            `json:"instructions,omitempty"`
}

type jobDeliveryAPI struct {
	Channel string `json:"channel"`
	Target  string `json:"target"`
}

func handleSchedulerList() error {
	jobs, err := fetchSchedulerJobs()
	if err != nil {
		return err
	}

	if len(jobs) == 0 {
		fmt.Println("No scheduled jobs.")
		fmt.Println("\nCreate jobs by chatting with the AI: \"Schedule X to run every day at 9am\"")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "NAME\tMODE\tSCHEDULE\tENABLED\tSTATUS\tNEXT RUN\n")
	fmt.Fprintf(w, "----\t----\t--------\t-------\t------\t--------\n")

	for _, j := range jobs {
		enabled := "yes"
		if !j.Enabled {
			enabled = "no"
		}
		nextRun := "-"
		if j.NextRun != nil {
			nextRun = *j.NextRun
			// Shorten ISO format for display
			if len(nextRun) > 16 {
				nextRun = nextRun[:16]
			}
		}
		tz := ""
		if j.Schedule.Timezone != "" {
			tz = " (" + j.Schedule.Timezone + ")"
		}
		fmt.Fprintf(w, "%s\t%s\t%s%s\t%s\t%s\t%s\n",
			j.Name, j.Mode, j.Schedule.Cron, tz, enabled, j.LastStatus, nextRun)
	}
	w.Flush()

	fmt.Printf("\n%d job(s) total\n", len(jobs))
	return nil
}

func handleSchedulerToggle(idOrName string, enable bool) error {
	job, err := findJobByIDOrName(idOrName)
	if err != nil {
		return err
	}

	job.Enabled = enable
	data, _ := json.Marshal(job)

	baseURL := getDaemonBaseURL()
	req, err := http.NewRequest(http.MethodPut, baseURL+"/api/scheduler/jobs/"+job.ID, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}

	action := "enabled"
	if !enable {
		action = "disabled"
	}
	fmt.Printf("Job %q %s\n", job.Name, action)
	return nil
}

func handleSchedulerRemove(idOrName string) error {
	job, err := findJobByIDOrName(idOrName)
	if err != nil {
		return err
	}

	baseURL := getDaemonBaseURL()
	req, err := http.NewRequest(http.MethodDelete, baseURL+"/api/scheduler/jobs/"+job.ID, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}

	fmt.Printf("Job %q removed\n", job.Name)
	return nil
}

func handleSchedulerRun(idOrName string) error {
	job, err := findJobByIDOrName(idOrName)
	if err != nil {
		return err
	}

	baseURL := getDaemonBaseURL()
	fmt.Printf("Running job %q...\n", job.Name)

	resp, err := http.Post(baseURL+"/api/scheduler/jobs/"+job.ID+"/run", "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Result string `json:"result"`
		Error  string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Error != "" {
		fmt.Printf("Error: %s\n", result.Error)
	}
	if result.Result != "" {
		fmt.Println("\nResult:")
		fmt.Println(result.Result)
	}
	return nil
}

func handleSchedulerStatus() error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println("Scheduler Status")
	fmt.Println("================")
	fmt.Printf("Enabled: %v\n", appCfg.Scheduler.IsSchedulerEnabled())

	jobs, err := fetchSchedulerJobs()
	if err != nil {
		fmt.Printf("Jobs: (daemon not reachable: %v)\n", err)
		return nil
	}

	enabled := 0
	for _, j := range jobs {
		if j.Enabled {
			enabled++
		}
	}
	fmt.Printf("Total jobs: %d (%d enabled)\n", len(jobs), enabled)
	return nil
}

// fetchSchedulerJobs fetches jobs from the daemon API.
func fetchSchedulerJobs() ([]schedulerJobAPI, error) {
	baseURL := getDaemonBaseURL()
	resp, err := http.Get(baseURL + "/api/scheduler/jobs")
	if err != nil {
		return nil, fmt.Errorf("failed to contact daemon (is it running?): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon returned HTTP %d — check that the daemon is running and accessible", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Jobs []schedulerJobAPI `json:"jobs"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid response from daemon")
	}
	return result.Jobs, nil
}

// findJobByIDOrName looks up a job by full/partial ID or by name.
func findJobByIDOrName(idOrName string) (*schedulerJobAPI, error) {
	jobs, err := fetchSchedulerJobs()
	if err != nil {
		return nil, err
	}

	// Exact ID match
	for _, j := range jobs {
		if j.ID == idOrName {
			return &j, nil
		}
	}

	// Partial ID match
	var partials []schedulerJobAPI
	for _, j := range jobs {
		if strings.HasPrefix(j.ID, idOrName) {
			partials = append(partials, j)
		}
	}
	if len(partials) == 1 {
		return &partials[0], nil
	}
	if len(partials) > 1 {
		return nil, fmt.Errorf("ambiguous ID prefix %q matches %d jobs", idOrName, len(partials))
	}

	// Name match (case-insensitive)
	for _, j := range jobs {
		if strings.EqualFold(j.Name, idOrName) {
			return &j, nil
		}
	}

	return nil, fmt.Errorf("no job found matching %q", idOrName)
}

// getDaemonBaseURL returns the daemon HTTP base URL.
func getDaemonBaseURL() string {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return "http://localhost:9393"
	}
	return fmt.Sprintf("http://localhost:%d", appCfg.Daemon.GetPort())
}
