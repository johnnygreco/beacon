package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/johnnygreco/beacon/internal/collector"
	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/spf13/cobra"
)

var errDoctorSetupFailed = errors.New("setup doctor found failures")

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
		Short: "Diagnose guided multi-machine setup",
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
		report.remediation("Run `beacon setup dashboard` on a dashboard machine or `beacon join <url>` on a collector.")
		return report.result()
	}

	report.line("PASS", "config", "loaded")
	fmt.Fprintf(out, "Role: %s\n", cfg.Fleet.Role)
	fmt.Fprintf(out, "Dashboard local URL: http://%s:%d\n", cfg.Server.Host, cfg.Server.Port)
	if strings.TrimSpace(cfg.Server.PublicURL) != "" {
		fmt.Fprintf(out, "Dashboard public URL: %s\n", cfg.Server.PublicURL)
	} else {
		fmt.Fprintln(out, "Dashboard public URL: not configured")
	}
	if strings.TrimSpace(cfg.Fleet.ControlPlaneURL) != "" {
		fmt.Fprintf(out, "Collector control-plane URL: %s\n", cfg.Fleet.ControlPlaneURL)
	} else {
		fmt.Fprintln(out, "Collector control-plane URL: not configured")
	}
	fmt.Fprintln(out)

	switch cfg.Fleet.Role {
	case config.FleetRoleCollector:
		runCollectorSetupDoctor(ctx, report, cfg)
	case config.FleetRoleBoth, config.FleetRoleControlPlane:
		runDashboardSetupDoctor(ctx, report, cfg)
	default:
		report.line("FAIL", "fleet.role", fmt.Sprintf("unsupported role %q", cfg.Fleet.Role))
	}
	return report.result()
}

func runDashboardSetupDoctor(ctx context.Context, report *doctorReport, cfg *config.Config) {
	report.line("INFO", "vantage", "local dashboard/control-plane machine")
	if checkServer(cfg.Server.Port) {
		report.line("PASS", "local health", fmt.Sprintf("http://127.0.0.1:%d/health responded", cfg.Server.Port))
	} else {
		report.line("WARN", "local health", fmt.Sprintf("Beacon is not responding at http://127.0.0.1:%d/health", cfg.Server.Port))
		report.remediation("Start the dashboard with `beacon up`.")
	}

	snapshot, err := controlPlaneStatus(ctx, cfg)
	if err != nil {
		report.line("FAIL", "control-plane metadata", err.Error())
		report.remediation("Run `beacon setup dashboard` to initialize dashboard metadata.")
	} else {
		report.line("PASS", "control-plane metadata", fmt.Sprintf("schema_epoch=%s nodes=%d collectors=%d sources=%d", snapshot.SchemaEpoch, len(snapshot.Nodes), len(snapshot.Collectors), len(snapshot.Sources)))
		if snapshot.ResetPending {
			report.line("FAIL", "control-plane reset", fmt.Sprintf("reset pending at epoch %s", snapshot.ResetPendingEpoch))
			report.remediation("Rerun `beacon db reset --force` after resolving the prior reset failure.")
		}
	}

	runDoctorClickHouseChecks(ctx, report, cfg)

	publicURL := strings.TrimSpace(cfg.Server.PublicURL)
	if publicURL == "" {
		report.line("WARN", "server.public_url", "not configured; remote collectors do not have an invite URL")
		report.remediation("Run `beacon setup dashboard --collector-url https://beacon.example.com`.")
		return
	}
	if config.IsLoopbackURL(publicURL) {
		report.line("INFO", "public URL", "loopback URL; remote collectors need an explicit local tunnel")
		report.remediation("Use `--local-tunnel` when setup/invite output intentionally targets collector-side loopback.")
		report.line("INFO", "public URL checks", "local-only; skipping protected-route public exposure checks for loopback")
		return
	}
	if err := runPublicURLChecks(ctx, publicURL, publicURLCheckOptions{}); err != nil {
		report.line("FAIL", "public URL checks", err.Error())
		report.remediation("Verify `/health` and `/api/ingest/v1/enroll` reach Beacon with `Authorization` preserved.")
		report.remediation("If Beacon rejects the proxy Host while loopback-bound, configure the upstream Host as `127.0.0.1:4600` or `localhost:4600`.")
		report.remediation("If dashboard/API/MCP are intentionally public, rerun startup/invite commands with `--unsafe-public-url` for that invocation.")
		return
	}
	report.line("PASS", "public URL checks", "/health, enrollment auth, and protected-route posture passed from this machine")
	report.remediation("Create collector invites with `beacon invite`.")
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

	ch, err := statusOpenStore(ctx, opts)
	if err != nil {
		report.line("FAIL", "ClickHouse migration", fmt.Sprintf("not migration-ready at %s database=%s (%v)", strings.Join(addrs, ","), opts.Database, err))
		report.remediation("Start ClickHouse with `beacon db up` or fix database.addrs, then run `beacon db migrate`.")
		return
	}
	defer ch.Close()
	report.line("PASS", "ClickHouse migration", fmt.Sprintf("migration-ready at %s database=%s", strings.Join(addrs, ","), opts.Database))
}

