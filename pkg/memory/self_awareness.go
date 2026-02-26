package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SelfMDConfig holds everything needed to generate SELF.md.
type SelfMDConfig struct {
	ConfigPath       string            // Path to config.yaml
	MCPConfigPath    string            // Path to mcp_config.json
	ProviderName     string            // Active provider name
	ModelName        string            // Active model name
	Providers        map[string]string // provider instance -> "type: model" summary
	MCPServers       []MCPServerInfo   // Installed MCP servers
	FlowEntries      []FlowInfo        // Saved flows from registry
	FlowDir          string            // Path to agents/ directory
	MemoryDir        string            // Path to memory/ directory
	MemoryEnabled    bool              // Whether memory system is active
	EmbeddingInfo    string            // Embedding provider summary (e.g., "openai (text-embedding-3-small)")
	ChunkCount       int               // Number of indexed chunks
	InternalTools    int               // Count of internal tools
	MCPTools         int               // Count of MCP tools
	CoreFiles        []string          // Core memory files found (MEMORY.md, INSTRUCTIONS.md, etc.)
	KnowledgeFiles   []string          // Knowledge tier files found
	SubAgentsEnabled bool              // Whether sub-agent delegation is available
	SkillNames       []string          // Names of loaded eligible skills

	// Channels
	ChannelsEnabled bool   // Whether the channels system is enabled
	TelegramEnabled bool   // Telegram channel active
	EmailEnabled    bool   // Inbound email channel active (IMAP polling)
	EmailAddress    string // The agent's email address (e.g., "agent@gmail.com")
	EmailToolsAvail bool   // Email tools available (true if IMAP/SMTP creds exist, even if channel disabled)

	// Browser / Handoff
	HandoffAvailable bool // Whether browser_request_human tool is registered

	// Agent Identity (web portal persona)
	IdentityConfigured bool   // Whether agent_identity is configured in config.yaml
	IdentityName       string // Display name for registrations
	IdentityUsername   string // Base username for registrations
	IdentityEmail      string // Email for registrations
}

// MCPServerInfo describes an installed MCP server for SELF.md.
type MCPServerInfo struct {
	Name     string
	Category string // "web", "browser", or ""
	Keyless  bool
	Active   bool
}

// FlowInfo describes a saved flow for SELF.md.
type FlowInfo struct {
	Name        string
	Description string
}

