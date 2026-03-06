package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderToString(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestChartContainer_NoToggle(t *testing.T) {
	html := renderToString(t, ChartContainer("myChart", "300px", "My Chart"))

	if !strings.Contains(html, `id="myChart"`) {
		t.Error("expected canvas with id=myChart")
	}
	if !strings.Contains(html, "My Chart") {
		t.Error("expected title in output")
	}
	if strings.Contains(html, "log-toggle") {
		t.Error("should not contain log toggle when not requested")
	}
	if strings.Contains(html, "Log Scale") {
		t.Error("should not contain 'Log Scale' button text when not requested")
	}
}

func TestChartContainerWithOptions_LogToggleEnabled(t *testing.T) {
	html := renderToString(t, ChartContainerWithOptions("tokensChart", "250px", "Token Throughput", true))

	if !strings.Contains(html, `id="tokensChart"`) {
		t.Error("expected canvas with id=tokensChart")
	}
	if !strings.Contains(html, "Token Throughput") {
		t.Error("expected title in output")
	}
	if !strings.Contains(html, `id="tokensChart-log-toggle"`) {
		t.Error("expected log toggle button with correct id")
	}
	if !strings.Contains(html, "Log Scale") {
		t.Error("expected 'Log Scale' button text")
	}
	if !strings.Contains(html, `data-log-active="false"`) {
		t.Error("expected data-log-active=false initially")
	}
	if !strings.Contains(html, `data-chart-id="tokensChart"`) {
		t.Error("expected data-chart-id attribute")
	}
}

func TestChartContainerWithOptions_LogToggleDisabled(t *testing.T) {
	html := renderToString(t, ChartContainerWithOptions("chart1", "200px", "Title", false))

	if !strings.Contains(html, `id="chart1"`) {
		t.Error("expected canvas with id=chart1")
	}
	if strings.Contains(html, "log-toggle") {
		t.Error("should not contain log toggle when logToggle is false")
	}
}

func TestChartContainerWithOptions_EmptyTitle(t *testing.T) {
	html := renderToString(t, ChartContainerWithOptions("chart2", "150px", "", true))

	if !strings.Contains(html, `id="chart2"`) {
		t.Error("expected canvas with id=chart2")
	}
	// Toggle should still be present even without title
	if !strings.Contains(html, "Log Scale") {
		t.Error("expected log toggle even with empty title")
	}
}

func TestChartContainerWithOptions_ToggleScriptGenerated(t *testing.T) {
	html := renderToString(t, ChartContainerWithOptions("myChart", "300px", "Test", true))

	// The templ script component should generate JS for toggling
	if !strings.Contains(html, "onclick") {
		t.Error("expected onclick handler on toggle button")
	}
}

func TestChartContainer_DelegatesToWithOptions(t *testing.T) {
	// ChartContainer should produce the same output as ChartContainerWithOptions with logToggle=false
	html1 := renderToString(t, ChartContainer("chart", "300px", "Title"))
	html2 := renderToString(t, ChartContainerWithOptions("chart", "300px", "Title", false))

	if html1 != html2 {
		t.Error("ChartContainer should delegate to ChartContainerWithOptions with logToggle=false")
	}
}
