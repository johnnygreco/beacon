package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

type config struct {
	reportPath         string
	baselinePath       string
	maxRegressionRatio float64
	minBrowserDelta    float64
	minGoDeltaMS       float64
	failOnMissing      bool
}

type labReport struct {
	Schema       string            `json:"schema"`
	Status       string            `json:"status"`
	GitRevision  string            `json:"git_revision"`
	GitBranch    string            `json:"git_branch"`
	Dataset      datasetReport     `json:"dataset"`
	GoBenchmarks []benchmarkReport `json:"go_benchmarks"`
	Browser      *browserReport    `json:"browser,omitempty"`
}

type datasetReport struct {
	Size              string `json:"size"`
	Database          string `json:"database"`
	LiveBenchDatabase string `json:"live_benchmark_database,omitempty"`
}

type benchmarkReport struct {
	Source       string  `json:"source"`
	Name         string  `json:"name"`
	Iterations   int64   `json:"iterations"`
	Milliseconds float64 `json:"milliseconds_per_op"`
}

type browserReport struct {
	Summary []browserMetric `json:"summary"`
}

type browserMetric struct {
	Name     string  `json:"name"`
	Viewport string  `json:"viewport"`
	Unit     string  `json:"unit"`
	Samples  int     `json:"samples"`
	Median   float64 `json:"median"`
	P95      float64 `json:"p95"`
	Max      float64 `json:"max"`
}

type browserBudget struct {
	Name     string
	Viewport string
	Stat     string
	Limit    float64
	Unit     string
}

type goBudget struct {
	Source string
	Name   string
	Limit  float64
}

type checkResult struct {
	Status string
	Text   string
}

func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "perf check failed: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.reportPath, "report", envString("PERF_REPORT", "test-results/perf/lab/latest/perf-lab-report.json"), "Perf lab report JSON to check")
	flag.StringVar(&cfg.baselinePath, "baseline", os.Getenv("PERF_BASELINE"), "Optional baseline perf lab report JSON for comparison")
	flag.Float64Var(&cfg.maxRegressionRatio, "max-regression-ratio", envPositiveFloat("PERF_MAX_REGRESSION_RATIO", 1.25), "Allowed current/baseline ratio before comparison fails")
	flag.Float64Var(&cfg.minBrowserDelta, "min-browser-regression", envNonNegativeFloat("PERF_MIN_BROWSER_REGRESSION", 5), "Minimum browser metric delta before comparison fails")
	flag.Float64Var(&cfg.minGoDeltaMS, "min-go-regression-ms", envNonNegativeFloat("PERF_MIN_GO_REGRESSION_MS", 0.05), "Minimum Go benchmark ms/op delta before comparison fails")
	flag.BoolVar(&cfg.failOnMissing, "fail-on-missing", envBool("PERF_FAIL_ON_MISSING", true), "Fail when budgeted metrics are missing")
	flag.Parse()
	return cfg
}

func run(cfg config) error {
	report, err := readReport(cfg.reportPath)
	if err != nil {
		return err
	}

	var results []checkResult
	if report.Status != "pass" {
		results = append(results, failf("report status is %q, want pass", report.Status))
	}
	results = append(results, checkBrowserBudgets(report, cfg.failOnMissing)...)
	results = append(results, checkGoBudgets(report, cfg.failOnMissing)...)

	if strings.TrimSpace(cfg.baselinePath) != "" {
		baseline, err := readReport(cfg.baselinePath)
		if err != nil {
			return fmt.Errorf("read baseline: %w", err)
		}
		results = append(results, compareReports(report, baseline, cfg)...)
	}

	failures := printResults(cfg, report, results)
	if failures > 0 {
		return fmt.Errorf("%d performance check(s) failed", failures)
	}
	return nil
}

func readReport(path string) (*labReport, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read report %s: %w", path, err)
	}
	var report labReport
	if err := json.Unmarshal(body, &report); err != nil {
		return nil, fmt.Errorf("parse report %s: %w", path, err)
	}
	return &report, nil
}

