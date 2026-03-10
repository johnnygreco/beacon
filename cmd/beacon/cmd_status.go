package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/johnnygreco/beacon/internal/config"

	_ "github.com/duckdb/duckdb-go/v2"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check if the beacon server is running and show database statistics",
		RunE:  runStatus,
	}
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		cfg = &config.Config{}
	}

	// Server status
	fmt.Println("Beacon Status")
	fmt.Println("=============")

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	serverUp := checkServer(cfg.Server.Port)
	if serverUp {
		pid := readPid()
		if pid > 0 {
			fmt.Printf("Server:  running at %s (pid %d)\n", addr, pid)
		} else {
			fmt.Printf("Server:  running at %s\n", addr)
		}
	} else {
		fmt.Printf("Server:  not running (expected at %s)\n", addr)
	}
	fmt.Println()

	// Database stats
	dbPath := resolveDBPath(cfg)
	fi, err := os.Stat(dbPath)
	if err != nil {
		fmt.Printf("Database: not found at %s\n", dbPath)
		return nil
	}

	db, err := sql.Open("duckdb", dbPath+"?access_mode=read_only")
	if err != nil {
		fmt.Printf("Database: %s (%.1f MB, locked by server)\n", dbPath, float64(fi.Size())/(1024*1024))
		return nil
	}
	// Verify the connection works (Open may succeed but queries fail due to lock)
	if err := db.Ping(); err != nil {
		db.Close()
		fmt.Printf("Database: %s (%.1f MB, locked by server)\n", dbPath, float64(fi.Size())/(1024*1024))
		return nil
	}
	defer db.Close()

	fmt.Printf("Database: %s (%.1f MB)\n", dbPath, float64(fi.Size())/(1024*1024))

	// Last event
	var lastEvent sql.NullTime
	if err := db.QueryRow("SELECT MAX(timestamp) FROM events").Scan(&lastEvent); err != nil {
		fmt.Println("Last event: unavailable")
	} else if lastEvent.Valid {
		fmt.Printf("Last event: %s\n", lastEvent.Time.Format(time.RFC3339))
	} else {
		fmt.Println("Last event: none")
	}
	fmt.Println()

	// Counts
	counts := []struct {
		label string
		query string
	}{
		{"Events", "SELECT COUNT(*) FROM events"},
		{"Sessions", "SELECT COUNT(DISTINCT session_id) FROM events"},
		{"Event Links", "SELECT COUNT(*) FROM event_links"},
		{"Tool I/O", "SELECT COUNT(*) FROM tool_io"},
		{"Ingest Errors", "SELECT COUNT(*) FROM ingest_errors"},
		{"Checkpoints", "SELECT COUNT(*) FROM ingest_checkpoints"},
	}

	for _, c := range counts {
		var count int64
		if err := db.QueryRow(c.query).Scan(&count); err != nil {
			fmt.Printf("%-18s error\n", c.label+":")
		} else {
			fmt.Printf("%-18s %d\n", c.label+":", count)
		}
	}

	// Active sessions in last hour
	var active int64
	if err := db.QueryRow("SELECT COUNT(DISTINCT session_id) FROM events WHERE timestamp > current_timestamp - INTERVAL '1 hour'").Scan(&active); err != nil {
		fmt.Fprintf(os.Stderr, "warning: active sessions query failed: %v\n", err)
	}
	fmt.Printf("%-18s %d active in last hour\n", "", active)

	// FTS status
	fmt.Println()
	ftsAvailable := true
	if _, err := db.Exec("INSTALL fts"); err != nil {
		ftsAvailable = false
	}
	if ftsAvailable {
		if _, err := db.Exec("LOAD fts"); err != nil {
			ftsAvailable = false
		}
	}
	if ftsAvailable {
		var ftsCount int
		err = db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'fts_main_events'").Scan(&ftsCount)
		if err == nil && ftsCount > 0 {
			fmt.Println("FTS Index: available")
		} else {
			fmt.Println("FTS Index: not built (run 'beacon serve' to build)")
		}
	} else {
		fmt.Println("FTS Index: extension not available")
	}

	return nil
}

// checkServer returns true if the beacon server responds on the given port.
func checkServer(port int) bool {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// readPid reads the PID from the pidfile. Returns 0 if not available or stale.
func readPid() int {
	data, err := os.ReadFile(pidfilePath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	// Check if process is alive
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return 0
	}
	return pid
}
