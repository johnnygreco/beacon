package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("BEACON_PERFLAB_LONG_OUTPUT_HELPER") == "1" {
		fmt.Println("pkg: github.com/johnnygreco/beacon/cmd/perflab")
		fmt.Println("BenchmarkEarlyRecord-10    \t       1\t         1.0 ns/op\t       0 B/op\t       0 allocs/op")
		fmt.Print(strings.Repeat("x", 21000))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

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

func TestRunCommandKeepsRawOutputForParsing(t *testing.T) {
	result := runCommand(context.Background(), "long-output", os.Args[0], nil, []string{"BEACON_PERFLAB_LONG_OUTPUT_HELPER=1"})
	if result.Err != nil {
		t.Fatalf("runCommand error = %v", result.Err)
	}
	if strings.Contains(result.Command.OutputTail, "BenchmarkEarlyRecord") {
		t.Fatalf("OutputTail unexpectedly retained early benchmark line")
	}
	got := parseBenchmarks(result.Output, "fast")
	if len(got) != 1 || got[0].Name != "BenchmarkEarlyRecord" {
		t.Fatalf("parse raw output = %#v, want BenchmarkEarlyRecord", got)
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

func TestDefaultLiveBenchmarkDatabase(t *testing.T) {
	tests := []struct {
		name     string
		database string
		want     string
	}{
		{name: "safe lab database", database: "beacon_perf_lab", want: "beacon_perf_lab_bench"},
		{name: "unsafe explicit app database", database: "custom_lab", want: "beacon_perf_lab_bench"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultLiveBenchmarkDatabase(tt.database); got != tt.want {
				t.Fatalf("defaultLiveBenchmarkDatabase(%q) = %q, want %q", tt.database, got, tt.want)
			}
		})
	}
}

func TestValidateLiveBenchmarkDatabaseNameRequiresSafePrefix(t *testing.T) {
	err := validateLiveBenchmarkDatabaseName("custom_lab")
	if err == nil || !strings.Contains(err.Error(), "live benchmark database") {
		t.Fatalf("validateLiveBenchmarkDatabaseName error = %v, want live database refusal", err)
	}
}

func TestValidateLabDatabaseNameRefusesInvalidPerfPrefix(t *testing.T) {
	err := validateLabDatabaseName("beacon_perf-lab", false)
	if err == nil || !strings.Contains(err.Error(), "invalid database name") {
		t.Fatalf("validateLabDatabaseName error = %v, want invalid database refusal", err)
	}

	err = validateLabDatabaseName("beacon_perf-lab", true)
	if err == nil || !strings.Contains(err.Error(), "invalid database name") {
		t.Fatalf("validateLabDatabaseName unsafe error = %v, want invalid database refusal", err)
	}
}

func TestWaitForHTTPRejectsNotFound(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	err := waitForHTTP(context.Background(), server.URL, 10*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("waitForHTTP error = %v, want 404 readiness failure", err)
	}
}

func TestMarkdownReportIncludesCoreSections(t *testing.T) {
	report := labReport{
		Schema:      reportSchema,
		Status:      "pass",
		GitRevision: "abc123",
		GitBranch:   "main",
		Dataset: datasetReport{
			Size:              "small",
			Database:          "beacon_perf_lab",
			LiveBenchDatabase: "beacon_perf_lab_bench",
			Seeded:            true,
			Sessions:          250,
			Events:            22954,
			Payloads:          14784,
			Duration:          "500ms",
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
			Iterations:   89844,
			Milliseconds: 0.013,
			BytesPerOp:   31595,
			AllocsPerOp:  117,
		}},
		Browser: &browserLabReport{Summary: []browserMetricSummary{{
			Name:     "dashboard.cold_load.ready",
			Viewport: "desktop",
			Unit:     "ms",
			Samples:  1,
			Median:   120,
			P95:      140,
			Max:      140,
		}}},
	}

	got := markdownReport(report)
	for _, want := range []string{"# Beacon Performance Lab", "## Commands", "## Go Benchmarks", "## Browser Summary", "Iterations", "Samples", "beacon_perf_lab", "beacon_perf_lab_bench"} {
		if !strings.Contains(got, want) {
			t.Fatalf("markdown report missing %q:\n%s", want, got)
		}
	}
}
