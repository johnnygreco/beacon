package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/spf13/cobra"
)

type setupDashboardOptions struct {
	CollectorURL     string
	LocalTunnel      bool
	Name             string
	Start            bool
	ControlPlaneOnly bool
	DryRun           bool
	Force            bool
	UnsafePublicURL  bool
}

type inviteOptions struct {
	CollectorURL    string
	LocalTunnel     bool
	SaveURL         bool
	TTL             time.Duration
	Format          string
	UnsafePublicURL bool
}

var newSetupPublicURLCheckClient = func() *http.Client {
	return &http.Client{Timeout: 2 * time.Second}
}

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure guided Beacon workflows",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newSetupDashboardCmd())
	return cmd
}

func newSetupDashboardCmd() *cobra.Command {
	opts := setupDashboardOptions{}
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Configure this machine as a Beacon dashboard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupDashboard(cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.CollectorURL, "collector-url", "", "public root URL collectors use to reach this dashboard")
	cmd.Flags().BoolVar(&opts.LocalTunnel, "local-tunnel", false, "label a loopback collector URL as a local tunnel URL")
	cmd.Flags().StringVar(&opts.Name, "name", "", "dashboard and local node display name")
	cmd.Flags().BoolVar(&opts.Start, "start", false, "start Beacon after writing setup")
	cmd.Flags().BoolVar(&opts.ControlPlaneOnly, "control-plane-only", false, "configure only control-plane services and disable local capture")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "show planned config changes without writing")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "overwrite existing guided config fields")
	cmd.Flags().BoolVar(&opts.UnsafePublicURL, "unsafe-public-url", false, "acknowledge unauthenticated public dashboard/API exposure when starting")
	return cmd
}

func newInviteCmd() *cobra.Command {
	opts := inviteOptions{TTL: 15 * time.Minute, Format: "text"}
	cmd := &cobra.Command{
		Use:   "invite",
		Short: "Create a one-use collector enrollment invite",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInvite(cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.CollectorURL, "collector-url", "", "public root URL collectors use to reach this dashboard")
	cmd.Flags().BoolVar(&opts.LocalTunnel, "local-tunnel", false, "label a loopback collector URL as a local tunnel URL")
	cmd.Flags().BoolVar(&opts.SaveURL, "save-url", false, "save --collector-url to server.public_url before creating the invite")
	cmd.Flags().DurationVar(&opts.TTL, "ttl", 15*time.Minute, "duration before the generated enrollment token expires")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "output format: text or json")
	cmd.Flags().BoolVar(&opts.UnsafePublicURL, "unsafe-public-url", false, "acknowledge unauthenticated public dashboard/API exposure")
	return cmd
}

