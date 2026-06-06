package main

import (
	"context"
	"strings"
	"testing"
)

func TestParseBenchmarks(t *testing.T) {
	output := strings.Join([]string{
		"goos: darwin",
		"pkg: github.com/johnnygreco/beacon/internal/mcp",
		"BenchmarkMCPDispatchSearchWithFakeResults-10    \t   89844\t     13342 ns/op\t   31595 B/op\t     117 allocs/op",
		"pkg: github.com/johnnygreco/beacon/internal/store",
		"BenchmarkStoreBuildSearchRows/1000Events-10     \t     184\t   6456049 ns/op\t25340468 B/op\t   92837 allocs/op",
	}, "\n")

	got := parseBenchmarks(output, "fast")
	if len(got) != 2 {
		t.Fatalf("parseBenchmarks returned %d results, want 2", len(got))
	}
	if got[0].Name != "BenchmarkMCPDispatchSearchWithFakeResults" || got[0].Milliseconds != 0.013 {
		t.Fatalf("first benchmark = %#v", got[0])
	}
	if got[1].Name != "BenchmarkStoreBuildSearchRows/1000Events" || got[1].BytesPerOp != 25340468 || got[1].AllocsPerOp != 92837 {
		t.Fatalf("second benchmark = %#v", got[1])
	}
}

func TestSeedPerfDatabaseRefusesUnsafeDatabaseName(t *testing.T) {
	_, err := seedPerfDatabase(context.Background(), labConfig{
		Database:   "beacon",
		Size:       "small",
		ClickHouse: "127.0.0.1:1",
	})
	if err == nil || !strings.Contains(err.Error(), "refusing to reset database") {
		t.Fatalf("seedPerfDatabase error = %v, want unsafe reset refusal", err)
	}
}

func TestMarkdownReportIncludesCoreSections(t *testing.T) {
	report := labReport{
		Schema:      reportSchema,
		Status:      "pass",
		GitRevision: "abc123",
		GitBranch:   "main",
		Dataset: datasetReport{
			Size:     "small",
			Database: "beacon_perf_lab",
			Seeded:   true,
			Sessions: 250,
			Events:   22954,
			Payloads: 14784,
			Duration: "500ms",
		},
		Server: serverReport{BaseURL: "http://127.0.0.1:4611", Started: true},
		Commands: []commandReport{{
			Name:       "fast-go-benchmarks",
			Status:     "pass",
			DurationMS: 1200,
		}},
		GoBenchmarks: []benchmarkReport{{
			Source:       "fast",
			Name:         "BenchmarkMCPDispatchSearchWithFakeResults",
			Milliseconds: 0.013,
			BytesPerOp:   31595,
			AllocsPerOp:  117,
		}},
		Browser: &browserLabReport{Summary: []browserMetricSummary{{
			Name:     "dashboard.cold_load.ready",
			Viewport: "desktop",
			Unit:     "ms",
			Median:   120,
			P95:      140,
			Max:      140,
		}}},
	}

	got := markdownReport(report)
	for _, want := range []string{"# Beacon Performance Lab", "## Commands", "## Go Benchmarks", "## Browser Summary", "beacon_perf_lab"} {
		if !strings.Contains(got, want) {
			t.Fatalf("markdown report missing %q:\n%s", want, got)
		}
	}
}
