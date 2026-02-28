package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
)

// --- Full Config API types ---

// FullConfigResponse is the response for GET /api/settings/full.
type FullConfigResponse struct {
	Chat          config.ChatConfig          `json:"chat"`
	Sessions      SessionsResponse           `json:"sessions"`
	Memory        MemoryResponse             `json:"memory"`
	Daemon        config.DaemonConfig        `json:"daemon"`
	Channels      ChannelsResponse           `json:"channels"`
	Scheduler     SchedulerResponse          `json:"scheduler"`
	Browser       config.BrowserAppConfig    `json:"browser"`
	SubAgents     SubAgentsResponse          `json:"sub_agents"`
	Skills        SkillsResponse             `json:"skills"`
	AgentIdentity config.AgentIdentityConfig `json:"agent_identity"`
}

// SessionsResponse wraps SessionConfig with resolved defaults for the UI.
type SessionsResponse struct {
	Storage    string             `json:"storage"`
	BaseDir    string             `json:"base_dir"`
	Compaction CompactionResponse `json:"compaction"`
}

// CompactionResponse wraps CompactionConfig with resolved booleans.
type CompactionResponse struct {
	Enabled        bool    `json:"enabled"`
	Threshold      float64 `json:"threshold"`
	PreserveRecent int     `json:"preserve_recent"`
}

// MemoryResponse wraps MemoryConfig with resolved defaults.
type MemoryResponse struct {
	Enabled   bool                  `json:"enabled"`
	MemoryDir string                `json:"memory_dir"`
	VectorDir string                `json:"vector_dir"`
	Embedding EmbeddingResponse     `json:"embedding"`
	Chunking  config.ChunkingConfig `json:"chunking"`
	Search    config.SearchConfig   `json:"search"`
	Sync      SyncResponse          `json:"sync"`
}

// EmbeddingResponse wraps EmbeddingConfig with secret masking.
type EmbeddingResponse struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
}

// SyncResponse wraps SyncConfig with resolved boolean.
type SyncResponse struct {
	Watch      bool `json:"watch"`
	DebounceMs int  `json:"debounce_ms"`
}

// ChannelsResponse wraps ChannelsConfig with resolved booleans and masked secrets.
type ChannelsResponse struct {
	Enabled  bool             `json:"enabled"`
	Telegram TelegramResponse `json:"telegram"`
	Email    EmailResponse    `json:"email"`
}

// TelegramResponse wraps TelegramConfig with resolved boolean and masked token.
type TelegramResponse struct {
	Enabled   bool     `json:"enabled"`
	BotToken  string   `json:"bot_token"`
	AllowFrom []string `json:"allow_from"`
}

// EmailResponse wraps EmailConfig with resolved booleans and masked password.
type EmailResponse struct {
	Enabled      bool     `json:"enabled"`
	Provider     string   `json:"provider"`
	IMAPServer   string   `json:"imap_server"`
	SMTPServer   string   `json:"smtp_server"`
	Address      string   `json:"address"`
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	PollInterval int      `json:"poll_interval"`
	AllowFrom    []string `json:"allow_from"`
	Folder       string   `json:"folder"`
	MarkRead     bool     `json:"mark_read"`
	MaxBodyChars int      `json:"max_body_chars"`
}

// SchedulerResponse wraps SchedulerConfig with resolved boolean.
type SchedulerResponse struct {
	Enabled bool `json:"enabled"`
}

// SubAgentsResponse wraps SubAgentAppConfig with resolved defaults.
type SubAgentsResponse struct {
	Enabled        bool `json:"enabled"`
	MaxDepth       int  `json:"max_depth"`
	MaxConcurrent  int  `json:"max_concurrent"`
	TaskTimeoutSec int  `json:"task_timeout_sec"`
}

// SkillsResponse wraps SkillsConfig with resolved defaults.
type SkillsResponse struct {
	Enabled   bool     `json:"enabled"`
	UserDir   string   `json:"user_dir"`
	ExtraDirs []string `json:"extra_dirs"`
	Allowlist []string `json:"allowlist"`
}

// --- Update request types ---

