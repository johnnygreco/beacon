package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
)

const (
	clickHouseContainerName  = "beacon-clickhouse"
	clickHouseImage          = "clickhouse/clickhouse-server:24.12"
	dockerCompatAPIVersion   = "1.41"
	dbRuntimeAuto            = "auto"
	dbRuntimeDocker          = "docker"
	dbRuntimeNative          = "native"
	nativeClickHouseHTTPPort = 8123
)

var (
	dbResetOpenStore = store.OpenForReset
	dbResetStore     = store.Reset
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

	refreshProjectionsCmd := &cobra.Command{
		Use:   "refresh-projections",
		Short: "Rebuild derived ClickHouse projections",
		RunE:  runDBRefreshProjections,
	}

	upCmd := &cobra.Command{
		Use:   "up",
		Short: "Start local ClickHouse and migrate the schema",
		RunE:  runDBUp,
	}
	upCmd.Flags().String("image", clickHouseImage, "ClickHouse Docker image")
	upCmd.Flags().String("runtime", dbRuntimeAuto, "ClickHouse runtime: auto, native, or docker")
	upCmd.Flags().Bool("no-migrate", false, "Start ClickHouse without running schema migration")

	downCmd := &cobra.Command{
		Use:   "down",
		Short: "Stop local ClickHouse started by beacon db up",
		RunE:  runDBDown,
	}

	dbCmd.AddCommand(upCmd, downCmd, migrateCmd, refreshProjectionsCmd, resetCmd)
	return dbCmd
}

func runDBUp(cmd *cobra.Command, args []string) error {
	image, _ := cmd.Flags().GetString("image")
	runtime, _ := cmd.Flags().GetString("runtime")
	noMigrate, _ := cmd.Flags().GetBool("no-migrate")

	cfg, err := config.Load(cfgFile)
	if err != nil {
		cfg = &config.Config{}
	}
	opts := storeOptionsFromConfig(cfg)

	if !clickHouseReachable(opts) {
		if err := startClickHouse(runtime, image, opts); err != nil {
			return err
		}
	} else {
		fmt.Println("ClickHouse is already reachable.")
	}

	if err := waitForClickHouse(opts, 45*time.Second); err != nil {
		return err
	}
	if noMigrate {
		fmt.Println("ClickHouse is running.")
		return nil
	}
	ch, err := store.Open(context.Background(), opts)
	if err != nil {
		return storeOpenError("migrate failed after ClickHouse start", err)
	}
	defer ch.Close()
	fmt.Println("ClickHouse is running and Beacon schema is migrated.")
	return nil
}

func ensureLocalClickHouse(opts store.Options) error {
	if !shouldAutoStartClickHouse(opts) || clickHouseReachable(opts) {
		return nil
	}
	if err := startClickHouse(dbRuntimeAuto, clickHouseImage, opts); err != nil {
		return err
	}
	return waitForClickHouse(opts, 45*time.Second)
}

func runDBDown(cmd *cobra.Command, args []string) error {
	stopped := false
	if pid := readNativeClickHousePID(); pid > 0 {
		if err := stopNativeClickHouse(pid); err != nil {
			return err
		}
		stopped = true
	}
	if !stopped {
		if baseDir, err := nativeClickHouseDir(); err == nil {
			for _, pid := range managedNativeClickHousePIDs(baseDir) {
				if err := stopNativeClickHouse(pid); err != nil {
					return err
				}
				stopped = true
			}
		}
	}

	if _, err := exec.LookPath("docker"); err == nil && containerExists(clickHouseContainerName) {
		if out, err := docker("container", "stop", clickHouseContainerName); err != nil {
			return fmt.Errorf("stopping ClickHouse container: %w\n%s", err, out)
		}
		fmt.Println("ClickHouse container stopped.")
		stopped = true
	}

	if !stopped {
		fmt.Println("No beacon-managed ClickHouse found.")
	}
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
		return storeOpenError("migrate failed", err)
	}
	defer ch.Close()
	fmt.Println("ClickHouse schema migrated.")
	return nil
}

func runDBRefreshProjections(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		cfg = &config.Config{}
	}
	opts := storeOptionsFromConfig(cfg)
	ch, err := store.Open(context.Background(), opts)
	if err != nil {
		return storeOpenError("refresh projections failed", err)
	}
	defer ch.Close()

	count, err := ch.RefreshAllProjections(context.Background(), 0)
	if err != nil {
		return fmt.Errorf("refresh projections failed: %w", err)
	}
	fmt.Printf("Refreshed projections for %d sessions.\n", count)
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
	ch, err := dbResetOpenStore(context.Background(), opts)
	if err != nil {
		return storeOpenError("opening clickhouse store", err)
	}
	defer ch.Close()

	if err := dbResetStore(context.Background(), ch.DB, ch.Database()); err != nil {
		return fmt.Errorf("reset failed: %w", err)
	}

	fmt.Println("Database reset complete.")
	return nil
}

