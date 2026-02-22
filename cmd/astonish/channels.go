package astonish

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/daemon"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func handleChannelsCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printChannelsUsage()
		return nil
	}

	subcommand := args[0]
	switch subcommand {
	case "status":
		return handleChannelsStatus()
	case "setup":
		if len(args) < 2 {
			fmt.Println("usage: astonish channels setup <channel>")
			fmt.Println("")
			fmt.Println("Available channels:")
			fmt.Println("  telegram      Set up Telegram bot integration")
			return nil
		}
		switch args[1] {
		case "telegram":
			return handleTelegramSetup()
		default:
			return fmt.Errorf("unknown channel: %s (available: telegram)", args[1])
		}
	case "disable":
		if len(args) < 2 {
			fmt.Println("usage: astonish channels disable <channel>")
			return nil
		}
		return handleChannelDisable(args[1])
	default:
		fmt.Printf("Unknown channels subcommand: %s\n", subcommand)
		printChannelsUsage()
		return fmt.Errorf("unknown subcommand: %s", subcommand)
	}
}

// handleTelegramSetup runs the interactive Telegram bot setup flow.
func handleTelegramSetup() error {
	cfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if already configured
	if cfg.Channels.Telegram.IsTelegramEnabled() {
		fmt.Println()
		fmt.Printf("  Telegram is already configured (bot token: ...%s)\n",
			maskToken(cfg.Channels.Telegram.BotToken))
		if len(cfg.Channels.Telegram.AllowFrom) > 0 {
			fmt.Printf("  Allowed users: %s\n", strings.Join(cfg.Channels.Telegram.AllowFrom, ", "))
		}
		fmt.Println()

		var reconfigure bool
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Reconfigure Telegram?").
					Description("This will replace your current Telegram configuration").
					Affirmative("Yes, reconfigure").
					Negative("No, keep current").
					Value(&reconfigure),
			),
		).Run()
		if err != nil || !reconfigure {
			if err == nil {
				fmt.Println("Keeping current configuration.")
			}
			return err
		}
	}

	// Step 1: Introduction and readiness check
	fmt.Println()
	channelsPrintHeader("Telegram Bot Setup")
	fmt.Println()
	fmt.Println("  To connect Astonish to Telegram, you need a bot token.")
	fmt.Println()
	fmt.Println("  1. Open Telegram and search for @BotFather")
	fmt.Println("  2. Send /newbot and follow the instructions")
	fmt.Println("  3. Copy the bot token you receive")
	fmt.Println()

	var ready string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Have you already created a bot?").
				Options(
					huh.NewOption("Yes, I have a token", "yes"),
					huh.NewOption("Not yet — I'll open BotFather first", "no"),
				).
				Value(&ready),
		),
	).Run()
	if err != nil {
		return nil // user aborted
	}

	if ready == "no" {
		fmt.Println()
		fmt.Println("  Open this link to create your bot:")
		fmt.Println()
		fmt.Printf("    \033[4;36mhttps://t.me/BotFather\033[0m\n")
		fmt.Println()
		fmt.Println("  Once you have your token, run this command again:")
		fmt.Println("    astonish channels setup telegram")
		fmt.Println()
		return nil
	}

	// Step 2: Token input + validation
	var botToken string
	var botUsername string

	for {
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Bot Token").
					Description("Paste the token from BotFather").
					EchoMode(huh.EchoModePassword).
					Value(&botToken),
			),
		).Run()
		if err != nil {
			return nil // user aborted
		}

		botToken = strings.TrimSpace(botToken)
		if botToken == "" {
			fmt.Println("  Token cannot be empty. Please try again.")
			continue
		}

		// Validate token by connecting to Telegram
		channelsRunSpinner("Connecting to Telegram...")
		bot, botErr := tgbotapi.NewBotAPI(botToken)
		if botErr != nil {
			fmt.Printf("  \033[31mInvalid token: %v\033[0m\n", botErr)
			fmt.Println("  Please check your token and try again.")
			fmt.Println()
			botToken = ""
			continue
		}

		botUsername = bot.Self.UserName
		fmt.Printf("  \033[32mConnected as @%s\033[0m\n\n", botUsername)
		break
	}

	// Step 3: Access control — require at least one allowed user.
	// The bot has full tool access with auto-approve, so open access is never safe.
	fmt.Println()
	fmt.Println("  Now let's register who can talk to your bot.")
	fmt.Println("  Open this link in Telegram and send /start to your bot:")
	fmt.Println()
	fmt.Printf("    \033[4;36mhttps://t.me/%s\033[0m\n", botUsername)
	fmt.Println()

	var allowFrom []string

	for {
		allowFrom, err = detectTelegramUsers(botToken)
		if err != nil {
			return err
		}

		if len(allowFrom) > 0 {
			break
		}

		fmt.Println("  \033[31mAt least one allowed user is required.\033[0m")
		fmt.Println("  The bot has full tool access — it cannot be left open to everyone.")
		fmt.Println()

		var retry bool
		retryErr := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Try detecting users again?").
					Affirmative("Yes, try again").
					Negative("Cancel setup").
					Value(&retry),
			),
		).Run()
		if retryErr != nil || !retry {
			fmt.Println("  Setup cancelled.")
			return nil
		}
		fmt.Println()
		fmt.Println("  Send /start to your bot in Telegram, then press Ctrl+C when done:")
		fmt.Println()
	}

	// Step 4: Save configuration
	enabled := true
	cfg.Channels.Enabled = &enabled
	cfg.Channels.Telegram.Enabled = &enabled
	cfg.Channels.Telegram.AllowFrom = allowFrom

	// Save bot token to encrypted credential store (keep config.yaml clean)
	cfg.Channels.Telegram.BotToken = botToken // fallback: stays in config if store fails
	configDir, _ := config.GetConfigDir()
	if configDir != "" {
		if store, storeErr := credentials.Open(configDir); storeErr == nil {
			if setErr := store.SetSecret("channels.telegram.bot_token", botToken); setErr == nil {
				cfg.Channels.Telegram.BotToken = "" // scrub from config.yaml
			}
		}
	}

	if err := config.SaveAppConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Success message
	summary := fmt.Sprintf("Telegram bot @%s configured successfully!", botUsername)
	channelsPrintSuccess(summary)

	fmt.Println()
	fmt.Printf("  Allowed users: %s\n", strings.Join(allowFrom, ", "))
	fmt.Printf("  Deep link:     https://t.me/%s\n", botUsername)
	fmt.Println()

	// Auto-restart daemon if it's running so the new config takes effect immediately.
	svc, svcErr := daemon.NewService()
	if svcErr == nil {
		if running, _ := svc.IsRunning(); running {
			fmt.Println("  Restarting daemon...")
			if restartErr := svc.Restart(); restartErr != nil {
				fmt.Printf("  \033[33mWarning: Could not restart daemon: %v\033[0m\n", restartErr)
				fmt.Println("  Restart it manually: astonish daemon restart")
			} else {
				fmt.Println("  Daemon restarted — your bot is live!")
			}
		} else {
			fmt.Println("  Start the daemon to activate:")
			fmt.Println("    astonish daemon start")
		}
	}
	fmt.Println()

	return nil
}

