package baseconfig

import (
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/sandbox/k8s"
)

// Render produces the ordered list of shell command strings suitable for
// K8sBackend.BuildTemplate(TemplateBuildSpec.Steps). Each entry is passed to
// `/bin/sh -c <step>` inside the builder pod.
//
// The order mirrors the personal-mode wizard:
//   1. Core tools (apt packages, node, python, uv, docker)
//   2. Optional tools (OpenCode, etc.)
//   3. Browser install (Chromium/CloakBrowser + KasmVNC + X11 deps)
//   4. Extra steps (raw shell, operator escape hatch)
func (c *BaseConfig) Render() ([]string, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	var steps []string

	// 1. Core tools
	if c.Core {
		for _, argv := range sandbox.CoreToolInstallCommands() {
			steps = append(steps, shellJoin(argv))
		}
	}

	// 2. Optional tools
	catalog := sandbox.OptionalTools()
	catalogMap := make(map[string]sandbox.OptionalTool, len(catalog))
	for _, t := range catalog {
		catalogMap[t.ID] = t
	}
	for _, id := range c.OptionalTools {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		tool, ok := catalogMap[id]
		if !ok {
			// Validate() should have caught this, but be safe.
			return nil, fmt.Errorf("baseconfig: unknown optional tool %q", id)
		}
		for _, argv := range tool.InstallCommands() {
			steps = append(steps, shellJoin(argv))
		}
	}

	// 3. Browser install
	// K8s sandbox-base is debian:bookworm-slim (see docker/sandbox-base/Dockerfile);
	// package names differ from the Ubuntu noble Incus default.
	engine := c.Browser.Engine
	if engine == "" {
		engine = "none"
	}
	if sandbox.IsContainerCompatibleEngine(engine) {
		arch := c.archForBrowser()
		for _, argv := range sandbox.BrowserContainerInstallCommands(engine, arch, k8s.SandboxBaseDistro) {
			steps = append(steps, shellJoin(argv))
		}
	}

	// 4. Extra steps
	for _, step := range c.ExtraSteps {
		step = strings.TrimSpace(step)
		if step != "" {
			steps = append(steps, step)
		}
	}

	if len(steps) == 0 {
		return nil, fmt.Errorf("baseconfig: rendered config produces zero steps (at least core=true or a tool must be selected)")
	}

	return steps, nil
}

// archForBrowser maps the BaseConfig.Architecture ("amd64", "arm64") to
// the format BrowserContainerInstallCommands expects ("x86_64", "aarch64").
func (c *BaseConfig) archForBrowser() string {
	switch c.Architecture {
	case "arm64":
		return "aarch64"
	default:
		return "x86_64"
	}
}

// shellJoin converts an argv slice into a single shell string.
// For simple commands (no special chars) it joins with spaces.
// For commands containing shell metacharacters it quotes arguments.
func shellJoin(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	// If the first element is "sh" with "-c", the third element is already
	// a shell string — pass it through as-is.
	if len(argv) >= 3 && argv[0] == "sh" && argv[1] == "-c" {
		return argv[2]
	}

	var b strings.Builder
	for i, arg := range argv {
		if i > 0 {
			b.WriteByte(' ')
		}
		if needsQuoting(arg) {
			b.WriteString(shellQuote(arg))
		} else {
			b.WriteString(arg)
		}
	}
	return b.String()
}

// needsQuoting returns true if a shell argument contains characters that
// require quoting.
func needsQuoting(s string) bool {
	for _, c := range s {
		switch c {
		case ' ', '\t', '\n', '"', '\'', '\\', '$', '`', '!', '#', '&', '|',
			';', '(', ')', '{', '}', '[', ']', '<', '>', '~', '*', '?':
			return true
		}
	}
	return false
}

// shellQuote wraps a string in single quotes, escaping embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
