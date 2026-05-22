package perf_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/perf"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/johnnygreco/beacon/internal/web"
)

// Shared database seeded once in TestMain for all benchmarks.
var (
	sharedStore *store.Store
	benchLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
)

func requirePerfStore(b *testing.B) *store.Store {
	b.Helper()
	if sharedStore == nil {
		b.Skip("set BEACON_TEST_CLICKHOUSE to run ClickHouse perf benchmarks")
	}
	return sharedStore
}

func TestMain(m *testing.M) {
	ctx := context.Background()
	addr := os.Getenv("BEACON_TEST_CLICKHOUSE")
	if addr == "" {
		fmt.Fprintln(os.Stderr, "BEACON_TEST_CLICKHOUSE not set; skipping perf benchmarks")
		os.Exit(m.Run())
	}

	sizeStr := os.Getenv("PERF_SIZE")
	if sizeStr == "" {
		sizeStr = "small"
	}
	seedSize := perf.ParseSeedSize(sizeStr)

	storeOpts := store.Options{Addrs: []string{addr}, Database: "beacon_perf", ReadPoolSize: 4}
	resetter, err := store.OpenForReset(ctx, storeOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open reset store: %v\n", err)
		os.Exit(1)
	}
	if err := store.Reset(ctx, resetter.DB, resetter.Database()); err != nil {
		resetter.Close()
		fmt.Fprintf(os.Stderr, "failed to reset perf database: %v\n", err)
		os.Exit(1)
	}
	resetter.Close()

	sharedStore, err = store.Open(ctx, storeOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open store: %v\n", err)
		os.Exit(1)
	}

	stats, err := perf.Seed(ctx, sharedStore, seedSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to seed: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Seeded %s dataset: %s\n", seedSize, stats)
	code := m.Run()
	sharedStore.Close()
	os.Exit(code)
}

func BenchmarkQueryDashboardData(b *testing.B) {
	ch := requirePerfStore(b)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		_ = web.QueryDashboardData(ctx, ch.DB)
	}
}

func BenchmarkQueryDashboardSessions(b *testing.B) {
	ch := requirePerfStore(b)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		web.QueryDashboardSessions(ctx, ch.DB)
	}
}

func BenchmarkQueryActiveSessions(b *testing.B) {
	ch := requirePerfStore(b)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		web.QueryActiveSessions(ctx, ch.DB)
	}
}

func BenchmarkQuerySessionConversation_Small(b *testing.B) {
	ch := requirePerfStore(b)
	// Use a normal-sized session (index beyond very-large and large)
	ctx := context.Background()
	sessionID := perf.SessionIDForBench(100)
	b.ResetTimer()
	for b.Loop() {
		web.QuerySessionConversation(ctx, ch.DB, sessionID)
	}
}

func BenchmarkQuerySessionConversation_Large(b *testing.B) {
	ch := requirePerfStore(b)
	// Use a very-large session (index 0)
	ctx := context.Background()
	sessionID := perf.SessionIDForBench(0)
	b.ResetTimer()
	for b.Loop() {
		web.QuerySessionConversation(ctx, ch.DB, sessionID)
	}
}

func BenchmarkSearchBM25(b *testing.B) {
	ch := requirePerfStore(b)
	ctx := context.Background()
	s := search.NewSearcher(ch.DB, benchLogger, 25, 0)
	s.MonitorIndex(ctx)

	q := search.SearchQuery{Query: "binary search", Limit: 25}
	b.ResetTimer()
	for b.Loop() {
		_, _ = s.Search(ctx, q)
	}
}

func BenchmarkSearchKeyword(b *testing.B) {
	ch := requirePerfStore(b)
	ctx := context.Background()
	s := search.NewSearcher(ch.DB, benchLogger, 25, 0)

	q := search.SearchQuery{Query: "database", Limit: 25}
	b.ResetTimer()
	for b.Loop() {
		_, _ = s.Search(ctx, q)
	}
}

func BenchmarkSearchBrowse(b *testing.B) {
	ch := requirePerfStore(b)
	ctx := context.Background()
	s := search.NewSearcher(ch.DB, benchLogger, 25, 0)

	q := search.SearchQuery{
		Limit:      25,
		EventKinds: []string{"tool_call"},
		FromTime:   time.Now().Add(-7 * 24 * time.Hour),
	}
	b.ResetTimer()
	for b.Loop() {
		_, _ = s.Browse(ctx, q)
	}
}

func BenchmarkQueryRecentActivity(b *testing.B) {
	ch := requirePerfStore(b)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		web.QueryRecentActivity(ctx, ch.DB)
	}
}

func BenchmarkQueryTokensTimeSeries(b *testing.B) {
	ch := requirePerfStore(b)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		web.QueryTotalTokensTimeSeries(ctx, ch.DB)
	}
}

func BenchmarkQuerySessionDetail(b *testing.B) {
	ch := requirePerfStore(b)
	ctx := context.Background()
	sessionID := perf.SessionIDForBench(0)
	b.ResetTimer()
	for b.Loop() {
		_, _ = web.QuerySessionDetail(ctx, ch.DB, sessionID)
	}
}

func BenchmarkAPIDashboardJSON(b *testing.B) {
	ch := requirePerfStore(b)
	api := web.NewAPIHandlers(ch.DB, nil, benchLogger)
	cases := []struct {
		name    string
		target  string
		handler http.HandlerFunc
	}{
		{"Metrics", "/api/metrics", api.GetMetrics},
		{"ActiveSessions", "/api/dashboard/sessions?state=active", api.GetDashboardSessions},
		{"CompletedSessions", "/api/dashboard/sessions?state=completed&limit=30", api.GetDashboardSessions},
		{"Activity", "/api/dashboard/activity?range=24h", api.GetActivity},
		{"Charts", "/api/dashboard/charts", api.GetDashboardCharts},
		{"TokensPerMinute", "/api/tokens-per-minute", api.GetTokensPerMinute},
		{"ToolStats", "/api/tool-stats", api.GetToolStats},
		{"TokensByModel", "/api/tokens-by-model", api.GetTokensByModel},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				req := httptest.NewRequest(http.MethodGet, tc.target, nil)
				rec := httptest.NewRecorder()
				tc.handler(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("%s returned %d: %s", tc.target, rec.Code, rec.Body.String())
				}
			}
		})
	}
}
