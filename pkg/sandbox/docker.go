package sandbox

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/version"
)

// Docker container and image constants for the Incus runtime on macOS/Windows.
const (
	// DockerContainerName is the name of the Docker container running Incus.
	DockerContainerName = "astonish-incus"

	// DockerVolumeName is the named Docker volume for persistent Incus data.
	// This volume survives container recreation (upgrades).
	DockerVolumeName = "astonish-incus-data"

	// DockerImageRepo is the Docker Hub repository for the Incus runtime image.
	DockerImageRepo = "schardosin/astonish-incus"

	// DockerVersionLabel is the container label storing the astonish version
	// that created/upgraded the container. Used for auto-upgrade detection.
	DockerVersionLabel = "com.astonish.version"

	// DockerIncusPort is the port Incus listens on inside the Docker container.
	// Mapped to the same port on the host for TCP API access.
	DockerIncusPort = "8443"

	// DockerClientCertPath is the path inside the Docker container where
	// the client certificate for host→Incus API authentication is stored.
	DockerClientCertPath = "/var/lib/incus/astonish-client/client.crt"

	// DockerClientKeyPath is the path inside the Docker container where
	// the client private key for host→Incus API authentication is stored.
	DockerClientKeyPath = "/var/lib/incus/astonish-client/client.key"
)

// DockerImageTag returns the full image reference for the current astonish version.
// For release builds (e.g., "v1.2.3"), returns "astonish/incus:v1.2.3".
// For dev builds, returns "astonish/incus:latest".
func DockerImageTag() string {
	v := version.GetVersion()
	if v == "" || v == "dev" {
		return DockerImageRepo + ":latest"
	}
	return DockerImageRepo + ":" + v
}

// isDevBuild returns true if the current binary is a development build
// (not a tagged release). Dev builds skip auto-pull and auto-upgrade.
func isDevBuild() bool {
	v := version.GetVersion()
	return v == "" || v == "dev"
}

// dockerImageExistsLocally checks if a Docker image exists in the local cache.
func dockerImageExistsLocally(imageTag string) bool {
	cmd := exec.Command("docker", "image", "inspect", imageTag)
	return cmd.Run() == nil
}

// pullOrFallback attempts to pull the Docker image from the registry.
// If the pull fails (e.g., dev build, no internet, image not published),
// it falls back to checking if the image exists locally. Returns an error
// only if the image is not available anywhere.
func pullOrFallback(imageTag string) error {
	log.Printf("[sandbox] Pulling Docker image %s...", imageTag)
	pullCmd := exec.Command("docker", "pull", imageTag)
	if output, err := pullCmd.CombinedOutput(); err != nil {
		// Pull failed — check if image exists locally
		if dockerImageExistsLocally(imageTag) {
			log.Printf("[sandbox] Pull failed but image exists locally, using cached image")
			return nil
		}
		return fmt.Errorf("Docker image %s not found (pull failed: %s).\n"+
			"For dev testing, build it locally:\n"+
			"  make docker-incus\n"+
			"  docker tag schardosin/astonish-incus:dev schardosin/astonish-incus:latest",
			imageTag, firstLine(output))
	}
	return nil
}

// IsIncusDockerContainerRunning checks if the astonish-incus Docker container
// exists and is currently running.
func IsIncusDockerContainerRunning() bool {
	cmd := exec.Command("docker", "inspect",
		"--format", "{{.State.Running}}",
		DockerContainerName,
	)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// IsIncusDockerContainerExists checks if the astonish-incus Docker container
// exists (running or stopped).
func IsIncusDockerContainerExists() bool {
	cmd := exec.Command("docker", "inspect", DockerContainerName)
	return cmd.Run() == nil
}

// GetDockerContainerVersion returns the astonish version label from the
// running Docker container. Returns empty string if the container doesn't
// exist or has no version label.
func GetDockerContainerVersion() string {
	cmd := exec.Command("docker", "inspect",
		"--format", fmt.Sprintf("{{index .Config.Labels %q}}", DockerVersionLabel),
		DockerContainerName,
	)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(output))
	// docker inspect returns "<no value>" for missing labels
	if v == "<no value>" || v == "" {
		return ""
	}
	return v
}

