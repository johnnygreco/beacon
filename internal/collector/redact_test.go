package collector

import (
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/models"
)

func TestRedactEventsHandlesQuotedJSONSecrets(t *testing.T) {
	events := RedactEvents([]capture.NormalizedEvent{{
		SessionID:    "session",
		SourceName:   "codex",
		Runtime:      models.RuntimeCodex,
		Provider:     models.ProviderOpenAI,
		Format:       models.FormatJSONL,
		EventKind:    models.EventKindToolCall,
		ActorRole:    models.ActorRoleAssistant,
		Timestamp:    time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
		TextContent:  `{"password":"text secret with space"}`,
		ToolInput:    `{"api_key":"tool,input,secret","nested":"ok"}`,
		ToolOutput:   `{\"token\":\"tool-output secret,with comma\"}`,
		ErrorMessage: `secret = error-secret`,
		RawPayload:   `{"api_key":"raw \"quoted\" secret","password": "raw-password"}`,
	}})
	got := events[0].TextContent + "\n" + events[0].ToolInput + "\n" + events[0].ToolOutput + "\n" + events[0].ErrorMessage + "\n" + events[0].RawPayload
	for _, leaked := range []string{"text secret", "tool,input", "tool-output secret", "error-secret", `raw \"quoted\" secret`, "raw-password"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted event leaked %q: %s", leaked, got)
		}
	}
	if strings.Count(got, "[REDACTED_SECRET]") < 6 {
		t.Fatalf("redacted event missing replacement markers: %s", got)
	}
}

func TestRedactCaptureErrorsHandlesQuotedJSONSecrets(t *testing.T) {
	errs := RedactCaptureErrors([]models.CaptureError{{
		ID:              "err",
		SourceName:      "codex",
		SourceFile:      "session.jsonl",
		ErrorClass:      "parse_error",
		ErrorMessage:    `{"password":"message secret,with comma"}`,
		ContextFragment: `{\"api_key\":\"context secret,with comma\"}`,
	}})
	got := errs[0].ErrorMessage + "\n" + errs[0].ContextFragment
	if strings.Contains(got, "message secret") || strings.Contains(got, "context secret") {
		t.Fatalf("redacted capture error leaked secret: %s", got)
	}
	if strings.Count(got, "[REDACTED_SECRET]") < 2 {
		t.Fatalf("redacted capture error missing replacement markers: %s", got)
	}
}
