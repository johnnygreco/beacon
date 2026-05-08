package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/johnnygreco/beacon/internal/store"
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