// detectTelegramUsers temporarily polls the bot for /start messages
// and auto-detects user IDs. Returns the list of allowed user ID strings.
func detectTelegramUsers(botToken string) ([]string, error) {
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	// Drain all pending updates so we only see new messages.
	// Use direct API calls (not channel) to reliably find the latest offset.
	drainOffset := 0
	drainConfig := tgbotapi.NewUpdate(0)
	drainConfig.Timeout = 0
	for {
		drained, drainErr := bot.GetUpdates(drainConfig)
		if drainErr != nil || len(drained) == 0 {
			break
		}
		drainOffset = drained[len(drained)-1].UpdateID + 1
		drainConfig.Offset = drainOffset
	}

	// Start long polling from the clean offset — only new messages arrive.
	updateConfig := tgbotapi.NewUpdate(drainOffset)
	updateConfig.Timeout = 30
	updates := bot.GetUpdatesChan(updateConfig)

	type detectedUser struct {
		ID   string
		Name string
	}

	var allowFrom []string
	seen := map[string]bool{}

	fmt.Println("  Waiting for messages... (press Ctrl+C when done)")
	fmt.Println()

	// Set up interrupt handler
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	// Poll with a timeout — wait up to 5 minutes total
	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-sigCh:
			fmt.Println()
			bot.StopReceivingUpdates()
			return allowFrom, nil

		case <-timeout:
			fmt.Println("\n  Timed out waiting for messages.")
			bot.StopReceivingUpdates()
			return allowFrom, nil

		case update, ok := <-updates:
			if !ok {
				return allowFrom, nil
			}
			if update.Message == nil {
				continue
			}

			msg := update.Message
			senderID := strconv.FormatInt(msg.From.ID, 10)

			if seen[senderID] {
				continue
			}

			// Build display name
			name := msg.From.FirstName
			if msg.From.LastName != "" {
				name += " " + msg.From.LastName
			}
			if name == "" {
				name = msg.From.UserName
			}

			user := detectedUser{ID: senderID, Name: name}

			fmt.Printf("  \033[32mDetected: %s (ID: %s)\033[0m\n", user.Name, user.ID)

			var addUser bool
			addErr := huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title(fmt.Sprintf("Add %s to allowed users?", user.Name)).
						Affirmative("Yes").
						Negative("No").
						Value(&addUser),
				),
			).Run()
			if addErr != nil {
				bot.StopReceivingUpdates()
				return allowFrom, nil
			}

			if addUser {
				allowFrom = append(allowFrom, senderID)
				seen[senderID] = true

				// Send confirmation to the user in Telegram
				reply := tgbotapi.NewMessage(msg.Chat.ID,
					fmt.Sprintf("You've been added to Astonish! (ID: %s)", senderID))
				bot.Send(reply)

				fmt.Printf("  Added %s (%s)\n\n", user.Name, senderID)

				// One instance = one owner. Done.
				bot.StopReceivingUpdates()
				return allowFrom, nil
			} else {
				seen[senderID] = true
				fmt.Println()
			}
		}
	}
}

