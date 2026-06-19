package imagebuilder

import (
	"fmt"
	"slices"
	"strings"
)

// GenerateDockerfile produces a Dockerfile that installs the given apt packages
// on top of the base image. Packages are sorted alphabetically for deterministic
// content hashing. The build runs as root for apt access, then switches back to
// the "sandbox" user to preserve the OpenShell security model.
func GenerateDockerfile(baseImage string, packages []string) string {
	sorted := slices.Clone(packages)
	slices.Sort(sorted)

	var b strings.Builder

	b.WriteString(fmt.Sprintf("FROM %s\n\n", baseImage))
	b.WriteString("# Install packages as root\n")
	b.WriteString("USER root\n\n")
	b.WriteString("RUN apt-get update && \\\n")
	b.WriteString("    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \\\n")
	for i, pkg := range sorted {
		if i < len(sorted)-1 {
			b.WriteString(fmt.Sprintf("        %s \\\n", pkg))
		} else {
			b.WriteString(fmt.Sprintf("        %s \\\n", pkg))
		}
	}
	b.WriteString("    && rm -rf /var/lib/apt/lists/*\n\n")
	b.WriteString("# Restore non-root user for runtime\n")
	b.WriteString("USER sandbox\n")

	return b.String()
}
