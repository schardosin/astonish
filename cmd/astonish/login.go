package astonish

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/charmbracelet/huh"
	"github.com/schardosin/astonish/pkg/client"
	"golang.org/x/term"
)

func handleLoginCommand(args []string) error {
	if len(args) == 0 {
		fmt.Println("Usage: astonish login <server-url> [flags]")
		fmt.Println("")
		fmt.Println("Flags:")
		fmt.Println("  --sso           Use SSO/OIDC login (opens browser)")
		fmt.Println("  --org <slug>    Select organization (skip prompt)")
		fmt.Println("  --team <slug>   Select team (skip prompt)")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  astonish login https://astonish.mycompany.com")
		fmt.Println("  astonish login https://astonish.mycompany.com --sso")
		fmt.Println("  astonish login https://astonish.mycompany.com --org my-org --team backend")
		return fmt.Errorf("server URL required")
	}

	serverURL := strings.TrimRight(args[0], "/")
	useSSO := false
	flagOrg := ""
	flagTeam := ""

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--sso":
			useSSO = true
		case "--org":
			if i+1 < len(args) {
				flagOrg = args[i+1]
				i++
			}
		case "--team":
			if i+1 < len(args) {
				flagTeam = args[i+1]
				i++
			}
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

	if useSSO {
		return handleSSOLogin(serverURL, flagOrg, flagTeam)
	}

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

	// Phase 1: Initial login to authenticate and get available orgs/teams
	result, err := client.LoginWithPassword(serverURL, email, password, flagOrg, flagTeam)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// Phase 2: Interactive org selection (if multiple orgs and no flag provided)
	if flagOrg == "" && len(result.AvailableOrgs) > 1 {
		selectedOrg, err := promptOrgSelection(result.AvailableOrgs, result.OrgSlug)
		if err != nil {
			return fmt.Errorf("org selection: %w", err)
		}

		if selectedOrg != result.OrgSlug {
			// Re-login with the selected org to get a properly scoped token
			result, err = client.LoginWithPassword(serverURL, email, password, selectedOrg, "")
			if err != nil {
				return fmt.Errorf("login failed (org switch): %w", err)
			}
		}
	}

	// Phase 3: Interactive team selection (if multiple teams and no flag provided)
	if flagTeam == "" && len(result.AvailableTeams) > 1 {
		selectedTeam, err := promptTeamSelection(result.AvailableTeams, result.TeamSlug)
		if err != nil {
			return fmt.Errorf("team selection: %w", err)
		}

		if selectedTeam != result.TeamSlug {
			// Re-login with the selected org+team to get a properly scoped token
			result, err = client.LoginWithPassword(serverURL, email, password, result.OrgSlug, selectedTeam)
			if err != nil {
				return fmt.Errorf("login failed (team switch): %w", err)
			}
		}
	}

	printLoginSuccess(result)
	return nil
}

// promptOrgSelection shows an interactive selector for organizations.
func promptOrgSelection(orgs []client.LoginOrgOption, currentSlug string) (string, error) {
	options := make([]huh.Option[string], 0, len(orgs))
	for _, org := range orgs {
		label := fmt.Sprintf("%s (%s)", org.Name, org.Slug)
		if org.Role != "" && org.Role != "member" {
			label += fmt.Sprintf(" [%s]", org.Role)
		}
		options = append(options, huh.NewOption(label, org.Slug))
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select organization").
				Options(options...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		return "", err
	}
	return selected, nil
}

// promptTeamSelection shows an interactive selector for teams.
func promptTeamSelection(teams []client.LoginTeamOption, currentSlug string) (string, error) {
	options := make([]huh.Option[string], 0, len(teams))
	for _, team := range teams {
		label := fmt.Sprintf("%s (%s)", team.Name, team.Slug)
		options = append(options, huh.NewOption(label, team.Slug))
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select team").
				Options(options...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		return "", err
	}
	return selected, nil
}

// printLoginSuccess displays the login success message.
func printLoginSuccess(result *client.LoginResult) {
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
}

// handleSSOLogin performs the SSO login flow with device-code polling.
func handleSSOLogin(serverURL, flagOrg, flagTeam string) error {
	// Check for available SSO providers
	providers, err := client.ListSSOProviders(serverURL)
	if err != nil || len(providers) == 0 {
		return fmt.Errorf("no SSO providers configured on this server")
	}

	// If multiple providers, let the user choose
	providerID := ""
	if len(providers) == 1 {
		providerID = providers[0].ID
		fmt.Printf("Using SSO provider: %s\n", providers[0].Name)
	} else {
		options := make([]huh.Option[string], 0, len(providers))
		for _, p := range providers {
			options = append(options, huh.NewOption(p.Name, p.ID))
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select SSO provider").
					Options(options...).
					Value(&providerID),
			),
		)
		if err := form.Run(); err != nil {
			return fmt.Errorf("provider selection: %w", err)
		}
	}

	// Perform the SSO login with status feedback
	fmt.Println("Initiating SSO login...")
	result, err := client.LoginWithSSO(serverURL, providerID, func(status string) {
		switch status {
		case "opening_browser":
			fmt.Println("Opening browser for authentication...")
			fmt.Println("If the browser doesn't open, check the URL above.")
		case "browser_failed":
			fmt.Println("Could not open browser automatically.")
			fmt.Println("Please open the URL printed above in your browser.")
		case "polling":
			fmt.Println("Waiting for authentication to complete...")
			fmt.Println("(Complete the login in your browser)")
		}
	})
	if err != nil {
		return fmt.Errorf("SSO login failed: %w", err)
	}

	// Phase 2: Interactive org selection (if multiple orgs and no flag provided)
	if flagOrg == "" && len(result.AvailableOrgs) > 1 {
		selectedOrg, err := promptOrgSelection(result.AvailableOrgs, result.OrgSlug)
		if err != nil {
			return fmt.Errorf("org selection: %w", err)
		}

		if selectedOrg != result.OrgSlug {
			// For SSO, we can't re-login with the IdP again easily.
			// Instead, update the remote config to the selected org.
			// The next refresh will pick up the correct org.
			result.OrgSlug = selectedOrg
			for _, o := range result.AvailableOrgs {
				if o.Slug == selectedOrg {
					result.OrgName = o.Name
					break
				}
			}
			cfg := &client.RemoteConfig{
				URL:       serverURL,
				Org:       selectedOrg,
				Team:      result.TeamSlug,
				UserEmail: result.UserEmail,
			}
			_ = client.SaveRemoteConfig(cfg)
		}
	}

	// Phase 3: Interactive team selection (if multiple teams and no flag provided)
	if flagTeam == "" && len(result.AvailableTeams) > 1 {
		selectedTeam, err := promptTeamSelection(result.AvailableTeams, result.TeamSlug)
		if err != nil {
			return fmt.Errorf("team selection: %w", err)
		}

		if selectedTeam != result.TeamSlug {
			result.TeamSlug = selectedTeam
			cfg := &client.RemoteConfig{
				URL:       serverURL,
				Org:       result.OrgSlug,
				Team:      selectedTeam,
				UserEmail: result.UserEmail,
			}
			_ = client.SaveRemoteConfig(cfg)
		}
	}

	printLoginSuccess(result)
	return nil
}
