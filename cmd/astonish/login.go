package astonish

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/schardosin/astonish/pkg/client"
	"golang.org/x/term"
)

func handleLoginCommand(args []string) error {
	if len(args) == 0 {
		fmt.Println("Usage: astonish login <server-url> [--sso]")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  astonish login https://astonish.mycompany.com")
		fmt.Println("  astonish login https://astonish.mycompany.com --sso")
		return fmt.Errorf("server URL required")
	}

	serverURL := strings.TrimRight(args[0], "/")
	useSSO := false

	for _, arg := range args[1:] {
		if arg == "--sso" {
			useSSO = true
		}
	}

	// Validate URL
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		serverURL = "https://" + serverURL
	}

	// Check if already logged in
	if client.IsRemoteMode() {
		cfg, _ := client.LoadRemoteConfig()
		if cfg != nil {
			fmt.Printf("Already connected to %s as %s\n", cfg.URL, cfg.UserEmail)
			fmt.Printf("Run 'astonish logout' first to disconnect.\n")
			return fmt.Errorf("already connected")
		}
	}

	var result *client.LoginResult
	var err error

	if useSSO {
		fmt.Println("Opening browser for SSO login...")
		result, err = client.LoginWithSSO(serverURL)
	} else {
		// Interactive email/password prompt
		reader := bufio.NewReader(os.Stdin)

		fmt.Print("Email: ")
		email, _ := reader.ReadString('\n')
		email = strings.TrimSpace(email)

		fmt.Print("Password: ")
		passwordBytes, passErr := term.ReadPassword(int(syscall.Stdin))
		fmt.Println() // newline after hidden input
		if passErr != nil {
			return fmt.Errorf("failed to read password: %w", passErr)
		}
		password := string(passwordBytes)

		if email == "" || password == "" {
			return fmt.Errorf("email and password are required")
		}

		result, err = client.LoginWithPassword(serverURL, email, password)
	}

	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	fmt.Printf("\nLogged in as %s", result.UserEmail)
	if result.DisplayName != "" {
		fmt.Printf(" (%s)", result.DisplayName)
	}
	fmt.Println()

	orgDisplay := result.OrgSlug
	if result.OrgName != "" {
		orgDisplay = result.OrgName
	}
	fmt.Printf("Organization: %s\n", orgDisplay)
	if result.TeamSlug != "" {
		fmt.Printf("Team: %s\n", result.TeamSlug)
	}

	return nil
}
