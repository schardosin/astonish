package main

import (
	"os"

	"github.com/schardosin/astonish/cmd/astonish"
)

func main() {
	if err := astonish.Execute(); err != nil {
		os.Exit(1)
	}
}
