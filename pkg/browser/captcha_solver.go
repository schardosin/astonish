package browser

import (
	"context"
	"fmt"
)

// CAPTCHASolver defines the interface for automated CAPTCHA solving services.
// Implementations wrap third-party APIs (2captcha, anti-captcha, etc.) to
// solve CAPTCHAs programmatically. The browser handoff mechanism is the
// primary CAPTCHA solution; solvers are an optional automated fallback.
type CAPTCHASolver interface {
	// Name returns a human-readable name for the solver (e.g. "2captcha").
	Name() string

	// Solve submits a CAPTCHA challenge and returns the solution token.
	// The token format depends on the CAPTCHA type:
	//   - reCAPTCHA v2/v3: g-recaptcha-response token
	//   - hCaptcha: h-captcha-response token
	//   - Turnstile: cf-turnstile-response token
	Solve(ctx context.Context, req CAPTCHASolveRequest) (*CAPTCHASolveResult, error)

	// Balance returns the current account balance with the solver service.
	// Returns -1 if balance checking is not supported.
	Balance(ctx context.Context) (float64, error)
}

// CAPTCHASolveRequest contains the parameters needed to submit a CAPTCHA
// to a solver service.
type CAPTCHASolveRequest struct {
	Type    CAPTCHAType `json:"type"`             // CAPTCHA type
	SiteKey string      `json:"site_key"`         // The data-sitekey from the page
	PageURL string      `json:"page_url"`         // URL of the page containing the CAPTCHA
	Action  string      `json:"action,omitempty"` // reCAPTCHA v3 action parameter
	Data    string      `json:"data,omitempty"`   // Extra data for specific CAPTCHA types
}

// CAPTCHASolveResult holds the response from a CAPTCHA solver.
type CAPTCHASolveResult struct {
	Token   string  `json:"token"`              // Solution token to inject into the page
	TaskID  string  `json:"task_id,omitempty"`  // Solver-assigned task ID (for debugging)
	CostUSD float64 `json:"cost_usd,omitempty"` // Cost of this solve, if reported
}

// CAPTCHASolverConfig holds configuration for CAPTCHA solver services.
// This is stored in the app config under browser.captcha_solver.
type CAPTCHASolverConfig struct {
	// Provider selects the solver backend. Currently stub-only.
	// Future values: "2captcha", "anti-captcha", "capsolver"
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`

	// APIKey is the authentication key for the solver service.
	// Should be stored in the credential store, not in config.yaml.
	// This field is only used as a fallback.
	APIKey string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
}

// NewCAPTCHASolver creates a solver instance from configuration.
// Currently returns an error for all providers since no solvers are implemented.
// This is a stub that will be extended when solver backends are added.
func NewCAPTCHASolver(cfg CAPTCHASolverConfig) (CAPTCHASolver, error) {
	switch cfg.Provider {
	case "":
		return nil, fmt.Errorf("no CAPTCHA solver configured (set browser.captcha_solver.provider in config.yaml)")
	case "2captcha":
		return nil, fmt.Errorf("2captcha solver not yet implemented (planned for future release)")
	case "anti-captcha":
		return nil, fmt.Errorf("anti-captcha solver not yet implemented (planned for future release)")
	case "capsolver":
		return nil, fmt.Errorf("capsolver not yet implemented (planned for future release)")
	default:
		return nil, fmt.Errorf("unknown CAPTCHA solver provider: %q", cfg.Provider)
	}
}
