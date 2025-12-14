package main

import (
	"fmt"
	"os"

	"github.com/schardosin/astonish/cmd/astonish"
)

func main() {
	if err := astonish.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
