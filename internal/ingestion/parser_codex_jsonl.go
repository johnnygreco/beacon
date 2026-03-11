package ingestion

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// ParseCodexJSONL parses a single JSONL line from Codex logs.
// Codex writes lines with: type, payload, timestamp, etc.
func ParseCodexJSONL(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, err
	}

	// Session ID from top-level or from filename
	sessionID := jsonStr(raw, "session_id")
	if sessionID == "" {
		base := filepath.Base(file)
		sessionID = strings.TrimSuffix(base, filepath.Ext(base))
	}

	ts := parseTimestamp(jsonStr(raw, "timestamp"))

	eventType := jsonStr(raw, "type")

	base := NormalizedEvent{
		SessionID:    sessionID,
		SourceName:   "codex",
		Provider:     "openai",
		Timestamp:    ts,
		SourceFile:   file,
		SourceLineNo: lineNo,
		SourceOffset: offset,
		RawPayload:   string(line),
	}

	payload, _ := raw["payload"].(map[string]any)
	if payload == nil {
		payload = raw // fallback: fields at top level
	}

	var events []NormalizedEvent

	switch eventType {
	case "session_meta":
		evt := base
		evt.EventKind = "session_meta"
		evt.ActorRole = "system"
		evt.TextContent = jsonStr(payload, "description")
		events = append(events, evt)

	case "turn_context":
		evt := base
		evt.EventKind = "turn_context"
		evt.ActorRole = "system"
		events = append(events, evt)

	case "response_item":
		payloadType := jsonStr(payload, "type")
		switch payloadType {
		case "message":
			evt := base
			evt.EventKind = "message"
			evt.ActorRole = jsonStr(payload, "role")
			if evt.ActorRole == "" {
				evt.ActorRole = "assistant"
			}
			// Extract text from content array
			if content, ok := payload["content"].([]any); ok {
				for _, c := range content {
					if cm, ok := c.(map[string]any); ok {
						if jsonStr(cm, "type") == "output_text" || jsonStr(cm, "type") == "text" {
							evt.TextContent = jsonStr(cm, "text")
						}
					}
				}
			}
			events = append(events, evt)

		case "function_call":
			evt := base
			evt.EventKind = "tool_call"
			evt.ActorRole = "assistant"
			evt.ToolName = jsonStr(payload, "name")
			evt.ToolPhase = "call"
			evt.ToolInput = jsonStr(payload, "arguments")
			evt.ToolUseID = jsonStr(payload, "call_id")
			evt.TextContent = evt.ToolName
			events = append(events, evt)

		case "function_call_output":
			evt := base
			evt.EventKind = "tool_result"
			evt.ActorRole = "tool"
			evt.ToolName = jsonStr(payload, "name")
			evt.ToolPhase = "result"
			evt.ToolOutput = jsonStr(payload, "output")
			evt.ToolUseID = jsonStr(payload, "call_id")
			evt.TextContent = jsonStr(payload, "output")
			events = append(events, evt)

		case "reasoning":
			evt := base
			evt.EventKind = "reasoning"
			evt.ActorRole = "assistant"
			if summary, ok := payload["summary"].([]any); ok {
				for _, s := range summary {
					if sm, ok := s.(map[string]any); ok {
						evt.TextContent += jsonStr(sm, "text")
					}
				}
			}
			events = append(events, evt)

		default:
			evt := base
			evt.EventKind = "event_msg"
			evt.PayloadType = payloadType
			events = append(events, evt)
		}

	case "event_msg":
		evt := base
		evt.EventKind = "event_msg"
		evt.PayloadType = jsonStr(payload, "type")
		evt.TextContent = jsonStr(payload, "message")
		events = append(events, evt)

	case "compacted":
		evt := base
		evt.EventKind = "context_snapshot"
		evt.ActorRole = "system"
		events = append(events, evt)

	case "error":
		evt := base
		evt.EventKind = "error"
		evt.ActorRole = "system"
		evt.ErrorCode = jsonStr(payload, "code")
		evt.ErrorMessage = jsonStr(payload, "message")
		evt.TextContent = evt.ErrorMessage
		events = append(events, evt)

	default:
		evt := base
		evt.EventKind = "event_msg"
		evt.PayloadType = eventType
		events = append(events, evt)
	}

	// Extract token usage from payload.info.last_token_usage
	if info, ok := payload["info"].(map[string]any); ok {
		if usage, ok := info["last_token_usage"].(map[string]any); ok {
			for i := range events {
				events[i].InputTokens = jsonInt64(usage, "input_tokens")
				events[i].OutputTokens = jsonInt64(usage, "output_tokens")
			}
		}
	}

	// Extract model from payload
	model := jsonStr(payload, "model")
	if model == "" {
		if info, ok := payload["info"].(map[string]any); ok {
			model = jsonStr(info, "model")
		}
	}
	for i := range events {
		if events[i].Model == "" {
			events[i].Model = model
		}
	}

	return events, nil
}

