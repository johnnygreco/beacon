package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
)

func TestDockerEnvWithAPIVersionUsesDetectedServerVersion(t *testing.T) {
	env := dockerEnvWithAPIVersion([]string{"PATH=/bin"}, func() string { return "1.47" })

	if !slices.Contains(env, "DOCKER_API_VERSION=1.47") {
		t.Fatalf("expected detected Docker API version in env, got %#v", env)
	}
}

func TestDockerEnvWithAPIVersionRespectsExistingEnv(t *testing.T) {
	env := dockerEnvWithAPIVersion([]string{"DOCKER_API_VERSION=1.43"}, func() string {
		t.Fatal("detectServerVersion should not be called when DOCKER_API_VERSION is set")
		return ""
	})

	if len(env) != 1 || env[0] != "DOCKER_API_VERSION=1.43" {
		t.Fatalf("expected existing env to be preserved, got %#v", env)
	}
}

func TestDockerEnvWithAPIVersionFallsBackToCompatVersion(t *testing.T) {
	env := dockerEnvWithAPIVersion(nil, func() string { return "" })

	if !slices.Contains(env, "DOCKER_API_VERSION="+dockerCompatAPIVersion) {
		t.Fatalf("expected fallback Docker API version in env, got %#v", env)
	}
}

func TestNativeClickHouseHostPort(t *testing.T) {
	tests := []struct {
		name     string
		addrs    []string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{name: "defaults", wantHost: "127.0.0.1", wantPort: 9000},
		{name: "localhost", addrs: []string{"localhost:19000"}, wantHost: "127.0.0.1", wantPort: 19000},
		{name: "custom host", addrs: []string{"0.0.0.0:9001"}, wantHost: "0.0.0.0", wantPort: 9001},
		{name: "invalid", addrs: []string{"127.0.0.1"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := nativeClickHouseHostPort(store.Options{Addrs: tt.addrs})
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if host != tt.wantHost || port != tt.wantPort {
				t.Fatalf("got %s:%d, want %s:%d", host, port, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestClickHouseBinaryUsesEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "clickhouse")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write clickhouse fixture: %v", err)
	}
	t.Setenv("BEACON_CLICKHOUSE_BIN", bin)
	t.Setenv("PATH", filepath.Join(tmp, "empty"))

	got, err := clickHouseBinary()
	if err != nil {
		t.Fatalf("clickHouseBinary returned error: %v", err)
	}
	if got != bin {
		t.Fatalf("got %q, want %q", got, bin)
	}
}

func TestClickHouseBinaryUsesManagedInstall(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".beacon", "bin", "clickhouse")
	if err := os.MkdirAll(filepath.Dir(bin), 0755); err != nil {
		t.Fatalf("create managed bin dir: %v", err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write managed clickhouse fixture: %v", err)
	}
	t.Setenv("BEACON_CLICKHOUSE_BIN", "")
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "empty"))

	got, err := clickHouseBinary()
	if err != nil {
		t.Fatalf("clickHouseBinary returned error: %v", err)
	}
	if got != bin {
		t.Fatalf("got %q, want %q", got, bin)
	}
}

func TestShouldAutoStartClickHouse(t *testing.T) {
	tests := []struct {
		name  string
		addrs []string
		want  bool
	}{
		{name: "empty defaults to local", want: true},
		{name: "loopback", addrs: []string{"127.0.0.1:9000"}, want: true},
		{name: "localhost", addrs: []string{"localhost:9000"}, want: true},
		{name: "unspecified", addrs: []string{"0.0.0.0:9000"}, want: true},
		{name: "ipv6 loopback", addrs: []string{"[::1]:9000"}, want: true},
		{name: "remote host", addrs: []string{"clickhouse.example.com:9000"}, want: false},
		{name: "remote ip", addrs: []string{"10.0.0.7:9000"}, want: false},
		{name: "invalid", addrs: []string{"127.0.0.1"}, want: false},
		{name: "mixed", addrs: []string{"127.0.0.1:9000", "clickhouse.example.com:9000"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAutoStartClickHouse(store.Options{Addrs: tt.addrs})
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDBCommandExposesExpectedCommandsAndFlags(t *testing.T) {
	cmd := newDBCmd()
	var names []string
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	for _, want := range []string{"up", "down", "migrate", "refresh-projections", "reset"} {
		if !slices.Contains(names, want) {
			t.Fatalf("db commands = %v, want %s", names, want)
		}
	}

	reset := dbSubcommand(t, cmd, "reset")
	if reset.Flags().Lookup("force") == nil {
		t.Fatal("db reset is missing --force")
	}

	up := dbSubcommand(t, cmd, "up")
	for _, flag := range []string{"image", "runtime", "no-migrate"} {
		if up.Flags().Lookup(flag) == nil {
			t.Fatalf("db up is missing --%s", flag)
		}
	}
}

func TestClickHouseBinaryRejectsNonExecutableEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "clickhouse")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0644); err != nil {
		t.Fatalf("write clickhouse fixture: %v", err)
	}
	t.Setenv("BEACON_CLICKHOUSE_BIN", bin)

	_, err := clickHouseBinary()
	if err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("clickHouseBinary error = %v, want not executable", err)
	}
}

func TestStartClickHouseRejectsUnknownRuntime(t *testing.T) {
	err := startClickHouse("podman", clickHouseImage, store.Options{})
	if err == nil || !strings.Contains(err.Error(), "unknown ClickHouse runtime") {
		t.Fatalf("startClickHouse error = %v, want unknown runtime", err)
	}
}

func TestStartClickHouseAutoErrorsWithoutNativeOrDockerRuntime(t *testing.T) {
	tmp := t.TempDir()
	pathDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(pathDir, 0755); err != nil {
		t.Fatalf("create PATH fixture: %v", err)
	}
	t.Setenv("BEACON_CLICKHOUSE_BIN", filepath.Join(tmp, "missing-clickhouse"))
	t.Setenv("PATH", pathDir)

	err := startClickHouseAuto(clickHouseImage, store.Options{})
	if err == nil || !strings.Contains(err.Error(), "requires either a local clickhouse binary or Docker") {
		t.Fatalf("startClickHouseAuto error = %v, want missing runtime guidance", err)
	}
}

func TestStartDockerClickHouseBindsPortsToLoopback(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	logPath := filepath.Join(tmp, "docker.log")
	dockerPath := filepath.Join(binDir, "docker")
	script := `#!/bin/sh
echo "$@" >> "$BEACON_DOCKER_LOG"
if [ "$1" = "version" ]; then
  echo "1.47"
  exit 0
fi
if [ "$1" = "container" ] && [ "$2" = "inspect" ]; then
  exit 1
fi
exit 0
`
	if err := os.WriteFile(dockerPath, []byte(script), 0755); err != nil {
		t.Fatalf("write docker fixture: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("BEACON_DOCKER_LOG", logPath)

	if err := startDockerClickHouse("clickhouse:test"); err != nil {
		t.Fatalf("startDockerClickHouse returned error: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	log := string(data)
	for _, want := range []string{"-p 127.0.0.1:9000:9000", "-p 127.0.0.1:8123:8123"} {
		if !strings.Contains(log, want) {
			t.Fatalf("docker log = %q, want %q", log, want)
		}
	}
	if strings.Contains(log, "-p 9000:9000") || strings.Contains(log, "-p 8123:8123") {
		t.Fatalf("docker run uses broad port binding: %q", log)
	}
}

func TestUnsafeDockerClickHouseBindings(t *testing.T) {
	tests := []struct {
		name      string
		portsJSON string
		want      []string
	}{
		{
			name: "loopback bindings",
			portsJSON: `{
				"9000/tcp":[{"HostIp":"127.0.0.1","HostPort":"9000"}],
				"8123/tcp":[{"HostIp":"::1","HostPort":"8123"}]
			}`,
		},
		{
			name: "broad bindings",
			portsJSON: `{
				"9000/tcp":[{"HostIp":"0.0.0.0","HostPort":"9000"}],
				"8123/tcp":[{"HostIp":"","HostPort":"8123"}]
			}`,
			want: []string{"0.0.0.0:9000->9000/tcp", "0.0.0.0:8123->8123/tcp"},
		},
		{
			name:      "invalid json",
			portsJSON: `{`,
			want:      []string{"unknown ports"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unsafeDockerClickHouseBindings(tt.portsJSON)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("unsafeDockerClickHouseBindings() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadNativeClickHousePIDRemovesStalePIDFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	baseDir := filepath.Join(home, ".beacon", "clickhouse")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatalf("create native ClickHouse dir: %v", err)
	}
	pidPath := nativeClickHousePIDPath(baseDir)
	if err := os.WriteFile(pidPath, []byte("999999999"), 0644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}

	if got := readNativeClickHousePID(); got != 0 {
		t.Fatalf("readNativeClickHousePID() = %d, want 0", got)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pidfile still exists; stat error = %v", err)
	}
}

func TestRunDBResetAbortsWhenConfirmationDeclined(t *testing.T) {
	resetConfigState(t)
	setStdin(t, "n\n")
	stubDBResetStore(t,
		func(context.Context, store.Options) (*store.Store, error) {
			t.Fatal("db reset opened store after declined confirmation")
			return nil, nil
		},
		func(context.Context, *sql.DB, string) error {
			t.Fatal("db reset ran reset after declined confirmation")
			return nil
		},
	)

	cmd := dbSubcommand(t, newDBCmd(), "reset")
	if err := runDBReset(cmd, nil); err != nil {
		t.Fatalf("runDBReset() returned error: %v", err)
	}
}

func TestRunDBResetForceSkipsConfirmationAndResets(t *testing.T) {
	resetConfigState(t)
	setStdin(t, "")
	opened := false
	reset := false
	stubDBResetStore(t,
		func(_ context.Context, opts store.Options) (*store.Store, error) {
			opened = true
			if !slices.Equal(opts.Addrs, store.DefaultOptions().Addrs) {
				t.Fatalf("Addrs = %v, want defaults", opts.Addrs)
			}
			return &store.Store{}, nil
		},
		func(context.Context, *sql.DB, string) error {
			reset = true
			return nil
		},
	)

	cmd := dbSubcommand(t, newDBCmd(), "reset")
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set force flag: %v", err)
	}
	if err := runDBReset(cmd, nil); err != nil {
		t.Fatalf("runDBReset() returned error: %v", err)
	}
	if !opened {
		t.Fatal("db reset --force did not open the store")
	}
	if !reset {
		t.Fatal("db reset --force did not reset the store")
	}
	snapshot := defaultControlPlaneSnapshot(t)
	if snapshot.ResetPending {
		t.Fatalf("reset_pending = true after successful reset: %#v", snapshot)
	}
	if snapshot.SchemaEpoch != "2" {
		t.Fatalf("schema_epoch = %q, want 2 after reset", snapshot.SchemaEpoch)
	}
	if len(snapshot.Collectors) == 0 || len(snapshot.Sources) == 0 {
		t.Fatalf("control-plane metadata was not preserved/initialized: %#v", snapshot)
	}
}

func TestRunDBResetHoldsRunLockDuringDestructiveReset(t *testing.T) {
	resetConfigState(t)
	setStdin(t, "")
	checkedRunLock := false
	stubDBResetStore(t,
		func(context.Context, store.Options) (*store.Store, error) {
			return &store.Store{}, nil
		},
		func(context.Context, *sql.DB, string) error {
			checkedRunLock = true
			second, err := acquireBeaconRunLock()
			if err == nil {
				_ = second.Close()
				t.Fatal("acquireBeaconRunLock succeeded during db reset, want lock rejection")
			}
			if !strings.Contains(err.Error(), "locked") {
				t.Fatalf("acquireBeaconRunLock error = %v, want locked rejection", err)
			}
			return nil
		},
	)

	cmd := dbSubcommand(t, newDBCmd(), "reset")
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set force flag: %v", err)
	}
	if err := runDBReset(cmd, nil); err != nil {
		t.Fatalf("runDBReset() returned error: %v", err)
	}
	if !checkedRunLock {
		t.Fatal("db reset did not reach destructive reset callback")
	}
	after, err := acquireBeaconRunLock()
	if err != nil {
		t.Fatalf("acquireBeaconRunLock after db reset: %v", err)
	}
	if err := after.Close(); err != nil {
		t.Fatalf("close run lock after db reset: %v", err)
	}
}

func TestRunDBResetRejectsHeldRunLock(t *testing.T) {
	resetConfigState(t)
	setStdin(t, "")
	lock, err := acquireBeaconRunLock()
	if err != nil {
		t.Fatalf("acquire run lock: %v", err)
	}
	t.Cleanup(func() {
		if err := lock.Close(); err != nil {
			t.Fatalf("close run lock: %v", err)
		}
	})
	stubDBResetStore(t,
		func(context.Context, store.Options) (*store.Store, error) {
			t.Fatal("db reset opened store while run lock was held")
			return nil, nil
		},
		func(context.Context, *sql.DB, string) error {
			t.Fatal("db reset ran reset while run lock was held")
			return nil
		},
	)

	cmd := dbSubcommand(t, newDBCmd(), "reset")
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set force flag: %v", err)
	}
	err = runDBReset(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "running local Beacon capture process or reset") || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("runDBReset error = %v, want run-lock rejection", err)
	}
}

func TestRunDBResetFailureLeavesResetPendingWithoutEpochAdvance(t *testing.T) {
	resetConfigState(t)
	setStdin(t, "")
	stubDBResetStore(t,
		func(context.Context, store.Options) (*store.Store, error) {
			return &store.Store{}, nil
		},
		func(context.Context, *sql.DB, string) error {
			return errors.New("drop tables failed")
		},
	)

	cmd := dbSubcommand(t, newDBCmd(), "reset")
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set force flag: %v", err)
	}
	err := runDBReset(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "reset_pending remains active") {
		t.Fatalf("runDBReset error = %v, want reset_pending failure", err)
	}
	snapshot := defaultControlPlaneSnapshot(t)
	if !snapshot.ResetPending || snapshot.ResetPendingEpoch != controlplane.InitialSchemaEpoch {
		t.Fatalf("reset-pending snapshot = %#v, want pending at initial epoch", snapshot)
	}
	if snapshot.SchemaEpoch != controlplane.InitialSchemaEpoch {
		t.Fatalf("schema_epoch = %q, want unchanged %q", snapshot.SchemaEpoch, controlplane.InitialSchemaEpoch)
	}
}

func TestRunDBResetRejectsCollectorRole(t *testing.T) {
	resetConfigState(t)
	configPath, _ := writeInitTestConfigWithRole(t, config.FleetRoleCollector)
	withConfigFile(t, configPath)
	setStdin(t, "")
	stubDBResetStore(t,
		func(context.Context, store.Options) (*store.Store, error) {
			t.Fatal("db reset opened store for collector role")
			return nil, nil
		},
		func(context.Context, *sql.DB, string) error {
			t.Fatal("db reset ran reset for collector role")
			return nil
		},
	)

	cmd := dbSubcommand(t, newDBCmd(), "reset")
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set force flag: %v", err)
	}
	err := runDBReset(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot reset control-plane ClickHouse data") {
		t.Fatalf("runDBReset error = %v, want collector-role rejection", err)
	}
}

func TestRunDBResetRejectsRunningLocalBeaconProcess(t *testing.T) {
	resetConfigState(t)
	setStdin(t, "")
	path := pidfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create pidfile dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	stubDBResetStore(t,
		func(context.Context, store.Options) (*store.Store, error) {
			t.Fatal("db reset opened store while local Beacon was running")
			return nil, nil
		},
		func(context.Context, *sql.DB, string) error {
			t.Fatal("db reset ran reset while local Beacon was running")
			return nil
		},
	)

	cmd := dbSubcommand(t, newDBCmd(), "reset")
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set force flag: %v", err)
	}
	err := runDBReset(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "stop the running local Beacon capture process") {
		t.Fatalf("runDBReset error = %v, want running-process rejection", err)
	}
}

func TestAcquireResetLockRejectsConcurrentReset(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "control-plane.db")
	first, err := acquireResetLock(metadataPath)
	if err != nil {
		t.Fatalf("acquire first reset lock: %v", err)
	}
	defer first.Close()

	second, err := acquireResetLock(metadataPath)
	if err == nil {
		_ = second.Close()
		t.Fatal("second acquireResetLock returned nil error")
	}
	if !strings.Contains(err.Error(), "another beacon db reset is already running") {
		t.Fatalf("second acquireResetLock error = %v, want active reset rejection", err)
	}
}

