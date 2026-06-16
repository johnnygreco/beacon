package main

import (
	"os"

	"github.com/johnnygreco/beacon/internal/beaconcli"
)

func main() {
	if err := beaconcli.Execute(); err != nil {
		os.Exit(1)
	}
}
