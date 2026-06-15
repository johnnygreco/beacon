package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type joinOptions struct {
	Name       string
	TokenStdin bool
	TokenEnv   string
	InviteFile string
	Start      bool
	Sources    string
	DryRun     bool
	Force      bool
}

type joinInviteFile struct {
	Schema          string `json:"schema"`
	ControlPlaneURL string `json:"control_plane_url"`
	EnrollmentToken string `json:"enrollment_token"`
}

func newJoinCmd() *cobra.Command {
	opts := joinOptions{}
	cmd := &cobra.Command{
		Use:   "join [control-plane-url]",
		Short: "Configure and enroll this machine as a Beacon collector",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("join accepts at most one control-plane URL")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJoin(cmd, args, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Name, "name", "", "collector/node display name")
	cmd.Flags().BoolVar(&opts.TokenStdin, "token-stdin", false, "read the enrollment token from stdin")
	cmd.Flags().StringVar(&opts.TokenEnv, "token-env", "", "read the enrollment token from the named environment variable")
	cmd.Flags().StringVar(&opts.InviteFile, "invite-file", "", "read URL and token from a JSON invite file")
	cmd.Flags().BoolVar(&opts.Start, "start", false, "run collection in the foreground after joining")
	cmd.Flags().StringVar(&opts.Sources, "sources", "", "comma-separated default source names to enable, e.g. codex,claude")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "show planned config and route checks without sending the real enrollment token")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "overwrite existing guided collector config fields")
	return cmd
}

func runJoin(cmd *cobra.Command, args []string, opts joinOptions) error {
	ctx := commandContext(cmd)
	invite, err := readJoinInviteFile(opts.InviteFile)
	if err != nil {
		return err
	}
	var normalizedURL string
	if len(args) == 1 {
		normalizedURL, err = config.NormalizeControlPlaneURL(args[0], "control-plane URL")
		if err != nil {
			return err
		}
		if invite.ControlPlaneURL != "" {
			normalizedInviteURL, err := config.NormalizeControlPlaneURL(invite.ControlPlaneURL, "invite control-plane URL")
			if err != nil {
				return err
			}
			if normalizedURL != normalizedInviteURL {
				return fmt.Errorf("join URL does not match invite file control_plane_url")
			}
		}
	} else {
		normalizedURL, err = config.NormalizeControlPlaneURL(invite.ControlPlaneURL, "control-plane URL")
		if err != nil {
			return err
		}
	}
	if err := validateJoinTokenSourceOptions(cmd, invite.EnrollmentToken, opts); err != nil {
		return err
	}
	if err := ensureJoinTargetMatchesExistingCollector(ctx, normalizedURL); err != nil {
		return err
	}
	sourceNames := normalizeJoinSources(opts.Sources)
	patch := config.GuidedConfigPatch{
		Path:                cfgFile,
		FleetRole:           config.FleetRoleCollector,
		Name:                opts.Name,
		ControlPlaneURL:     normalizedURL,
		ApplyDefaultSources: true,
		DefaultSourceNames:  sourceNames,
	}
	plan, err := config.PlanGuidedConfigPatch(patch)
	if err != nil {
		return err
	}
	if err := requireForceForGuidedConfigConflicts(plan, opts.Force); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	printGuidedConfigPlan(out, "Beacon collector join", plan)
	if config.IsLoopbackURL(normalizedURL) {
		fmt.Fprintln(out, "Control plane URL is loopback; this only works from this collector machine when a local tunnel forwards to the dashboard.")
	}
	if err := preflightEnrollmentRoute(ctx, normalizedURL, publicURLCheckOptions{}); err != nil {
		return fmt.Errorf("control-plane enrollment preflight failed before sending the enrollment token: %w; run `beacon doctor setup` for diagnostics", err)
	}
	fmt.Fprintln(out, "Enrollment route preflight: passed.")
	if opts.DryRun {
		fmt.Fprintln(out, "Dry run: config was not written and the real enrollment token was not sent.")
		return nil
	}

	token, err := readJoinEnrollmentToken(cmd, invite.EnrollmentToken, opts)
	if err != nil {
		return err
	}
	if err := plan.Write(); err != nil {
		return err
	}
	printGuidedConfigWrite(out, plan)
	cfg, err := config.Load(plan.Path)
	if err != nil {
		return fmt.Errorf("loading collector config: %w", err)
	}
	if err := runRemoteEnroll(cmd, cfg, normalizedURL, token); err != nil {
		return err
	}
	cfg, err = config.Load(plan.Path)
	if err != nil {
		return fmt.Errorf("reload collector config: %w", err)
	}
	if err := runCollectorJoinChecks(cmd, cfg); err != nil {
		return err
	}
	if opts.Start {
		fmt.Fprintln(out, "Starting collector in the foreground.")
		return runCollect(newJoinCollectCmd(cmd), false, "")
	}
	fmt.Fprintln(out, "Collector join complete.")
	fmt.Fprintln(out, "Run collector: "+remoteCollectCommand(normalizedURL))
	return nil
}

