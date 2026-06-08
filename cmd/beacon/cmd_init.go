package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var enrollTTL time.Duration
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Beacon owner and enrollment tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, enrollTTL)
		},
	}
	cmd.Flags().DurationVar(&enrollTTL, "enroll-ttl", 15*time.Minute, "duration before the generated enrollment token expires")
	return cmd
}

func newEnrollCmd() *cobra.Command {
	var tokenStdin bool
	var tokenEnv string
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Enroll this machine with a one-use Beacon token",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("enroll does not accept positional arguments; use --token-stdin or --token-env")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnroll(cmd, tokenStdin, tokenEnv)
		},
	}
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "read the enrollment token from stdin")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "read the enrollment token from the named environment variable")
	return cmd
}

func runInit(cmd *cobra.Command, enrollTTL time.Duration) error {
	if enrollTTL <= 0 {
		return fmt.Errorf("--enroll-ttl must be positive")
	}
	ctx := commandContext(cmd)
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	store, err := controlplane.Open(cfg.Fleet.MetadataPath)
	if err != nil {
		return fmt.Errorf("open control-plane metadata: %w", err)
	}
	defer store.Close()
	snapshot, err := store.EnsureLocal(ctx, controlPlaneBootstrap(cfg))
	if err != nil {
		return fmt.Errorf("initialize local control-plane metadata: %w", err)
	}

	owner, err := store.CreateToken(ctx, controlplane.CreateTokenRequest{Type: controlplane.TokenTypeOwner})
	if err != nil {
		return fmt.Errorf("create owner token: %w", err)
	}
	expiresAt := time.Now().UTC().Add(enrollTTL)
	enroll, err := store.CreateToken(ctx, controlplane.CreateTokenRequest{
		Type:      controlplane.TokenTypeEnroll,
		ExpiresAt: &expiresAt,
	})
	if err != nil {
		return fmt.Errorf("create enrollment token: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Beacon initialized at %s\n", snapshot.Path)
	fmt.Fprintf(out, "Owner token (shown once):\n%s\n\n", owner.Plaintext)
	fmt.Fprintf(out, "Enrollment token (shown once, expires %s):\n%s\n\n", expiresAt.Format(time.RFC3339), enroll.Plaintext)
	fmt.Fprintln(out, "On the collector, pass the enrollment token through stdin or an environment variable name:")
	fmt.Fprintf(out, "  printf '%%s\\n' \"$BEACON_ENROLL_TOKEN\" | beacon enroll --token-stdin\n")
	fmt.Fprintln(out, "  beacon enroll --token-env BEACON_ENROLL_TOKEN")
	return nil
}

func runEnroll(cmd *cobra.Command, tokenStdin bool, tokenEnv string) error {
	token, err := readEnrollmentToken(cmd, tokenStdin, tokenEnv)
	if err != nil {
		return err
	}
	ctx := commandContext(cmd)
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	store, err := controlplane.Open(cfg.Fleet.MetadataPath)
	if err != nil {
		return fmt.Errorf("open control-plane metadata: %w", err)
	}
	defer store.Close()

	result, err := store.CompleteEnrollment(ctx, token, controlPlaneBootstrap(cfg))
	if err != nil {
		return fmt.Errorf("complete enrollment: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Beacon enrollment complete at %s\n", result.Snapshot.Path)
	fmt.Fprintf(out, "Node: %s\n", result.IngestToken.Record.NodeID)
	fmt.Fprintf(out, "Collector: %s\n", result.IngestToken.Record.CollectorID)
	fmt.Fprintf(out, "Ingest token (shown once):\n%s\n", result.IngestToken.Plaintext)
	return nil
}

func commandContext(cmd *cobra.Command) context.Context {
	if cmd != nil && cmd.Context() != nil {
		return cmd.Context()
	}
	return context.Background()
}

func readEnrollmentToken(cmd *cobra.Command, tokenStdin bool, tokenEnv string) (string, error) {
	tokenEnv = strings.TrimSpace(tokenEnv)
	if tokenStdin && tokenEnv != "" {
		return "", fmt.Errorf("use only one of --token-stdin or --token-env")
	}
	var token string
	switch {
	case tokenStdin:
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("read enrollment token from stdin: %w", err)
		}
		token = string(data)
	case tokenEnv != "":
		token = os.Getenv(tokenEnv)
	default:
		return "", fmt.Errorf("enrollment token must be supplied with --token-stdin or --token-env")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("enrollment token is empty")
	}
	return token, nil
}
