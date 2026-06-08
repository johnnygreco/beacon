package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	beacon "github.com/johnnygreco/beacon"
	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/mcp"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/sse"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/johnnygreco/beacon/internal/web"
	"github.com/spf13/cobra"
)

func runServe(cmd *cobra.Command, args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.Fleet.Role == config.FleetRoleCollector {
		return fmt.Errorf("fleet.role %q uses beacon collect, not beacon up", config.FleetRoleCollector)
	}
	if cfg.Fleet.Role == config.FleetRoleControlPlane {
		cfg.Capture.Enabled = false
	}

	pidFile, err := acquirePIDFile()
	if err != nil {
		return fmt.Errorf("acquire beacon pidfile: %w", err)
	} else {
		defer pidFile.Close()
	}

	var sources []capture.WatchSource
	if cfg.Capture.Enabled {
		sources, err = buildSources(cfg)
		if err != nil {
			return fmt.Errorf("capture source config: %w", err)
		}
	}

	controlStore, controlSnapshot, err := initializeControlPlane(context.Background(), cfg, logger)
	if err != nil {
		return fmt.Errorf("initializing control-plane metadata: %w", err)
	}
	defer controlStore.Close()
	if err := ensureNoResetPending(controlSnapshot); err != nil {
		return err
	}
	authOptions, err := dashboardAuthOptions(context.Background(), cfg, controlStore)
	if err != nil {
		return fmt.Errorf("dashboard auth: %w", err)
	}

	storeOpts := storeOptionsFromConfig(cfg)
	if err := ensureLocalClickHouse(storeOpts); err != nil {
		return fmt.Errorf("starting clickhouse: %w", err)
	}

	logger.Info("starting beacon",
		"host", cfg.Server.Host,
		"port", cfg.Server.Port,
		"clickhouse", storeOpts.Addrs,
		"database", storeOpts.Database,
	)

	ch, err := store.Open(context.Background(), storeOpts)
	if err != nil {
		return storeOpenError("opening clickhouse store", err)
	}
	defer ch.Close()

	projectionStart := time.Now()
	if count, refreshed, err := ch.RefreshOutdatedProjections(context.Background()); err != nil {
		logger.Warn("session projection refresh failed", "error", err)
	} else if refreshed {
		logger.Info("session projections refreshed", "sessions", count, "duration", time.Since(projectionStart))
	} else {
		logger.Debug("session projections current", "duration", time.Since(projectionStart))
	}

	searchIndexStart := time.Now()
	if count, refreshed, err := ch.RefreshOutdatedSearchIndex(context.Background()); err != nil {
		logger.Warn("search index refresh failed", "error", err)
	} else if refreshed {
		logger.Info("search index refreshed", "events", count, "duration", time.Since(searchIndexStart))
	} else {
		logger.Debug("search index current", "duration", time.Since(searchIndexStart))
	}

	broker := sse.NewBroker(cfg.SSE.SubscriberBuffer, logger)
	updater := web.NewUpdater(broker, logger)
	redactionPolicy := redactionPolicyFromConfig(cfg)
	batcher := capture.NewBatcher(
		ch,
		500,
		2*time.Second,
		cfg.Pricing.DefaultInputCost,
		cfg.Pricing.DefaultOutputCost,
		updater.MarkDirty,
		logger,
		capture.WithFleetIdentity(captureFleetIdentity(controlSnapshot)),
		capture.WithRedactionPolicy(redactionPolicy),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bg := newBackgroundGroup(ctx, cancel, logger)

	var watcher *capture.Watcher
	if cfg.Capture.Enabled {
		for _, s := range sources {
			logger.Info("capture source configured", "name", s.Name, "runtime", s.Runtime, "provider", s.Provider, "globs", s.Globs)
		}
		watcher = capture.NewWatcher(
			sources,
			batcher.EventCh(),
			ch,
			logger,
			time.Duration(cfg.Capture.DebounceMs)*time.Millisecond,
			cfg.Capture.ReconcileInterval,
			cfg.Capture.BackfillOnStart,
			cfg.Capture.BackfillWorkers,
			capture.WithWatcherRedactionPolicy(redactionPolicy),
		)
	}

	searcher := search.NewSearcher(ch.DB, logger, cfg.Search.MaxResults, cfg.Search.RebuildInterval)

	// Web server
	handlers := web.NewHandlers(ch.DB, searcher, logger, cfg.Dashboard.Name)
	apiHandlers := web.NewAPIHandlers(ch.DB, searcher, logger, controlStore)
	mcpHTTPServer := mcp.NewServer(ch.DB, searcher, logger)
	mcpHTTPServer.SetDefaultContextWindow(cfg.MCP.ContextWindow)
	ingestHandlers := web.NewIngestHandlers(
		controlStore,
		ch,
		cfg.Pricing.DefaultInputCost,
		cfg.Pricing.DefaultOutputCost,
		updater.MarkDirty,
		logger,
		web.WithIngestRedactionPolicy(redactionPolicy),
	)
	staticFS, err := fs.Sub(beacon.StaticFS, "static")
	if err != nil {
		cancel()
		_ = bg.Wait()
		return fmt.Errorf("preparing static filesystem: %w", err)
	}
	routerOptions := append(authOptions, web.WithIngestHandlers(ingestHandlers), web.WithMCPHandler(mcpHTTPServer.HTTPHandler()))
	router := web.NewRouter(staticFS, broker, handlers, apiHandlers, routerOptions...)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	bg.Go("capture batcher", func(ctx context.Context) error {
		batcher.Run(ctx)
		return nil
	})
	bg.Go("dashboard updater", func(ctx context.Context) error {
		updater.Run(ctx)
		return nil
	})
	if watcher != nil {
		bg.Go("capture watcher", watcher.Run)
	}
	bg.Go("search index monitor", func(ctx context.Context) error {
		searcher.MonitorIndex(ctx)
		return nil
	})
	bg.Go("signal handler", signalCancelWorker(sigCh, cancel, logger, "shutting down..."))
	bg.Go("http shutdown", func(ctx context.Context) error {
		return shutdownHTTPServerOnContext(ctx, srv, 10*time.Second)
	})

	logger.Info("server listening", "addr", addr)
	err = srv.ListenAndServe()
	cancel()
	bgErr := bg.Wait()
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	if bgErr != nil {
		return bgErr
	}

	logger.Info("server stopped")
	return nil
}

// pidfilePath returns the path to the beacon pidfile.
func pidfilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/beacon.pid"
	}
	return filepath.Join(home, ".beacon", "beacon.pid")
}

