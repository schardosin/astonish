package astonish

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	persistentsession "github.com/schardosin/astonish/pkg/session"
)

func handleSessionsCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printSessionsUsage()
		return nil
	}

	subcommand := args[0]
	switch subcommand {
	case "list", "ls":
		return handleSessionsList()
	case "show":
		if len(args) < 2 {
			fmt.Println("Error: session ID required")
			fmt.Println("Usage: astonish sessions show <session-id> [flags]")
			return fmt.Errorf("session ID required")
		}
		return handleSessionsShow(args[1], args[2:])
	case "delete", "rm":
		if len(args) < 2 {
			fmt.Println("Error: session ID required")
			fmt.Println("Usage: astonish sessions delete <session-id>")
			return fmt.Errorf("session ID required")
		}
		return handleSessionsDelete(args[1])
	case "clear":
		return handleSessionsClear()
	default:
		fmt.Printf("Unknown sessions subcommand: %s\n", subcommand)
		printSessionsUsage()
		return fmt.Errorf("unknown subcommand: %s", subcommand)
	}
}

func handleSessionsList() error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if appCfg.Sessions.Storage == "memory" {
		fmt.Println("Session persistence is disabled (storage: memory).")
		fmt.Println("Remove 'sessions: { storage: memory }' from your config to enable it.")
		return nil
	}

	sessDir, err := config.GetSessionsDir(&appCfg.Sessions)
	if err != nil {
		return fmt.Errorf("failed to resolve sessions dir: %w", err)
	}

	index := persistentsession.NewSessionIndex(sessDir + "/index.json")
	data, err := index.Load()
	if err != nil {
		return fmt.Errorf("failed to load session index: %w", err)
	}

	if len(data.Sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	// Sort by UpdatedAt descending, excluding sub-sessions
	metas := make([]persistentsession.SessionMeta, 0, len(data.Sessions))
	for _, m := range data.Sessions {
		if m.ParentID != "" {
			continue // skip sub-agent sessions
		}
		metas = append(metas, m)
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})

	if len(metas) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tMESSAGES\tUPDATED\tAGE")
	for _, m := range metas {
		id := m.ID
		if len(id) > 8 {
			id = id[:8]
		}
		title := m.Title
		if title == "" {
			title = "-"
		}
		if len(title) > 40 {
			title = title[:37] + "..."
		}
		age := formatAge(m.UpdatedAt)
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n",
			id, title, m.MessageCount,
			m.UpdatedAt.Format("2006-01-02 15:04"), age)
	}
	w.Flush()

	fmt.Printf("\n%d session(s) total\n", len(metas))
	return nil
}

