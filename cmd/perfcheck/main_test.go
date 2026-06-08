package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPassesBudgetedReport(t *testing.T) {
	path := writeReport(t, passingReport())

	err := run(config{reportPath: path, maxRegressionRatio: 1.25, minBrowserDelta: 5, minGoDeltaMS: 0.05, failOnMissing: true})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunFailsMissingBudgetedMetrics(t *testing.T) {
	report := passingReport()
	report.Browser = nil
	path := writeReport(t, report)

	err := run(config{reportPath: path, maxRegressionRatio: 1.25, minBrowserDelta: 5, minGoDeltaMS: 0.05, failOnMissing: true})
	if err == nil || !strings.Contains(err.Error(), "performance check") {
		t.Fatalf("run() error = %v, want missing metric failure", err)
	}
}

func TestRunDetectsComparisonRegression(t *testing.T) {
	baseline := passingReport()
	current := passingReport()
	current.Browser.Summary[0].P95 = baseline.Browser.Summary[0].P95*2 + 10
	current.Browser.Summary[0].Max = current.Browser.Summary[0].P95

	baselinePath := writeReport(t, baseline)
	currentPath := writeReport(t, current)

	err := run(config{
		reportPath:         currentPath,
		baselinePath:       baselinePath,
		maxRegressionRatio: 1.25,
		minBrowserDelta:    5,
		minGoDeltaMS:       0.05,
		failOnMissing:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "performance check") {
		t.Fatalf("run() error = %v, want comparison failure", err)
	}
}

func TestRunFailsFailedBaselineReport(t *testing.T) {
	baseline := passingReport()
	baseline.Status = "fail"
	current := passingReport()

	baselinePath := writeReport(t, baseline)
	currentPath := writeReport(t, current)

	err := run(config{
		reportPath:         currentPath,
		baselinePath:       baselinePath,
		maxRegressionRatio: 1.25,
		minBrowserDelta:    5,
		minGoDeltaMS:       0.05,
		failOnMissing:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "performance check") {
		t.Fatalf("run() error = %v, want failed baseline failure", err)
	}
}

func TestRunFailsInvalidBaselineSchema(t *testing.T) {
	baseline := passingReport()
	baseline.Schema = "beacon.browser_performance.v1"
	current := passingReport()

	baselinePath := writeReport(t, baseline)
	currentPath := writeReport(t, current)

	err := run(config{
		reportPath:         currentPath,
		baselinePath:       baselinePath,
		maxRegressionRatio: 1.25,
		minBrowserDelta:    5,
		minGoDeltaMS:       0.05,
		failOnMissing:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "performance check") {
		t.Fatalf("run() error = %v, want invalid baseline schema failure", err)
	}
}

func TestRunFailsSeededReportMissingFleetMetadata(t *testing.T) {
	report := passingReport()
	report.Dataset.Seeded = true
	report.Dataset.Sessions = 250
	report.Dataset.Events = 1000
	report.Dataset.Payloads = 100
	report.Dataset.SearchPostings = 5000
	path := writeReport(t, report)

	err := run(config{reportPath: path, maxRegressionRatio: 1.25, minBrowserDelta: 5, minGoDeltaMS: 0.05, failOnMissing: true})
	if err == nil || !strings.Contains(err.Error(), "performance check") {
		t.Fatalf("run() error = %v, want missing seeded metadata failure", err)
	}
}

func TestRunFailsMismatchedDatasetSize(t *testing.T) {
	baseline := passingReport()
	baseline.Dataset.Size = "medium"
	current := passingReport()

	baselinePath := writeReport(t, baseline)
	currentPath := writeReport(t, current)

	err := run(config{
		reportPath:         currentPath,
		baselinePath:       baselinePath,
		maxRegressionRatio: 1.25,
		minBrowserDelta:    5,
		minGoDeltaMS:       0.05,
		failOnMissing:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "performance check") {
		t.Fatalf("run() error = %v, want dataset mismatch failure", err)
	}
}

func TestRunAllowsMismatchedDatasetSizeWithOverride(t *testing.T) {
	baseline := passingReport()
	baseline.Dataset.Size = "medium"
	current := passingReport()

	baselinePath := writeReport(t, baseline)
	currentPath := writeReport(t, current)

	err := run(config{
		reportPath:           currentPath,
		baselinePath:         baselinePath,
		maxRegressionRatio:   1.25,
		minBrowserDelta:      5,
		minGoDeltaMS:         0.05,
		failOnMissing:        true,
		allowDatasetMismatch: true,
	})
	if err != nil {
		t.Fatalf("run() error = %v, want override to allow mismatched dataset comparison", err)
	}
}

func TestRunFailsNoOverlappingBaselineMetrics(t *testing.T) {
	baseline := passingReport()
	baseline.Browser.Summary = nil
	baseline.GoBenchmarks = nil
	current := passingReport()

	baselinePath := writeReport(t, baseline)
	currentPath := writeReport(t, current)

	err := run(config{
		reportPath:         currentPath,
		baselinePath:       baselinePath,
		maxRegressionRatio: 1.25,
		minBrowserDelta:    5,
		minGoDeltaMS:       0.05,
		failOnMissing:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "performance check") {
		t.Fatalf("run() error = %v, want no-overlap comparison failure", err)
	}
}

func TestEnvFloatHelpers(t *testing.T) {
	t.Setenv("PERF_RATIO", "0")
	t.Setenv("PERF_DELTA", "0")
	t.Setenv("PERF_BAD", "-1")

	if got := envPositiveFloat("PERF_RATIO", 1.25); got != 1.25 {
		t.Fatalf("envPositiveFloat zero = %v, want fallback", got)
	}
	if got := envNonNegativeFloat("PERF_DELTA", 5); got != 0 {
		t.Fatalf("envNonNegativeFloat zero = %v, want 0", got)
	}
	if got := envNonNegativeFloat("PERF_BAD", 5); got != 5 {
		t.Fatalf("envNonNegativeFloat negative = %v, want fallback", got)
	}
}

func passingReport() labReport {
	report := labReport{
		Schema:      reportSchema,
		Status:      "pass",
		GitRevision: "testrev",
		GitBranch:   "test",
		Dataset: datasetReport{
			Size:     "small",
			Database: "beacon_perf_lab",
		},
		Browser: &browserReport{},
	}
	for _, budget := range defaultBrowserBudgets() {
		value := budget.Limit / 2
		report.Browser.Summary = append(report.Browser.Summary, browserMetric{
			Name:     budget.Name,
			Viewport: budget.Viewport,
			Unit:     budget.Unit,
			Samples:  3,
			Median:   value,
			P95:      value,
			Max:      value,
		})
	}
	for _, budget := range defaultGoBudgets() {
		report.GoBenchmarks = append(report.GoBenchmarks, benchmarkReport{
			Source:       budget.Source,
			Name:         budget.Name,
			Iterations:   10,
			Milliseconds: budget.Limit / 2,
		})
	}
	return report
}

func writeReport(t *testing.T, report labReport) string {
	t.Helper()
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	path := filepath.Join(t.TempDir(), "perf-lab-report.json")
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	return path
}
