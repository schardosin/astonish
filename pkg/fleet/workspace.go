package fleet

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SetupSessionWorkspace creates an isolated workspace directory for a fleet
// session. The workspace path should be obtained from ResolveSessionWorkspaceDir.
//
// For git_repo sources, it uses "git clone --local" from the base workspace
// (~/astonish_projects/<repo-name>/) which hardlinks the object store and is
// near-instant. If the base workspace doesn't exist or isn't a git repo, it
// falls back to a full "git clone" from the remote URL.
//
// For local sources, it uses "git clone --local" if the source is a git repo,
// otherwise "cp -a" for a faithful copy.
//
// If the workspace directory already exists (e.g., from a previous run or
// a recovered session), it is left as-is and no clone/copy is performed.
//
// baseDir is the permanent base workspace (~/astonish_projects/<repo-name>/)
// where the wizard cloned the repo and generated AGENTS.md. It may be empty
// if no base workspace exists.
func SetupSessionWorkspace(workspaceDir string, source *ProjectSourceConfig, baseDir string) error {
	// If the workspace already exists, assume it was set up previously.
	if info, err := os.Stat(workspaceDir); err == nil && info.IsDir() {
		log.Printf("[fleet-workspace] Workspace %s already exists, reusing", workspaceDir)
		return nil
	}

	// No source configured: just create the empty directory.
	if source == nil {
		log.Printf("[fleet-workspace] No project source, creating empty workspace %s", workspaceDir)
		return os.MkdirAll(workspaceDir, 0755)
	}

	// Ensure the parent directory exists before any clone/copy.
	if err := ensureParentDir(workspaceDir); err != nil {
		return err
	}

	switch source.Type {
	case "git_repo":
		return setupGitRepo(workspaceDir, source.Repo, baseDir)
	case "local":
		return setupLocal(workspaceDir, source.Path)
	default:
		log.Printf("[fleet-workspace] Unknown project source type %q, creating empty workspace", source.Type)
		return os.MkdirAll(workspaceDir, 0755)
	}
}

// CleanupSessionWorkspace removes the per-session workspace directory.
// It is a no-op if the path is empty or does not exist.
// The base workspace (~/astonish_projects/<repo-name>/) is never touched.
//
// Safety: refuses to delete paths that don't look like session workspaces.
// Legitimate workspace paths are always under a "workspaces/" subdirectory
// (created by ResolveSessionWorkspaceDir). This guards against accidentally
// deleting host project directories if a container-internal path leaks
// into WorkspaceDir metadata.
func CleanupSessionWorkspace(workspaceDir string) error {
	if workspaceDir == "" {
		return nil
	}
	if _, err := os.Stat(workspaceDir); os.IsNotExist(err) {
		return nil
	}

	// Safety guard: only allow deletion of paths under a "workspaces/" directory.
	// ResolveSessionWorkspaceDir always produces paths like:
	//   <baseDir>/workspaces/<sessionID>
	// Container-internal paths (e.g., "/root/astonish", "/root") never contain
	// this component and must NOT be deleted on the host.
	absPath, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("cannot resolve workspace path %q: %w", workspaceDir, err)
	}
	if !isUnderWorkspacesDir(absPath) {
		log.Printf("[fleet-workspace] SAFETY: refusing to delete %q — not under a workspaces/ directory", absPath)
		return nil
	}

	log.Printf("[fleet-workspace] Removing workspace %s", workspaceDir)
	return os.RemoveAll(workspaceDir)
}

// isUnderWorkspacesDir checks whether the given absolute path is a child of a
// directory named "workspaces". This ensures we only delete paths created by
// ResolveSessionWorkspaceDir (which places them under .../workspaces/<id>).
func isUnderWorkspacesDir(absPath string) bool {
	// Walk up the path looking for a "workspaces" component that is a proper ancestor.
	// E.g., "/root/.config/astonish/sessions/workspaces/abc123" → true
	//        "/root/astonish" → false
	//        "/root/workspaces" → false (is the workspaces dir itself, not a child)
	parts := strings.Split(filepath.Clean(absPath), string(filepath.Separator))
	for i, part := range parts {
		if part == "workspaces" && i < len(parts)-1 {
			return true
		}
	}
	return false
}

