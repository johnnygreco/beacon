package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
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
	if err := os.WriteFile(cfgPath, []byte("[server]\nhost = \"127.0.0.1\"\nport = 0\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfgFile = cfgPath

	if err := runStop(newDownCmd(), nil); err != nil {
		t.Fatalf("runStop() returned error: %v", err)
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