type beaconRunLock struct {
	file *os.File
}

func acquireBeaconRunLock() (*beaconRunLock, error) {
	path := pidfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lockPath := path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("beacon pidfile is locked by another process")
		}
		return nil, err
	}
	return &beaconRunLock{file: lock}, nil
}

func (l *beaconRunLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	var err error
	if flockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); flockErr != nil {
		err = flockErr
	}
	if closeErr := l.file.Close(); err == nil {
		err = closeErr
	}
	l.file = nil
	return err
}

type pidFileLock struct {
	path    string
	runLock *beaconRunLock
}

func acquirePIDFile() (*pidFileLock, error) {
	path := pidfilePath()
	runLock, err := acquireBeaconRunLock()
	if err != nil {
		return nil, err
	}
	if existing := readPidFromFile(); existing > 0 && existing != os.Getpid() {
		_ = runLock.Close()
		return nil, fmt.Errorf("beacon process already running with pid %d", existing)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		_ = runLock.Close()
		return nil, err
	}
	return &pidFileLock{path: path, runLock: runLock}, nil
}

func (p *pidFileLock) Close() error {
	if p == nil {
		return nil
	}
	if strings.TrimSpace(p.path) != "" {
		data, err := os.ReadFile(p.path)
		if err == nil && strings.TrimSpace(string(data)) == strconv.Itoa(os.Getpid()) {
			_ = os.Remove(p.path)
		}
	}
	err := p.runLock.Close()
	p.runLock = nil
	return err
}

type captureParserKey struct {
	runtime string
	format  string
}

type captureParserBinding struct {
	lineParser func(line []byte, file string, lineNo int, offset int64) ([]capture.NormalizedEvent, error)
	fileParser func(file string) ([]capture.NormalizedEvent, error)
}

var captureParserRegistry = map[captureParserKey]captureParserBinding{
	{runtime: models.RuntimeClaudeCode, format: models.FormatJSONL}:    {lineParser: capture.ParseClaudeJSONL},
	{runtime: models.RuntimeCodex, format: models.FormatJSONL}:         {lineParser: capture.ParseCodexJSONL},
	{runtime: models.RuntimeHermesAgent, format: models.FormatSQLite}:  {fileParser: capture.ParseHermesSQLite},
	{runtime: models.RuntimeOpenCode, format: models.FormatSQLite}:     {fileParser: capture.ParseOpenCodeSQLite},
	{runtime: models.RuntimePiCodingAgent, format: models.FormatJSONL}: {fileParser: capture.ParsePiSessionFile},
}

func buildSources(cfg *config.Config) ([]capture.WatchSource, error) {
	var sources []capture.WatchSource
	for _, sc := range cfg.Capture.Sources {
		key := captureParserKey{
			runtime: strings.TrimSpace(sc.Runtime),
			format:  strings.TrimSpace(sc.Format),
		}
		binding, err := parserBindingForSource(sc.Name, key)
		if err != nil {
			return nil, err
		}
		globs := append([]string{}, sc.Globs...)
		if sc.Glob != "" {
			globs = append(globs, sc.Glob)
		}
		sources = append(sources, capture.WatchSource{
			Name:       sc.Name,
			Runtime:    key.runtime,
			Provider:   sc.Provider,
			Format:     key.format,
			Globs:      globs,
			WatchRoots: []string{sc.WatchRoot},
			Parser:     binding.lineParser,
			FileParser: binding.fileParser,
		})
	}
	return sources, nil
}

func parserBindingForSource(name string, key captureParserKey) (captureParserBinding, error) {
	if binding, ok := captureParserRegistry[key]; ok {
		return binding, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "<unnamed>"
	}
	return captureParserBinding{}, fmt.Errorf("unsupported capture source %q runtime/format %q/%q; supported runtime/format pairs: %s",
		name,
		key.runtime,
		key.format,
		supportedCaptureParserPairs(),
	)
}

func supportedCaptureParserPairs() string {
	pairs := make([]string, 0, len(captureParserRegistry))
	for key := range captureParserRegistry {
		pairs = append(pairs, key.runtime+"/"+key.format)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ", ")
}
