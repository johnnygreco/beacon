package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/perf"
	"github.com/johnnygreco/beacon/internal/store"
)

const reportSchema = "beacon.performance_lab.v1"

type labConfig struct {
	OutputDir        string
	Size             string
	ClickHouse       string
	Database         string
	Port             int
	BaseURL          string
	BeaconBin        string
	LiveDatabase     string
	FastBench        string
	FastBenchtime    string
	LiveBench        string
	LiveBenchtime    string
	BrowserRepeats   int
	SkipFast         bool
	SkipLive         bool
	SkipBrowser      bool
	SkipServe        bool
	AllowUnsafeReset bool
}

type labReport struct {
	Schema       string            `json:"schema"`
	GeneratedAt  time.Time         `json:"generated_at"`
	Status       string            `json:"status"`
	GitRevision  string            `json:"git_revision"`
	GitBranch    string            `json:"git_branch"`
	Environment  environmentReport `json:"environment"`
	Dataset      datasetReport     `json:"dataset"`
	Server       serverReport      `json:"server"`
	Commands     []commandReport   `json:"commands"`
	GoBenchmarks []benchmarkReport `json:"go_benchmarks"`
	Browser      *browserLabReport `json:"browser,omitempty"`
	Artifacts    map[string]string `json:"artifacts"`
	Notes        []string          `json:"notes,omitempty"`
}

type environmentReport struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"go_version"`
	Node      string `json:"node_version,omitempty"`
	NPM       string `json:"npm_version,omitempty"`
}

type datasetReport struct {
	Size              string `json:"size"`
	Database          string `json:"database"`
	LiveBenchDatabase string `json:"live_benchmark_database,omitempty"`
	Sessions          int    `json:"sessions,omitempty"`
	Events            int    `json:"events,omitempty"`
	Payloads          int    `json:"payloads,omitempty"`
	Duration          string `json:"duration,omitempty"`
	Seeded            bool   `json:"seeded"`
	SeedError         string `json:"seed_error,omitempty"`
}

type serverReport struct {
	BaseURL      string `json:"base_url"`
	Started      bool   `json:"started"`
	Command      string `json:"command,omitempty"`
	ConfigPath   string `json:"config_path,omitempty"`
	LogPath      string `json:"log_path,omitempty"`
	HomePath     string `json:"home_path,omitempty"`
	Ready        bool   `json:"ready"`
	ReadyError   string `json:"ready_error,omitempty"`
	ExternalMode bool   `json:"external_mode"`
}

type commandReport struct {
	Name       string `json:"name"`
	Command    string `json:"command"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	ExitCode   int    `json:"exit_code"`
	OutputTail string `json:"output_tail,omitempty"`
}

type benchmarkReport struct {
	Source       string  `json:"source"`
	Package      string  `json:"package,omitempty"`
	Name         string  `json:"name"`
	Iterations   int64   `json:"iterations"`
	NSPerOp      float64 `json:"ns_per_op"`
	BytesPerOp   int64   `json:"bytes_per_op,omitempty"`
	AllocsPerOp  int64   `json:"allocs_per_op,omitempty"`
	Milliseconds float64 `json:"milliseconds_per_op"`
}

type browserLabReport struct {
	Path    string                 `json:"path"`
	Schema  string                 `json:"schema,omitempty"`
	Mode    string                 `json:"mode,omitempty"`
	BaseURL string                 `json:"base_url,omitempty"`
	Repeats int                    `json:"repeats,omitempty"`
	Summary []browserMetricSummary `json:"summary"`
}

type browserMetricSummary struct {
	Name     string  `json:"name"`
	Viewport string  `json:"viewport"`
	Unit     string  `json:"unit"`
	Samples  int     `json:"samples"`
	Min      float64 `json:"min"`
	Median   float64 `json:"median"`
	P95      float64 `json:"p95"`
	Max      float64 `json:"max"`
}

