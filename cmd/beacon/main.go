package main

import (
	"os"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
)

var cfgFile string
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "beacon",
		Short:        "Real-time AI coding agent monitoring dashboard",
		Version:      version,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: beacon.toml)")

	rootCmd.AddCommand(
		newUpCmd(),
		newDownCmd(),
		newWatchCmd(),
		newMCPCmd(),
		newStatusCmd(),
		newDBCmd(),
	)

	return rootCmd
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
