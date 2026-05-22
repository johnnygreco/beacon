package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
