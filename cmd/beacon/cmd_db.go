package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/database"
)

func newDBCmd() *cobra.Command {
	dbCmd := &cobra.Command{
		Use:   "db",
		Short: "Database management commands",
	}

	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Drop and recreate all tables (destructive)",
		RunE:  runDBReset,
	}
	resetCmd.Flags().Bool("force", false, "Skip confirmation")

	dbCmd.AddCommand(resetCmd)
	return dbCmd
}

func runDBReset(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	if !force {
		fmt.Print("This will destroy all data. Are you sure? [y/N] ")
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		if answer != "y" && answer != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		cfg = &config.Config{}
	}

	dbPath := resolveDBPath(cfg)

	db, err := database.Open(dbPath, 1)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	if err := database.ResetSchema(context.Background(), db); err != nil {
		return fmt.Errorf("reset failed: %w", err)
	}

	fmt.Println("Database reset complete.")
	return nil
}
