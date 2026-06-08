package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/store"
)

func TestPIDFilePathUsesUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := pidfilePath()
	want := filepath.Join(home, ".beacon", "beacon.pid")
	if got != want {
		t.Fatalf("pidfilePath() = %q, want %q", got, want)
	}
}

func TestReadPidFromFileReturnsLivePID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := pidfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create pidfile dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}

	if got := readPidFromFile(); got != os.Getpid() {
		t.Fatalf("readPidFromFile() = %d, want %d", got, os.Getpid())
	}
}

func TestReadPidFromFileRemovesStalePIDFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := pidfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create pidfile dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("999999999"), 0644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}

	if got := readPidFromFile(); got != 0 {
		t.Fatalf("readPidFromFile() = %d, want 0", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale pidfile still exists; stat error = %v", err)
	}
}

func TestReadPidReturnsZeroForInvalidPIDFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := pidfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create pidfile dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("not-a-pid"), 0644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}

	if got := readPid(); got != 0 {
		t.Fatalf("readPid() = %d, want 0", got)
	}
}

func TestReadPidReturnsLivePID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := pidfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create pidfile dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}

	if got := readPid(); got != os.Getpid() {
		t.Fatalf("readPid() = %d, want %d", got, os.Getpid())
	}
}

func TestCheckServerHealthStatus(t *testing.T) {
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(okServer.Close)
	if !checkServer(serverPort(t, okServer)) {
		t.Fatal("checkServer() = false, want true for healthy server")
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(errorServer.Close)
	if checkServer(serverPort(t, errorServer)) {
		t.Fatal("checkServer() = true, want false for non-200 health response")
	}
}

func TestWaitForExitReturnsTrueForMissingProcess(t *testing.T) {
	if !waitForExit(999999999, time.Millisecond) {
		t.Fatal("waitForExit() = false, want true for missing process")
	}
}

func TestRunStopReturnsNilWhenServerIsNotRunning(t *testing.T) {
	resetConfigState(t)
	cfgPath := filepath.Join(t.TempDir(), "beacon.toml")
	if err := os.WriteFile(cfgPath, []byte("[server]\nhost = \"127.0.0.1\"\nport = 19001\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfgFile = cfgPath

	if err := runStop(newDownCmd(), nil); err != nil {
		t.Fatalf("runStop() returned error: %v", err)
	}
}

func TestRunStopUsesPidfileBeforeConfig(t *testing.T) {
	resetConfigState(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := pidfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create pidfile dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	cfgPath := filepath.Join(t.TempDir(), "beacon.toml")
	if err := os.WriteFile(cfgPath, []byte("[server]\nport = 999999\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfgFile = cfgPath

	oldStop := stopProcess
	t.Cleanup(func() { stopProcess = oldStop })
	stoppedPID := 0
	stopProcess = func(pid int) error {
		stoppedPID = pid
		return nil
	}

	if err := runStop(newDownCmd(), nil); err != nil {
		t.Fatalf("runStop() returned error: %v", err)
	}
	if stoppedPID != os.Getpid() {
		t.Fatalf("stopped pid = %d, want %d", stoppedPID, os.Getpid())
	}
}

func TestRunStatusReportsClickHouseUnavailable(t *testing.T) {
	resetConfigState(t)
	cfgPath := filepath.Join(t.TempDir(), "beacon.toml")
	metadataPath := filepath.Join(t.TempDir(), "control-plane.db")
	control, err := controlplane.Open(metadataPath)
	if err != nil {
		t.Fatalf("Open control-plane: %v", err)
	}
	if _, err := control.EnsureLocal(context.Background(), controlplane.Bootstrap{
		NodeID:      "node-status",
		NodeName:    "Status",
		CollectorID: "collector-status",
		Sources:     []controlplane.SourceRegistration{{Name: "codex", Runtime: "codex", Provider: "openai", Format: "jsonl", WatchRoot: "/tmp/codex"}},
	}); err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	if _, err := control.BeginReset(context.Background()); err != nil {
		t.Fatalf("BeginReset: %v", err)
	}
	if err := control.Close(); err != nil {
		t.Fatalf("Close control-plane: %v", err)
	}
	config := "[server]\nhost = \"127.0.0.1\"\nport = 19001\n\n[database]\naddrs = [\"127.0.0.1:19000\"]\n\n[fleet]\nmetadata_path = \"" + metadataPath + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(config), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfgFile = cfgPath

	oldOpen := statusOpenStore
	statusOpenStore = func(_ context.Context, opts store.Options) (*store.Store, error) {
		if len(opts.Addrs) != 1 || opts.Addrs[0] != "127.0.0.1:19000" {
			t.Fatalf("status store addrs = %v, want configured addr", opts.Addrs)
		}
		return nil, errors.New("offline")
	}
	t.Cleanup(func() { statusOpenStore = oldOpen })

	output := captureStdout(t, func() {
		if err := runStatus(newStatusCmd(), nil); err != nil {
			t.Fatalf("runStatus() returned error: %v", err)
		}
	})
	for _, want := range []string{
		"Beacon Status",
		"Server:  not running",
		"Control Plane: schema_epoch=1 reset_pending=true",
		"Reset Pending: epoch=1",
		"ClickHouse: unavailable at 127.0.0.1:19000 (offline)",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status output missing %q:\n%s", want, output)
		}
	}
}

func serverPort(t *testing.T, server *httptest.Server) int {
	t.Helper()
	_, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("parse test server address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return port
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
		_ = r.Close()
	})

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	os.Stdout = oldStdout
	return string(data)
}