// FullConfigUpdateRequest is the request for PUT /api/settings/full.
// Only non-nil sections are updated (partial patch).
type FullConfigUpdateRequest struct {
	Chat          *ChatUpdateRequest      `json:"chat,omitempty"`
	Sessions      *SessionsUpdateRequest  `json:"sessions,omitempty"`
	Memory        *MemoryUpdateRequest    `json:"memory,omitempty"`
	Daemon        *DaemonUpdateRequest    `json:"daemon,omitempty"`
	Channels      *ChannelsUpdateRequest  `json:"channels,omitempty"`
	Scheduler     *SchedulerUpdateRequest `json:"scheduler,omitempty"`
	Browser       *BrowserUpdateRequest   `json:"browser,omitempty"`
	SubAgents     *SubAgentsUpdateRequest `json:"sub_agents,omitempty"`
	Skills        *SkillsUpdateRequest    `json:"skills,omitempty"`
	AgentIdentity *IdentityUpdateRequest  `json:"agent_identity,omitempty"`
}

// ChatUpdateRequest for updating chat settings.
type ChatUpdateRequest struct {
	SystemPrompt string `json:"system_prompt"`
	MaxToolCalls int    `json:"max_tool_calls"`
	MaxTools     int    `json:"max_tools"`
	AutoApprove  bool   `json:"auto_approve"`
	WorkspaceDir string `json:"workspace_dir"`
	FlowSaveDir  string `json:"flow_save_dir"`
}

// SessionsUpdateRequest for updating session settings.
type SessionsUpdateRequest struct {
	Storage    string                  `json:"storage"`
	BaseDir    string                  `json:"base_dir"`
	Compaction CompactionUpdateRequest `json:"compaction"`
}

// CompactionUpdateRequest for updating compaction settings.
type CompactionUpdateRequest struct {
	Enabled        bool    `json:"enabled"`
	Threshold      float64 `json:"threshold"`
	PreserveRecent int     `json:"preserve_recent"`
}

// MemoryUpdateRequest for updating memory settings.
type MemoryUpdateRequest struct {
	Enabled   bool                   `json:"enabled"`
	MemoryDir string                 `json:"memory_dir"`
	VectorDir string                 `json:"vector_dir"`
	Embedding EmbeddingUpdateRequest `json:"embedding"`
	Chunking  config.ChunkingConfig  `json:"chunking"`
	Search    config.SearchConfig    `json:"search"`
	Sync      SyncUpdateRequest      `json:"sync"`
}

// EmbeddingUpdateRequest for updating embedding settings.
type EmbeddingUpdateRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
}

// SyncUpdateRequest for updating sync settings.
type SyncUpdateRequest struct {
	Watch      bool `json:"watch"`
	DebounceMs int  `json:"debounce_ms"`
}

// DaemonUpdateRequest for updating daemon settings.
type DaemonUpdateRequest struct {
	Port   int               `json:"port"`
	LogDir string            `json:"log_dir"`
	Auth   AuthUpdateRequest `json:"auth"`
}

// AuthUpdateRequest for updating auth settings.
type AuthUpdateRequest struct {
	Disabled       bool `json:"disabled"`
	SessionTTLDays int  `json:"session_ttl_days"`
}

// ChannelsUpdateRequest for updating channel settings.
type ChannelsUpdateRequest struct {
	Enabled  bool                  `json:"enabled"`
	Telegram TelegramUpdateRequest `json:"telegram"`
	Email    EmailUpdateRequest    `json:"email"`
}

// TelegramUpdateRequest for updating Telegram settings.
type TelegramUpdateRequest struct {
	Enabled   bool     `json:"enabled"`
	BotToken  string   `json:"bot_token"`
	AllowFrom []string `json:"allow_from"`
}

// EmailUpdateRequest for updating email settings.
type EmailUpdateRequest struct {
	Enabled      bool     `json:"enabled"`
	Provider     string   `json:"provider"`
	IMAPServer   string   `json:"imap_server"`
	SMTPServer   string   `json:"smtp_server"`
	Address      string   `json:"address"`
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	PollInterval int      `json:"poll_interval"`
	AllowFrom    []string `json:"allow_from"`
	Folder       string   `json:"folder"`
	MarkRead     bool     `json:"mark_read"`
	MaxBodyChars int      `json:"max_body_chars"`
}

// SchedulerUpdateRequest for updating scheduler settings.
type SchedulerUpdateRequest struct {
	Enabled bool `json:"enabled"`
}

// BrowserUpdateRequest for updating browser settings.
type BrowserUpdateRequest struct {
	Headless            *bool  `json:"headless"`
	ViewportWidth       int    `json:"viewport_width"`
	ViewportHeight      int    `json:"viewport_height"`
	NoSandbox           *bool  `json:"no_sandbox"`
	ChromePath          string `json:"chrome_path"`
	UserDataDir         string `json:"user_data_dir"`
	NavigationTimeout   int    `json:"navigation_timeout"`
	Proxy               string `json:"proxy"`
	RemoteCDPURL        string `json:"remote_cdp_url"`
	FingerprintSeed     string `json:"fingerprint_seed"`
	FingerprintPlatform string `json:"fingerprint_platform"`
	HandoffBindAddress  string `json:"handoff_bind_address"`
	HandoffPort         int    `json:"handoff_port"`
}

