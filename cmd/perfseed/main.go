package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/johnnygreco/beacon/internal/perf"
	"github.com/johnnygreco/beacon/internal/store"
)

func main() {
	size := flag.String("size", "small", "Dataset size: small, medium, large")
	addr := flag.String("clickhouse", "127.0.0.1:9000", "ClickHouse address")
	database := flag.String("database", "beacon", "ClickHouse database")
	reset := flag.Bool("reset", true, "Reset schema before seeding")
	flag.Parse()

	seedSize := perf.ParseSeedSize(*size)
	fmt.Printf("Seeding %s dataset into ClickHouse %s/%s\n", seedSize, *addr, *database)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	ch, err := store.Open(ctx, store.Options{Addrs: []string{*addr}, Database: *database, ReadPoolSize: 4})
	if err != nil {
		fmt.Fprintf(os.Stderr, "open clickhouse store: %v\n", err)
		os.Exit(1)
	}
	defer ch.Close()

	var stats perf.Stats
	if *reset {
		stats, err = perf.ResetAndSeed(ctx, ch, seedSize)
	} else {
		stats, err = perf.Seed(ctx, ch, seedSize)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Done: %s\n", stats)
	fmt.Println("Run beacon with: beacon up --config beacon.toml")
}
