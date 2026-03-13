package web

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	beacon "github.com/johnnygreco/beacon"
	"github.com/johnnygreco/beacon/internal/database"
	"github.com/johnnygreco/beacon/internal/perf"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/sse"
)

// Shared benchmark infrastructure — one seeded database and server for all benchmarks.
var (
	benchServer    *httptest.Server
	benchUpdater   *Updater
	benchSessionID string
	benchReady     bool
)

func TestMain(m *testing.M) {
	code := m.Run()
	if benchServer != nil {
		benchServer.Close()
	}
	os.Exit(code)
}

// ensureBenchServer lazily creates the shared seeded server on first use.
func ensureBenchServer(b *testing.B) {
	b.Helper()
	if benchReady {
		return
	}

	size := perf.SizeSmall
	if v := os.Getenv("BEACON_BENCH_SIZE"); v != "" {
		size = perf.ParseSeedSize(v)
	}

	db, err := database.Open("", 2)
	if err != nil {
		b.Fatalf("open db: %v", err)
	}

	stats, err := perf.Seed(context.Background(), db, size)
	if err != nil {
		b.Fatalf("seed db: %v", err)
	}
	b.Logf("seeded: %s", stats)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	broker := sse.NewBroker(16, logger)
	searcher := search.NewSearcher(db.ReadPool, logger, 25, 0)
	updater := NewUpdater(db.ReadPool, broker, logger)
	handlers := NewHandlers(db.ReadPool, searcher, logger, updater)
	apiHandlers := NewAPIHandlers(db.ReadPool, searcher, logger)
	staticFS, err := fs.Sub(beacon.StaticFS, "static")
	if err != nil {
		b.Fatalf("static fs: %v", err)
	}
	router := NewRouter(staticFS, broker, handlers, apiHandlers)
	benchServer = httptest.NewServer(router)

	// Warm the snapshot
	updater.NotifyDashboard()

	// Build FTS index for search benchmarks
	searcher.RebuildIndex(context.Background())

	benchUpdater = updater
	benchSessionID = perf.SessionIDForBench(0)
	benchReady = true
}

func printBenchHeader(b *testing.B) {
	b.Helper()
	rev := "unknown"
	if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
		rev = strings.TrimSpace(string(out))
	}
	size := perf.SizeSmall
	if v := os.Getenv("BEACON_BENCH_SIZE"); v != "" {
		size = perf.ParseSeedSize(v)
	}
	b.Logf("dataset=%s git=%s status=warm", size, rev)
}

func BenchmarkDashboard(b *testing.B) {
	ensureBenchServer(b)
	printBenchHeader(b)
	url := benchServer.URL + "/"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkSessionDetail(b *testing.B) {
	ensureBenchServer(b)
	printBenchHeader(b)
	url := fmt.Sprintf("%s/sessions/%s", benchServer.URL, benchSessionID)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkSessionConversation(b *testing.B) {
	ensureBenchServer(b)
	printBenchHeader(b)
	url := fmt.Sprintf("%s/sessions/%s/conversation", benchServer.URL, benchSessionID)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkSearch(b *testing.B) {
	ensureBenchServer(b)
	printBenchHeader(b)
	url := benchServer.URL + "/search/results?q=binary+search"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkUpdaterRefresh(b *testing.B) {
	ensureBenchServer(b)
	printBenchHeader(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchUpdater.NotifyDashboard()
	}
}

func BenchmarkAPIMetrics(b *testing.B) {
	ensureBenchServer(b)
	printBenchHeader(b)
	url := benchServer.URL + "/api/metrics"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkAPISessions(b *testing.B) {
	ensureBenchServer(b)
	printBenchHeader(b)
	url := benchServer.URL + "/api/sessions"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}