// SubAgentsUpdateRequest for updating sub-agent settings.
type SubAgentsUpdateRequest struct {
	Enabled        bool `json:"enabled"`
	MaxDepth       int  `json:"max_depth"`
	MaxConcurrent  int  `json:"max_concurrent"`
	TaskTimeoutSec int  `json:"task_timeout_sec"`
}

// SkillsUpdateRequest for updating skills settings.
type SkillsUpdateRequest struct {
	Enabled   bool     `json:"enabled"`
	UserDir   string   `json:"user_dir"`
	ExtraDirs []string `json:"extra_dirs"`
	Allowlist []string `json:"allowlist"`
}

// IdentityUpdateRequest for updating agent identity settings.
type IdentityUpdateRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Bio      string `json:"bio"`
	Website  string `json:"website"`
	Locale   string `json:"locale"`
	Timezone string `json:"timezone"`
}

// --- Handlers ---

// GetFullConfigHandler handles GET /api/settings/full
func GetFullConfigHandler(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadAppConfig()
	if err != nil {
		http.Error(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	store := getAPICredentialStore()

	resp := FullConfigResponse{
		Chat: cfg.Chat,
		Sessions: SessionsResponse{
			Storage: cfg.Sessions.Storage,
			BaseDir: cfg.Sessions.BaseDir,
			Compaction: CompactionResponse{
				Enabled:        cfg.Sessions.Compaction.IsCompactionEnabled(),
				Threshold:      cfg.Sessions.Compaction.GetThreshold(),
				PreserveRecent: cfg.Sessions.Compaction.GetPreserveRecent(),
			},
		},
		Memory: MemoryResponse{
			Enabled:   cfg.Memory.IsMemoryEnabled(),
			MemoryDir: cfg.Memory.MemoryDir,
			VectorDir: cfg.Memory.VectorDir,
			Embedding: EmbeddingResponse{
				Provider: cfg.Memory.Embedding.Provider,
				Model:    cfg.Memory.Embedding.Model,
				BaseURL:  cfg.Memory.Embedding.BaseURL,
				APIKey:   maskSecret(cfg.Memory.Embedding.APIKey),
			},
			Chunking: cfg.Memory.Chunking,
			Search:   cfg.Memory.Search,
			Sync: SyncResponse{
				Watch:      cfg.Memory.IsWatchEnabled(),
				DebounceMs: cfg.Memory.Sync.DebounceMs,
			},
		},
		Daemon: cfg.Daemon,
		Channels: ChannelsResponse{
			Enabled: cfg.Channels.IsChannelsEnabled(),
			Telegram: TelegramResponse{
				Enabled:   cfg.Channels.Telegram.IsTelegramEnabled(),
				BotToken:  maskSecret(resolveTelegramToken(cfg, store)),
				AllowFrom: cfg.Channels.Telegram.AllowFrom,
			},
			Email: EmailResponse{
				Enabled:      cfg.Channels.Email.IsEmailEnabled(),
				Provider:     cfg.Channels.Email.Provider,
				IMAPServer:   cfg.Channels.Email.IMAPServer,
				SMTPServer:   cfg.Channels.Email.SMTPServer,
				Address:      cfg.Channels.Email.Address,
				Username:     cfg.Channels.Email.Username,
				Password:     maskSecret(resolveEmailPassword(cfg, store)),
				PollInterval: cfg.Channels.Email.GetPollInterval(),
				AllowFrom:    cfg.Channels.Email.AllowFrom,
				Folder:       cfg.Channels.Email.Folder,
				MarkRead:     cfg.Channels.Email.IsMarkRead(),
				MaxBodyChars: cfg.Channels.Email.MaxBodyChars,
			},
		},
		Scheduler: SchedulerResponse{
			Enabled: cfg.Scheduler.IsSchedulerEnabled(),
		},
		Browser: cfg.Browser,
		SubAgents: SubAgentsResponse{
			Enabled:        cfg.SubAgents.IsSubAgentsEnabled(),
			MaxDepth:       resolveIntDefault(cfg.SubAgents.MaxDepth, 2),
			MaxConcurrent:  resolveIntDefault(cfg.SubAgents.MaxConcurrent, 5),
			TaskTimeoutSec: resolveIntDefault(cfg.SubAgents.TaskTimeoutSec, 300),
		},
		Skills: SkillsResponse{
			Enabled:   cfg.Skills.IsSkillsEnabled(),
			UserDir:   cfg.Skills.GetUserSkillsDir(),
			ExtraDirs: cfg.Skills.ExtraDirs,
			Allowlist: cfg.Skills.Allowlist,
		},
		AgentIdentity: cfg.AgentIdentity,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// UpdateFullConfigHandler handles PUT /api/settings/full
func UpdateFullConfigHandler(w http.ResponseWriter, r *http.Request) {
	var req FullConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadAppConfig()
	if err != nil {
		http.Error(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	store := getAPICredentialStore()
	needsRestart := false

	if req.Chat != nil {
		cfg.Chat = config.ChatConfig{
			SystemPrompt: req.Chat.SystemPrompt,
			MaxToolCalls: req.Chat.MaxToolCalls,
			MaxTools:     req.Chat.MaxTools,
			AutoApprove:  req.Chat.AutoApprove,
			WorkspaceDir: req.Chat.WorkspaceDir,
			FlowSaveDir:  req.Chat.FlowSaveDir,
		}
	}

	if req.Sessions != nil {
		enabled := req.Sessions.Compaction.Enabled
		cfg.Sessions = config.SessionConfig{
			Storage: req.Sessions.Storage,
			BaseDir: req.Sessions.BaseDir,
			Compaction: config.CompactionConfig{
				Enabled:        &enabled,
				Threshold:      req.Sessions.Compaction.Threshold,
				PreserveRecent: req.Sessions.Compaction.PreserveRecent,
			},
		}
	}

	if req.Memory != nil {
		memEnabled := req.Memory.Enabled
		watchEnabled := req.Memory.Sync.Watch
		apiKey := req.Memory.Embedding.APIKey
		if isMaskedValue(apiKey) {
			apiKey = cfg.Memory.Embedding.APIKey
		}
		cfg.Memory = config.MemoryConfig{
			Enabled:   &memEnabled,
			MemoryDir: req.Memory.MemoryDir,
			VectorDir: req.Memory.VectorDir,
			Embedding: config.EmbeddingConfig{
				Provider: req.Memory.Embedding.Provider,
				Model:    req.Memory.Embedding.Model,
				BaseURL:  req.Memory.Embedding.BaseURL,
				APIKey:   apiKey,
			},
			Chunking: req.Memory.Chunking,
			Search:   req.Memory.Search,
			Sync: config.SyncConfig{
				Watch:      &watchEnabled,
				DebounceMs: req.Memory.Sync.DebounceMs,
			},
		}
	}

	if req.Daemon != nil {
		if cfg.Daemon.Port != req.Daemon.Port || cfg.Daemon.Auth.Disabled != req.Daemon.Auth.Disabled {
			needsRestart = true
		}
		cfg.Daemon = config.DaemonConfig{
			Port:   req.Daemon.Port,
			LogDir: req.Daemon.LogDir,
			Auth: config.StudioAuthConfig{
				Disabled:       req.Daemon.Auth.Disabled,
				SessionTTLDays: req.Daemon.Auth.SessionTTLDays,
			},
		}
	}

	if req.Channels != nil {
		chEnabled := req.Channels.Enabled
		tgEnabled := req.Channels.Telegram.Enabled
		emEnabled := req.Channels.Email.Enabled
		emMarkRead := req.Channels.Email.MarkRead

		tgToken := req.Channels.Telegram.BotToken
		if isMaskedValue(tgToken) {
			tgToken = resolveTelegramToken(cfg, store)
		}

		emPassword := req.Channels.Email.Password
		if isMaskedValue(emPassword) {
			emPassword = resolveEmailPassword(cfg, store)
		}

		cfg.Channels = config.ChannelsConfig{
			Enabled: &chEnabled,
			Telegram: config.TelegramConfig{
				Enabled:   &tgEnabled,
				BotToken:  tgToken,
				AllowFrom: req.Channels.Telegram.AllowFrom,
			},
			Email: config.EmailConfig{
				Enabled:      &emEnabled,
				Provider:     req.Channels.Email.Provider,
				IMAPServer:   req.Channels.Email.IMAPServer,
				SMTPServer:   req.Channels.Email.SMTPServer,
				Address:      req.Channels.Email.Address,
				Username:     req.Channels.Email.Username,
				Password:     emPassword,
				PollInterval: req.Channels.Email.PollInterval,
				AllowFrom:    req.Channels.Email.AllowFrom,
				Folder:       req.Channels.Email.Folder,
				MarkRead:     &emMarkRead,
				MaxBodyChars: req.Channels.Email.MaxBodyChars,
			},
		}

		if store != nil {
			secrets := make(map[string]string)
			if tgToken != "" && !isMaskedValue(tgToken) {
				secrets["channels.telegram.bot_token"] = tgToken
			}
			if emPassword != "" && !isMaskedValue(emPassword) {
				secrets["channels.email.password"] = emPassword
			}
			if len(secrets) > 0 {
				if err := store.SetSecretBatch(secrets); err != nil {
					log.Printf("Warning: Failed to save channel secrets: %v", err)
				} else {
					cfg.Channels.Telegram.BotToken = ""
					cfg.Channels.Email.Password = ""
				}
			}
		}
	}

	if req.Scheduler != nil {
		enabled := req.Scheduler.Enabled
		cfg.Scheduler = config.SchedulerConfig{
			Enabled: &enabled,
		}
	}

	if req.Browser != nil {
		cfg.Browser = config.BrowserAppConfig{
			Headless:            req.Browser.Headless,
			ViewportWidth:       req.Browser.ViewportWidth,
			ViewportHeight:      req.Browser.ViewportHeight,
			NoSandbox:           req.Browser.NoSandbox,
			ChromePath:          req.Browser.ChromePath,
			UserDataDir:         req.Browser.UserDataDir,
			NavigationTimeout:   req.Browser.NavigationTimeout,
			Proxy:               req.Browser.Proxy,
			RemoteCDPURL:        req.Browser.RemoteCDPURL,
			FingerprintSeed:     req.Browser.FingerprintSeed,
			FingerprintPlatform: req.Browser.FingerprintPlatform,
			HandoffBindAddress:  req.Browser.HandoffBindAddress,
			HandoffPort:         req.Browser.HandoffPort,
		}
	}

	if req.SubAgents != nil {
		enabled := req.SubAgents.Enabled
		cfg.SubAgents = config.SubAgentAppConfig{
			Enabled:        &enabled,
			MaxDepth:       req.SubAgents.MaxDepth,
			MaxConcurrent:  req.SubAgents.MaxConcurrent,
			TaskTimeoutSec: req.SubAgents.TaskTimeoutSec,
		}
	}

	if req.Skills != nil {
		enabled := req.Skills.Enabled
		cfg.Skills = config.SkillsConfig{
			Enabled:   &enabled,
			UserDir:   req.Skills.UserDir,
			ExtraDirs: req.Skills.ExtraDirs,
			Allowlist: req.Skills.Allowlist,
		}
	}

	if req.AgentIdentity != nil {
		cfg.AgentIdentity = config.AgentIdentityConfig{
			Name:     req.AgentIdentity.Name,
			Username: req.AgentIdentity.Username,
			Email:    req.AgentIdentity.Email,
			Bio:      req.AgentIdentity.Bio,
			Website:  req.AgentIdentity.Website,
			Locale:   req.AgentIdentity.Locale,
			Timezone: req.AgentIdentity.Timezone,
		}
	}

	if err := config.SaveAppConfig(cfg); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Reset the Studio chat agent so the next request picks up fresh config.
	GetChatManager().Reset()

	resp := map[string]interface{}{
		"status": "ok",
	}
	if needsRestart {
		resp["restart_required"] = true
		resp["restart_message"] = "Daemon port or authentication settings changed. Restart the daemon for changes to take effect."
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// --- Helper functions ---

// maskSecret masks a secret string, showing only the last 4 chars.
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}

// resolveTelegramToken resolves the Telegram bot token from credential store or config.
func resolveTelegramToken(cfg *config.AppConfig, store *credentials.Store) string {
	if store != nil {
		if val := store.GetSecret("channels.telegram.bot_token"); val != "" {
			return val
		}
	}
	return cfg.Channels.Telegram.BotToken
}

// resolveEmailPassword resolves the email password from credential store or config.
func resolveEmailPassword(cfg *config.AppConfig, store *credentials.Store) string {
	if store != nil {
		if val := store.GetSecret("channels.email.password"); val != "" {
			return val
		}
	}
	return cfg.Channels.Email.Password
}

// resolveIntDefault returns val if > 0, otherwise the default.
func resolveIntDefault(val, def int) int {
	if val <= 0 {
		return def
	}
	return val
}