// GenerateSelfMD produces the markdown content for SELF.md from the given config.
func GenerateSelfMD(cfg *SelfMDConfig) string {
	var sb strings.Builder

	sb.WriteString("# Astonish Self-Configuration\n\n")
	sb.WriteString(fmt.Sprintf("_Auto-generated at %s. Do not edit manually — changes will be overwritten._\n\n", time.Now().Format("2006-01-02 15:04 MST")))

	// Config file location
	sb.WriteString("## Config Files\n")
	if cfg.ConfigPath != "" {
		sb.WriteString(fmt.Sprintf("- Main config: `%s`\n", cfg.ConfigPath))
	}
	if cfg.MCPConfigPath != "" {
		sb.WriteString(fmt.Sprintf("- MCP servers: `%s`\n", cfg.MCPConfigPath))
	}
	sb.WriteString("Use `read_file` to inspect, `edit_file` to modify. Provider/model changes take effect automatically on the next message.\n\n")

	// Active provider
	sb.WriteString("## Active Provider\n")
	sb.WriteString(fmt.Sprintf("- Provider: %s\n", cfg.ProviderName))
	sb.WriteString(fmt.Sprintf("- Model: %s\n", cfg.ModelName))
	sb.WriteString("To switch: edit `general.default_provider`/`general.default_model` in config.yaml. Changes are detected and applied automatically.\n\n")

	// All providers
	if len(cfg.Providers) > 0 {
		sb.WriteString("## All Configured Providers\n")
		// Sort for stable output
		var names []string
		for name := range cfg.Providers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			info := cfg.Providers[name]
			marker := ""
			if name == cfg.ProviderName {
				marker = " **(active)**"
			}
			sb.WriteString(fmt.Sprintf("- %s: %s%s\n", name, info, marker))
		}
		sb.WriteString("\n")
	}

	// MCP servers
	if len(cfg.MCPServers) > 0 {
		sb.WriteString("## MCP Servers\n")
		sb.WriteString(fmt.Sprintf("Config: `%s`\n", cfg.MCPConfigPath))
		for _, srv := range cfg.MCPServers {
			status := "active"
			if !srv.Active {
				status = "inactive"
			}
			extra := ""
			if srv.Keyless {
				extra = ", keyless"
			}
			if srv.Category != "" {
				extra += ", " + srv.Category
			}
			sb.WriteString(fmt.Sprintf("- %s: %s%s\n", srv.Name, status, extra))
		}
		sb.WriteString("To add/remove: edit mcp_config.json or use `astonish setup`.\n\n")
	}

	// Saved flows
	if len(cfg.FlowEntries) > 0 {
		sb.WriteString("## Saved Flows\n")
		if cfg.FlowDir != "" {
			sb.WriteString(fmt.Sprintf("Directory: `%s`\n", cfg.FlowDir))
		}
		for _, f := range cfg.FlowEntries {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", f.Name, f.Description))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("## Saved Flows\nNo flows saved yet. Flows are auto-saved after reusable multi-step tasks.\n\n")
	}

	// Tools summary
	sb.WriteString("## Tools\n")
	sb.WriteString(fmt.Sprintf("- Internal tools: %d\n", cfg.InternalTools))
	sb.WriteString(fmt.Sprintf("- MCP tools: %d\n", cfg.MCPTools))
	sb.WriteString(fmt.Sprintf("- Total: %d\n\n", cfg.InternalTools+cfg.MCPTools))

	// Sub-agents
	if cfg.SubAgentsEnabled {
		sb.WriteString("## Sub-Agents\n")
		sb.WriteString("- Task delegation: enabled (delegate_tasks tool)\n")
		sb.WriteString("- Parallel sub-agent execution for independent tasks\n\n")
	}

	// Skills
	if len(cfg.SkillNames) > 0 {
		sb.WriteString("## Skills\n")
		sb.WriteString(fmt.Sprintf("- Loaded: %d eligible skills\n", len(cfg.SkillNames)))
		sb.WriteString(fmt.Sprintf("- Available: %s\n", strings.Join(cfg.SkillNames, ", ")))
		sb.WriteString("- Retrieval: automatic (vector search) + explicit (skill_lookup tool)\n\n")
	}

	// Channels
	if cfg.ChannelsEnabled || cfg.EmailToolsAvail {
		sb.WriteString("## Channels\n")
		if cfg.ChannelsEnabled {
			sb.WriteString("- Channels system: enabled\n")
		}
		if cfg.TelegramEnabled {
			sb.WriteString("- Telegram: active (inbound messages trigger agent runs)\n")
		}
		if cfg.EmailEnabled {
			sb.WriteString(fmt.Sprintf("- Email channel: active (IMAP polling, address: %s)\n", cfg.EmailAddress))
			sb.WriteString("  - Inbound emails from allowed senders trigger agent runs\n")
			sb.WriteString("  - Agent replies via SMTP with proper threading\n")
		} else if cfg.EmailAddress != "" && !cfg.EmailEnabled {
			sb.WriteString(fmt.Sprintf("- Email channel: disabled (address: %s)\n", cfg.EmailAddress))
		}
		sb.WriteString("\n")
	}

	// Email tools (shown when available, regardless of channel state)
	if cfg.EmailToolsAvail {
		sb.WriteString("## Email Tools\n")
		sb.WriteString(fmt.Sprintf("- Email address: %s\n", cfg.EmailAddress))
		sb.WriteString("- 8 tools available: email_list, email_read, email_search, email_send, email_reply, email_mark_read, email_delete, email_wait\n")
		sb.WriteString("- Tools work independently of the email channel (no polling required)\n")
		sb.WriteString("- **email_wait**: polls inbox for matching email with timeout. Use for registration flows where you sign up on a website and need to receive a verification email. Supports substring matching on sender/subject.\n")
		if !cfg.EmailEnabled {
			sb.WriteString("- Note: the inbound email channel is disabled, but you can still read, send, and search emails using these tools\n")
		}
		sb.WriteString("\n")
	}

	// Browser handoff
	if cfg.HandoffAvailable {
		sb.WriteString("## Browser Handoff\n")
		sb.WriteString("- Tool: browser_request_human (pauses agent, exposes browser via CDP)\n")
		sb.WriteString("- User connects via chrome://inspect to interact with the browser directly\n")
		sb.WriteString("- Primary use: CAPTCHA solving, complex auth flows, payment forms\n")
		sb.WriteString("- Completion: auto-detected when DevTools disconnects, or user visits /handoff/done\n")
		sb.WriteString("\n")
	}

	// Agent Identity
	if cfg.IdentityConfigured {
		sb.WriteString("## Agent Identity\n")
		sb.WriteString("Configured persona for web portal registrations:\n")
		if cfg.IdentityName != "" {
			sb.WriteString(fmt.Sprintf("- Name: %s\n", cfg.IdentityName))
		}
		if cfg.IdentityUsername != "" {
			sb.WriteString(fmt.Sprintf("- Username: %s\n", cfg.IdentityUsername))
		}
		if cfg.IdentityEmail != "" {
			sb.WriteString(fmt.Sprintf("- Email: %s\n", cfg.IdentityEmail))
		}
		sb.WriteString("Use these when registering on websites. Combine with email tools (email_wait) for verification.\n")
		sb.WriteString("\n")
	}

	// Memory system
	sb.WriteString("## Memory System\n")
	if cfg.MemoryEnabled {
		sb.WriteString("- Status: enabled\n")
		if cfg.EmbeddingInfo != "" {
			sb.WriteString(fmt.Sprintf("- Embedding: %s\n", cfg.EmbeddingInfo))
		}
		sb.WriteString(fmt.Sprintf("- Indexed chunks: %d\n", cfg.ChunkCount))
		if cfg.MemoryDir != "" {
			sb.WriteString(fmt.Sprintf("- Memory directory: `%s`\n", cfg.MemoryDir))
		}
		if len(cfg.CoreFiles) > 0 {
			sb.WriteString(fmt.Sprintf("- Core files: %s\n", strings.Join(cfg.CoreFiles, ", ")))
		}
		if len(cfg.KnowledgeFiles) > 0 {
			sb.WriteString(fmt.Sprintf("- Knowledge files: %s\n", strings.Join(cfg.KnowledgeFiles, ", ")))
		}
	} else {
		sb.WriteString("- Status: disabled\n")
	}
	sb.WriteString("\n")

	// Self-management guide
	sb.WriteString("## Self-Management\n")
	sb.WriteString("The user may ask you to change configuration. Common operations:\n")
	sb.WriteString("- **Switch model**: edit `general.default_model` in config.yaml (takes effect immediately)\n")
	sb.WriteString("- **Switch provider**: edit `general.default_provider` in config.yaml (takes effect immediately)\n")
	sb.WriteString("- **Add provider**: add a new section under `providers` in config.yaml\n")
	sb.WriteString("- **Install MCP server**: add entry to mcp_config.json, then run `astonish tools refresh`\n")
	sb.WriteString("- **Disable memory**: set `memory.enabled: false` in config.yaml\n")
	sb.WriteString("- **Show config**: read and display config.yaml\n")
	sb.WriteString("- **Show MCP config**: read and display mcp_config.json\n")
	sb.WriteString("\n### Credential CLI Commands\n")
	sb.WriteString("If the user asks about managing credentials from the command line:\n")
	sb.WriteString("- `astonish credential add <name>` — Interactive TUI form (no flags; prompts for type and fields)\n")
	sb.WriteString("- `astonish credential list` — Show stored credentials (metadata only, never secret values)\n")
	sb.WriteString("- `astonish credential remove <name>` — Remove a credential\n")
	sb.WriteString("- `astonish credential test <name>` — Test a credential (OAuth: token flow, others: config check)\n")
	sb.WriteString("Available types: api_key, bearer, basic, password (SSH/FTP/SMTP/databases), oauth_client_credentials, oauth_authorization_code (Google/GitHub/etc.)\n")
	sb.WriteString("**Note:** Prefer using your `save_credential` tool directly over suggesting CLI commands — it's faster and doesn't require the user to leave the chat.\n")
	sb.WriteString("\n### Channel CLI Commands\n")
	sb.WriteString("If the user asks about managing communication channels:\n")
	sb.WriteString("- `astonish channels setup telegram` — Interactive Telegram bot setup (token validation, user ID detection)\n")
	sb.WriteString("- `astonish channels setup email` — Interactive IMAP/SMTP email setup (connection test, allowlist config)\n")
	sb.WriteString("- `astonish channels status` — Show active channel status\n")
	sb.WriteString("- `astonish channels disable` — Disable all channels\n")
	sb.WriteString("Channels require the daemon to be running (`astonish daemon start`).\n")
	sb.WriteString("\nIf a requested model or provider is not in this file, read config.yaml directly to check the full configuration.\n")

	return sb.String()
}

// SelfMDPath returns the path to SELF.md within the memory directory.
func SelfMDPath(memoryDir string) string {
	return filepath.Join(memoryDir, "SELF.md")
}

// WriteSelfMD writes the generated SELF.md to the memory directory.
func WriteSelfMD(memoryDir, content string) error {
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return fmt.Errorf("failed to create memory directory: %w", err)
	}
	path := SelfMDPath(memoryDir)
	return os.WriteFile(path, []byte(content), 0644)
}

// LoadSelfMD reads SELF.md from the memory directory.
// Returns empty string if the file doesn't exist.
func LoadSelfMD(memoryDir string) (string, error) {
	path := SelfMDPath(memoryDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read self-awareness file: %w", err)
	}
	return string(data), nil
}
