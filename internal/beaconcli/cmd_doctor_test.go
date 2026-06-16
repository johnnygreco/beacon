package beaconcli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
)

func TestRunDoctorSetupFreshHomeReportsLocalChecks(t *testing.T) {
	resetConfigState(t)
	withNoDockerOnPath(t)
	withDoctorClickHouseReady(t)

	cmd, out := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); err != nil {
		t.Fatalf("runDoctorSetup error = %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"[PASS] config",
		"[INFO] mode: single-machine local dashboard",
		"local health",
		"[PASS] ClickHouse migration",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestRunDoctorSetupInvalidConfigReportsConfigFailure(t *testing.T) {
	resetConfigState(t)
	path := filepath.Join(t.TempDir(), "beacon.toml")
	if err := os.WriteFile(path, []byte("[server]\nport = 0\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withConfigFile(t, path)

	cmd, out := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); !errors.Is(err, errDoctorSetupFailed) {
		t.Fatalf("runDoctorSetup error = %v, want doctor failure", err)
	}
	output := out.String()
	if !strings.Contains(output, "[FAIL] config") || !strings.Contains(output, "server.port") {
		t.Fatalf("doctor output = %q, want config failure", output)
	}
}

func TestRunDoctorSetupUsesReadOnlyClickHouseCheck(t *testing.T) {
	resetConfigState(t)
	withNoDockerOnPath(t)

	oldStatusOpenStore := statusOpenStore
	statusOpenStore = func(context.Context, store.Options) (*store.Store, error) {
		t.Fatal("doctor setup called statusOpenStore, which runs migrations")
		return nil, nil
	}
	t.Cleanup(func() { statusOpenStore = oldStatusOpenStore })

	calledReadOnly := false
	oldReadOnly := doctorOpenClickHouseReadOnly
	doctorOpenClickHouseReadOnly = func(context.Context, store.Options) (*store.Store, error) {
		calledReadOnly = true
		return &store.Store{}, nil
	}
	t.Cleanup(func() { doctorOpenClickHouseReadOnly = oldReadOnly })

	cmd, _ := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); err != nil {
		t.Fatalf("runDoctorSetup returned error: %v", err)
	}
	if !calledReadOnly {
		t.Fatal("doctor setup did not call read-only ClickHouse check")
	}
}

func TestRunDoctorSetupReportsBroadManagedDockerClickHouseBindings(t *testing.T) {
	resetConfigState(t)
	withDoctorClickHouseReady(t)
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	dockerPath := filepath.Join(binDir, "docker")
	script := `#!/bin/sh
if [ "$1" = "container" ] && [ "$2" = "inspect" ] && [ "$3" = "beacon-clickhouse" ]; then
  exit 0
fi
if [ "$1" = "container" ] && [ "$2" = "inspect" ] && [ "$3" = "--format" ]; then
  echo '{"9000/tcp":[{"HostIp":"0.0.0.0","HostPort":"9000"}],"8123/tcp":[{"HostIp":"127.0.0.1","HostPort":"8123"}]}'
  exit 0
fi
exit 1
`
	if err := os.WriteFile(dockerPath, []byte(script), 0755); err != nil {
		t.Fatalf("write docker fixture: %v", err)
	}
	t.Setenv("PATH", binDir)

	cmd, out := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); !errors.Is(err, errDoctorSetupFailed) {
		t.Fatalf("runDoctorSetup error = %v, want doctor failure", err)
	}
	output := out.String()
	for _, want := range []string{
		"[FAIL] ClickHouse managed Docker",
		"beyond loopback",
		"docker rm beacon-clickhouse",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func doctorTestCommand() (*cobra.Command, *bytes.Buffer) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd, &out
}

func withDoctorClickHouseReady(t *testing.T) {
	t.Helper()
	oldOpen := doctorOpenClickHouseReadOnly
	doctorOpenClickHouseReadOnly = func(context.Context, store.Options) (*store.Store, error) {
		return &store.Store{}, nil
	}
	t.Cleanup(func() { doctorOpenClickHouseReadOnly = oldOpen })
}

func withNoDockerOnPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}
