package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/johnnygreco/beacon/internal/database"
	"github.com/johnnygreco/beacon/internal/perf"
)

func main() {
	size := flag.String("size", "small", "Dataset size: small, medium, large")
	dbPath := flag.String("db", "", "Database path (default: ~/.beacon/beacon-perf-{size}.duckdb)")
	reset := flag.Bool("reset", true, "Reset schema before seeding")
	flag.Parse()

	seedSize := perf.ParseSeedSize(*size)

	path := *dbPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "home dir: %v\n", err)
			os.Exit(1)
		}
		beaconDir := filepath.Join(home, ".beacon")
		if err := os.MkdirAll(beaconDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "create dir: %v\n", err)
			os.Exit(1)
		}
		path = filepath.Join(beaconDir, fmt.Sprintf("beacon-perf-%s.duckdb", seedSize))
	}

	fmt.Printf("Seeding %s dataset at %s\n", seedSize, path)

	db, err := database.Open(path, 2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	var stats perf.Stats
	if *reset {
		stats, err = perf.ResetAndSeed(ctx, db, seedSize)
	} else {
		stats, err = perf.Seed(ctx, db, seedSize)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Done: %s\n", stats)
	fmt.Printf("Database: %s\n", path)
	fmt.Printf("Run beacon with: beacon serve --db %s\n", path)
}
