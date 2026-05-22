package partials

import (
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/views"
)

func TestDashboardMetricsRendersMetricCards(t *testing.T) {
	html := renderToString(t, DashboardMetrics([]views.MetricData{
		{Label: "Total Sessions", Value: "42", Sublabel: "all time", Trend: "up"},
		{Label: "Active Sessions", Value: "3", Trend: "down"},
	}))

	for _, want := range []string{
		"Total Sessions",
		"42",
		"all time",
		"Active Sessions",
		"3",
		"text-green-400",
		"text-red-400",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected dashboard metrics to contain %q: %s", want, html)
		}
	}
}

func TestSidebarMetricsRendersCompactStats(t *testing.T) {
	html := renderToString(t, SidebarMetrics([]views.MetricData{
		{Label: "Total Tokens", Value: "9.8K", Sublabel: "today"},
		{Label: "Custom <Metric>", Value: "7"},
	}))

	for _, want := range []string{
		"Total Tokens",
		"9.8K",
		"today",
		"Custom &lt;Metric&gt;",
		"7",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected sidebar metrics to contain %q: %s", want, html)
		}
	}
	if strings.Contains(html, "Custom <Metric>") {
		t.Fatalf("sidebar metric label was not escaped: %s", html)
	}
}