func storeOpenError(prefix string, err error) error {
	if hint := managedClickHouseMetadataHint(err); hint != "" {
		return fmt.Errorf("%s: %w\n\n%s", prefix, err, hint)
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func managedClickHouseMetadataHint(err error) string {
	if err == nil {
		return ""
	}
	baseDir, dirErr := nativeClickHouseDir()
	if dirErr != nil {
		return ""
	}
	errText := err.Error()
	dataDir := filepath.Join(baseDir, "data")
	if !strings.Contains(errText, "Cannot open file") ||
		!strings.Contains(errText, ".sql") ||
		(!strings.Contains(errText, dataDir) && !strings.Contains(errText, filepath.ToSlash(dataDir))) {
		return ""
	}
	return strings.Join([]string{
		"Beacon's managed ClickHouse metadata looks inconsistent. This can happen if the managed ClickHouse data directory was removed while the server was still running.",
		"To reset only Beacon's local ClickHouse data, run:",
		"  beacon db down",
		"  rm -rf ~/.beacon/clickhouse/data ~/.beacon/clickhouse/logs ~/.beacon/clickhouse/access ~/.beacon/clickhouse/clickhouse.pid",
		"  beacon up",
	}, "\n")
}

func waitForClickHouse(opts store.Options, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		for _, addr := range clickHouseAddrs(opts) {
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

func clickHouseReachable(opts store.Options) bool {
	for _, addr := range clickHouseAddrs(opts) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

func clickHouseAddrs(opts store.Options) []string {
	if len(opts.Addrs) == 0 {
		return store.DefaultOptions().Addrs
	}
	return opts.Addrs
}

func startClickHouse(runtime, image string, opts store.Options) error {
	switch runtime {
	case "", dbRuntimeAuto:
		return startClickHouseAuto(image, opts)
	case dbRuntimeNative:
		return startNativeClickHouse(opts)
	case dbRuntimeDocker:
		return startDockerClickHouse(image)
	default:
		return fmt.Errorf("unknown ClickHouse runtime %q; use auto, native, or docker", runtime)
	}
}

func startClickHouseAuto(image string, opts store.Options) error {
	if _, err := clickHouseBinary(); err == nil {
		return startNativeClickHouse(opts)
	}
	if _, err := exec.LookPath("docker"); err == nil && containerExists(clickHouseContainerName) {
		return startDockerClickHouse(image)
	}
	if _, err := exec.LookPath("docker"); err == nil {
		return startDockerClickHouse(image)
	}
	return fmt.Errorf("starting ClickHouse requires either a local clickhouse binary or Docker; rerun the Beacon installer, install ClickHouse, start ClickHouse yourself and run `beacon db migrate`, or install Docker")
}

func startDockerClickHouse(image string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker is required for --runtime docker")
	}
	if containerExists(clickHouseContainerName) {
		if out, err := docker("container", "start", clickHouseContainerName); err != nil {
			return fmt.Errorf("starting ClickHouse container: %w\n%s", err, out)
		}
		fmt.Println("ClickHouse container started.")
		return nil
	}
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
	fmt.Println("ClickHouse container created.")
	return nil
}

func startNativeClickHouse(opts store.Options) error {
	bin, err := clickHouseBinary()
	if err != nil {
		return fmt.Errorf("clickhouse binary is required for --runtime native; rerun the Beacon installer, install ClickHouse, or use --runtime docker")
	}

	baseDir, err := nativeClickHouseDir()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(baseDir, "data")
	logDir := filepath.Join(baseDir, "logs")
	accessDir := filepath.Join(baseDir, "access")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("creating ClickHouse data dir: %w", err)
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("creating ClickHouse log dir: %w", err)
	}
	if err := os.MkdirAll(accessDir, 0755); err != nil {
		return fmt.Errorf("creating ClickHouse access dir: %w", err)
	}

	host, tcpPort, err := nativeClickHouseHostPort(opts)
	if err != nil {
		return err
	}
	args := []string{
		"server",
		"--daemon",
		"--pid-file=" + nativeClickHousePIDPath(baseDir),
		"--",
		"--path=" + dataDir,
		"--logger.log=" + filepath.Join(logDir, "clickhouse.log"),
		"--logger.errorlog=" + filepath.Join(logDir, "clickhouse.err.log"),
		"--tcp_port=" + strconv.Itoa(tcpPort),
		"--http_port=" + strconv.Itoa(nativeClickHouseHTTPPort),
		"--listen_host=" + host,
		"--user_directories.local_directory.path=" + accessDir,
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("starting native ClickHouse: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	fmt.Printf("Native ClickHouse started at %s:%d (data: %s).\n", host, tcpPort, dataDir)
	return nil
}

func clickHouseBinary() (string, error) {
	if path := strings.TrimSpace(os.Getenv("BEACON_CLICKHOUSE_BIN")); path != "" {
		if isExecutableFile(path) {
			return path, nil
		}
		return "", fmt.Errorf("BEACON_CLICKHOUSE_BIN is not executable: %s", path)
	}
	if bin, err := exec.LookPath("clickhouse"); err == nil {
		return bin, nil
	}
	for _, path := range managedClickHousePaths() {
		if isExecutableFile(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("clickhouse binary not found")
}

func managedClickHousePaths() []string {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".beacon", "bin", "clickhouse"))
	}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "clickhouse"))
	}
	return paths
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}

func shouldAutoStartClickHouse(opts store.Options) bool {
	for _, addr := range clickHouseAddrs(opts) {
		if !isLocalClickHouseAddr(addr) {
			return false
		}
	}
	return true
}

func isLocalClickHouseAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func nativeClickHouseHostPort(opts store.Options) (string, int, error) {
	if len(opts.Addrs) == 0 || strings.TrimSpace(opts.Addrs[0]) == "" {
		return "127.0.0.1", 9000, nil
	}
	host, portText, err := net.SplitHostPort(opts.Addrs[0])
	if err != nil {
		return "", 0, fmt.Errorf("invalid ClickHouse address %q: %w", opts.Addrs[0], err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 {
		return "", 0, fmt.Errorf("invalid ClickHouse port %q", portText)
	}
	if strings.TrimSpace(host) == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	return host, port, nil
}

func nativeClickHouseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".beacon", "clickhouse"), nil
}

func nativeClickHousePIDPath(baseDir string) string {
	return filepath.Join(baseDir, "clickhouse.pid")
}

func readNativeClickHousePID() int {
	baseDir, err := nativeClickHouseDir()
	if err != nil {
		return 0
	}
	data, err := os.ReadFile(nativeClickHousePIDPath(baseDir))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		_ = os.Remove(nativeClickHousePIDPath(baseDir))
		return 0
	}
	return pid
}