func main() {
	cfg := parseFlags()
	if err := run(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "perf lab failed: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() labConfig {
	var cfg labConfig
	flag.StringVar(&cfg.OutputDir, "output-dir", envString("PERF_LAB_OUTPUT_DIR", "test-results/perf/lab/latest"), "Report output directory")
	flag.StringVar(&cfg.Size, "size", envString("PERF_LAB_SIZE", "small"), "Dataset size: small, medium, large")
	flag.StringVar(&cfg.ClickHouse, "clickhouse", envString("PERF_LAB_CLICKHOUSE", "127.0.0.1:9000"), "ClickHouse address")
	flag.StringVar(&cfg.Database, "database", envString("PERF_LAB_DATABASE", "beacon_perf_lab"), "Disposable ClickHouse database for the served lab app")
	flag.IntVar(&cfg.Port, "port", envInt("PERF_LAB_PORT", 4611), "Port for a lab-started Beacon server")
	flag.StringVar(&cfg.BaseURL, "base-url", os.Getenv("PERF_LAB_BASE_URL"), "Already-running Beacon base URL; skips local serve when set")
	flag.StringVar(&cfg.BeaconBin, "beacon-bin", os.Getenv("PERF_LAB_BEACON_BIN"), "Beacon binary used for local serve; defaults to a temporary workspace build")
	flag.StringVar(&cfg.LiveDatabase, "live-database", os.Getenv("PERF_LAB_LIVE_DATABASE"), "Disposable ClickHouse database for live benchmarks")
	flag.StringVar(&cfg.FastBench, "fast-bench", envString("PERF_LAB_FAST_BENCH", "."), "Regex for fast non-ClickHouse Go benchmarks")
	flag.StringVar(&cfg.FastBenchtime, "fast-benchtime", envString("PERF_LAB_FAST_BENCHTIME", "100ms"), "Benchtime for fast Go benchmarks")
	flag.StringVar(&cfg.LiveBench, "live-bench", envString("PERF_LAB_LIVE_BENCH", "Benchmark(SearchBM25|SearchKeyword|SearchBrowse|MCPTool)"), "Regex for live ClickHouse benchmarks")
	flag.StringVar(&cfg.LiveBenchtime, "live-benchtime", envString("PERF_LAB_LIVE_BENCHTIME", "100ms"), "Benchtime for live ClickHouse benchmarks")
	flag.IntVar(&cfg.BrowserRepeats, "browser-repeats", envInt("PERF_LAB_BROWSER_REPEATS", 1), "Browser perf repeats per viewport")
	flag.BoolVar(&cfg.SkipFast, "skip-fast", envBool("PERF_LAB_SKIP_FAST", false), "Skip fast Go benchmarks")
	flag.BoolVar(&cfg.SkipLive, "skip-live", envBool("PERF_LAB_SKIP_LIVE", false), "Skip live ClickHouse benchmarks")
	flag.BoolVar(&cfg.SkipBrowser, "skip-browser", envBool("PERF_LAB_SKIP_BROWSER", false), "Skip browser perf flow")
	flag.BoolVar(&cfg.SkipServe, "skip-serve", envBool("PERF_LAB_SKIP_SERVE", false), "Skip seeding and starting a local Beacon server")
	flag.BoolVar(&cfg.AllowUnsafeReset, "allow-unsafe-database-reset", false, "Allow resetting a database not prefixed with beacon_perf")
	flag.Parse()
	return normalizeLabConfig(cfg)
}

func run(ctx context.Context, cfg labConfig) error {
	cfg = normalizeLabConfig(cfg)
	ctx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	jsonPath := filepath.Join(cfg.OutputDir, "perf-lab-report.json")
	mdPath := filepath.Join(cfg.OutputDir, "perf-lab-report.md")
	browserPath := filepath.Join(cfg.OutputDir, "browser-performance.json")
	report := labReport{
		Schema:      reportSchema,
		GeneratedAt: time.Now().UTC(),
		Status:      "pass",
		GitRevision: gitOutput("rev-parse", "--short", "HEAD"),
		GitBranch:   gitOutput("rev-parse", "--abbrev-ref", "HEAD"),
		Environment: collectEnvironment(ctx),
		Dataset: datasetReport{
			Size:     cfg.Size,
			Database: cfg.Database,
		},
		Artifacts: map[string]string{
			"json":     jsonPath,
			"markdown": mdPath,
			"browser":  browserPath,
		},
	}
	if cfg.BaseURL != "" {
		report.Server = serverReport{BaseURL: cfg.BaseURL, ExternalMode: true}
	}

	if err := validateLabPlan(cfg); err != nil {
		report.Status = "fail"
		return writeReportAndError(report, jsonPath, mdPath, err)
	}

	serverProcess := (*labServerProcess)(nil)
	if cfg.BaseURL == "" && !cfg.SkipServe {
		seed, err := seedPerfDatabase(ctx, cfg)
		report.Dataset = seed
		if err != nil {
			report.Status = "fail"
			report.Dataset.SeedError = err.Error()
			return writeReportAndError(report, jsonPath, mdPath, fmt.Errorf("seed lab database: %w", err))
		}

		server, cmd, err := startLabServer(ctx, cfg)
		report.Server = server
		serverProcess = cmd
		defer stopServer(serverProcess)
		if err != nil {
			report.Status = "fail"
			report.Server.ReadyError = err.Error()
			return writeReportAndError(report, jsonPath, mdPath, fmt.Errorf("start lab server: %w", err))
		}
		cfg.BaseURL = server.BaseURL
	} else {
		if cfg.BaseURL == "" {
			cfg.BaseURL = fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
		}
		report.Server = serverReport{BaseURL: cfg.BaseURL, ExternalMode: true}
		if !cfg.SkipServe {
			if err := waitForHTTP(ctx, cfg.BaseURL+"/health", 30*time.Second); err != nil {
				report.Status = "fail"
				report.Server.ReadyError = err.Error()
				return writeReportAndError(report, jsonPath, mdPath, fmt.Errorf("external server not ready: %w", err))
			}
			report.Server.Ready = true
		}
	}

	if !cfg.SkipFast {
		result := runGoBenchmarks(ctx, "fast-go-benchmarks", cfg.FastBench, cfg.FastBenchtime, fastBenchmarkPackages(), nil)
		report.Commands = append(report.Commands, result.Command)
		report.GoBenchmarks = append(report.GoBenchmarks, parseBenchmarks(result.Output, "fast")...)
		if result.Err != nil {
			report.Status = "fail"
		}
	}

	if !cfg.SkipLive {
		env := []string{"BEACON_TEST_CLICKHOUSE=" + cfg.ClickHouse, "BEACON_PERF_DATABASE=" + cfg.LiveDatabase, "PERF_SIZE=" + cfg.Size}
		result := runGoBenchmarks(ctx, "live-clickhouse-benchmarks", cfg.LiveBench, cfg.LiveBenchtime, []string{"./internal/perf"}, env)
		report.Commands = append(report.Commands, result.Command)
		report.GoBenchmarks = append(report.GoBenchmarks, parseBenchmarks(result.Output, "live")...)
		if result.Err != nil {
			report.Status = "fail"
		}
	}

	if !cfg.SkipBrowser {
		if err := removeStaleBrowserReport(browserPath); err != nil {
			report.Status = "fail"
			report.Notes = append(report.Notes, "browser report could not be prepared: "+err.Error())
		} else {
			result := runBrowserPerf(ctx, cfg, browserPath)
			report.Commands = append(report.Commands, result.Command)
			if result.Err != nil {
				report.Status = "fail"
				report.Notes = append(report.Notes, "browser command failed; browser metrics omitted")
			} else {
				browser, err := readBrowserReport(browserPath)
				if err != nil {
					report.Status = "fail"
					report.Notes = append(report.Notes, "browser report was not readable: "+err.Error())
				} else {
					report.Browser = browser
				}
			}
		}
	}

	if err := writeReport(report, jsonPath, mdPath); err != nil {
		return err
	}
	fmt.Printf("Beacon perf lab report: %s\n", mdPath)
	if report.Status != "pass" {
		return errors.New("one or more perf lab steps failed")
	}
	return nil
}

func normalizeLabConfig(cfg labConfig) labConfig {
	cfg.LiveDatabase = strings.TrimSpace(cfg.LiveDatabase)
	if cfg.LiveDatabase == "" {
		cfg.LiveDatabase = defaultLiveBenchmarkDatabase(cfg.Database)
	}
	return cfg
}

func validateLabPlan(cfg labConfig) error {
	if cfg.SkipLive {
		return nil
	}
	if cfg.BaseURL != "" {
		return errors.New("live benchmarks are disabled for --base-url runs; use --skip-live because the external server database cannot be verified")
	}
	if err := validateLiveBenchmarkDatabaseName(cfg.LiveDatabase); err != nil {
		return fmt.Errorf("live benchmark database: %w", err)
	}
	if cfg.LiveDatabase == cfg.Database {
		return fmt.Errorf("live benchmark database %q must differ from served database; use --live-database %s or --skip-live", cfg.LiveDatabase, defaultLiveBenchmarkDatabase(cfg.Database))
	}
	return nil
}

type commandRun struct {
	Command commandReport
	Output  string
	Err     error
}

func runGoBenchmarks(ctx context.Context, name, bench, benchtime string, packages, extraEnv []string) commandRun {
	args := []string{"test", "-run", "^$", "-bench", bench, "-benchtime", benchtime, "-benchmem", "-count", "1", "-timeout", "10m"}
	args = append(args, packages...)
	return runCommand(ctx, name, "go", args, extraEnv)
}

func runBrowserPerf(ctx context.Context, cfg labConfig, outputPath string) commandRun {
	env := []string{
		"BEACON_BROWSER_PERF_FIXTURES=0",
		"BEACON_E2E_BASE_URL=" + cfg.BaseURL,
		"BEACON_BROWSER_PERF_REPEATS=" + strconv.Itoa(cfg.BrowserRepeats),
		"BEACON_BROWSER_PERF_OUTPUT=" + outputPath,
		"BEACON_BROWSER_PERF_SEARCH_QUERY=database",
		"BEACON_BROWSER_PERF_EVENT_QUERY=binary search",
	}
	return runCommand(ctx, "browser-performance", "npm", []string{"run", "test:perf:browser"}, env)
}

func runCommand(ctx context.Context, name, executable string, args, extraEnv []string) commandRun {
	start := time.Now()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	exitCode := 0
	status := "pass"
	if err != nil {
		status = "fail"
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return commandRun{
		Command: commandReport{
			Name:       name,
			Command:    shellCommand(executable, args),
			Status:     status,
			DurationMS: time.Since(start).Milliseconds(),
			ExitCode:   exitCode,
			OutputTail: tailString(string(out), 20000),
		},
		Output: string(out),
		Err:    err,
	}
}

func fastBenchmarkPackages() []string {
	return []string{
		"./internal/capture",
		"./internal/store",
		"./internal/textindex",
		"./internal/mcp",
		"./internal/web",
		"./internal/views/pages",
		"./internal/views/components",
	}
}

func seedPerfDatabase(ctx context.Context, cfg labConfig) (datasetReport, error) {
	report := datasetReport{Size: cfg.Size, Database: cfg.Database}
	if !cfg.SkipLive {
		report.LiveBenchDatabase = cfg.LiveDatabase
	}
	if err := validateLabDatabaseName(cfg.Database, cfg.AllowUnsafeReset); err != nil {
		return report, err
	}

	start := time.Now()
	opts := store.Options{Addrs: []string{cfg.ClickHouse}, Database: cfg.Database, ReadPoolSize: 4}
	resetter, err := store.OpenForReset(ctx, opts)
	if err != nil {
		return report, err
	}
	if err := store.Reset(ctx, resetter.DB, resetter.Database()); err != nil {
		resetter.Close()
		return report, err
	}
	resetter.Close()

	ch, err := store.Open(ctx, opts)
	if err != nil {
		return report, err
	}
	defer ch.Close()
	stats, err := perf.Seed(ctx, ch, perf.ParseSeedSize(cfg.Size))
	if err != nil {
		return report, err
	}
	report.Seeded = true
	report.Sessions = stats.Sessions
	report.Events = stats.Events
	report.Payloads = stats.Payloads
	report.Duration = time.Since(start).Truncate(time.Millisecond).String()
	return report, nil
}

type labServerProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func startLabServer(ctx context.Context, cfg labConfig) (serverReport, *labServerProcess, error) {
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
	configPath := filepath.Join(cfg.OutputDir, "beacon-perf-lab.toml")
	logPath := filepath.Join(cfg.OutputDir, "beacon-server.log")
	if err := ensurePortAvailable(cfg.Port); err != nil {
		return serverReport{BaseURL: baseURL, ConfigPath: configPath, LogPath: logPath}, nil, err
	}
	if err := writeLabConfig(configPath, cfg); err != nil {
		return serverReport{BaseURL: baseURL, ConfigPath: configPath, LogPath: logPath}, nil, err
	}

	cmd, commandText, err := beaconServerCommand(ctx, cfg, configPath)
	if err != nil {
		return serverReport{BaseURL: baseURL, ConfigPath: configPath, LogPath: logPath}, nil, err
	}
	homePath, err := prepareLabServerHome(cfg.OutputDir)
	if err != nil {
		return serverReport{BaseURL: baseURL, ConfigPath: configPath, LogPath: logPath}, nil, err
	}
	cmd.Env = labServerEnv(os.Environ(), homePath)
	logFile, err := os.Create(logPath)
	if err != nil {
		return serverReport{BaseURL: baseURL, ConfigPath: configPath, LogPath: logPath, HomePath: homePath}, nil, err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return serverReport{BaseURL: baseURL, ConfigPath: configPath, LogPath: logPath, HomePath: homePath, Command: commandText}, nil, err
	}
	proc := &labServerProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		proc.err = cmd.Wait()
		_ = logFile.Close()
		close(proc.done)
	}()

	report := serverReport{BaseURL: baseURL, Started: true, Command: commandText, ConfigPath: configPath, LogPath: logPath, HomePath: homePath}
	if err := waitForLabServerReady(ctx, baseURL+"/health", 45*time.Second, proc); err != nil {
		report.ReadyError = err.Error()
		return report, proc, err
	}
	report.Ready = true
	return report, proc, nil
}

func beaconServerCommand(ctx context.Context, cfg labConfig, configPath string) (*exec.Cmd, string, error) {
	if beaconBin := strings.TrimSpace(cfg.BeaconBin); beaconBin != "" {
		args := []string{"--config", configPath, "up"}
		return exec.Command(beaconBin, args...), shellCommand(beaconBin, args), nil
	}
	beaconBin, err := buildLabBeaconBinary(ctx, cfg.OutputDir)
	if err != nil {
		return nil, "", err
	}
	args := []string{"--config", configPath, "up"}
	return exec.Command(beaconBin, args...), shellCommand(beaconBin, args), nil
}

func buildLabBeaconBinary(ctx context.Context, outputDir string) (string, error) {
	beaconBin := filepath.Join(outputDir, "beacon-perf-lab-server")
	if runtime.GOOS == "windows" {
		beaconBin += ".exe"
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-o", beaconBin, "./cmd/beacon")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build lab beacon binary: %w\n%s", err, tailString(string(out), 4000))
	}
	return beaconBin, nil
}

