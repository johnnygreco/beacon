package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
)

var cfgFile string

func main() {
	rootCmd := &cobra.Command{
		Use:   "beacon",
		Short: "Real-time AI coding agent monitoring dashboard",
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: beacon.toml)")

	rootCmd.AddCommand(
		newUpCmd(),
		newDownCmd(),
		newRunCmd(),
		newServeCmd(),
		newWatchCmd(),
		newMCPCmd(),
		newStatusCmd(),
		newStopCmd(),
		newDBCmd(),
	)

	// Default to serve if no subcommand
	rootCmd.RunE = runServe

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Start Beacon web and capture services",
		RunE:  runServe,
	}
}

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the running Beacon server",
		RunE:  runStop,
	}
}

func newRunCmd() *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run one Beacon service",
	}
	runCmd.AddCommand(
		&cobra.Command{
			Use:   "capture",
			Short: "Run capture only",
			RunE:  runWatch,
		},
		&cobra.Command{
			Use:   "web",
			Short: "Run web and capture services",
			RunE:  runServe,
		},
		&cobra.Command{
			Use:   "mcp",
			Short: "Run the MCP server",
			RunE:  runMCP,
		},
	)
	return runCmd
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func storeOptionsFromConfig(cfg *config.Config) store.Options {
	opts := store.DefaultOptions()
	if len(cfg.Database.Addrs) > 0 {
		opts.Addrs = cfg.Database.Addrs
	}
	if cfg.Database.Database != "" {
		opts.Database = cfg.Database.Database
	}
	if cfg.Database.Username != "" {
		opts.Username = cfg.Database.Username
	}
	opts.Password = cfg.Database.Password
	opts.Secure = cfg.Database.Secure
	if cfg.Database.ReadPoolSize > 0 {
		opts.ReadPoolSize = cfg.Database.ReadPoolSize
	}
	return opts
}