// NeedsUpgrade returns true if the Docker container's version label doesn't
// match the current astonish binary version. Dev builds never trigger
// auto-upgrade (they use manual sandbox refresh instead).
func NeedsUpgrade() bool {
	currentVersion := version.GetVersion()
	if currentVersion == "" || currentVersion == "dev" {
		return false // dev builds don't auto-upgrade
	}

	containerVersion := GetDockerContainerVersion()
	if containerVersion == "" {
		return true // no version label → needs upgrade
	}

	return containerVersion != currentVersion
}

// EnsureIncusDockerContainer ensures the astonish-incus Docker container is
// running with the correct image. If the container doesn't exist, it pulls
// the image (or uses a local build) and creates it. If it exists but is
// stopped, it starts it. After the container is running, waits for the
// Incus API to be reachable.
//
// For dev builds, auto-upgrade is skipped — use `astonish sandbox refresh`
// after rebuilding the image locally.
func EnsureIncusDockerContainer() error {
	imageTag := DockerImageTag()
	currentVersion := version.GetVersion()

	if IsIncusDockerContainerRunning() {
		// Container is running — check if upgrade needed (skip for dev builds)
		if !isDevBuild() && NeedsUpgrade() {
			log.Printf("[sandbox] Docker container version mismatch, upgrading...")
			if err := UpgradeIncusDockerContainer(); err != nil {
				return fmt.Errorf("failed to upgrade Docker container: %w", err)
			}
		}
		return WaitForIncusAPI(60 * time.Second)
	}

	if IsIncusDockerContainerExists() {
		// Container exists but stopped — check version before starting (skip for dev)
		if !isDevBuild() && NeedsUpgrade() {
			log.Printf("[sandbox] Docker container version mismatch, upgrading...")
			if err := UpgradeIncusDockerContainer(); err != nil {
				return fmt.Errorf("failed to upgrade Docker container: %w", err)
			}
			return WaitForIncusAPI(60 * time.Second)
		}

		// Start existing container
		log.Printf("[sandbox] Starting existing Docker container %s...", DockerContainerName)
		cmd := exec.Command("docker", "start", DockerContainerName)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to start Docker container: %w\n%s", err, firstLine(output))
		}
		return WaitForIncusAPI(60 * time.Second)
	}

	// Container doesn't exist — pull image (or use local) and create it
	if err := pullOrFallback(imageTag); err != nil {
		return err
	}

	// Ensure the named volume exists
	volCmd := exec.Command("docker", "volume", "create", DockerVolumeName)
	if output, err := volCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create Docker volume: %w\n%s", err, firstLine(output))
	}

	// Create and start the container
	log.Printf("[sandbox] Creating Docker container %s...", DockerContainerName)
	args := []string{
		"run", "-d",
		"--name", DockerContainerName,
		"--privileged",
		"--restart", "unless-stopped",
		// Persist all Incus data (containers, images, storage pools)
		"-v", DockerVolumeName + ":/var/lib/incus",
		// Expose Incus API for the host astonish to connect via TCP (localhost only)
		"-p", "127.0.0.1:" + DockerIncusPort + ":" + DockerIncusPort,
		// Label with astonish version for upgrade detection
		"--label", DockerVersionLabel + "=" + currentVersion,
		imageTag,
	}

	runCmd := exec.Command("docker", args...)
	if output, err := runCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create Docker container: %w\n%s", err, firstLine(output))
	}

	log.Printf("[sandbox] Docker container %s created, waiting for Incus API...", DockerContainerName)
	return WaitForIncusAPI(90 * time.Second)
}