// setupGitRepo creates a per-session workspace for a git_repo source.
// It tries "git clone --local" from the base workspace first (near-instant,
// hardlinks object store). If the base doesn't exist, falls back to a full
// network clone from the remote URL.
func setupGitRepo(workspaceDir, repo, baseDir string) error {
	// Try --local clone from the base workspace first.
	if baseDir != "" && isGitRepo(baseDir) {
		log.Printf("[fleet-workspace] git clone --local %s -> %s", baseDir, workspaceDir)
		if err := gitCloneLocal(baseDir, workspaceDir); err != nil {
			log.Printf("[fleet-workspace] --local clone failed, falling back to remote: %v", err)
		} else {
			return nil
		}
	}

	// Fallback: full clone from remote URL.
	if repo == "" {
		return fmt.Errorf("git_repo source has empty repo field and no base workspace")
	}
	return gitCloneRemote(workspaceDir, repo)
}

// setupLocal creates a per-session workspace for a local source.
// If the source is a git repo, uses "git clone --local" for efficiency.
// Otherwise, uses "cp -a" for a faithful copy.
func setupLocal(workspaceDir, srcPath string) error {
	if srcPath == "" {
		return fmt.Errorf("local source has empty path field")
	}

	srcPath = expandHome(srcPath)

	info, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("source path %s: %w", srcPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source path %s is not a directory", srcPath)
	}

	// If the local source is a git repo, use --local clone for efficiency.
	if isGitRepo(srcPath) {
		log.Printf("[fleet-workspace] git clone --local %s -> %s (local git source)", srcPath, workspaceDir)
		if err := gitCloneLocal(srcPath, workspaceDir); err != nil {
			log.Printf("[fleet-workspace] --local clone failed, falling back to cp -a: %v", err)
		} else {
			return nil
		}
	}

	// Non-git local directory or --local failed: full copy.
	log.Printf("[fleet-workspace] cp -a %s -> %s", srcPath, workspaceDir)
	cmd := exec.Command("cp", "-a", srcPath, workspaceDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cp -a %s %s: %w", srcPath, workspaceDir, err)
	}
	return nil
}

// gitCloneLocal runs "git clone --local <src> <dst>".
// This hardlinks the git object store making it near-instant (<1s for large repos).
// The cloned repo retains the original's remote configuration so "git fetch origin"
// still works against the upstream remote.
func gitCloneLocal(srcDir, dstDir string) error {
	cmd := exec.Command("git", "clone", "--local", srcDir, dstDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone --local %s %s: %w", srcDir, dstDir, err)
	}
	return nil
}

// gitCloneRemote runs a full "git clone" from a remote URL.
// The repo can be in "owner/repo" shorthand (expanded to a GitHub HTTPS URL)
// or a full URL.
func gitCloneRemote(workspaceDir, repo string) error {
	if repo == "" {
		return fmt.Errorf("git_repo source has empty repo field")
	}

	repoURL := repo
	if !strings.Contains(repo, "://") && !strings.HasPrefix(repo, "git@") {
		repoURL = "https://github.com/" + repo + ".git"
	}

	log.Printf("[fleet-workspace] git clone %s -> %s (full remote clone)", repoURL, workspaceDir)
	cmd := exec.Command("git", "clone", repoURL, workspaceDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone %s: %w", repoURL, err)
	}
	return nil
}

// isGitRepo returns true if the directory exists and contains a .git directory.
func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

// ensureParentDir creates the parent directory of the given path if needed.
func ensureParentDir(path string) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("creating parent directory %s: %w", parent, err)
	}
	return nil
}
