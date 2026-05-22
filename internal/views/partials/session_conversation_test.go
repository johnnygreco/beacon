package partials

import (
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/views"
)

func TestSessionConversationRendersChatAndTimelineViews(t *testing.T) {
	html := renderToString(t, SessionConversation(
		[]views.ChatTurn{{
			TurnSeq: 1,
			Blocks: []views.ChatBlock{{
				Kind: views.ChatBlockUserMessage,
				Message: &views.EventSummary{
					EventUID:    "event-user-1",
					ActorRole:   "user",
					EventKind:   "message",
					TextContent: "show the dashboard",
				},
			}},
		}},
		[]views.TurnDetail{{
			TurnSeq:     1,
			TotalTokens: 125,
			Events: []views.EventSummary{{
				ActorRole:   "assistant",
				EventKind:   "message",
				TextPreview: "dashboard ready",
			}},
		}},
	))

	for _, want := range []string{
		`id="chat-view"`,
		`id="timeline-view"`,
		"show the dashboard",
		"Turn 1",
		"1 events",
		"125 tokens",
		"dashboard ready",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected session conversation to contain %q: %s", want, html)
		}
	}
}
