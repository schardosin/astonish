package astonish

import (
	"fmt"
)

func handleMemoryCommand(args []string) error {
	fmt.Println("The 'memory' CLI command is no longer available.")
	fmt.Println()
	fmt.Println("Memory is now managed through the platform database (SQLite or PostgreSQL).")
	fmt.Println("Use the Astonish Studio web interface or the memory_save/memory_search")
	fmt.Println("tools during chat to interact with the memory system.")
	return nil
}
