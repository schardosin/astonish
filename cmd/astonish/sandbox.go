package astonish

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/sandbox"
)

func handleSandboxCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printSandboxUsage()
		return nil
	}

	switch args[0] {
	case "status":
		return handleSandboxStatus()
	case "init":
		return handleSandboxInit()
	case "list", "ls":
		return handleSandboxList()
	case "refresh":
		return handleSandboxRefresh()
	case "destroy", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish sandbox destroy <session-id>")
		}
		return handleSandboxDestroy(args[1])
	case "shell":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish sandbox shell <session-id>")
		}
		return handleSandboxShell(args[1])
	case "prune":
		return handleSandboxPrune()
	case "template", "tpl":
		return handleSandboxTemplateCommand(args[1:])
	default:
		printSandboxUsage()
		return fmt.Errorf("unknown sandbox subcommand: %s", args[0])
	}
}

func handleSandboxTemplateCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printSandboxTemplateUsage()
		return nil
	}

	switch args[0] {
	case "list", "ls":
		return handleTemplateList()
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish sandbox template create <name> [--description \"...\"]")
		}
		description := ""
		for i, a := range args[2:] {
			if a == "--description" || a == "-d" {
				if i+1 < len(args[2:]) {
					description = args[2:][i+1]
				}
			}
		}
		return handleTemplateCreate(args[1], description)
	case "shell":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish sandbox template shell <name>")
		}
		return handleTemplateShell(args[1])
	case "snapshot":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish sandbox template snapshot <name>")
		}
		return handleTemplateSnapshot(args[1])
	case "promote":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish sandbox template promote <name>")
		}
		return handleTemplatePromote(args[1])
	case "delete", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish sandbox template delete <name>")
		}
		return handleTemplateDelete(args[1])
	case "info":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish sandbox template info <name>")
		}
		return handleTemplateInfo(args[1])
	default:
		printSandboxTemplateUsage()
		return fmt.Errorf("unknown template subcommand: %s", args[0])
	}
}

func printSandboxUsage() {
	fmt.Println("usage: astonish sandbox {status,init,list,shell,refresh,destroy,prune,template} ...")
	fmt.Println("")
	fmt.Println("Manage session container isolation.")
	fmt.Println("")
	fmt.Println("subcommands:")
	fmt.Println("  status              Show sandbox environment info")
	fmt.Println("  init                One-time setup: create base template with core tools")
	fmt.Println("  list (ls)           List active session containers")
	fmt.Println("  shell <session-id>  Open interactive shell in a session container")
	fmt.Println("  refresh             Re-snapshot templates with updated binary (--force for all)")
	fmt.Println("  destroy (rm) <id>   Destroy a session container")
	fmt.Println("  prune               Remove orphaned session containers")
	fmt.Println("  template (tpl)      Manage container templates")
}

func printSandboxTemplateUsage() {
	fmt.Println("usage: astonish sandbox template {list,create,shell,snapshot,promote,delete,info} ...")
	fmt.Println("")
	fmt.Println("Manage container templates for session isolation.")
	fmt.Println("")
	fmt.Println("subcommands:")
	fmt.Println("  list (ls)           List all templates")
	fmt.Println("  create <name>       Create a new template from @base")
	fmt.Println("  shell <name>        Open interactive shell in template")
	fmt.Println("  snapshot <name>     Freeze template state for cloning")
	fmt.Println("  promote <name>      Override @base with this template")
	fmt.Println("  delete (rm) <name>  Delete a template")
	fmt.Println("  info <name>         Show detailed template info")
}

// --- Status ---

func handleSandboxStatus() error {
	platform := sandbox.DetectPlatform()
	fmt.Printf("Platform:         %s\n", platform)

	if platform == sandbox.PlatformUnsupported {
		fmt.Println("Status:           No container runtime available")
		fmt.Println("")
		fmt.Println("To enable session containers:")
		fmt.Println("  Linux:         apt install incus && incus admin init")
		fmt.Println("  macOS/Windows: Install Docker (any Docker-compatible runtime)")
		return nil
	}

	client, err := sandbox.Connect(platform)
	if err != nil {
		fmt.Printf("Incus connection: FAILED (%v)\n", err)
		return nil
	}

	tplRegistry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		return err
	}

	sessRegistry, err := sandbox.NewSessionRegistry()
	if err != nil {
		return err
	}

	status, err := sandbox.Status(client, tplRegistry, sessRegistry)
	if err != nil {
		return err
	}

	fmt.Printf("Incus connected:  yes\n")
	if status.IncusVersion != "" {
		fmt.Printf("Incus version:    %s\n", status.IncusVersion)
	}
	if status.StorageBackend != "" {
		fmt.Printf("Storage backend:  %s\n", status.StorageBackend)
	}
	if status.OverlayReady {
		fmt.Printf("Session creation: instant (overlayfs)\n")
	} else {
		fmt.Printf("Session creation: not configured (run 'astonish sandbox init')\n")
	}
	fmt.Printf("Templates:        %d\n", status.TemplateCount)
	fmt.Printf("Session containers: %d\n", status.SessionCount)
	if status.OrphanCount > 0 {
		fmt.Printf("Orphan containers:  %d (run 'astonish sandbox prune' to clean up)\n", status.OrphanCount)
	}

	return nil
}

