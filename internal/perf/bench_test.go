package perf_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/database"
	"github.com/johnnygreco/beacon/internal/perf"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/web"
)

// Shared database seeded once in TestMain for all benchmarks.
var (
	sharedDB    *database.DB
	benchLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	sizeStr := os.Getenv("PERF_SIZE")
	if sizeStr == "" {
		sizeStr = "small"
	}
	seedSize := perf.ParseSeedSize(sizeStr)

	var err error
	sharedDB, err = database.Open("", 4)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open db: %v\n", err)
		os.Exit(1)
	}

	stats, err := perf.Seed(ctx, sharedDB, seedSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to seed: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Seeded %s dataset: %s\n", seedSize, stats)
	code := m.Run()
	sharedDB.Close()
	os.Exit(code)
}

func BenchmarkQueryDashboardData(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		_ = web.QueryDashboardData(ctx, sharedDB.ReadPool)
	}
}

func BenchmarkQueryDashboardSessions(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		web.QueryDashboardSessions(ctx, sharedDB.ReadPool)
	}
}

func BenchmarkQuerySessionConversation_Small(b *testing.B) {
	// Use a normal-sized session (index beyond very-large and large)
	ctx := context.Background()
	sessionID := perf.SessionIDForBench(100)
	b.ResetTimer()
	for b.Loop() {
		web.QuerySessionConversation(ctx, sharedDB.ReadPool, sessionID)
	}
}

func BenchmarkQuerySessionConversation_Large(b *testing.B) {
	// Use a very-large session (index 0)
	ctx := context.Background()
	sessionID := perf.SessionIDForBench(0)
	b.ResetTimer()
	for b.Loop() {
		web.QuerySessionConversation(ctx, sharedDB.ReadPool, sessionID)
	}
}

func BenchmarkSearchBM25(b *testing.B) {
	ctx := context.Background()
	s := search.NewSearcher(sharedDB.ReadPool, benchLogger, 25, 0)
	s.RunIndexer(ctx)

	q := search.SearchQuery{Query: "binary search", Limit: 25}
	b.ResetTimer()
	for b.Loop() {
		_, _ = s.Search(ctx, q)
	}
}

func BenchmarkSearchILIKE(b *testing.B) {
	ctx := context.Background()
	// Searcher without FTS index forces ILIKE-only path
	s := search.NewSearcher(sharedDB.ReadPool, benchLogger, 25, 0)

	q := search.SearchQuery{Query: "database", Limit: 25}
	b.ResetTimer()
	for b.Loop() {
		_, _ = s.Search(ctx, q)
	}
}

func BenchmarkSearchBrowse(b *testing.B) {
	ctx := context.Background()
	s := search.NewSearcher(sharedDB.ReadPool, benchLogger, 25, 0)

	q := search.SearchQuery{
		Limit:     25,
		EventKinds: []string{"tool_call"},
		FromTime:  time.Now().Add(-7 * 24 * time.Hour),
	}
	b.ResetTimer()
	for b.Loop() {
		_, _ = s.Browse(ctx, q)
	}
}

func BenchmarkQueryRecentActivity(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		web.QueryRecentActivity(ctx, sharedDB.ReadPool)
	}
}

func BenchmarkQueryTokensTimeSeries(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		web.QueryTotalTokensTimeSeries(ctx, sharedDB.ReadPool)
	}
}

func BenchmarkQuerySessionDetail(b *testing.B) {
	ctx := context.Background()
	sessionID := perf.SessionIDForBench(0)
	b.ResetTimer()
	for b.Loop() {
		_, _ = web.QuerySessionDetail(ctx, sharedDB.ReadPool, sessionID)
	}
}
