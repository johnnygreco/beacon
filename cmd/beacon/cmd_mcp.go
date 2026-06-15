package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/mcp"
	"github.com/spf13/cobra"
)

var mcpClickHouseAddr string
var mcpRemoteURL string
var mcpReadTokenFile string
var mcpReadTokenEnv string

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server (stdin/stdout JSON-RPC)",
		RunE:  runMCP,
	}
	cmd.Flags().StringVar(&mcpClickHouseAddr, "clickhouse", "", "ClickHouse address (overrides config)")
	cmd.Flags().StringVar(&mcpRemoteURL, "remote-url", "", "Remote Beacon control-plane MCP URL (or BEACON_MCP_URL)")
	cmd.Flags().StringVar(&mcpReadTokenFile, "read-token-file", "", "File containing a Beacon read token for --remote-url")
	cmd.Flags().StringVar(&mcpReadTokenEnv, "read-token-env", "BEACON_READ_TOKEN", "Environment variable containing a Beacon read token for --remote-url")
	return cmd
}

func runMCP(cmd *cobra.Command, args []string) error {
	// All logging to stderr (stdout is the transport)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	remoteURL := firstNonEmptyString(mcpRemoteURL, os.Getenv("BEACON_MCP_URL"))
	if remoteURL != "" {
		endpoint, err := normalizeRemoteMCPEndpoint(remoteURL)
		if err != nil {
			return err
		}
		token, err := readMCPReadToken(mcpReadTokenEnv, mcpReadTokenFile)
		if err != nil {
			return err
		}
		proxy := mcp.NewRemoteProxy(endpoint, token)
		return runMCPServerLifecycle(ctxWithSignals(logger, "shutting down remote mcp..."), func(ctx context.Context) error {
			return proxy.Serve(ctx, os.Stdin, os.Stdout)
		})
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	opts := storeOptionsFromConfig(cfg)
	if mcpClickHouseAddr != "" {
		opts.Addrs = []string{mcpClickHouseAddr}
	}

	maxResults := cfg.MCP.MaxResults
	if maxResults <= 0 {
		maxResults = 25
	}

	backend := mcp.NewClickHouseBackend(opts, logger, maxResults)
	defer backend.Close()

	server := mcp.NewServerWithBackend(backend, logger)
	server.SetDefaultContextWindow(cfg.MCP.ContextWindow)

	return runMCPServerLifecycle(ctxWithSignals(logger, "shutting down mcp..."), server.Run)
}

type mcpLifecycleContext struct {
	ctx    context.Context
	cancel context.CancelFunc
	bg     *backgroundGroup
	sigCh  chan os.Signal
}

func ctxWithSignals(logger *slog.Logger, message string) mcpLifecycleContext {
	ctx, cancel := context.WithCancel(context.Background())
	bg := newBackgroundGroup(ctx, cancel, logger)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	bg.Go("signal handler", signalCancelWorker(sigCh, cancel, logger, message))
	return mcpLifecycleContext{ctx: ctx, cancel: cancel, bg: bg, sigCh: sigCh}
}

func runMCPServerLifecycle(lifecycle mcpLifecycleContext, run func(context.Context) error) error {
	defer signal.Stop(lifecycle.sigCh)
	defer lifecycle.cancel()
	err := run(lifecycle.ctx)
	lifecycle.cancel()
	bgErr := lifecycle.bg.Wait()
	return commandLifecycleError(err, bgErr)
}

func normalizeRemoteMCPEndpoint(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("remote MCP URL must be an absolute URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("remote MCP URL must use http or https")
	}
	if u.Scheme == "http" && !config.IsLoopbackURLHost(u.Host) {
		return "", fmt.Errorf("remote MCP URL must use https for non-loopback hosts")
	}
	if strings.TrimRight(u.Path, "/") == "" {
		u.Path = "/api/mcp"
	}
	return u.String(), nil
}

func readMCPReadToken(envName, filePath string) (string, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		envName = "BEACON_READ_TOKEN"
	}
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("read MCP token file: %w", err)
		}
		if token := strings.TrimSpace(string(data)); token != "" {
			return token, nil
		}
		return "", fmt.Errorf("MCP token file is empty")
	}
	if token := strings.TrimSpace(os.Getenv(envName)); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("remote MCP read token is required; set %s or pass --read-token-file", envName)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
