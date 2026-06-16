package beaconcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
)

var errDoctorSetupFailed = errors.New("setup doctor found failures")
var doctorOpenClickHouseReadOnly = store.OpenReadOnly

type doctorReport struct {
	out    interface{ Write([]byte) (int, error) }
	failed bool
}

func (d *doctorReport) line(status, label, detail string) {
	if status == "FAIL" {
		d.failed = true
	}
	doctorLine(d.out, status, label, detail)
}

func (d *doctorReport) remediation(detail string) {
	doctorRemediation(d.out, detail)
}

func (d *doctorReport) result() error {
	if d.failed {
		return errDoctorSetupFailed
	}
	return nil
}

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run Beacon diagnostics",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newDoctorSetupCmd())
	return cmd
}

func newDoctorSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Diagnose local Beacon setup",
		Args:  cobra.NoArgs,
		RunE:  runDoctorSetup,
	}
}

func runDoctorSetup(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	ctx := commandContext(cmd)
	fmt.Fprintln(out, "Beacon setup doctor")
	fmt.Fprintln(out, "===================")
	fmt.Fprintf(out, "Config path: %s\n", doctorConfigPath())
	report := &doctorReport{out: out}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		report.line("FAIL", "config", fmt.Sprintf("could not load config: %v", err))
		report.remediation("Fix the config file or remove it to use local defaults.")
		return report.result()
	}

	report.line("PASS", "config", "loaded")
	fmt.Fprintf(out, "Dashboard URL: http://%s:%d\n", cfg.Server.Host, cfg.Server.Port)
	fmt.Fprintln(out)
	runLocalSetupDoctor(ctx, report, cfg)
	return report.result()
}

func runLocalSetupDoctor(ctx context.Context, report *doctorReport, cfg *config.Config) {
	report.line("INFO", "mode", "single-machine local dashboard")
	if checkServer(cfg.Server.Port) {
		report.line("PASS", "local health", fmt.Sprintf("http://127.0.0.1:%d/health responded", cfg.Server.Port))
	} else {
		report.line("WARN", "local health", fmt.Sprintf("Beacon is not responding at http://127.0.0.1:%d/health", cfg.Server.Port))
		report.remediation("Start the dashboard with `beacon up`.")
	}

	runDoctorClickHouseChecks(ctx, report, cfg)
}

func runDoctorClickHouseChecks(ctx context.Context, report *doctorReport, cfg *config.Config) {
	opts := storeOptionsFromConfig(cfg)
	addrs := clickHouseAddrs(opts)
	location := "remote"
	if shouldAutoStartClickHouse(opts) {
		location = "local"
	}
	report.line("INFO", "ClickHouse config", fmt.Sprintf("%s addrs=%s database=%s", location, strings.Join(addrs, ","), opts.Database))

	if shouldAutoStartClickHouse(opts) {
		if err := ensureLocalManagedDockerClickHousePrivate(opts); err != nil {
			report.line("FAIL", "ClickHouse managed Docker", err.Error())
			report.remediation("Remove the broad-bound managed ClickHouse container with `docker rm beacon-clickhouse`, then rerun `beacon db up` or `beacon up`.")
		} else {
			report.line("PASS", "ClickHouse managed Docker", "no broad-published beacon-managed Docker bindings detected")
		}
	} else {
		report.line("INFO", "ClickHouse managed Docker", "remote ClickHouse configured; skipping local managed Docker binding check")
	}

	ch, err := doctorOpenClickHouseReadOnly(ctx, opts)
	if err != nil {
		report.line("FAIL", "ClickHouse migration", fmt.Sprintf("not migration-ready at %s database=%s (%v)", strings.Join(addrs, ","), opts.Database, err))
		report.remediation("Start ClickHouse with `beacon db up` or fix database.addrs, then run `beacon db migrate`.")
		return
	}
	defer ch.Close()
	report.line("PASS", "ClickHouse migration", fmt.Sprintf("migration-ready at %s database=%s", strings.Join(addrs, ","), opts.Database))
}

func doctorConfigPath() string {
	if strings.TrimSpace(cfgFile) != "" {
		return cfgFile
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".beacon", "beacon.toml")
	}
	return filepath.Join("$HOME", ".beacon", "beacon.toml")
}

func doctorLine(out interface{ Write([]byte) (int, error) }, status, label, detail string) {
	fmt.Fprintf(out, "[%s] %s: %s\n", status, label, detail)
}

func doctorRemediation(out interface{ Write([]byte) (int, error) }, detail string) {
	fmt.Fprintf(out, "  -> %s\n", detail)
}
