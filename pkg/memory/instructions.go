package memory

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultInstructions is the default INSTRUCTIONS.md content shipped with Astonish.
// Users can edit this file directly or via memory_save with file="INSTRUCTIONS.md".
const DefaultInstructions = `# Agent Instructions

## General Behavior
- Attempt to accomplish tasks using tools before asking the user
- Present options when multiple approaches exist
- When a task fails, try at least 2 alternative approaches before giving up
- Only ask the user when genuinely stuck or when a decision requires their input
- For multi-step tasks, execute steps sequentially and report progress

## Permissions
- Always ask before running destructive commands (rm -rf, DROP TABLE, format, etc.)
- Ask before modifying system-level configuration files (/etc/*, systemd units)
- File edits within the workspace do not require confirmation
- Shell commands that only read information can run without asking

## Communication Style
- Be concise and direct
- Use tables for structured data
- Include file paths with line numbers when referencing code
- Skip unnecessary pleasantries and filler text

## Memory Guidelines
- Save durable facts (IPs, usernames, project settings) to MEMORY.md
- Save procedural knowledge (command recipes, workarounds) to topic files
- Never save volatile data (pod lists, disk usage, command output)
- When correcting facts, use overwrite mode to replace the section
`

// InstructionsPath returns the default INSTRUCTIONS.md path within the memory directory.
func InstructionsPath(memoryDir string) string {
	return filepath.Join(memoryDir, "INSTRUCTIONS.md")
}

// LoadInstructions reads INSTRUCTIONS.md from the given memory directory.
// Returns empty string if the file doesn't exist.
func LoadInstructions(memoryDir string) (string, error) {
	path := InstructionsPath(memoryDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read instructions file: %w", err)
	}
	return string(data), nil
}

// EnsureInstructions creates INSTRUCTIONS.md with default content if it doesn't exist.
// Returns true if the file was created, false if it already existed.
func EnsureInstructions(memoryDir string) (bool, error) {
	path := InstructionsPath(memoryDir)

	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}

	// Ensure directory exists
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create memory directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(DefaultInstructions), 0644); err != nil {
		return false, fmt.Errorf("failed to write instructions file: %w", err)
	}

	return true, nil
}
