package components

import (
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/views"
)

func TestMetricCardRendersEscapedMetricContent(t *testing.T) {
	html := renderToString(t, MetricCard(views.MetricData{
		Label:    `Total <Sessions>`,
		Value:    `42 & counting`,
		Sublabel: `<script>alert(1)</script>`,
		Trend:    "up",
	}))

	for _, want := range []string{
		"Total &lt;Sessions&gt;",
		"42 &amp; counting",
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		"text-green-400",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected rendered metric card to contain %q: %s", want, html)
		}
	}
	if strings.Contains(strings.ToLower(html), "<script>") {
		t.Fatalf("metric content was not escaped: %s", html)
	}
}

func TestMetricCardTrendStates(t *testing.T) {
	tests := []struct {
		name       string
		trend      string
		wantClass  string
		avoidClass string
	}{
		{name: "up", trend: "up", wantClass: "text-green-400", avoidClass: "text-red-400"},
		{name: "down", trend: "down", wantClass: "text-red-400", avoidClass: "text-green-400"},
		{name: "neutral", trend: "neutral", avoidClass: "text-green-400"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html := renderToString(t, MetricCard(views.MetricData{
				Label: "Tokens",
				Value: "1.2K",
				Trend: tt.trend,
			}))

			if tt.wantClass != "" && !strings.Contains(html, tt.wantClass) {
				t.Fatalf("expected trend class %q in rendered metric card: %s", tt.wantClass, html)
			}
			if tt.avoidClass != "" && strings.Contains(html, tt.avoidClass) {
				t.Fatalf("did not expect trend class %q in rendered metric card: %s", tt.avoidClass, html)
			}
		})
	}
}