func printResults(cfg config, report *labReport, results []checkResult) int {
	failures := 0
	fmt.Printf("Beacon perf check: %s (%s on %s, dataset=%s/%s)\n",
		cfg.reportPath,
		emptyAs(report.GitRevision, "unknown"),
		emptyAs(report.GitBranch, "unknown"),
		emptyAs(report.Dataset.Size, "unknown"),
		emptyAs(report.Dataset.Database, "unknown"),
	)
	for _, result := range results {
		fmt.Printf("%s %s\n", result.Status, result.Text)
		if result.Status == "FAIL" {
			failures++
		}
	}
	if failures == 0 {
		fmt.Println("Beacon perf check: pass")
	} else {
		fmt.Printf("Beacon perf check: fail (%d failure(s))\n", failures)
	}
	return failures
}

func checkBrowserBudgets(report *labReport, failOnMissing bool) []checkResult {
	index := browserIndex(report)
	results := make([]checkResult, 0, len(defaultBrowserBudgets()))
	for _, budget := range defaultBrowserBudgets() {
		metric, ok := index[browserKey(budget.Name, budget.Viewport)]
		if !ok {
			results = append(results, missingResult(failOnMissing, "browser %s/%s", budget.Name, budget.Viewport))
			continue
		}
		actual := browserStat(metric, budget.Stat)
		if actual > budget.Limit {
			results = append(results, failf("browser %s/%s %s %.2f%s > budget %.2f%s", budget.Name, budget.Viewport, budget.Stat, actual, unitSuffix(budget.Unit), budget.Limit, unitSuffix(budget.Unit)))
			continue
		}
		results = append(results, passf("browser %s/%s %s %.2f%s <= %.2f%s", budget.Name, budget.Viewport, budget.Stat, actual, unitSuffix(budget.Unit), budget.Limit, unitSuffix(budget.Unit)))
	}
	return results
}

func checkGoBudgets(report *labReport, failOnMissing bool) []checkResult {
	index := goIndex(report)
	results := make([]checkResult, 0, len(defaultGoBudgets()))
	for _, budget := range defaultGoBudgets() {
		benchmark, ok := index[goKey(budget.Source, budget.Name)]
		if !ok {
			results = append(results, missingResult(failOnMissing, "go %s/%s", budget.Source, budget.Name))
			continue
		}
		if benchmark.Milliseconds > budget.Limit {
			results = append(results, failf("go %s/%s %.3fms/op > budget %.3fms/op", budget.Source, budget.Name, benchmark.Milliseconds, budget.Limit))
			continue
		}
		results = append(results, passf("go %s/%s %.3fms/op <= %.3fms/op", budget.Source, budget.Name, benchmark.Milliseconds, budget.Limit))
	}
	return results
}

func compareReports(current, baseline *labReport, cfg config) []checkResult {
	var results []checkResult
	results = append(results, compareBrowser(current, baseline, cfg)...)
	results = append(results, compareGo(current, baseline, cfg)...)
	if len(results) == 0 {
		return []checkResult{warnf("comparison found no overlapping metrics")}
	}
	return results
}

func compareBrowser(current, baseline *labReport, cfg config) []checkResult {
	currentIndex := browserIndex(current)
	baselineIndex := browserIndex(baseline)
	keys := sortedCommonKeys(currentIndex, baselineIndex)
	results := make([]checkResult, 0, len(keys))
	for _, key := range keys {
		cur := currentIndex[key]
		base := baselineIndex[key]
		stat := "p95"
		curValue := cur.P95
		baseValue := base.P95
		minDelta := browserMinDelta(cur.Unit, cfg.minBrowserDelta)
		if regression(curValue, baseValue, cfg.maxRegressionRatio, minDelta) {
			results = append(results, failf("compare browser %s/%s %s %.2f%s vs %.2f%s (%s)", cur.Name, cur.Viewport, stat, curValue, unitSuffix(cur.Unit), baseValue, unitSuffix(cur.Unit), ratioText(curValue, baseValue)))
			continue
		}
		results = append(results, passf("compare browser %s/%s %s %.2f%s vs %.2f%s (%s)", cur.Name, cur.Viewport, stat, curValue, unitSuffix(cur.Unit), baseValue, unitSuffix(cur.Unit), ratioText(curValue, baseValue)))
	}
	return results
}

