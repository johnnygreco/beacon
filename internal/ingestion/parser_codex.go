package ingestion

import (
	"time"

	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// ParseCodexEvent normalizes an OpenAI Codex OTel log record into NormalizedEvents.
func ParseCodexEvent(lr *logspb.LogRecord, source string) []NormalizedEvent {
	attrs := make(map[string]any)
	for _, kv := range lr.GetAttributes() {
		key := kv.GetKey()
		val := kv.GetValue()
		switch {
		case val.GetStringValue() != "":
			attrs[key] = val.GetStringValue()
		case val.GetIntValue() != 0:
			attrs[key] = val.GetIntValue()
		case val.GetDoubleValue() != 0:
			attrs[key] = val.GetDoubleValue()
		case val.GetBoolValue():
			attrs[key] = val.GetBoolValue()
		default:
			attrs[key] = val.GetStringValue()
		}
	}

	eventName := strAttr(attrs, "event.name")
	if eventName == "" {
		eventName = strAttr(attrs, "event_name")
	}

	ts := time.Unix(0, int64(lr.GetTimeUnixNano()))
	if ts.IsZero() {
		ts = time.Now()
	}

	sessionID := strAttr(attrs, "thread_id")
	if sessionID == "" {
		sessionID = strAttr(attrs, "session_id")
	}

	base := NormalizedEvent{
		SessionID: sessionID,
		TurnID:    strAttr(attrs, "turn_id"),
		Source:    "codex",
		Timestamp: ts,
		UserID:    strAttr(attrs, "user_id"),
		MachineID: strAttr(attrs, "machine_id"),
		CWD:       strAttr(attrs, "cwd"),
	}

	var events []NormalizedEvent

	switch eventName {
	case "conversation.message", "user_prompt":
		evt := base
		evt.EventType = "user_prompt"
		evt.UserPrompt = strAttr(attrs, "content")
		if evt.UserPrompt == "" {
			evt.UserPrompt = lr.GetBody().GetStringValue()
		}
		evt.DocContent = evt.UserPrompt
		evt.DocType = "prompt"
		events = append(events, evt)

	case "response.completed":
		evt := base
		evt.EventType = "api_request"
		evt.Model = strAttr(attrs, "model")
		evt.Provider = "openai"
		evt.InputTokens = int64Attr(attrs, "input_tokens")
		evt.OutputTokens = int64Attr(attrs, "output_tokens")
		evt.DurationMs = int64Attr(attrs, "duration_ms")
		evt.CostUSD = floatAttr(attrs, "cost_usd")
		events = append(events, evt)

	case "tool_call", "function_call":
		evt := base
		evt.EventType = "tool_use"
		evt.ToolName = strAttr(attrs, "tool_name")
		if evt.ToolName == "" {
			evt.ToolName = strAttr(attrs, "function_name")
		}
		evt.ToolInput = strAttr(attrs, "arguments")
		events = append(events, evt)

	case "tool_result", "function_result":
		evt := base
		evt.EventType = "tool_result"
		evt.ToolName = strAttr(attrs, "tool_name")
		if evt.ToolName == "" {
			evt.ToolName = strAttr(attrs, "function_name")
		}
		evt.ToolOutput = strAttr(attrs, "result")
		evt.ToolSuccess = !boolAttr(attrs, "error")
		evt.DurationMs = int64Attr(attrs, "duration_ms")
		events = append(events, evt)

	case "error":
		evt := base
		evt.EventType = "api_error"
		evt.ErrorCode = strAttr(attrs, "error_code")
		evt.ErrorClass = strAttr(attrs, "error_type")
		evt.ErrorMsg = strAttr(attrs, "error_message")
		evt.Provider = "openai"
		events = append(events, evt)

	case "thread.tokenUsage.updated", "token_usage":
		evt := base
		evt.EventType = "context_snapshot"
		evt.TokensInContext = int64Attr(attrs, "tokens_used")
		evt.MaxTokens = int64Attr(attrs, "max_tokens")
		events = append(events, evt)

	default:
		evt := base
		evt.EventType = eventName
		if evt.EventType == "" {
			evt.EventType = "unknown"
		}
		events = append(events, evt)
	}

	return events
}