// UpgradeIncusDockerContainer stops the current container, pulls the new
// image (or uses a local build), removes the old container, and creates a
// new one with the same persistent volume. Templates, sessions, and
// registries are preserved.
func UpgradeIncusDockerContainer() error {
	imageTag := DockerImageTag()
	currentVersion := version.GetVersion()

	// Pull the new image first (before stopping anything)
	if err := pullOrFallback(imageTag); err != nil {
		return err
	}

	// Stop the running container
	if IsIncusDockerContainerRunning() {
		log.Printf("[sandbox] Stopping Docker container %s...", DockerContainerName)
		stopCmd := exec.Command("docker", "stop", DockerContainerName)
		if output, err := stopCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to stop Docker container: %w\n%s", err, firstLine(output))
		}
	}

	// Remove the old container (volume is preserved)
	if IsIncusDockerContainerExists() {
		log.Printf("[sandbox] Removing old Docker container %s...", DockerContainerName)
		rmCmd := exec.Command("docker", "rm", DockerContainerName)
		if output, err := rmCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to remove Docker container: %w\n%s", err, firstLine(output))
		}
	}

	// Create new container with the same volume
	log.Printf("[sandbox] Creating upgraded Docker container %s...", DockerContainerName)
	args := []string{
		"run", "-d",
		"--name", DockerContainerName,
		"--privileged",
		"--restart", "unless-stopped",
		"-v", DockerVolumeName + ":/var/lib/incus",
		"-p", "127.0.0.1:" + DockerIncusPort + ":" + DockerIncusPort,
		"--label", DockerVersionLabel + "=" + currentVersion,
		imageTag,
	}

	runCmd := exec.Command("docker", args...)
	if output, err := runCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create upgraded Docker container: %w\n%s", err, firstLine(output))
	}

	log.Printf("[sandbox] Docker container upgraded, waiting for Incus API...")
	return WaitForIncusAPI(90 * time.Second)
}

// StopIncusDockerContainer stops the astonish-incus Docker container.
func StopIncusDockerContainer() error {
	if !IsIncusDockerContainerRunning() {
		return nil
	}

	cmd := exec.Command("docker", "stop", DockerContainerName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop Docker container: %w\n%s", err, firstLine(output))
	}
	return nil
}

// WaitForIncusAPI polls the Incus HTTPS API endpoint until it responds
// or the timeout is reached. This accounts for the Incus daemon startup
// time inside the Docker container. Also waits for the client certificate
// to be generated (needed for host→Incus TCP authentication).
func WaitForIncusAPI(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Phase 1: Wait for Incus daemon to be ready
	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "exec", DockerContainerName,
			"incus", "info",
		)
		if err := cmd.Run(); err == nil {
			break
		}
		time.Sleep(time.Second)
	}

	if time.Now().After(deadline) {
		return fmt.Errorf("timeout waiting for Incus daemon on %s (waited %s)", DockerContainerName, timeout)
	}

	// Phase 2: Wait for client certificate to be generated by entrypoint
	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "exec", DockerContainerName,
			"test", "-f", DockerClientCertPath,
		)
		if err := cmd.Run(); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for client certificate on %s (waited %s)", DockerContainerName, timeout)
}

// ExecInDockerHost runs a command inside the astonish-incus Docker container
// and returns its combined stdout/stderr output. This is the core primitive
// for remote filesystem operations (overlay mounts, directory creation, etc.)
// that need to execute where Incus lives (inside the Docker VM on macOS).
func ExecInDockerHost(args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("ExecInDockerHost: empty command")
	}

	dockerArgs := append([]string{"exec", DockerContainerName}, args...)
	cmd := exec.Command("docker", dockerArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Include stderr in the error for debugging
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = strings.TrimSpace(stdout.String())
		}
		return nil, fmt.Errorf("docker exec failed: %w: %s", err, firstLine([]byte(errMsg)))
	}

	return stdout.Bytes(), nil
}

// ExecInDockerHostInteractive runs an interactive command inside the Docker
// container with stdin/stdout/stderr attached to the current terminal.
// Used for ShellIntoTemplate on Docker+Incus platforms.
func ExecInDockerHostInteractive(args []string) *exec.Cmd {
	dockerArgs := append([]string{"exec", "-it", DockerContainerName}, args...)
	return exec.Command("docker", dockerArgs...)
}

// ReadDockerClientCert reads the client TLS certificate and key from the
// Docker container. These are generated by the entrypoint script and stored
// on the persistent volume. Returns the PEM-encoded cert and key as strings.
func ReadDockerClientCert() (certPEM string, keyPEM string, err error) {
	certBytes, err := ExecInDockerHost([]string{"cat", DockerClientCertPath})
	if err != nil {
		return "", "", fmt.Errorf("failed to read client cert from Docker container: %w", err)
	}

	keyBytes, err := ExecInDockerHost([]string{"cat", DockerClientKeyPath})
	if err != nil {
		return "", "", fmt.Errorf("failed to read client key from Docker container: %w", err)
	}

	return string(certBytes), string(keyBytes), nil
}
