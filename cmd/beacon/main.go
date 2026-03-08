package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/johnnygreco/beacon/internal/config"
)

var cfgFile string

func main() {
	rootCmd := &cobra.Command{
		Use:   "beacon",
		Short: "Real-time AI coding agent monitoring dashboard",
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: beacon.toml)")

	rootCmd.AddCommand(
		newServeCmd(),
		newWatchCmd(),
		newMCPCmd(),
		newStatusCmd(),
		newDBCmd(),
	)

	// Default to serve if no subcommand
	rootCmd.RunE = runServe

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// resolveDBPath resolves the database path from config with fallback default.
func resolveDBPath(cfg *config.Config) string {
	dbPath := cfg.Database.Path
	if dbPath == "" {
		dbPath = "~/.beacon/beacon.duckdb"
	}
	dbPath = expandHome(dbPath)
	os.MkdirAll(filepath.Dir(dbPath), 0755)
	return dbPath
}
