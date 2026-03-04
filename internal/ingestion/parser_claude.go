package ingestion

import (
	"time"

	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// ParseClaudeCodeEvent normalizes a Claude Code OTel log record into NormalizedEvents.
func ParseClaudeCodeEvent(lr *logspb.LogRecord, source string) []NormalizedEvent {
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

	sessionID := strAttr(attrs, "session_id")
	if sessionID == "" {
		sessionID = strAttr(attrs, "conversation_id")
	}

	base := NormalizedEvent{
		SessionID: sessionID,
		TurnID:    strAttr(attrs, "turn_id"),
		Source:    source,
		Timestamp: ts,
		UserID:    strAttr(attrs, "user_id"),
		MachineID: strAttr(attrs, "machine_id"),
		CWD:       strAttr(attrs, "cwd"),
		GitRepo:   strAttr(attrs, "git_repo"),
	}

	var events []NormalizedEvent

	switch eventName {
	case "user_prompt":
		evt := base
		evt.EventType = "user_prompt"
		evt.UserPrompt = strAttr(attrs, "prompt")
		if evt.UserPrompt == "" {
			evt.UserPrompt = lr.GetBody().GetStringValue()
		}
		evt.TurnNumber = intAttr(attrs, "turn_number")
		evt.DocContent = evt.UserPrompt
		evt.DocType = "prompt"
		events = append(events, evt)

	case "api_request", "api_response":
		evt := base
		evt.EventType = "api_request"
		evt.Model = strAttr(attrs, "model")
		evt.Provider = "anthropic"
		evt.InputTokens = int64Attr(attrs, "input_tokens")
		evt.OutputTokens = int64Attr(attrs, "output_tokens")
		evt.CacheRead = int64Attr(attrs, "cache_read_input_tokens")
		evt.CacheCreate = int64Attr(attrs, "cache_creation_input_tokens")
		evt.DurationMs = int64Attr(attrs, "duration_ms")
		evt.StatusCode = intAttr(attrs, "status_code")
		evt.CostUSD = floatAttr(attrs, "cost_usd")
		events = append(events, evt)

	case "tool_use", "tool_decision":
		evt := base
		evt.EventType = "tool_use"
		evt.ToolName = strAttr(attrs, "tool_name")
		evt.ToolInput = strAttr(attrs, "tool_input")
		events = append(events, evt)

	case "tool_result":
		evt := base
		evt.EventType = "tool_result"
		evt.ToolName = strAttr(attrs, "tool_name")
		evt.ToolOutput = strAttr(attrs, "tool_output")
		evt.ToolSuccess = boolAttr(attrs, "success")
		evt.DurationMs = int64Attr(attrs, "duration_ms")
		evt.DocContent = strAttr(attrs, "tool_output")
		evt.DocType = "tool_output"
		events = append(events, evt)

	case "api_error":
		evt := base
		evt.EventType = "api_error"
		evt.ErrorCode = strAttr(attrs, "error_code")
		evt.ErrorClass = strAttr(attrs, "error_class")
		evt.ErrorMsg = strAttr(attrs, "error_message")
		evt.Provider = "anthropic"
		evt.RetryCount = intAttr(attrs, "retry_count")
		events = append(events, evt)

	case "context_snapshot", "context_window":
		evt := base
		evt.EventType = "context_snapshot"
		evt.TokensInContext = int64Attr(attrs, "tokens_in_context")
		evt.MaxTokens = int64Attr(attrs, "max_tokens")
		evt.CompactionEvent = boolAttr(attrs, "compaction_event")
		events = append(events, evt)

	default:
		// Store as raw event
		evt := base
		evt.EventType = eventName
		if evt.EventType == "" {
			evt.EventType = "unknown"
		}
		events = append(events, evt)
	}

	return events
}

func strAttr(attrs map[string]any, key string) string {
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func intAttr(attrs map[string]any, key string) int {
	if v, ok := attrs[key]; ok {
		switch n := v.(type) {
		case int64:
			return int(n)
		case int:
			return n
		case float64:
			return int(n)
		}
	}
	return 0
}

func int64Attr(attrs map[string]any, key string) int64 {
	if v, ok := attrs[key]; ok {
		switch n := v.(type) {
		case int64:
			return n
		case int:
			return int64(n)
		case float64:
			return int64(n)
		}
	}
	return 0
}

func floatAttr(attrs map[string]any, key string) float64 {
	if v, ok := attrs[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int64:
			return float64(n)
		case int:
			return float64(n)
		}
	}
	return 0
}

func boolAttr(attrs map[string]any, key string) bool {
	if v, ok := attrs[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}