// handleChannelDisable disables a channel in the config.
func handleChannelDisable(channel string) error {
	cfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	switch channel {
	case "telegram":
		if !cfg.Channels.Telegram.IsTelegramEnabled() {
			fmt.Println("Telegram is not currently enabled.")
			return nil
		}
		disabled := false
		cfg.Channels.Telegram.Enabled = &disabled
		if err := config.SaveAppConfig(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		fmt.Println("Telegram channel disabled.")
		fmt.Println("Restart the daemon for changes to take effect: astonish daemon restart")
	default:
		return fmt.Errorf("unknown channel: %s (available: telegram)", channel)
	}

	return nil
}

func handleChannelsStatus() error {
	// Load config to get daemon port
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	port := appCfg.Daemon.GetPort()
	url := fmt.Sprintf("http://localhost:%d/api/channels/status", port)

	resp, err := http.Get(url)
	if err != nil {
		// Daemon is not running — show config-based status instead
		return handleChannelsStatusFromConfig(appCfg)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		Channels map[string]struct {
			Connected    bool   `json:"connected"`
			AccountID    string `json:"account_id"`
			ConnectedAt  string `json:"connected_at"`
			Error        string `json:"error"`
			MessageCount int64  `json:"message_count"`
		} `json:"channels"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Channels) == 0 {
		return handleChannelsStatusFromConfig(appCfg)
	}

	fmt.Println()
	fmt.Println("  Channel Status:")
	fmt.Println()

	for name, ch := range result.Channels {
		status := "\033[31mdisconnected\033[0m"
		if ch.Connected {
			status = "\033[32mconnected\033[0m"
		}

		fmt.Printf("    %-12s %s", name, status)
		if ch.AccountID != "" {
			fmt.Printf("  @%s", ch.AccountID)
		}
		if ch.Error != "" {
			fmt.Printf("  \033[31m(%s)\033[0m", ch.Error)
		}
		fmt.Printf("  — %d messages", ch.MessageCount)
		fmt.Println()
	}
	fmt.Println()

	return nil
}

// handleChannelsStatusFromConfig shows channel configuration when daemon is not running.
func handleChannelsStatusFromConfig(cfg *config.AppConfig) error {
	fmt.Println()

	if !cfg.Channels.IsChannelsEnabled() {
		fmt.Println("  No channels configured.")
		fmt.Println()
		fmt.Println("  Set up a channel:")
		fmt.Println("    astonish channels setup telegram")
		fmt.Println()
		return nil
	}

	fmt.Println("  Channel Configuration:")
	fmt.Println()

	if cfg.Channels.Telegram.IsTelegramEnabled() {
		fmt.Printf("    telegram     \033[33mconfigured\033[0m (daemon not running)")
		fmt.Println()
	} else if cfg.Channels.Telegram.BotToken != "" {
		fmt.Printf("    telegram     \033[90mdisabled\033[0m")
		fmt.Println()
	}

	fmt.Println()
	fmt.Println("  Start the daemon to activate channels:")
	fmt.Println("    astonish daemon start")
	fmt.Println()

	return nil
}

func printChannelsUsage() {
	fmt.Println("usage: astonish channels {status,setup,disable}")
	fmt.Println("")
	fmt.Println("Manage communication channels (Telegram, Slack, etc.)")
	fmt.Println("")
	fmt.Println("subcommands:")
	fmt.Println("  status              Show status of all channels")
	fmt.Println("  setup <channel>     Interactive channel setup")
	fmt.Println("  disable <channel>   Disable a channel")
	fmt.Println("")
	fmt.Println("available channels:")
	fmt.Println("  telegram            Telegram bot integration")
}

// maskToken returns the last 8 chars of a token, with the rest replaced by dots.
func maskToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return "..." + token[len(token)-8:]
}

// channelsPrintHeader prints a styled header.
func channelsPrintHeader(title string) {
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("63")).
		Padding(0, 2)
	fmt.Println(style.Render(title))
}

// channelsRunSpinner prints a spinner-style status message.
func channelsRunSpinner(msg string) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("63"))
	fmt.Printf("  %s %s\n", style.Render("•"), msg)
}

// channelsPrintSuccess prints a styled success message in a box.
func channelsPrintSuccess(msg string) {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42"))
	fmt.Println(style.Render("✓ " + msg))
}