func runCollectorSetupDoctor(ctx context.Context, report *doctorReport, cfg *config.Config) {
	report.line("INFO", "vantage", "local collector machine")
	controlPlaneURL := strings.TrimSpace(cfg.Fleet.ControlPlaneURL)
	if controlPlaneURL == "" {
		report.line("FAIL", "fleet.control_plane_url", "not configured")
		report.remediation("Run `beacon join <control-plane-url>` from this collector.")
	} else {
		if config.IsLoopbackURL(controlPlaneURL) {
			report.line("INFO", "control-plane URL", "loopback URL; this collector must have an active local tunnel")
		}
		if err := preflightEnrollmentRoute(ctx, controlPlaneURL, publicURLCheckOptions{}); err != nil {
			report.line("FAIL", "enrollment route preflight", err.Error())
			report.remediation("Fix the proxy/tunnel route before running `beacon join`; the check uses an invalid bearer and does not consume real tokens.")
		} else {
			report.line("PASS", "enrollment route preflight", "/health and invalid-bearer enrollment auth passed from this collector")
		}
	}

	snapshot, err := controlPlaneStatus(ctx, cfg)
	if err != nil {
		report.line("FAIL", "collector metadata", err.Error())
		report.remediation("Run `beacon join <control-plane-url>` to enroll this collector.")
	} else {
		report.line("PASS", "collector metadata", fmt.Sprintf("schema_epoch=%s node=%s collector=%s", snapshot.SchemaEpoch, emptyLabel(snapshot.LocalNodeID), emptyLabel(snapshot.LocalCollectorID)))
		checkCollectorAssignments(report, cfg, snapshot)
	}

	if err := checkDoctorIngestToken(cfg); err != nil {
		report.line("FAIL", "ingest token", err.Error())
		report.remediation("Run `beacon join <control-plane-url>` with a fresh one-use enrollment token.")
	} else {
		report.line("PASS", "ingest token", "configured token source is present")
	}

	if stats, err := doctorSpoolStats(cfg); err != nil {
		report.line("FAIL", "collector spool", err.Error())
		report.remediation("Check `fleet.spool_dir` permissions or rerun `beacon join --force` after reviewing config changes.")
	} else {
		report.line("PASS", "collector spool", fmt.Sprintf("pending=%d inflight=%d corrupt=%d active_bytes=%d max_bytes=%d", stats.PendingCount, stats.InflightCount, stats.CorruptCount, stats.ActiveBytes, stats.MaxBytes))
	}
	report.remediation("Run a one-cycle collector validation with `beacon collect --once`.")
}

func checkCollectorAssignments(report *doctorReport, cfg *config.Config, snapshot *controlplane.Snapshot) {
	if snapshot == nil || snapshot.LocalNodeID == "" || snapshot.LocalCollectorID == "" {
		report.line("FAIL", "collector identity", "local node/collector assignment is missing")
		return
	}
	missing := missingCollectorSources(cfg, snapshot)
	if len(missing) > 0 {
		report.line("FAIL", "source assignments", fmt.Sprintf("missing source_id for %s", strings.Join(missing, ", ")))
		report.remediation("Rerun `beacon join <control-plane-url>` so the control plane can assign all configured sources.")
		return
	}
	report.line("PASS", "source assignments", fmt.Sprintf("%d configured sources assigned", len(cfg.Capture.Sources)))
}

func missingCollectorSources(cfg *config.Config, snapshot *controlplane.Snapshot) []string {
	if cfg == nil || snapshot == nil || snapshot.LocalCollectorID == "" {
		return nil
	}
	assigned := map[string]struct{}{}
	for _, source := range snapshot.Sources {
		if source.CollectorID == snapshot.LocalCollectorID {
			assigned[source.Name] = struct{}{}
		}
	}
	var missing []string
	for _, source := range cfg.Capture.Sources {
		if _, ok := assigned[source.Name]; !ok {
			missing = append(missing, source.Name)
		}
	}
	return missing
}

func checkDoctorIngestToken(cfg *config.Config) error {
	_, err := readIngestToken(cfg)
	return err
}

func doctorSpoolStats(cfg *config.Config) (collector.SpoolStats, error) {
	return collector.ReadSpoolStats(cfg.Fleet.SpoolDir, cfg.Fleet.SpoolMaxBytes)
}

func doctorConfigPath() string {
	if strings.TrimSpace(cfgFile) != "" {
		return cfgFile
	}
	return config.DefaultConfigPath()
}

func emptyLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(missing)"
	}
	return value
}

func doctorLine(out interface{ Write([]byte) (int, error) }, status, label, detail string) {
	fmt.Fprintf(out, "[%s] %s: %s\n", status, label, detail)
}

func doctorRemediation(out interface{ Write([]byte) (int, error) }, detail string) {
	fmt.Fprintf(out, "  -> %s\n", detail)
}