func compareGo(current, baseline *labReport, cfg config) []checkResult {
	currentIndex := goIndex(current)
	baselineIndex := goIndex(baseline)
	keys := sortedCommonKeys(currentIndex, baselineIndex)
	results := make([]checkResult, 0, len(keys))
	for _, key := range keys {
		cur := currentIndex[key]
		base := baselineIndex[key]
		if regression(cur.Milliseconds, base.Milliseconds, cfg.maxRegressionRatio, cfg.minGoDeltaMS) {
			results = append(results, failf("compare go %s/%s %.3fms/op vs %.3fms/op (%s)", cur.Source, cur.Name, cur.Milliseconds, base.Milliseconds, ratioText(cur.Milliseconds, base.Milliseconds)))
			continue
		}
		results = append(results, passf("compare go %s/%s %.3fms/op vs %.3fms/op (%s)", cur.Source, cur.Name, cur.Milliseconds, base.Milliseconds, ratioText(cur.Milliseconds, base.Milliseconds)))
	}
	return results
}

func defaultBrowserBudgets() []browserBudget {
	return []browserBudget{
		{Name: "dashboard.cold_load.ready", Viewport: "desktop", Stat: "p95", Limit: 600, Unit: "ms"},
		{Name: "dashboard.cold_load.ready", Viewport: "mobile", Stat: "p95", Limit: 700, Unit: "ms"},
		{Name: "dashboard.cold_load.api_max", Viewport: "desktop", Stat: "p95", Limit: 250, Unit: "ms"},
		{Name: "dashboard.cold_load.api_max", Viewport: "mobile", Stat: "p95", Limit: 300, Unit: "ms"},
		{Name: "dashboard.warm_reload.ready", Viewport: "desktop", Stat: "p95", Limit: 300, Unit: "ms"},
		{Name: "dashboard.warm_reload.ready", Viewport: "mobile", Stat: "p95", Limit: 350, Unit: "ms"},
		{Name: "search.session.input_to_rows", Viewport: "desktop", Stat: "p95", Limit: 700, Unit: "ms"},
		{Name: "search.session.input_to_rows", Viewport: "mobile", Stat: "p95", Limit: 800, Unit: "ms"},
		{Name: "search.event.input_to_rows", Viewport: "desktop", Stat: "p95", Limit: 800, Unit: "ms"},
		{Name: "search.event.input_to_rows", Viewport: "mobile", Stat: "p95", Limit: 900, Unit: "ms"},
		{Name: "interaction.chart_range_to_paint", Viewport: "desktop", Stat: "p95", Limit: 350, Unit: "ms"},
		{Name: "interaction.chart_range_to_paint", Viewport: "mobile", Stat: "p95", Limit: 400, Unit: "ms"},
		{Name: "interaction.active_sort_to_paint", Viewport: "desktop", Stat: "p95", Limit: 150, Unit: "ms"},
		{Name: "interaction.active_sort_to_paint", Viewport: "mobile", Stat: "p95", Limit: 180, Unit: "ms"},
		{Name: "interaction.inspector_open.ready", Viewport: "desktop", Stat: "p95", Limit: 180, Unit: "ms"},
		{Name: "interaction.inspector_open.ready", Viewport: "mobile", Stat: "p95", Limit: 220, Unit: "ms"},
		{Name: "browser.long_tasks.max", Viewport: "desktop", Stat: "max", Limit: 50, Unit: "ms"},
		{Name: "browser.long_tasks.max", Viewport: "mobile", Stat: "max", Limit: 50, Unit: "ms"},
		{Name: "browser.layout_shift.cumulative", Viewport: "desktop", Stat: "max", Limit: 0.10, Unit: "score"},
		{Name: "browser.layout_shift.cumulative", Viewport: "mobile", Stat: "max", Limit: 0.15, Unit: "score"},
	}
}

