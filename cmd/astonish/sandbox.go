package astonish

import (
	"fmt"
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
	fmt.Println("usage: astonish sandbox {status,init,list,refresh,destroy,prune,template} ...")
	fmt.Println("")
	fmt.Println("Manage session container isolation.")
	fmt.Println("")
	fmt.Println("subcommands:")
	fmt.Println("  status              Show sandbox environment info")
	fmt.Println("  init                One-time setup: create base template with core tools")
	fmt.Println("  list (ls)           List active session containers")
	fmt.Println("  refresh             Re-snapshot the @base template")
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
	fmt.Printf("Templates:        %d\n", status.TemplateCount)
	fmt.Printf("Session containers: %d\n", status.SessionCount)

	if status.StorageBackend == "dir" {
		fmt.Println("")
		fmt.Println("Note: 'dir' backend uses full copies (10-30s per clone).")
		fmt.Println("      ZFS or btrfs is recommended for fast CoW clones (<1s).")
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

	entries := sessRegistry.List()
	if len(entries) == 0 {
		fmt.Println("No active session containers.")
		return nil
	}

	fmt.Printf("%-20s %-12s %-12s %-10s %-20s\n", "CONTAINER", "SESSION", "TEMPLATE", "STATUS", "CREATED")
	fmt.Printf("%-20s %-12s %-12s %-10s %-20s\n", strings.Repeat("-", 20), strings.Repeat("-", 12), strings.Repeat("-", 12), strings.Repeat("-", 10), strings.Repeat("-", 20))

	for _, entry := range entries {
		var status string
		if client.IsRunning(entry.ContainerName) {
			status = "running"
		} else if client.InstanceExists(entry.ContainerName) {
			status = "stopped"
		} else {
			status = "missing"
		}

		sessionShort := entry.SessionID
		if len(sessionShort) > 10 {
			sessionShort = sessionShort[:10] + ".."
		}

		fmt.Printf("%-20s %-12s %-12s %-10s %-20s\n",
			entry.ContainerName,
			sessionShort,
			entry.TemplateName,
			status,
			entry.CreatedAt.Format("2006-01-02 15:04:05"),
		)
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

	return sandbox.RefreshBase(client, registry)
}

// --- Destroy ---

func handleSandboxDestroy(sessionID string) error {
	client, err := connectOrFail()
	if err != nil {
		return err
	}

	registry, err := sandbox.NewSessionRegistry()
	if err != nil {
		return err
	}

	return sandbox.DestroyForSession(client, registry, sessionID)
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

	_ = client // verify connection only
	return sandbox.ShellIntoTemplate(client, name)
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