// --- Init ---

func handleSandboxInit() error {
	platform := sandbox.DetectPlatform()

	if platform == sandbox.PlatformUnsupported {
		return fmt.Errorf("no container runtime available.\nLinux: install Incus (apt install incus && incus admin init)\nmacOS/Windows: install Docker (any Docker-compatible runtime)")
	}

	if platform == sandbox.PlatformDockerIncus {
		return fmt.Errorf("Docker+Incus setup is not yet implemented (Phase 5).\nCurrently only Linux with native Incus is supported")
	}

	client, err := sandbox.Connect(platform)
	if err != nil {
		return fmt.Errorf("failed to connect to Incus: %w", err)
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		return err
	}

	return sandbox.InitBaseTemplate(client, registry)
}

// --- List ---

func handleSandboxList() error {
	platform := sandbox.DetectPlatform()
	if platform == sandbox.PlatformUnsupported {
		return fmt.Errorf("no container runtime available")
	}

	client, err := sandbox.Connect(platform)
	if err != nil {
		return fmt.Errorf("failed to connect to Incus: %w", err)
	}

	sessRegistry, err := sandbox.NewSessionRegistry()
	if err != nil {
		return err
	}

	// Auto-reap stale entries whose containers no longer exist.
	// This self-heals after code paths that destroy containers without
	// cleaning the registry (e.g., fleet session exit, node cleanup).
	if reaped := sessRegistry.Reap(client); reaped > 0 {
		fmt.Printf("(cleaned up %d stale registry entries)\n\n", reaped)
	}

	entries := sessRegistry.List()
	if len(entries) == 0 {
		fmt.Println("No active session containers.")
		return nil
	}

	fmt.Printf("%-20s %-38s %-12s %-10s %-20s\n", "CONTAINER", "SESSION", "TEMPLATE", "STATUS", "CREATED")
	fmt.Printf("%-20s %-38s %-12s %-10s %-20s\n", strings.Repeat("-", 20), strings.Repeat("-", 38), strings.Repeat("-", 12), strings.Repeat("-", 10), strings.Repeat("-", 20))

	registeredNames := make(map[string]bool)
	for _, entry := range entries {
		registeredNames[entry.ContainerName] = true

		var status string
		if client.IsRunning(entry.ContainerName) {
			status = "running"
		} else if client.InstanceExists(entry.ContainerName) {
			status = "stopped"
		} else {
			status = "missing"
		}

		fmt.Printf("%-20s %-38s %-12s %-10s %-20s\n",
			entry.ContainerName,
			entry.SessionID,
			entry.TemplateName,
			status,
			entry.CreatedAt.Format("2006-01-02 15:04:05"),
		)
	}

	// Check for unregistered session containers (containers that exist in
	// Incus but have no registry entry). These are orphans from crashes,
	// failed registrations, or Incus being down during cleanup.
	sessionContainers, err := client.ListSessionContainers()
	if err == nil {
		var orphans []string
		for _, inst := range sessionContainers {
			if !registeredNames[inst.Name] {
				orphans = append(orphans, inst.Name)
			}
		}
		if len(orphans) > 0 {
			fmt.Printf("\nWarning: %d unregistered container(s) found:\n", len(orphans))
			for _, name := range orphans {
				fmt.Printf("  %s\n", name)
			}
			fmt.Println("Run 'astonish sandbox prune' to clean up.")
		}
	}

	return nil
}

// --- Refresh ---