func TestParseManagedNativeClickHousePIDs(t *testing.T) {
	dataDir := "/home/ubuntu/.beacon/clickhouse/data"
	psOutput := `
    100 /usr/bin/clickhouse server --daemon -- --path=/home/ubuntu/.beacon/clickhouse/data --tcp_port=9000
    101 /usr/bin/clickhouse server --daemon -- --path=/tmp/other --tcp_port=9001
    102 /bin/sh -c echo clickhouse --path=/home/ubuntu/.beacon/clickhouse/data
`

	got := parseManagedNativeClickHousePIDs(psOutput, dataDir, 102)
	if !slices.Equal(got, []int{100}) {
		t.Fatalf("pids = %#v, want [100]", got)
	}
}

func TestManagedClickHouseMetadataHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := filepath.Join(home, ".beacon", "clickhouse", "data")
	err := errors.New("migrate clickhouse: code: 107, message: Cannot open file " +
		filepath.Join(dataDir, "store", "7ab", "uuid", "capture_checkpoints.sql") +
		": , errno: 2, strerror: No such file or directory")

	hint := managedClickHouseMetadataHint(err)
	if !strings.Contains(hint, "beacon db down") || !strings.Contains(hint, "rm -rf ~/.beacon/clickhouse/data") {
		t.Fatalf("missing recovery hint: %q", hint)
	}
}