func managedNativeClickHousePIDs(baseDir string) []int {
	dataDir := filepath.Join(baseDir, "data")
	out, err := exec.Command("ps", "-eo", "pid=,args=").Output()
	if err != nil {
		return nil
	}
	return parseManagedNativeClickHousePIDs(string(out), dataDir, os.Getpid())
}

func parseManagedNativeClickHousePIDs(psOutput, dataDir string, selfPID int) []int {
	var pids []int
	pathArg := "--path=" + dataDir
	for _, line := range strings.Split(psOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 || pid == selfPID {
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		if strings.Contains(command, "clickhouse") && strings.Contains(command, " server ") && strings.Contains(command, pathArg) {
			pids = append(pids, pid)
		}
	}
	return pids
}

func stopNativeClickHouse(pid int) error {
	fmt.Printf("Stopping native ClickHouse (pid %d)...\n", pid)
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding native ClickHouse process %d: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("stopping native ClickHouse pid %d: %w", pid, err)
	}
	if waitForExit(pid, 10*time.Second) {
		fmt.Println("Native ClickHouse stopped.")
	} else {
		fmt.Println("Native ClickHouse may still be shutting down.")
	}
	return nil
}

func containerExists(name string) bool {
	_, err := docker("container", "inspect", name)
	return err == nil
}

func docker(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	cmd.Env = dockerEnvWithAPIVersion(os.Environ(), detectDockerServerAPIVersion)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func dockerEnvWithAPIVersion(env []string, detectServerVersion func() string) []string {
	if envHasKey(env, "DOCKER_API_VERSION") {
		return env
	}
	version := strings.TrimSpace(detectServerVersion())
	if version == "" {
		version = dockerCompatAPIVersion
	}
	return append(env, "DOCKER_API_VERSION="+version)
}

func envHasKey(env []string, key string) bool {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}

func detectDockerServerAPIVersion() string {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.APIVersion}}").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