func defaultGoBudgets() []goBudget {
	return []goBudget{
		{Source: "fast", Name: "BenchmarkCaptureParseClaudeJSONL", Limit: 0.050},
		{Source: "fast", Name: "BenchmarkCaptureParseCodexJSONL", Limit: 0.075},
		{Source: "fast", Name: "BenchmarkCaptureBuildInsertRowBatch", Limit: 1.500},
		{Source: "fast", Name: "BenchmarkStoreBuildSearchRows/1000Events", Limit: 12.000},
		{Source: "fast", Name: "BenchmarkStoreBuildSearchRows/5000Events", Limit: 75.000},
		{Source: "fast", Name: "BenchmarkTextIndexTokenize/Transcript", Limit: 0.150},
		{Source: "fast", Name: "BenchmarkAPIJSONSerialization/DashboardSessions", Limit: 0.600},
		{Source: "fast", Name: "BenchmarkDashboardSearchResultShaping", Limit: 0.150},
		{Source: "fast", Name: "BenchmarkMCPDispatchSearchWithFakeResults", Limit: 0.050},
		{Source: "fast", Name: "BenchmarkViewRenderDashboard", Limit: 0.100},
		{Source: "fast", Name: "BenchmarkViewRenderChatTranscript", Limit: 2.500},
		{Source: "live", Name: "BenchmarkSearchBM25", Limit: 30.000},
		{Source: "live", Name: "BenchmarkSearchKeyword", Limit: 25.000},
		{Source: "live", Name: "BenchmarkSearchBrowse", Limit: 8.000},
		{Source: "live", Name: "BenchmarkMCPToolSearchSessions", Limit: 30.000},
		{Source: "live", Name: "BenchmarkMCPToolOpen", Limit: 25.000},
		{Source: "live", Name: "BenchmarkMCPToolListSessions", Limit: 8.000},
	}
}

func browserIndex(report *labReport) map[string]browserMetric {
	index := make(map[string]browserMetric)
	if report.Browser == nil {
		return index
	}
	for _, metric := range report.Browser.Summary {
		index[browserKey(metric.Name, metric.Viewport)] = metric
	}
	return index
}

func goIndex(report *labReport) map[string]benchmarkReport {
	index := make(map[string]benchmarkReport)
	for _, benchmark := range report.GoBenchmarks {
		index[goKey(benchmark.Source, benchmark.Name)] = benchmark
	}
	return index
}

func browserKey(name, viewport string) string {
	return name + "\t" + viewport
}

func goKey(source, name string) string {
	return source + "\t" + name
}

func browserStat(metric browserMetric, stat string) float64 {
	switch stat {
	case "median":
		return metric.Median
	case "max":
		return metric.Max
	default:
		return metric.P95
	}
}

func browserMinDelta(unit string, fallback float64) float64 {
	switch unit {
	case "score":
		return 0.02
	case "count":
		return 1
	default:
		return fallback
	}
}

func regression(current, baseline, maxRatio, minDelta float64) bool {
	if baseline <= 0 {
		return current > baseline+minDelta
	}
	return current > baseline+minDelta && current/baseline > maxRatio
}

func ratioText(current, baseline float64) string {
	if baseline == 0 {
		if current == 0 {
			return "0.0%"
		}
		return "+inf"
	}
	pct := (current/baseline - 1) * 100
	return fmt.Sprintf("%+.1f%%", pct)
}

func sortedCommonKeys[V any](current, baseline map[string]V) []string {
	var keys []string
	for key := range current {
		if _, ok := baseline[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func missingResult(failOnMissing bool, format string, args ...any) checkResult {
	msg := fmt.Sprintf("missing "+format, args...)
	if failOnMissing {
		return checkResult{Status: "FAIL", Text: msg}
	}
	return checkResult{Status: "WARN", Text: msg}
}

func passf(format string, args ...any) checkResult {
	return checkResult{Status: "PASS", Text: fmt.Sprintf(format, args...)}
}

func failf(format string, args ...any) checkResult {
	return checkResult{Status: "FAIL", Text: fmt.Sprintf(format, args...)}
}

func warnf(format string, args ...any) checkResult {
	return checkResult{Status: "WARN", Text: fmt.Sprintf(format, args...)}
}

func unitSuffix(unit string) string {
	if unit == "" || unit == "score" || unit == "count" {
		return ""
	}
	return unit
}

func emptyAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envPositiveFloat(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envNonNegativeFloat(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || parsed < 0 {
		return fallback
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
