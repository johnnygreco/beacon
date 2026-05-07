package main

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
)

const (
	clickHouseContainerName = "beacon-clickhouse"
	clickHouseImage         = "clickhouse/clickhouse-server:24.12"
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

	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Create or update ClickHouse tables",
		RunE:  runDBMigrate,
	}

	upCmd := &cobra.Command{
		Use:   "up",
		Short: "Start a local ClickHouse container and migrate the schema",
		RunE:  runDBUp,
	}
	upCmd.Flags().String("image", clickHouseImage, "ClickHouse Docker image")
	upCmd.Flags().Bool("no-migrate", false, "Start ClickHouse without running schema migration")

	downCmd := &cobra.Command{
		Use:   "down",
		Short: "Stop the local ClickHouse container started by beacon db up",
		RunE:  runDBDown,
	}

	dbCmd.AddCommand(upCmd, downCmd, migrateCmd, resetCmd)
	return dbCmd
}

func runDBUp(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker is required for beacon db up; install Docker or start ClickHouse manually")
	}

	image, _ := cmd.Flags().GetString("image")
	noMigrate, _ := cmd.Flags().GetBool("no-migrate")

	if containerExists(clickHouseContainerName) {
		if out, err := docker("container", "start", clickHouseContainerName); err != nil {
			return fmt.Errorf("starting ClickHouse container: %w\n%s", err, out)
		}
	} else {
		if out, err := docker(
			"run", "-d",
			"--name", clickHouseContainerName,
			"-p", "9000:9000",
			"-p", "8123:8123",
			"-v", "beacon-clickhouse-data:/var/lib/clickhouse",
			image,
		); err != nil {
			return fmt.Errorf("creating ClickHouse container: %w\n%s", err, out)
		}
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		cfg = &config.Config{}
	}
	opts := storeOptionsFromConfig(cfg)
	if err := waitForClickHouse(opts, 45*time.Second); err != nil {
		return err
	}
	if noMigrate {
		fmt.Println("ClickHouse is running.")
		return nil
	}
	ch, err := store.Open(context.Background(), opts)
	if err != nil {
		return fmt.Errorf("migrate failed after ClickHouse start: %w", err)
	}
	defer ch.Close()
	fmt.Println("ClickHouse is running and Beacon schema is migrated.")
	return nil
}

func runDBDown(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker is required for beacon db down")
	}
	if !containerExists(clickHouseContainerName) {
		fmt.Println("No beacon-clickhouse container found.")
		return nil
	}
	if out, err := docker("container", "stop", clickHouseContainerName); err != nil {
		return fmt.Errorf("stopping ClickHouse container: %w\n%s", err, out)
	}
	fmt.Println("ClickHouse container stopped.")
	return nil
}

func runDBMigrate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		cfg = &config.Config{}
	}
	opts := storeOptionsFromConfig(cfg)
	ch, err := store.Open(context.Background(), opts)
	if err != nil {
		return fmt.Errorf("migrate failed: %w", err)
	}
	defer ch.Close()
	fmt.Println("ClickHouse schema migrated.")
	return nil
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

	opts := storeOptionsFromConfig(cfg)
	ch, err := store.Open(context.Background(), opts)
	if err != nil {
		return fmt.Errorf("opening clickhouse store: %w", err)
	}
	defer ch.Close()

	if err := store.Reset(context.Background(), ch.DB, ch.Database()); err != nil {
		return fmt.Errorf("reset failed: %w", err)
	}

	fmt.Println("Database reset complete.")
	return nil
}

func waitForClickHouse(opts store.Options, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		for _, addr := range opts.Addrs {
			conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return nil
			}
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("ClickHouse did not become ready within %s: %w", timeout, lastErr)
}

func containerExists(name string) bool {
	_, err := docker("container", "inspect", name)
	return err == nil
}

func docker(args ...string) (string, error) {
	out, err := exec.Command("docker", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