func runSetupDashboard(cmd *cobra.Command, opts setupDashboardOptions) error {
	ctx := commandContext(cmd)
	collectorURL, err := normalizeCollectorURL(opts.CollectorURL)
	if err != nil {
		return err
	}
	if collectorURL != "" && config.IsLoopbackURL(collectorURL) && !opts.LocalTunnel {
		return fmt.Errorf("--collector-url points at loopback; pass --local-tunnel only when collectors reach it through a local tunnel")
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = defaultSetupName()
	}
	role := config.FleetRoleBoth
	captureEnabled := true
	if opts.ControlPlaneOnly {
		role = config.FleetRoleControlPlane
		captureEnabled = false
	}
	patch := config.GuidedConfigPatch{
		Path:                cfgFile,
		FleetRole:           role,
		Name:                name,
		PublicURL:           collectorURL,
		CaptureEnabled:      &captureEnabled,
		ApplyDefaultSources: !opts.ControlPlaneOnly,
	}
	if collectorURL != "" && !config.IsLoopbackURL(collectorURL) {
		patch.AuthMode = config.AuthModeOwnerToken
	}
	plan, err := config.PlanGuidedConfigPatch(patch)
	if err != nil {
		return err
	}
	if err := requireForceForGuidedConfigConflicts(plan, opts.Force); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	printGuidedConfigPlan(out, "Beacon dashboard setup", plan)
	if opts.DryRun {
		return nil
	}
	if err := plan.Write(); err != nil {
		return err
	}
	printGuidedConfigWrite(out, plan)

	cfg, err := config.Load(plan.Path)
	if err != nil {
		return fmt.Errorf("loading guided config: %w", err)
	}
	store, snapshot, err := initializeControlPlane(ctx, cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if err != nil {
		return fmt.Errorf("initializing control-plane metadata: %w", err)
	}
	defer store.Close()
	owner, err := ensureOwnerToken(ctx, store)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Control-plane metadata: %s\n", snapshot.Path)
	if owner != nil {
		fmt.Fprintf(out, "Owner token (shown once):\n%s\n", owner.Plaintext)
	} else {
		fmt.Fprintln(out, "Owner token: active token already exists.")
	}
	if collectorURL == "" {
		fmt.Fprintf(out, "Dashboard URL: http://%s:%d\n", cfg.Server.Host, cfg.Server.Port)
	} else {
		fmt.Fprintf(out, "Collector URL: %s\n", collectorURL)
		if opts.LocalTunnel {
			fmt.Fprintln(out, "Collector URL mode: local tunnel")
			fmt.Fprintln(out, "Public URL checks: local-only; ensure the tunnel is running and forwards to this dashboard before starting collectors.")
		} else if !opts.Start {
			reportSetupPublicURLChecks(ctx, out, collectorURL, opts.UnsafePublicURL)
		}
	}
	fmt.Fprintln(out, "Create collector enrollment invites with `beacon invite`.")

	if opts.Start {
		upCmd := newUpCmd()
		if opts.UnsafePublicURL {
			if err := upCmd.Flags().Set("unsafe-public-url", "true"); err != nil {
				return err
			}
		}
		return runServe(upCmd, nil)
	}
	return nil
}

func runInvite(cmd *cobra.Command, opts inviteOptions) error {
	ctx := commandContext(cmd)
	if opts.TTL <= 0 {
		return fmt.Errorf("--ttl must be positive")
	}
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return fmt.Errorf("--format must be text or json")
	}
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.Fleet.Role == config.FleetRoleCollector {
		return fmt.Errorf("fleet.role %q cannot create dashboard invites", config.FleetRoleCollector)
	}

	collectorURL := strings.TrimSpace(opts.CollectorURL)
	if collectorURL == "" {
		collectorURL = cfg.Server.PublicURL
	}
	collectorURL, err = normalizeCollectorURL(collectorURL)
	if err != nil {
		return err
	}
	if collectorURL == "" {
		return fmt.Errorf("collector URL is required; pass --collector-url or set server.public_url")
	}
	if config.IsLoopbackURL(collectorURL) && !opts.LocalTunnel {
		return fmt.Errorf("collector URL %s is loopback; pass --local-tunnel only when collectors reach it through a local tunnel", collectorURL)
	}
	var saveURLPlan *config.GuidedConfigPlan
	if opts.SaveURL {
		patch := config.GuidedConfigPatch{Path: cfgFile, PublicURL: collectorURL}
		if !config.IsLoopbackURL(collectorURL) {
			patch.AuthMode = config.AuthModeOwnerToken
		}
		plan, err := config.PlanGuidedConfigPatch(patch)
		if err != nil {
			return err
		}
		saveURLPlan = plan
	}
	if !config.IsLoopbackURL(collectorURL) {
		if err := runPublicURLChecks(ctx, collectorURL, publicURLCheckOptions{Unsafe: opts.UnsafePublicURL}); err != nil {
			return fmt.Errorf("refusing to create invite before public URL checks pass: %w", err)
		}
		if opts.UnsafePublicURL {
			fmt.Fprintln(cmd.ErrOrStderr(), "Warning: public URL connectivity checks passed, but protected dashboard/API/MCP route checks were skipped because --unsafe-public-url was set.")
		}
	}
	if saveURLPlan != nil {
		if err := saveURLPlan.Write(); err != nil {
			return err
		}
		cfg, err = config.Load(saveURLPlan.Path)
		if err != nil {
			return fmt.Errorf("loading saved config: %w", err)
		}
	}

	store, _, err := initializeControlPlane(ctx, cfg, nil)
	if err != nil {
		return fmt.Errorf("initializing control-plane metadata: %w", err)
	}
	defer store.Close()
	expiresAt := time.Now().UTC().Add(opts.TTL)
	token, err := store.CreateToken(ctx, controlplane.CreateTokenRequest{
		Type:      controlplane.TokenTypeEnroll,
		ExpiresAt: &expiresAt,
	})
	if err != nil {
		return fmt.Errorf("create enrollment token: %w", err)
	}

	out := cmd.OutOrStdout()
	if format == "json" {
		return writeInviteJSON(out, collectorURL, token.Plaintext, expiresAt, opts.LocalTunnel)
	}
	writeInviteText(out, collectorURL, token.Plaintext, expiresAt, opts.LocalTunnel)
	return nil
}

func normalizeCollectorURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	return config.NormalizeRootURL(raw, "--collector-url")
}

func defaultSetupName() string {
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return hostname
	}
	return "Beacon Dashboard"
}

