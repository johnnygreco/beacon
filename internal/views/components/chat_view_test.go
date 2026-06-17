package components

import (
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/views"
)

func TestChatViewEscapesUntrustedPayloads(t *testing.T) {
	payload := `<img src=x onerror="alert(1)"><script>alert(1)</script>`
	user := views.EventSummary{
		EventUID:    `user-" onmouseover="alert(1)`,
		EventKind:   "message",
		ActorRole:   "user",
		TextContent: "please inspect " + payload,
		Model:       `gpt-"<script>alert(1)</script>`,
	}
	assistant := views.EventSummary{
		EventUID:    `assistant-" onclick="alert(1)`,
		EventKind:   "message",
		ActorRole:   "assistant",
		TextContent: "assistant reply " + payload,
		Model:       `claude-"<img src=x onerror="alert(1)">`,
	}
	errEvent := views.EventSummary{
		EventUID:    "err-xss",
		EventKind:   "error",
		TextContent: "api failed " + payload,
	}
	toolErr := views.EventSummary{
		EventUID:    "tool-err-xss",
		EventKind:   "tool_error",
		ToolName:    `Bash"><img src=x onerror="alert(1)">`,
		TextContent: "tool failed " + payload,
	}
	resultEvent := views.EventSummary{
		EventUID:    "tool-result-xss",
		EventKind:   "tool_result",
		TextContent: `agentId: child-1`,
		TextPreview: `output ` + payload,
	}

	html := renderToString(t, ChatView([]views.ChatTurn{{
		TurnSeq: 1,
		Blocks: []views.ChatBlock{
			{Kind: views.ChatBlockUserMessage, Message: &user},
			{Kind: views.ChatBlockAssistantMessage, Message: &assistant},
			{Kind: views.ChatBlockError, Message: &errEvent},
			{Kind: views.ChatBlockToolError, Message: &toolErr},
			{
				Kind: views.ChatBlockToolChain,
				ToolChain: []views.ToolChainItem{{
					CallEvent:   views.EventSummary{EventUID: `call-" onclick="alert(1)`},
					ResultEvent: &resultEvent,
					ToolName:    `Bash"><script>alert(1)</script>`,
					OutputJSON:  `{"stdout":"` + payload + `"}`,
					Params: &views.ToolCallParams{
						Command:     `printf '` + payload + `'`,
						Description: `run ` + payload,
					},
				}},
			},
			{
				Kind: views.ChatBlockSubagentDispatch,
				ToolChain: []views.ToolChainItem{{
					CallEvent:   views.EventSummary{EventUID: "agent-call-xss"},
					ResultEvent: &resultEvent,
					ToolName:    "Agent",
					Params: &views.ToolCallParams{
						Description: `delegate ` + payload,
						Prompt:      `review ` + payload,
					},
				}},
			},
		},
	}}))

	for _, raw := range []string{"<script", "<img src=x", "<iframe", "<object"} {
		if strings.Contains(strings.ToLower(html), raw) {
			t.Fatalf("rendered transcript contains raw payload tag %q: %s", raw, html)
		}
	}
	for _, raw := range []string{`onclick="alert(1)`, `onmouseover="alert(1)`, `onerror="alert(1)`} {
		if strings.Contains(strings.ToLower(html), raw) {
			t.Fatalf("rendered transcript contains raw payload handler %q: %s", raw, html)
		}
	}
	if !strings.Contains(html, "raw HTML omitted") {
		t.Fatalf("markdown raw HTML was not omitted: %s", html)
	}
	if !strings.Contains(html, "&lt;img src=x") || !strings.Contains(html, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("tool/error payloads were not HTML-escaped: %s", html)
	}
	for _, want := range []string{
		`data-annotation-target="event"`,
		`data-annotation-event-uid="user-&#34; onmouseover=&#34;alert(1)"`,
		`data-annotation-event-uid="assistant-&#34; onclick=&#34;alert(1)"`,
		`data-annotation-event-uid="call-&#34; onclick=&#34;alert(1)"`,
		`data-annotation-event-uid="tool-result-xss"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered transcript missing annotation hook %q: %s", want, html)
		}
	}
}
