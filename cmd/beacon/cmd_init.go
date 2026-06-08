package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/ingest"
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
		Use:   "enroll [control-plane-url]",
		Short: "Enroll this machine with a one-use Beacon token",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("enroll accepts at most one control-plane URL")
			}
			if len(args) == 1 && !looksLikeURL(args[0]) {
				return fmt.Errorf("enroll positional argument must be a control-plane URL; use --token-stdin or --token-env for tokens")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnroll(cmd, args, tokenStdin, tokenEnv)
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
	fmt.Fprintln(out, "For this configured metadata store, pass the enrollment token through stdin or an environment variable name:")
	fmt.Fprintf(out, "  printf '%%s\\n' \"$BEACON_ENROLL_TOKEN\" | beacon enroll --token-stdin\n")
	fmt.Fprintln(out, "  beacon enroll --token-env BEACON_ENROLL_TOKEN")
	fmt.Fprintln(out, "Remote collectors can pass the control-plane URL as the enroll argument and then run beacon collect.")
	return nil
}

func runEnroll(cmd *cobra.Command, args []string, tokenStdin bool, tokenEnv string) error {
	token, err := readEnrollmentToken(cmd, tokenStdin, tokenEnv)
	if err != nil {
		return err
	}
	ctx := commandContext(cmd)
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	controlPlaneURL := cfg.Fleet.ControlPlaneURL
	if len(args) == 1 {
		controlPlaneURL = strings.TrimSpace(args[0])
	}
	if controlPlaneURL != "" {
		return runRemoteEnroll(cmd, cfg, controlPlaneURL, token)
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

func runRemoteEnroll(cmd *cobra.Command, cfg *config.Config, controlPlaneURL, token string) error {
	normalizedURL, err := config.NormalizeControlPlaneURL(controlPlaneURL, "control-plane URL")
	if err != nil {
		return err
	}
	if err := preflightRemoteEnrollmentPersistence(cfg); err != nil {
		return err
	}
	boot, hasLocalIdentity, err := remoteEnrollmentBootstrap(commandContext(cmd), cfg)
	if err != nil {
		return err
	}
	var existingIngestToken string
	if hasLocalIdentity {
		existingIngestToken, err = readIngestToken(cfg)
		if err != nil {
			return fmt.Errorf("read existing ingest token for re-enrollment: %w", err)
		}
	}
	resp, err := postRemoteEnrollment(commandContext(cmd), normalizedURL, token, existingIngestToken, enrollBootstrapFromControlPlane(boot))
	if err != nil {
		return err
	}
	store, err := controlplane.Open(cfg.Fleet.MetadataPath)
	if err != nil {
		return fmt.Errorf("open local collector metadata: %w", err)
	}
	defer store.Close()

	localBoot := controlPlaneBootstrap(cfg)
	localBoot.NodeID = resp.Assignment.NodeID
	localBoot.CollectorID = resp.Assignment.CollectorID
	snapshot, err := store.EnsureLocal(commandContext(cmd), localBoot)
	if err != nil {
		return fmt.Errorf("write local collector metadata: %w", err)
	}
	if err := verifyRemoteSourceAssignments(snapshot, resp.Assignment); err != nil {
		return err
	}
	if err := writeIngestTokenFile(cfg.Fleet.IngestTokenFile, resp.IngestToken); err != nil {
		return fmt.Errorf("write ingest token file: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Beacon remote enrollment complete\n")
	fmt.Fprintf(out, "Control plane: %s\n", normalizedURL)
	fmt.Fprintf(out, "Node: %s\n", resp.Assignment.NodeID)
	fmt.Fprintf(out, "Collector: %s\n", resp.Assignment.CollectorID)
	fmt.Fprintf(out, "Ingest token file: %s\n", cfg.Fleet.IngestTokenFile)
	fmt.Fprintf(out, "Run collector: %s\n", remoteCollectCommand(normalizedURL))
	return nil
}

func preflightRemoteEnrollmentPersistence(cfg *config.Config) error {
	if strings.TrimSpace(cfg.Fleet.IngestTokenFile) == "" {
		return fmt.Errorf("fleet.ingest_token_file is required for remote enrollment")
	}
	store, err := controlplane.Open(cfg.Fleet.MetadataPath)
	if err != nil {
		return fmt.Errorf("open local collector metadata: %w", err)
	}
	if err := store.Close(); err != nil {
		return fmt.Errorf("close local collector metadata: %w", err)
	}
	if err := preflightIngestTokenFile(cfg.Fleet.IngestTokenFile); err != nil {
		return fmt.Errorf("preflight ingest token file: %w", err)
	}
	return nil
}

func preflightIngestTokenFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("ingest token file path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".preflight.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := tmp.Write([]byte("preflight\n")); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Remove(tmpPath); err != nil {
		return err
	}
	return syncFileDir(dir)
}

func remoteEnrollmentBootstrap(ctx context.Context, cfg *config.Config) (controlplane.Bootstrap, bool, error) {
	boot := controlPlaneBootstrap(cfg)
	if !controlplane.Exists(cfg.Fleet.MetadataPath) {
		return boot, false, nil
	}
	store, err := controlplane.Open(cfg.Fleet.MetadataPath)
	if err != nil {
		return controlplane.Bootstrap{}, false, fmt.Errorf("open local collector metadata: %w", err)
	}
	defer store.Close()
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		return controlplane.Bootstrap{}, false, fmt.Errorf("read local collector metadata: %w", err)
	}
	if snapshot.LocalNodeID != "" && snapshot.LocalCollectorID != "" {
		boot.NodeID = snapshot.LocalNodeID
		boot.CollectorID = snapshot.LocalCollectorID
		return boot, true, nil
	}
	return boot, false, nil
}

func postRemoteEnrollment(ctx context.Context, controlPlaneURL, token, existingIngestToken string, boot ingest.EnrollBootstrap) (*ingest.EnrollResponse, error) {
	body, err := json.Marshal(ingest.EnrollRequest{
		Schema:              ingest.SchemaV1,
		Bootstrap:           boot,
		ExistingIngestToken: strings.TrimSpace(existingIngestToken),
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, controlPlaneURL+"/api/ingest/v1/enroll", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	httpResp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote enrollment request: %w", err)
	}
	defer httpResp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("remote enrollment failed with status %d", httpResp.StatusCode)
	}
	var resp ingest.EnrollResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode remote enrollment response: %w", err)
	}
	if strings.TrimSpace(resp.IngestToken) == "" {
		return nil, fmt.Errorf("remote enrollment response did not include an ingest token")
	}
	if strings.TrimSpace(resp.Assignment.NodeID) == "" || strings.TrimSpace(resp.Assignment.CollectorID) == "" || len(resp.Assignment.SourceIDs) == 0 || len(resp.Assignment.Sources) == 0 {
		return nil, fmt.Errorf("remote enrollment response did not include a complete assignment")
	}
	return &resp, nil
}

func enrollBootstrapFromControlPlane(boot controlplane.Bootstrap) ingest.EnrollBootstrap {
	out := ingest.EnrollBootstrap{
		NodeID:        boot.NodeID,
		NodeName:      boot.NodeName,
		CollectorID:   boot.CollectorID,
		CollectorName: boot.CollectorName,
		Sources:       make([]ingest.EnrollSourceRegistration, 0, len(boot.Sources)),
	}
	for _, source := range boot.Sources {
		out.Sources = append(out.Sources, ingest.EnrollSourceRegistration{
			Name:      source.Name,
			Runtime:   source.Runtime,
			Provider:  source.Provider,
			Format:    source.Format,
			WatchRoot: source.WatchRoot,
		})
	}
	return out
}

func verifyRemoteSourceAssignments(snapshot *controlplane.Snapshot, assignment ingest.EnrollAssignment) error {
	if snapshot == nil {
		return fmt.Errorf("local collector metadata snapshot is nil")
	}
	if len(assignment.Sources) == 0 {
		return fmt.Errorf("remote enrollment response did not include source assignments")
	}
	byName := make(map[string]string, len(assignment.Sources))
	for _, source := range assignment.Sources {
		name := strings.TrimSpace(source.Name)
		sourceID := strings.TrimSpace(source.SourceID)
		if name == "" || sourceID == "" {
			return fmt.Errorf("remote enrollment response included an incomplete source assignment")
		}
		byName[name] = sourceID
	}
	for _, source := range snapshot.Sources {
		if source.CollectorID != assignment.CollectorID {
			continue
		}
		want, ok := byName[source.Name]
		if !ok {
			return fmt.Errorf("remote enrollment response omitted source assignment for %q", source.Name)
		}
		if source.ID != want {
			return fmt.Errorf("remote source assignment mismatch for %q: local %s remote %s", source.Name, source.ID, want)
		}
		delete(byName, source.Name)
	}
	if len(byName) > 0 {
		return fmt.Errorf("remote enrollment response included unknown source assignments")
	}
	return nil
}

func remoteCollectCommand(controlPlaneURL string) string {
	parts := []string{"beacon"}
	if strings.TrimSpace(cfgFile) != "" {
		parts = append(parts, "--config", shellQuote(cfgFile))
	}
	parts = append(parts, "collect", "--control-plane-url", shellQuote(controlPlaneURL))
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= 'A' && r <= 'Z':
			return false
		case r >= '0' && r <= '9':
			return false
		case r == '/' || r == '.' || r == '_' || r == '-' || r == ':' || r == '@':
			return false
		default:
			return true
		}
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
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

func looksLikeURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}
