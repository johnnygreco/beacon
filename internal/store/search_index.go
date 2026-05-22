package store

import (
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/textindex"
)

func buildSearchRows(events []models.Event, payloads []models.ToolPayload) ([]models.SearchDocument, []models.SearchPosting) {
	payloadByEvent := make(map[string]models.ToolPayload, len(payloads))
	for _, payload := range payloads {
		payloadByEvent[payload.EventUID] = payload
	}

	now := time.Now().UTC()
	var docs []models.SearchDocument
	var postings []models.SearchPosting
	for _, event := range events {
		text := searchableText(event, payloadByEvent[event.EventUID])
		tokens := textindex.Tokenize(text)
		if len(tokens) == 0 {
			continue
		}
		preview := searchPreview(event, payloadByEvent[event.EventUID])
		doc := models.SearchDocument{
			EventUID:       event.EventUID,
			SessionID:      event.SessionID,
			EventKind:      event.EventKind,
			Timestamp:      event.Timestamp,
			TextPreview:    preview,
			ToolName:       event.ToolName,
			Model:          event.Model,
			Provider:       event.Provider,
			SearchableText: text,
			DocumentLength: len(tokens),
			UpdatedAt:      now,
		}
		docs = append(docs, doc)
		for token, frequency := range textindex.Frequencies(tokens) {
			postings = append(postings, models.SearchPosting{
				Token:          token,
				EventUID:       event.EventUID,
				SessionID:      event.SessionID,
				EventKind:      event.EventKind,
				Timestamp:      event.Timestamp,
				TermFrequency:  frequency,
				DocumentLength: len(tokens),
				TextPreview:    preview,
				ToolName:       event.ToolName,
				Model:          event.Model,
				Provider:       event.Provider,
				UpdatedAt:      now,
			})
		}
	}
	return docs, postings
}

func searchPreview(event models.Event, payload models.ToolPayload) string {
	eventPreview := strings.TrimSpace(event.TextPreview)
	if !isToolEvent(event) {
		return eventPreview
	}
	if eventPreview != "" && eventPreview != strings.TrimSpace(event.ToolName) {
		return truncateString(eventPreview, 512)
	}
	candidates := []string{payload.InputPreview, payload.OutputPreview}
	if event.EventKind == "tool_result" || event.EventKind == "tool_error" {
		candidates = []string{payload.OutputPreview, payload.InputPreview}
	}
	for _, candidate := range candidates {
		preview := strings.TrimSpace(candidate)
		if preview != "" {
			return truncateString(preview, 512)
		}
	}
	return eventPreview
}

func isToolEvent(event models.Event) bool {
	return event.ToolName != "" || strings.HasPrefix(event.EventKind, "tool_")
}

func searchableText(event models.Event, payload models.ToolPayload) string {
	parts := []string{
		event.EventKind,
		event.PayloadType,
		event.ActorRole,
		event.TextContent,
		event.TextPreview,
		event.ToolName,
		event.Model,
		event.ErrorCode,
		event.ErrorMessage,
		payload.InputPreview,
		payload.OutputPreview,
	}
	text := strings.Join(parts, " ")
	return truncateString(strings.TrimSpace(text), 4096)
}