func readJoinInviteFile(path string) (joinInviteFile, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return joinInviteFile{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return joinInviteFile{}, fmt.Errorf("read invite file: %w", err)
	}
	var invite joinInviteFile
	if err := json.Unmarshal(data, &invite); err != nil {
		return joinInviteFile{}, fmt.Errorf("decode invite file: %w", err)
	}
	invite.ControlPlaneURL = strings.TrimSpace(invite.ControlPlaneURL)
	invite.EnrollmentToken = strings.TrimSpace(invite.EnrollmentToken)
	invite.Schema = strings.TrimSpace(invite.Schema)
	if invite.ControlPlaneURL == "" || invite.EnrollmentToken == "" {
		return joinInviteFile{}, fmt.Errorf("invite file must include control_plane_url and enrollment_token")
	}
	if invite.Schema != "" && invite.Schema != joinInviteSchema {
		return joinInviteFile{}, fmt.Errorf("invite file schema %q is unsupported; want %q", invite.Schema, joinInviteSchema)
	}
	return invite, nil
}

func readJoinEnrollmentToken(cmd *cobra.Command, inviteToken string, opts joinOptions) (string, error) {
	if err := validateJoinTokenSourceOptions(cmd, inviteToken, opts); err != nil {
		return "", err
	}
	inviteToken = strings.TrimSpace(inviteToken)
	if inviteToken != "" {
		return inviteToken, nil
	}
	if !opts.TokenStdin && strings.TrimSpace(opts.TokenEnv) == "" {
		return promptJoinEnrollmentToken(cmd)
	}
	return readEnrollmentToken(cmd, opts.TokenStdin, opts.TokenEnv)
}

func validateJoinTokenSourceOptions(cmd *cobra.Command, inviteToken string, opts joinOptions) error {
	sources := 0
	if strings.TrimSpace(inviteToken) != "" {
		sources++
	}
	if opts.TokenStdin {
		sources++
	}
	if strings.TrimSpace(opts.TokenEnv) != "" {
		sources++
	}
	if sources > 1 {
		return fmt.Errorf("use only one of --invite-file, --token-stdin, or --token-env")
	}
	if sources == 0 && !opts.DryRun && !canPromptJoinEnrollmentToken(cmd) {
		return joinEnrollmentTokenRequiredError()
	}
	return nil
}

func canPromptJoinEnrollmentToken(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	input, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(input.Fd()))
}

func promptJoinEnrollmentToken(cmd *cobra.Command) (string, error) {
	if cmd == nil {
		return "", joinEnrollmentTokenRequiredError()
	}
	input, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return "", joinEnrollmentTokenRequiredError()
	}
	fmt.Fprint(cmd.ErrOrStderr(), "Enrollment token: ")
	data, err := term.ReadPassword(int(input.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("read enrollment token: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("enrollment token is empty")
	}
	return token, nil
}

func joinEnrollmentTokenRequiredError() error {
	return fmt.Errorf("enrollment token must be supplied with --token-stdin, --token-env, --invite-file, or an interactive terminal")
}

func ensureJoinTargetMatchesExistingCollector(ctx context.Context, targetURL string) error {
	if cfgFile != "" {
		if _, err := os.Stat(cfgFile); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	existingCfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading existing config before join: %w", err)
	}
	_, hasLocalIdentity, err := remoteEnrollmentBootstrap(ctx, existingCfg)
	if err != nil {
		return err
	}
	if !hasLocalIdentity {
		return nil
	}
	if existingCfg.Fleet.Role != config.FleetRoleCollector {
		return nil
	}
	return requireReEnrollmentControlPlaneMatch(existingCfg.Fleet.ControlPlaneURL, targetURL)
}

func runCollectorJoinChecks(cmd *cobra.Command, cfg *config.Config) error {
	service, cleanup, err := buildCollectorService(commandContext(cmd), cfg, nil)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := service.SendHeartbeat(commandContext(cmd)); err != nil {
		return fmt.Errorf("send authenticated heartbeat: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Authenticated heartbeat: passed.")
	if err := runCollect(newJoinCollectCmd(cmd), true, ""); err != nil {
		return fmt.Errorf("collector smoke collection: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Collector smoke collection: passed.")
	return nil
}

func newJoinCollectCmd(parent *cobra.Command) *cobra.Command {
	cmd := newCollectCmd()
	cmd.SetContext(commandContext(parent))
	cmd.SetIn(parent.InOrStdin())
	cmd.SetOut(parent.OutOrStdout())
	cmd.SetErr(parent.ErrOrStderr())
	return cmd
}

func normalizeJoinSources(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
