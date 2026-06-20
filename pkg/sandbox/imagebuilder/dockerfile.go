package imagebuilder

import (
	"fmt"
	"strings"
)

// GenerateDockerfile produces a complete Dockerfile by prepending the FROM
// instruction and concatenating the platform body (Layer 2) and team body
// (Layer 3). Either body may be empty. The base image is always the original
// application-delivered image (Layer 1).
//
// For platform builds: platformBody is the admin's recipe, teamBody is "".
// For team builds: both are included, producing a single self-contained image.
func GenerateDockerfile(baseImage, platformBody, teamBody string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("FROM %s\n", baseImage))

	platform := strings.TrimSpace(platformBody)
	team := strings.TrimSpace(teamBody)

	if platform != "" {
		b.WriteString("\n# --- Platform recipe ---\n")
		b.WriteString(platform)
		b.WriteByte('\n')
	}

	if team != "" {
		b.WriteString("\n# --- Team recipe ---\n")
		b.WriteString(team)
		b.WriteByte('\n')
	}

	return b.String()
}

// MigratePackagesToDockerfile converts a legacy packages list into an
// equivalent Dockerfile body. Used for backward-compatible migration of
// templates that were created with the old packages-only API.
func MigratePackagesToDockerfile(packages []string) string {
	if len(packages) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("USER root\n\n")
	b.WriteString("RUN apt-get update && \\\n")
	b.WriteString("    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \\\n")
	for _, pkg := range packages {
		b.WriteString(fmt.Sprintf("        %s \\\n", pkg))
	}
	b.WriteString("    && rm -rf /var/lib/apt/lists/*\n\n")
	b.WriteString("USER sandbox\n")

	return b.String()
}
