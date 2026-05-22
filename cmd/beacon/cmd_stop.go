package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/spf13/cobra"
)

var stopProcess = stopPid

func runStop(cmd *cobra.Command, args []string) error {
	// A pidfile is enough to stop a running Beacon process. Try it before
	// loading config so a malformed config cannot block shutdown.
	if pid := readPidFromFile(); pid > 0 {
		return stopProcess(pid)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// No pidfile — try to find PID by port
	if checkServer(cfg.Server.Port) {
		pid, err := findPidByPort(cfg.Server.Port)
		if err != nil || pid <= 0 {
			return fmt.Errorf("server is running on port %d but could not determine PID\nStop it manually with: kill $(lsof -ti :%d)", cfg.Server.Port, cfg.Server.Port)
		}
		return stopProcess(pid)
	}

	fmt.Println("No running beacon server found.")
	return nil
}

// findPidByPort uses lsof to find the PID listening on a given port.
func findPidByPort(port int) (int, error) {
	out, err := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port), "-sTCP:LISTEN").Output()
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return 0, fmt.Errorf("no process found")
	}
	return strconv.Atoi(strings.TrimSpace(lines[0]))
}

// stopPid sends SIGTERM to a process and waits for it to exit.
func stopPid(pid int) error {
	fmt.Printf("Stopping beacon server (pid %d)...\n", pid)
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process %d: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sending SIGTERM to pid %d: %w", pid, err)
	}
	if waitForExit(pid, 10*time.Second) {
		fmt.Println("Server stopped.")
	} else {
		fmt.Println("Server may still be shutting down.")
	}
	return nil
}

// readPidFromFile reads and validates the PID from the pidfile.
func readPidFromFile() int {
	data, err := os.ReadFile(pidfilePath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	// Verify process is alive
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Stale pidfile — clean up
		os.Remove(pidfilePath())
		return 0
	}
	return pid
}

// waitForExit polls until the process exits or the timeout elapses.
func waitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		proc, err := os.FindProcess(pid)
		if err != nil {
			return true
		}
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return true // process no longer exists
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