func handleSandboxRefresh() error {
	client, err := connectOrFail()
	if err != nil {
		return err
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		return err
	}

	// Check for --force flag (refresh all templates unconditionally)
	// Without --force, only refresh templates with stale binaries
	for _, arg := range os.Args {
		if arg == "--force" || arg == "-f" {
			fmt.Println("Force-refreshing all templates...")
			return sandbox.RefreshAll(client, registry)
		}
	}

	// Smart refresh: compute current binary hash and only refresh stale templates
	currentHash, hashErr := sandbox.ComputeBinaryHash()
	if hashErr != nil {
		fmt.Printf("Warning: could not compute binary hash: %v\nFalling back to refreshing @base only.\n", hashErr)
		return sandbox.RefreshTemplate(client, registry, sandbox.BaseTemplate)
	}

	templates := registry.List()
	refreshed := 0
	for _, meta := range templates {
		if meta.BinaryHash == currentHash {
			fmt.Printf("Template %q: binary is current, skipping.\n", meta.Name)
			continue
		}

		containerName := sandbox.TemplateName(meta.Name)
		if !client.InstanceExists(containerName) {
			fmt.Printf("Template %q: container missing, skipping.\n", meta.Name)
			continue
		}

		fmt.Printf("Refreshing template %q (stale binary)...\n", meta.Name)
		if err := sandbox.RefreshTemplate(client, registry, meta.Name); err != nil {
			fmt.Printf("  Warning: failed to refresh %q: %v\n", meta.Name, err)
		} else {
			fmt.Printf("  Done.\n")
			refreshed++
		}
	}

	if refreshed == 0 {
		fmt.Println("All templates are up to date.")
	} else {
		fmt.Printf("Refreshed %d template(s).\n", refreshed)
	}

	return nil
}

// --- Destroy ---

func handleSandboxDestroy(identifier string) error {
	client, err := connectOrFail()
	if err != nil {
		return err
	}

	registry, err := sandbox.NewSessionRegistry()
	if err != nil {
		return err
	}

	// Resolve the identifier (session ID, container name, or prefix)
	sessionID, found := registry.ResolveSessionID(identifier)
	if !found {
		return fmt.Errorf("no container found for %q\nUse 'astonish sandbox list' to see active containers", identifier)
	}

	entry := registry.Get(sessionID)
	containerName := ""
	if entry != nil {
		containerName = entry.ContainerName
	}

	if err := sandbox.DestroyForSession(client, registry, sessionID); err != nil {
		return err
	}

	if containerName != "" {
		fmt.Printf("Destroyed container %s (session %s)\n", containerName, sessionID[:min(8, len(sessionID))])
	} else {
		fmt.Printf("Destroyed session %s\n", sessionID[:min(8, len(sessionID))])
	}

	return nil
}

// --- Shell (session) ---

func handleSandboxShell(sessionID string) error {
	client, err := connectOrFail()
	if err != nil {
		return err
	}

	registry, err := sandbox.NewSessionRegistry()
	if err != nil {
		return err
	}

	// Look up the container — accept session ID, container name, or prefix
	containerName := registry.GetContainerName(sessionID)
	if containerName == "" {
		for _, entry := range registry.List() {
			// Match by container name
			if entry.ContainerName == sessionID {
				containerName = entry.ContainerName
				break
			}
			// Match by session ID prefix
			if strings.HasPrefix(entry.SessionID, sessionID) {
				containerName = entry.ContainerName
				break
			}
		}
	}

	if containerName == "" {
		return fmt.Errorf("no container found for session %q\nUse 'astonish sandbox list' to see active sessions", sessionID)
	}

	if !client.InstanceExists(containerName) {
		return fmt.Errorf("container %q no longer exists (stale registry entry)", containerName)
	}

	if !client.IsRunning(containerName) {
		fmt.Printf("Starting container %q...\n", containerName)
		if err := client.StartInstance(containerName); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
	}

	// Use the incus CLI for interactive shell (it handles PTY properly)
	cmd := exec.Command("incus", "exec", containerName, "--", "bash", "-l")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Entering session container %q. Type 'exit' to leave.\n", containerName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("shell session ended with error: %w", err)
	}

	return nil
}

// --- Prune ---

func handleSandboxPrune() error {
	client, err := connectOrFail()
	if err != nil {
		return err
	}

	registry, err := sandbox.NewSessionRegistry()
	if err != nil {
		return err
	}

	// For now, pass empty map (prune all). In Phase 2+, we'll check active sessions.
	pruned, err := sandbox.PruneOrphans(client, registry, map[string]bool{})
	if err != nil {
		return err
	}

	if pruned == 0 {
		fmt.Println("No orphaned containers found.")
	} else {
		fmt.Printf("Pruned %d orphaned container(s).\n", pruned)
	}

	return nil
}

// --- Template List ---

