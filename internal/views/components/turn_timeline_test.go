package components

import (
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/views"
)

func TestTurnTimelineRendersTurnAndEventDetails(t *testing.T) {
	html := renderToString(t, TurnTimeline([]views.TurnDetail{{
		TurnSeq:     2,
		TotalTokens: 2_048,
		Events: []views.EventSummary{
			{
				EventKind:   "message",
				ActorRole:   "user",
				TextPreview: "please inspect <script>alert(1)</script>",
			},
			{
				EventKind:  "tool_call",
				ActorRole:  "tool",
				ToolName:   "Bash",
				Model:      "gpt-5.4",
				Tokens:     1_024,
				DurationMs: 125,
			},
			{
				EventKind: "custom_event",
				ActorRole: "planner",
			},
		},
	}}))

	for _, want := range []string{
		"Turn 2",
		"3 events",
		"2.0K tokens",
		"user",
		"tool_call",
		"Bash",
		"gpt-5.4",
		"1.0K tokens",
		"125ms",
		"planner",
		"custom_event",
		"please inspect &lt;script&gt;alert(1)&lt;/script&gt;",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected timeline to contain %q: %s", want, html)
		}
	}
	if strings.Contains(strings.ToLower(html), "<script>") {
		t.Fatalf("timeline preview was not escaped: %s", html)
	}
}
