package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/technodrome-ai/technodrome/internal/config"
	"github.com/technodrome-ai/technodrome/internal/database"
)

func newDBCmd() *cobra.Command {
	dbCmd := &cobra.Command{
		Use:   "db",
		Short: "Database management commands",
	}

	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
		RunE:  runDBMigrate,
	}

	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Drop and recreate all tables (destructive)",
		RunE:  runDBReset,
	}
	resetCmd.Flags().Bool("force", false, "Skip confirmation")

	dbCmd.AddCommand(migrateCmd, resetCmd)
	return dbCmd
}

func runDBMigrate(cmd *cobra.Command, args []string) error {
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

	fmt.Println("Migrations applied successfully.")
	return nil
}

func runDBReset(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	if !force {
		fmt.Print("This will destroy all data. Are you sure? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
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