func prepareLabServerHome(outputDir string) (string, error) {
	homePath := filepath.Join(outputDir, "beacon-home")
	if err := os.MkdirAll(filepath.Join(homePath, ".beacon"), 0755); err != nil {
		return "", fmt.Errorf("prepare lab server home: %w", err)
	}
	return homePath, nil
}

func labServerEnv(base []string, homePath string) []string {
	env := make([]string, 0, len(base)+2)
	for _, entry := range base {
		if strings.HasPrefix(entry, "HOME=") || strings.HasPrefix(entry, "USERPROFILE=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "HOME="+homePath, "USERPROFILE="+homePath)
	return env
}

func writeLabConfig(path string, cfg labConfig) error {
	body := fmt.Sprintf(`[server]
host = "127.0.0.1"
port = %d

[database]
addrs = ["%s"]
database = "%s"
read_pool_size = 4

[capture]
enabled = false
backfill_on_start = false

[search]
rebuild_interval = "1h"

[dashboard]
name = "Beacon Perf Lab"
`, cfg.Port, cfg.ClickHouse, cfg.Database)
	return os.WriteFile(path, []byte(body), 0644)
}

func stopServer(proc *labServerProcess) {
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return
	}
	_ = proc.cmd.Process.Signal(os.Interrupt)
	select {
	case <-proc.done:
	case <-time.After(3 * time.Second):
		_ = proc.cmd.Process.Kill()
		<-proc.done
	}
}

func waitForHTTP(ctx context.Context, url string, timeout time.Duration) error {
	return waitForHTTPReady(ctx, url, timeout, nil)
}

func waitForLabServerReady(ctx context.Context, url string, timeout time.Duration, proc *labServerProcess) error {
	return waitForHTTPReady(ctx, url, timeout, proc)
}

func waitForHTTPReady(ctx context.Context, url string, timeout time.Duration, proc *labServerProcess) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	client := http.Client{Timeout: 750 * time.Millisecond}
	for time.Now().Before(deadline) {
		if proc != nil {
			if err, exited := proc.exited(); exited {
				return labServerExitedError(err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if proc != nil {
					select {
					case <-proc.done:
						return labServerExitedError(proc.err)
					case <-time.After(100 * time.Millisecond):
					}
					if err, exited := proc.exited(); exited {
						return labServerExitedError(err)
					}
				}
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("%s did not become ready within %s: %w", url, timeout, lastErr)
}

func (proc *labServerProcess) exited() (error, bool) {
	select {
	case <-proc.done:
		return proc.err, true
	default:
		return nil, false
	}
}

func labServerExitedError(err error) error {
	if err == nil {
		return errors.New("lab server exited before readiness")
	}
	return fmt.Errorf("lab server exited before readiness: %w", err)
}

var validLabDatabase = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var safeLabDatabase = regexp.MustCompile(`^beacon_perf[A-Za-z0-9_]*$`)

func defaultLiveBenchmarkDatabase(database string) string {
	if safeLabDatabase.MatchString(database) {
		return database + "_bench"
	}
	return "beacon_perf_lab_bench"
}

func validateLabDatabaseName(database string, allowUnsafe bool) error {
	if !validLabDatabase.MatchString(database) {
		return fmt.Errorf("refusing to reset invalid database name %q; use an identifier containing only letters, numbers, and underscores", database)
	}
	if !allowUnsafe && !safeLabDatabase.MatchString(database) {
		return fmt.Errorf("refusing to reset database %q; use a beacon_perf* database or --allow-unsafe-database-reset", database)
	}
	return nil
}

func validateLiveBenchmarkDatabaseName(database string) error {
	if !validLabDatabase.MatchString(database) {
		return fmt.Errorf("refusing to reset invalid database name %q; use an identifier containing only letters, numbers, and underscores", database)
	}
	if !safeLabDatabase.MatchString(database) {
		return fmt.Errorf("refusing to reset live benchmark database %q; use a beacon_perf* database or --skip-live", database)
	}
	return nil
}

func ensurePortAvailable(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("lab port %d is invalid", port)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("lab port %d is not available; stop the process using it or pass --base-url for an existing Beacon server: %w", port, err)
	}
	return ln.Close()
}

var benchmarkLinePattern = regexp.MustCompile(`^(Benchmark\S+)\s+(\d+)\s+([0-9.]+) ns/op(?:\s+([0-9]+) B/op)?(?:\s+([0-9]+) allocs/op)?`)

func parseBenchmarks(output, source string) []benchmarkReport {
	var results []benchmarkReport
	pkg := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "pkg: ") {
			pkg = strings.TrimSpace(strings.TrimPrefix(line, "pkg: "))
			continue
		}
		m := benchmarkLinePattern.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		ns, _ := strconv.ParseFloat(m[3], 64)
		iterations, _ := strconv.ParseInt(m[2], 10, 64)
		bytesPerOp := parseInt64(m[4])
		allocsPerOp := parseInt64(m[5])
		results = append(results, benchmarkReport{
			Source:       source,
			Package:      pkg,
			Name:         trimBenchmarkCPU(m[1]),
			Iterations:   iterations,
			NSPerOp:      ns,
			BytesPerOp:   bytesPerOp,
			AllocsPerOp:  allocsPerOp,
			Milliseconds: round(ns / 1_000_000),
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Source != results[j].Source {
			return results[i].Source < results[j].Source
		}
		return results[i].Name < results[j].Name
	})
	return results
}

func trimBenchmarkCPU(name string) string {
	if idx := strings.LastIndex(name, "-"); idx >= 0 {
		if _, err := strconv.Atoi(name[idx+1:]); err == nil {
			return name[:idx]
		}
	}
	return name
}

func readBrowserReport(path string) (*browserLabReport, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Schema  string                 `json:"schema"`
		Mode    string                 `json:"mode"`
		BaseURL string                 `json:"base_url"`
		Repeats int                    `json:"repeats"`
		Summary []browserMetricSummary `json:"summary"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return &browserLabReport{
		Path:    path,
		Schema:  raw.Schema,
		Mode:    raw.Mode,
		BaseURL: raw.BaseURL,
		Repeats: raw.Repeats,
		Summary: raw.Summary,
	}, nil
}

func removeStaleBrowserReport(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func writeReportAndError(report labReport, jsonPath, mdPath string, err error) error {
	if writeErr := writeReport(report, jsonPath, mdPath); writeErr != nil {
		return writeErr
	}
	return err
}

func writeReport(report labReport, jsonPath, mdPath string) error {
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(body, '\n'), 0644); err != nil {
		return err
	}
	return os.WriteFile(mdPath, []byte(markdownReport(report)), 0644)
}

func markdownReport(report labReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Beacon Performance Lab\n\n")
	fmt.Fprintf(&b, "- Status: %s\n", report.Status)
	fmt.Fprintf(&b, "- Generated: %s\n", report.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Git: %s (%s)\n", report.GitRevision, report.GitBranch)
	fmt.Fprintf(&b, "- Dataset: %s in `%s`", report.Dataset.Size, report.Dataset.Database)
	if report.Dataset.Seeded {
		fmt.Fprintf(&b, " (%d sessions, %d events, %d payloads, seed %s)", report.Dataset.Sessions, report.Dataset.Events, report.Dataset.Payloads, report.Dataset.Duration)
	}
	fmt.Fprintf(&b, "\n")
	if report.Dataset.LiveBenchDatabase != "" {
		fmt.Fprintf(&b, "- Live benchmark database: `%s`\n", report.Dataset.LiveBenchDatabase)
	}
	fmt.Fprintf(&b, "- Server: %s", report.Server.BaseURL)
	if report.Server.Started {
		fmt.Fprintf(&b, " (started locally)")
	} else if report.Server.ExternalMode {
		fmt.Fprintf(&b, " (external)")
	}
	fmt.Fprintf(&b, "\n\n")

	fmt.Fprintf(&b, "## Commands\n\n")
	fmt.Fprintf(&b, "| Name | Status | Duration | Exit |\n| --- | --- | ---: | ---: |\n")
	for _, cmd := range report.Commands {
		fmt.Fprintf(&b, "| %s | %s | %.2fs | %d |\n", cmd.Name, cmd.Status, float64(cmd.DurationMS)/1000, cmd.ExitCode)
	}

	if len(report.GoBenchmarks) > 0 {
		fmt.Fprintf(&b, "\n## Go Benchmarks\n\n")
		fmt.Fprintf(&b, "| Source | Benchmark | Iterations | ms/op | B/op | allocs/op |\n| --- | --- | ---: | ---: | ---: | ---: |\n")
		for _, bm := range report.GoBenchmarks {
			fmt.Fprintf(&b, "| %s | `%s` | %d | %.3f | %d | %d |\n", bm.Source, bm.Name, bm.Iterations, bm.Milliseconds, bm.BytesPerOp, bm.AllocsPerOp)
		}
	}

	if report.Browser != nil && len(report.Browser.Summary) > 0 {
		fmt.Fprintf(&b, "\n## Browser Summary\n\n")
		fmt.Fprintf(&b, "| Metric | Viewport | Samples | Median | P95 | Max | Unit |\n| --- | --- | ---: | ---: | ---: | ---: | --- |\n")
		for _, metric := range report.Browser.Summary {
			fmt.Fprintf(&b, "| `%s` | %s | %d | %.2f | %.2f | %.2f | %s |\n", metric.Name, metric.Viewport, metric.Samples, metric.Median, metric.P95, metric.Max, metric.Unit)
		}
	}

	if len(report.Notes) > 0 {
		fmt.Fprintf(&b, "\n## Notes\n\n")
		for _, note := range report.Notes {
			fmt.Fprintf(&b, "- %s\n", note)
		}
	}
	return b.String()
}

func collectEnvironment(ctx context.Context) environmentReport {
	return environmentReport{
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
		Node:      commandVersion(ctx, "node", "--version"),
		NPM:       commandVersion(ctx, "npm", "--version"),
	}
}

func commandVersion(ctx context.Context, executable string, args ...string) string {
	cmd := exec.CommandContext(ctx, executable, args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitOutput(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseInt64(value string) int64 {
	if value == "" {
		return 0
	}
	n, _ := strconv.ParseInt(value, 10, 64)
	return n
}

func tailString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[len(value)-max:]
}

func shellCommand(executable string, args []string) string {
	parts := append([]string{executable}, args...)
	return strings.Join(parts, " ")
}

func round(value float64) float64 {
	return float64(int(value*1000+0.5)) / 1000
}
