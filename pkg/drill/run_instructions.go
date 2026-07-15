package drill

import (
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
)

// GenerateRunInstructions returns Studio chat prep text for a suite.
// If suite_config.run_instructions is set, it is returned as-is (plus a final
// run_drill reminder if missing). Otherwise instructions are derived from
// template, workspace/branch, configure, setup/services, and ready_check.
//
// Fleet sessions skip this — they call run_drill on an already-prepared stack.
func GenerateRunInstructions(suiteName string, sc *config.DrillSuiteConfig) string {
	suiteName = strings.TrimSpace(suiteName)
	if suiteName == "" {
		suiteName = "<suite>"
	}

	if sc != nil && strings.TrimSpace(sc.RunInstructions) != "" {
		msg := strings.TrimSpace(sc.RunInstructions)
		if !strings.Contains(msg, "run_drill") {
			msg += fmt.Sprintf("\n\nThen call run_drill(suite_name: %q). Do not write credential files manually — run_drill injects them.", suiteName)
		}
		return msg
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Prepare then run the drill suite %q.\n\n", suiteName))
	b.WriteString("Do these prep steps with tools before calling run_drill. ")
	b.WriteString("run_drill only injects credentials and executes tests — it does not switch templates, git-pull, or start services.\n\n")

	step := 1
	template := ""
	if sc != nil {
		template = strings.TrimSpace(sc.Template)
	}
	if template != "" {
		display := strings.TrimPrefix(template, "@")
		fmt.Fprintf(&b, "%d. If the current sandbox is not already on template %q, call use_sandbox_template(template_name: %q).\n", step, display, display)
		step++
	}

	workspace := ""
	branch := "main"
	if sc != nil {
		workspace = strings.TrimSpace(sc.Workspace)
		if strings.TrimSpace(sc.Branch) != "" {
			branch = strings.TrimSpace(sc.Branch)
		}
	}
	if workspace != "" {
		fmt.Fprintf(&b, "%d. Sync the workspace to the latest %s (do not force-reset dirty trees):\n", step, branch)
		fmt.Fprintf(&b, "   shell_command: `cd %s && git fetch && git checkout %s && git pull --ff-only`\n", workspace, branch)
		fmt.Fprintf(&b, "   If the pull fails (dirty or diverged), stop and report — do not force-reset.\n")
		step++
	}

	if SuiteDeclaresCredentials(sc) {
		fmt.Fprintf(&b, "%d. Call inject_drill_credentials(suite_name: %q) BEFORE start-services so apps that read secrets at boot see them. Do not write secret files with write_file.\n", step, suiteName)
		step++
	}

	if sc != nil {
		for _, cmd := range sc.Configure {
			cmd = strings.TrimSpace(cmd)
			if cmd == "" {
				continue
			}
			fmt.Fprintf(&b, "%d. Configure: shell_command `%s`\n", step, cmd)
			step++
		}

		for _, cmd := range sc.Setup {
			cmd = strings.TrimSpace(cmd)
			if cmd == "" {
				continue
			}
			fmt.Fprintf(&b, "%d. Start services: shell_command `%s`\n", step, cmd)
			step++
		}

		for _, svc := range sc.Services {
			setup := strings.TrimSpace(svc.Setup)
			if setup == "" {
				continue
			}
			name := svc.Name
			if name == "" {
				name = "service"
			}
			fmt.Fprintf(&b, "%d. Start %s: shell_command `%s`\n", step, name, setup)
			step++
		}

		if sc.ReadyCheck != nil && isReadyCheckConfigured(sc.ReadyCheck) {
			if hint := readyCheckInstruction(sc.ReadyCheck); hint != "" {
				fmt.Fprintf(&b, "%d. Wait until ready: %s\n", step, hint)
				step++
			}
		}
		for _, svc := range sc.Services {
			if svc.ReadyCheck == nil || !isReadyCheckConfigured(svc.ReadyCheck) {
				continue
			}
			if hint := readyCheckInstruction(svc.ReadyCheck); hint != "" {
				name := svc.Name
				if name == "" {
					name = "service"
				}
				fmt.Fprintf(&b, "%d. Wait until %s is ready: %s\n", step, name, hint)
				step++
			}
		}
	}

	fmt.Fprintf(&b, "%d. Call run_drill(suite_name: %q). Do not write credential files manually — run_drill also injects credentials before tests (idempotent).\n", step, suiteName)
	return b.String()
}

// SuiteDeclaresCredentials reports whether suite_config lists credentials or
// credential_injection that Studio prep should materialize before start-services.
func SuiteDeclaresCredentials(sc *config.DrillSuiteConfig) bool {
	if sc == nil {
		return false
	}
	if len(sc.Credentials) > 0 {
		return true
	}
	if sc.CredentialInjection == nil {
		return false
	}
	return len(sc.CredentialInjection.Env) > 0 || len(sc.CredentialInjection.Files) > 0
}

func readyCheckInstruction(rc *config.ReadyCheck) string {
	if rc == nil {
		return ""
	}
	timeout := rc.Timeout
	if timeout <= 0 {
		timeout = 60
	}
	interval := rc.Interval
	if interval <= 0 {
		interval = 2
	}
	switch strings.ToLower(strings.TrimSpace(rc.Type)) {
	case "http":
		url := strings.TrimSpace(rc.URL)
		if url == "" {
			return ""
		}
		return fmt.Sprintf("poll with curl until HTTP success (timeout ~%ds, every %ds): `for i in $(seq 1 %d); do curl -sf %q >/dev/null && exit 0; sleep %d; done; exit 1`",
			timeout, interval, max(1, timeout/max(1, interval)), url, interval)
	case "port":
		host := strings.TrimSpace(rc.Host)
		if host == "" {
			host = "127.0.0.1"
		}
		if rc.Port <= 0 {
			return ""
		}
		return fmt.Sprintf("wait until TCP %s:%d accepts connections (timeout ~%ds)", host, rc.Port, timeout)
	case "output_contains":
		pat := strings.TrimSpace(rc.Pattern)
		if pat == "" {
			return ""
		}
		return fmt.Sprintf("confirm setup output contains %q", pat)
	default:
		return ""
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