func TestManagedClickHouseMetadataHintIgnoresRemoteErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	err := errors.New("migrate clickhouse: code: 107, message: Cannot open file /var/lib/clickhouse/store/x/table.sql")
	if hint := managedClickHouseMetadataHint(err); hint != "" {
		t.Fatalf("hint = %q, want empty", hint)
	}
}

func dbSubcommand(t *testing.T, cmd *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	t.Fatalf("missing db subcommand %q", name)
	return nil
}

func resetConfigState(t *testing.T) {
	t.Helper()
	oldCfgFile := cfgFile
	cfgFile = ""
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() {
		cfgFile = oldCfgFile
	})
}

func setStdin(t *testing.T, input string) {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	if input != "" {
		if _, err := w.WriteString(input); err != nil {
			t.Fatalf("write stdin fixture: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})
}

func defaultControlPlaneSnapshot(t *testing.T) *controlplane.Snapshot {
	t.Helper()
	control, err := controlplane.Open(filepath.Join(os.Getenv("HOME"), ".beacon", "control-plane.db"))
	if err != nil {
		t.Fatalf("Open control-plane: %v", err)
	}
	defer control.Close()
	snapshot, err := control.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return snapshot
}

func prepareResetPendingControlPlane(t *testing.T, metadataPath string) {
	t.Helper()
	control, err := controlplane.Open(metadataPath)
	if err != nil {
		t.Fatalf("Open control-plane: %v", err)
	}
	defer control.Close()
	if _, err := control.EnsureLocal(context.Background(), controlplane.Bootstrap{
		NodeID:      "node-reset-pending",
		NodeName:    "Reset Pending",
		CollectorID: "collector-reset-pending",
	}); err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	if _, err := control.BeginReset(context.Background()); err != nil {
		t.Fatalf("BeginReset: %v", err)
	}
}

func stubDBResetStore(
	t *testing.T,
	open func(context.Context, store.Options) (*store.Store, error),
	reset func(context.Context, *sql.DB, string) error,
) {
	t.Helper()
	oldOpen := dbResetOpenStore
	oldReset := dbResetStore
	dbResetOpenStore = open
	dbResetStore = reset
	t.Cleanup(func() {
		dbResetOpenStore = oldOpen
		dbResetStore = oldReset
	})
}