func ensureOwnerToken(ctx context.Context, store *controlplane.Store) (*controlplane.CreatedToken, error) {
	hasOwner, err := store.HasActiveOwnerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("check owner token: %w", err)
	}
	if hasOwner {
		return nil, nil
	}
	owner, err := store.CreateToken(ctx, controlplane.CreateTokenRequest{Type: controlplane.TokenTypeOwner})
	if err != nil {
		return nil, fmt.Errorf("create owner token: %w", err)
	}
	return owner, nil
}

func reportSetupPublicURLChecks(ctx context.Context, out interface{ Write([]byte) (int, error) }, collectorURL string, unsafe bool) {
	if config.IsLoopbackURL(collectorURL) {
		fmt.Fprintln(out, "Public URL checks: local-only.")
		return
	}
	client := newSetupPublicURLCheckClient()
	if err := runPublicURLChecks(ctx, collectorURL, publicURLCheckOptions{Unsafe: unsafe, Client: client}); err != nil {
		fmt.Fprintf(out, "Public URL checks: pending/failed (%v)\n", err)
		fmt.Fprintln(out, "Run `beacon up` to perform mandatory startup checks before creating invites.")
		return
	}
	if unsafe {
		fmt.Fprintln(out, "Public URL checks: connectivity passed; protected dashboard/API/MCP route checks skipped due to --unsafe-public-url.")
		return
	}
	fmt.Fprintln(out, "Public URL checks: passed.")
}

func requireForceForGuidedConfigConflicts(plan *config.GuidedConfigPlan, force bool) error {
	if force || plan == nil {
		return nil
	}
	var conflicts []string
	for _, change := range plan.Changes {
		if change.Old != "(unset)" {
			conflicts = append(conflicts, change.Field)
		}
	}
	if len(conflicts) == 0 {
		return nil
	}
	return fmt.Errorf("guided setup would overwrite existing config fields %s; rerun with --force to apply these changes", strings.Join(conflicts, ", "))
}

func printGuidedConfigPlan(out interface{ Write([]byte) (int, error) }, title string, plan *config.GuidedConfigPlan) {
	fmt.Fprintf(out, "%s config: %s\n", title, plan.Path)
	if !plan.Changed {
		fmt.Fprintln(out, "No config changes needed.")
		return
	}
	fmt.Fprintln(out, "Planned changes:")
	for _, change := range plan.Changes {
		fmt.Fprintf(out, "  %s: %s -> %s\n", change.Field, change.Old, change.New)
	}
}

func printGuidedConfigWrite(out interface{ Write([]byte) (int, error) }, plan *config.GuidedConfigPlan) {
	if !plan.Changed {
		return
	}
	fmt.Fprintln(out, "Config updated.")
	if plan.BackupPath != "" {
		fmt.Fprintf(out, "Config backup: %s\n", plan.BackupPath)
	}
}

func writeInviteText(out interface{ Write([]byte) (int, error) }, collectorURL, token string, expiresAt time.Time, localTunnel bool) {
	fmt.Fprintln(out, "Beacon collector invite")
	fmt.Fprintf(out, "URL: %s\n", collectorURL)
	fmt.Fprintf(out, "Expires: %s\n", expiresAt.Format(time.RFC3339))
	if localTunnel {
		fmt.Fprintln(out, "Mode: local tunnel")
		fmt.Fprintln(out, "Checks: local-only; ensure the tunnel is running and forwards to this dashboard before starting collectors.")
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "On the collector machine:")
	fmt.Fprintf(out, "  printf '%%s\\n' \"$BEACON_ENROLL_TOKEN\" | beacon enroll %s --token-stdin\n", collectorURL)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Set BEACON_ENROLL_TOKEN to this one-use token:")
	fmt.Fprintf(out, "%s\n", token)
}

func writeInviteJSON(out interface{ Write([]byte) (int, error) }, collectorURL, token string, expiresAt time.Time, localTunnel bool) error {
	payload := struct {
		Schema              string `json:"schema"`
		ControlPlaneURL     string `json:"control_plane_url"`
		EnrollmentToken     string `json:"enrollment_token"`
		ExpiresAt           string `json:"expires_at"`
		RecommendedCommand  string `json:"recommended_command"`
		LocalTunnelRequired bool   `json:"local_tunnel_required"`
	}{
		Schema:              "beacon.invite.v1",
		ControlPlaneURL:     collectorURL,
		EnrollmentToken:     token,
		ExpiresAt:           expiresAt.Format(time.RFC3339),
		RecommendedCommand:  fmt.Sprintf("printf '%%s\\n' \"$BEACON_ENROLL_TOKEN\" | beacon enroll %s --token-stdin", collectorURL),
		LocalTunnelRequired: localTunnel,
	}
	return json.NewEncoder(out).Encode(payload)
}
