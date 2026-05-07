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

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
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

	// Store stats
	opts := storeOptionsFromConfig(cfg)
	ch, err := store.Open(cmd.Context(), opts)
	if err != nil {
		fmt.Printf("ClickHouse: unavailable at %s (%v)\n", strings.Join(opts.Addrs, ","), err)
		return nil
	}
	defer ch.Close()

	fmt.Printf("ClickHouse: connected at %s database=%s\n", strings.Join(opts.Addrs, ","), opts.Database)

	// Last event
	var lastEvent sql.NullTime
	if err := ch.DB.QueryRow(`SELECT max(ended_at) FROM (
		SELECT session_id, argMax(ended_at, updated_at) AS ended_at
		FROM session_projection
		GROUP BY session_id
	)`).Scan(&lastEvent); err != nil {
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
		{"Raw Records", "SELECT count() FROM raw_records"},
		{"Activity Events", "SELECT count() FROM activity_events"},
		{"Sessions", "SELECT uniqExact(session_id) FROM session_projection"},
		{"Event Links", "SELECT COUNT(*) FROM event_links"},
		{"Tool Payloads", "SELECT COUNT(*) FROM tool_payloads"},
		{"Capture Errors", "SELECT COUNT(*) FROM capture_errors"},
		{"Checkpoints", "SELECT COUNT(*) FROM capture_checkpoints"},
		{"Search Docs", "SELECT COUNT(*) FROM search_documents"},
		{"Search Postings", "SELECT COUNT(*) FROM search_postings"},
	}

	for _, c := range counts {
		var count int64
		if err := ch.DB.QueryRow(c.query).Scan(&count); err != nil {
			fmt.Printf("%-18s error\n", c.label+":")
		} else {
			fmt.Printf("%-18s %d\n", c.label+":", count)
		}
	}

	// Active sessions in last hour
	var active int64
	if err := ch.DB.QueryRow(`SELECT count() FROM (
		SELECT session_id,
		       argMax(ended_at, updated_at) AS ended_at,
		       argMax(has_session_end, updated_at) AS has_session_end
		FROM session_projection
		GROUP BY session_id
	) WHERE ended_at > now() - INTERVAL 1 HOUR AND has_session_end = 0`).Scan(&active); err != nil {
		fmt.Fprintf(os.Stderr, "warning: active sessions query failed: %v\n", err)
	}
	fmt.Printf("%-18s %d active in last hour\n", "", active)

	fmt.Println()
	var docs, postings int64
	if err := ch.DB.QueryRow("SELECT count() FROM search_documents").Scan(&docs); err == nil {
		_ = ch.DB.QueryRow("SELECT count() FROM search_postings").Scan(&postings)
		fmt.Printf("Search Index: %d documents, %d postings\n", docs, postings)
	} else {
		fmt.Println("Search Index: unavailable")
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