func handleTemplateList() error {
	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		return err
	}

	templates := registry.List()
	if len(templates) == 0 {
		fmt.Println("No templates. Run 'astonish sandbox init' to create the base template.")
		return nil
	}

	fmt.Printf("%-16s %-30s %-20s %-20s %-12s\n", "NAME", "DESCRIPTION", "CREATED", "LAST SNAPSHOT", "FLEET PLANS")
	fmt.Printf("%-16s %-30s %-20s %-20s %-12s\n", strings.Repeat("-", 16), strings.Repeat("-", 30), strings.Repeat("-", 20), strings.Repeat("-", 20), strings.Repeat("-", 12))

	for _, t := range templates {
		desc := t.Description
		if len(desc) > 28 {
			desc = desc[:28] + ".."
		}
		if desc == "" && t.Name == sandbox.BaseTemplate {
			desc = "(default base template)"
		}

		snapshotStr := "-"
		if !t.SnapshotAt.IsZero() {
			snapshotStr = t.SnapshotAt.Format("2006-01-02 15:04:05")
		}

		plans := "-"
		if len(t.FleetPlans) > 0 {
			plans = strings.Join(t.FleetPlans, ", ")
		}

		fmt.Printf("%-16s %-30s %-20s %-20s %-12s\n",
			t.Name,
			desc,
			t.CreatedAt.Format("2006-01-02 15:04:05"),
			snapshotStr,
			plans,
		)
	}

	return nil
}

// --- Template Create ---

func handleTemplateCreate(name, description string) error {
	client, err := connectOrFail()
	if err != nil {
		return err
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		return err
	}

	return sandbox.CreateTemplate(client, registry, name, description)
}

// --- Template Shell ---

func handleTemplateShell(name string) error {
	// Shell uses the incus CLI directly, but we still verify connectivity first.
	platform := sandbox.DetectPlatform()
	if platform == sandbox.PlatformUnsupported {
		return fmt.Errorf("no container runtime available")
	}

	client, err := sandbox.Connect(platform)
	if err != nil {
		return fmt.Errorf("failed to connect to Incus: %w", err)
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		return fmt.Errorf("failed to load template registry: %w", err)
	}

	return sandbox.ShellIntoTemplate(client, registry, name)
}

// --- Template Snapshot ---

func handleTemplateSnapshot(name string) error {
	client, err := connectOrFail()
	if err != nil {
		return err
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		return err
	}

	return sandbox.SnapshotTemplate(client, registry, name)
}

// --- Template Promote ---

func handleTemplatePromote(name string) error {
	client, err := connectOrFail()
	if err != nil {
		return err
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		return err
	}

	return sandbox.PromoteTemplate(client, registry, name)
}

// --- Template Delete ---

func handleTemplateDelete(name string) error {
	client, err := connectOrFail()
	if err != nil {
		return err
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		return err
	}

	return sandbox.DeleteTemplate(client, registry, name)
}

// --- Template Info ---

func handleTemplateInfo(name string) error {
	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		return err
	}

	meta := registry.Get(name)
	if meta == nil {
		return fmt.Errorf("template %q not found", name)
	}

	fmt.Printf("Name:          %s\n", meta.Name)
	if meta.Description != "" {
		fmt.Printf("Description:   %s\n", meta.Description)
	}
	fmt.Printf("Created:       %s\n", meta.CreatedAt.Format(time.RFC3339))
	if !meta.SnapshotAt.IsZero() {
		fmt.Printf("Last snapshot: %s\n", meta.SnapshotAt.Format(time.RFC3339))
	} else {
		fmt.Printf("Last snapshot: (none)\n")
	}
	if meta.BasedOn != "" {
		fmt.Printf("Based on:      @%s\n", meta.BasedOn)
	}
	if meta.BinaryHash != "" {
		fmt.Printf("Binary hash:   %s\n", meta.BinaryHash[:min(16, len(meta.BinaryHash))]+"...")
	}
	if len(meta.FleetPlans) > 0 {
		fmt.Printf("Fleet plans:   %s\n", strings.Join(meta.FleetPlans, ", "))
	}

	// Try to get Incus-level info
	platform := sandbox.DetectPlatform()
	if platform != sandbox.PlatformUnsupported {
		client, err := sandbox.Connect(platform)
		if err == nil {
			containerName := sandbox.TemplateName(name)
			if client.InstanceExists(containerName) {
				inst, err := client.GetInstance(containerName)
				if err == nil {
					fmt.Printf("Container:     %s\n", inst.Name)
					fmt.Printf("Status:        %s\n", inst.Status)
				}

				hasSnap := client.HasSnapshot(containerName, sandbox.SnapshotName)
				if hasSnap {
					fmt.Printf("Snapshot:      ready (cloneable)\n")
				} else {
					fmt.Printf("Snapshot:      (none — run 'astonish sandbox template snapshot %s')\n", name)
				}
			} else {
				fmt.Printf("Container:     MISSING (metadata exists but container was deleted)\n")
			}
		}
	}

	return nil
}

// connectOrFail is a helper that detects the platform and connects to Incus,
// returning an error if not available.
func connectOrFail() (*sandbox.IncusClient, error) {
	platform := sandbox.DetectPlatform()
	if platform == sandbox.PlatformUnsupported {
		return nil, fmt.Errorf("no container runtime available")
	}

	client, err := sandbox.Connect(platform)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Incus: %w", err)
	}

	return client, nil
}