func handleSessionsShow(sessionID string, flags []string) error {
	// Parse flags
	opts := TraceOpts{}
	for i := 0; i < len(flags); i++ {
		switch flags[i] {
		case "--json":
			opts.JSONOutput = true
		case "--tools-only", "-t":
			opts.ToolsOnly = true
		case "--verbose", "-v":
			opts.Verbose = true
		case "--recursive", "-r":
			opts.Recursive = true
		case "--last", "-n":
			if i+1 < len(flags) {
				i++
				n, err := strconv.Atoi(flags[i])
				if err != nil {
					return fmt.Errorf("invalid value for --last: %s", flags[i])
				}
				opts.LastN = n
			} else {
				return fmt.Errorf("--last requires a number")
			}
		default:
			// Check for -n<number> form (e.g. -n20)
			if strings.HasPrefix(flags[i], "-n") && len(flags[i]) > 2 {
				n, err := strconv.Atoi(flags[i][2:])
				if err == nil {
					opts.LastN = n
					continue
				}
			}
			return fmt.Errorf("unknown flag: %s", flags[i])
		}
	}

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if appCfg.Sessions.Storage == "memory" {
		fmt.Println("Session persistence is disabled (storage: memory).")
		return nil
	}

	sessDir, err := config.GetSessionsDir(&appCfg.Sessions)
	if err != nil {
		return fmt.Errorf("failed to resolve sessions dir: %w", err)
	}

	// Try to find session by prefix match
	index := persistentsession.NewSessionIndex(sessDir + "/index.json")
	fullID, err := resolveSessionID(index, sessionID)
	if err != nil {
		return err
	}

	meta, err := index.Get(fullID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	// Load the transcript
	transcriptPath := fmt.Sprintf("%s/%s/%s/%s.jsonl", sessDir, meta.AppName, meta.UserID, meta.ID)
	transcript := persistentsession.NewTranscript(transcriptPath)

	if !transcript.Exists() {
		fmt.Printf("Session: %s\n", meta.ID)
		fmt.Printf("App:     %s\n", meta.AppName)
		fmt.Printf("User:    %s\n", meta.UserID)
		fmt.Printf("Created: %s\n", meta.CreatedAt.Format(time.RFC3339))
		fmt.Printf("Updated: %s\n", meta.UpdatedAt.Format(time.RFC3339))
		fmt.Println("\nNo transcript file found.")
		return nil
	}

	events, err := transcript.ReadEvents()
	if err != nil {
		return fmt.Errorf("failed to read transcript: %w", err)
	}

	if opts.JSONOutput {
		renderSessionTraceJSON(meta.ID, meta.AppName, meta.UserID, events, opts)
		return nil
	}

	// Print metadata header
	fmt.Printf("Session: %s\n", meta.ID)
	fmt.Printf("App:     %s\n", meta.AppName)
	fmt.Printf("User:    %s\n", meta.UserID)
	fmt.Printf("Created: %s\n", meta.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Updated: %s\n", meta.UpdatedAt.Format(time.RFC3339))
	if meta.Title != "" {
		fmt.Printf("Title:   %s\n", meta.Title)
	}
	fmt.Printf("Events:  %d\n", len(events))

	// Show sub-session count if any
	children, _ := index.ListChildren(fullID)
	if len(children) > 0 {
		fmt.Printf("Sub-sessions: %d\n", len(children))
	}

	if len(events) == 0 {
		fmt.Println("\nNo events recorded.")
		return nil
	}

	// Render the trace
	renderSessionTrace(events, opts)

	// Render child sessions inline if --recursive
	if opts.Recursive && len(children) > 0 {
		renderChildSessions(sessDir, fullID, index, opts)
	}

	return nil
}

func handleSessionsDelete(sessionID string) error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if appCfg.Sessions.Storage == "memory" {
		fmt.Println("Session persistence is disabled (storage: memory).")
		return nil
	}

	sessDir, err := config.GetSessionsDir(&appCfg.Sessions)
	if err != nil {
		return fmt.Errorf("failed to resolve sessions dir: %w", err)
	}

	index := persistentsession.NewSessionIndex(sessDir + "/index.json")
	fullID, err := resolveSessionID(index, sessionID)
	if err != nil {
		return err
	}

	// Get metadata for transcript path
	meta, err := index.Get(fullID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	// Cascade: remove child sub-session transcripts
	children, _ := index.ListChildren(fullID)
	for _, child := range children {
		childPath := fmt.Sprintf("%s/%s/%s/%s.jsonl", sessDir, child.AppName, child.UserID, child.ID)
		os.Remove(childPath)
	}

	// Remove transcript file
	transcriptPath := fmt.Sprintf("%s/%s/%s/%s.jsonl", sessDir, meta.AppName, meta.UserID, meta.ID)
	os.Remove(transcriptPath)

	// Remove from index (cascades children automatically)
	if err := index.Remove(fullID); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	if len(children) > 0 {
		fmt.Printf("Deleted session %s and %d sub-session(s)\n", fullID, len(children))
	} else {
		fmt.Printf("Deleted session %s\n", fullID)
	}
	return nil
}

func handleSessionsClear() error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if appCfg.Sessions.Storage == "memory" {
		fmt.Println("Session persistence is disabled (storage: memory).")
		return nil
	}

	sessDir, err := config.GetSessionsDir(&appCfg.Sessions)
	if err != nil {
		return fmt.Errorf("failed to resolve sessions dir: %w", err)
	}

	index := persistentsession.NewSessionIndex(sessDir + "/index.json")
	data, err := index.Load()
	if err != nil {
		return fmt.Errorf("failed to load session index: %w", err)
	}

	count := len(data.Sessions)
	if count == 0 {
		fmt.Println("No sessions to delete.")
		return nil
	}

	// Confirmation prompt
	fmt.Printf("This will delete all %d session(s). Are you sure? [y/N] ", count)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("Cancelled.")
		return nil
	}

	// Delete all transcript files
	deleted := 0
	for _, meta := range data.Sessions {
		transcriptPath := fmt.Sprintf("%s/%s/%s/%s.jsonl", sessDir, meta.AppName, meta.UserID, meta.ID)
		os.Remove(transcriptPath)
		deleted++
	}

	// Clear the index
	data.Sessions = make(map[string]persistentsession.SessionMeta)
	if err := index.Save(data); err != nil {
		return fmt.Errorf("failed to save cleared index: %w", err)
	}

	fmt.Printf("Deleted %d session(s).\n", deleted)
	return nil
}

// resolveSessionID resolves a potentially partial session ID to a full one.
func resolveSessionID(index *persistentsession.SessionIndex, partial string) (string, error) {
	data, err := index.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load index: %w", err)
	}

	// Exact match first
	if _, ok := data.Sessions[partial]; ok {
		return partial, nil
	}

	// Prefix match
	var matches []string
	for id := range data.Sessions {
		if strings.HasPrefix(id, partial) {
			matches = append(matches, id)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no session matching %q", partial)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous session ID %q matches %d sessions", partial, len(matches))
	}
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func printSessionsUsage() {
	fmt.Println("usage: astonish sessions <command> [args]")
	fmt.Println("")
	fmt.Println("Manage and inspect persistent chat sessions.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  list, ls              List all sessions")
	fmt.Println("  show <id> [flags]     Show session trace (tool calls, LLM responses, errors)")
	fmt.Println("  delete, rm <id>       Delete a session")
	fmt.Println("  clear                 Delete all sessions")
	fmt.Println("")
	fmt.Println("show flags:")
	fmt.Println("  --json                Output as JSON")
	fmt.Println("  -t, --tools-only      Only show tool calls (skip LLM text)")
	fmt.Println("  -v, --verbose         Show full tool args/results (no truncation)")
	fmt.Println("  -r, --recursive       Include sub-agent session traces inline")
	fmt.Println("  -n, --last N          Only show last N events")
	fmt.Println("")
	fmt.Println("Session IDs can be abbreviated (prefix match).")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish sessions list")
	fmt.Println("  astonish sessions show abc123")
	fmt.Println("  astonish sessions show abc123 -t")
	fmt.Println("  astonish sessions show abc123 -r              # include sub-agent traces")
	fmt.Println("  astonish sessions show abc123 -r -v           # recursive + verbose")
	fmt.Println("  astonish sessions show telegram:direct:123 --tools-only --last 20")
	fmt.Println("  astonish sessions show abc123 --json")
	fmt.Println("  astonish sessions delete abc123")
	fmt.Println("  astonish sessions clear")
}
