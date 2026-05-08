package capture

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

// ParseClaudeJSONL parses a single JSONL line from Claude Code logs.
// Claude Code writes lines with: sessionId, uuid, parentUuid, timestamp, type, message, etc.
func ParseClaudeJSONL(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, err
	}

	sessionID := jsonStr(raw, "sessionId")
	if sessionID == "" {
		// Fallback: use filename stem as session ID
		base := filepath.Base(file)
		sessionID = strings.TrimSuffix(base, filepath.Ext(base))
	}

	uuid := jsonStr(raw, "uuid")
	parentUUID := jsonStr(raw, "parentUuid")

	ts := parseTimestamp(jsonStr(raw, "timestamp"))

	eventType := jsonStr(raw, "type")

	cwd := jsonStr(raw, "cwd")

	// Subagent detection: Claude Code subagents have isSidechain=true and agentId set.
	// The sessionId in the JSONL refers to the parent session, so we use agentId as
	// the session ID for subagent events and store the original as parentSessionID.
	var parentSessionID string
	agentID := jsonStr(raw, "agentId")
	if agentID != "" {
		if isSidechain, ok := raw["isSidechain"].(bool); ok && isSidechain {
			parentSessionID = sessionID
			sessionID = agentID
		}
	}

	base := NormalizedEvent{
		SessionID:       sessionID,
		SourceName:      "claude",
		Provider:        "anthropic",
		Timestamp:       ts,
		ParentUUID:      parentUUID,
		MessageUUID:     uuid,
		CWD:             cwd,
		ParentSessionID: parentSessionID,
		SourceFile:      file,
		SourceLineNo:    lineNo,
		SourceOffset:    offset,
		RawPayload:      string(line),
	}

	var events []NormalizedEvent

	switch eventType {
	case "summary":
		// Summary messages are metadata about the session
		evt := base
		evt.EventKind = "session_meta"
		evt.PayloadType = "summary"
		evt.ActorRole = "system"
		evt.TextContent = jsonStr(raw, "summary")
		events = append(events, evt)

	case "last-prompt":
		// Definitive session-end signal emitted when Claude Code exits.
		evt := base
		evt.EventKind = "session_end"
		evt.PayloadType = "last-prompt"
		evt.ActorRole = "system"
		events = append(events, evt)

	default:
		// Most Claude Code lines have a "message" field with role and content
		msg, _ := raw["message"].(map[string]any)
		if msg == nil {
			// Fallback: treat as generic event
			evt := base
			evt.EventKind = "event_msg"
			evt.PayloadType = eventType
			events = append(events, evt)
			return events, nil
		}

		// Detect API error messages (e.g. authentication_failed, overloaded_error).
		// Claude Code sets "error" and "isApiErrorMessage" at the top level.
		if errCode := jsonStr(raw, "error"); errCode != "" {
			evt := base
			evt.EventKind = "error"
			evt.ActorRole = "system"
			evt.ErrorCode = errCode
			// Extract error text from content blocks
			if content, ok := msg["content"].([]any); ok {
				for _, block := range content {
					if bm, ok := block.(map[string]any); ok {
						if t := jsonStr(bm, "text"); t != "" {
							evt.ErrorMessage = t
							evt.TextContent = t
							break
						}
					}
				}
			}
			if evt.ErrorMessage == "" {
				evt.ErrorMessage = errCode
				evt.TextContent = errCode
			}
			events = append(events, evt)
			return events, nil
		}

		role := jsonStr(msg, "role")
		model := jsonStr(msg, "model")

		// Extract usage from message
		var inputTokens, outputTokens, cacheRead, cacheCreate int64
		if usage, ok := msg["usage"].(map[string]any); ok {
			inputTokens = jsonInt64(usage, "input_tokens")
			outputTokens = jsonInt64(usage, "output_tokens")
			cacheRead = jsonInt64(usage, "cache_read_input_tokens")
			cacheCreate = jsonInt64(usage, "cache_creation_input_tokens")
		}

		// Parse content — may be a plain string or an array of content blocks.
		// Real Claude Code JSONL uses a string for user prompts and an array
		// of content blocks for assistant / tool-result messages.
		var content []any
		switch c := msg["content"].(type) {
		case []any:
			content = c
		case string:
			// Plain-text content (common for user prompts)
			if c != "" {
				evt := base
				evt.EventKind = "message"
				evt.ActorRole = role
				evt.Model = model
				evt.TextContent = c
				evt.InputTokens = inputTokens
				evt.OutputTokens = outputTokens
				evt.CacheReadTokens = cacheRead
				evt.CacheCreateTokens = cacheCreate
				events = append(events, evt)
				return events, nil
			}
		}

		if len(content) == 0 {
			// No content at all — emit a bare message event
			evt := base
			evt.EventKind = "message"
			evt.ActorRole = role
			evt.Model = model
			evt.InputTokens = inputTokens
			evt.OutputTokens = outputTokens
			evt.CacheReadTokens = cacheRead
			evt.CacheCreateTokens = cacheCreate
			events = append(events, evt)
			return events, nil
		}

		tokensAssigned := false
		for _, block := range content {
			bm, ok := block.(map[string]any)
			if !ok {
				continue
			}

			blockType := jsonStr(bm, "type")
			evt := base

			switch blockType {
			case "text":
				evt.EventKind = "message"
				evt.ActorRole = role
				evt.Model = model
				evt.TextContent = jsonStr(bm, "text")

			case "tool_use":
				evt.EventKind = "tool_call"
				evt.ActorRole = "assistant"
				evt.ToolName = jsonStr(bm, "name")
				evt.ToolPhase = "call"
				evt.ToolUseID = jsonStr(bm, "id")
				if inputRaw, ok := bm["input"]; ok {
					if b, err := json.Marshal(inputRaw); err == nil {
						evt.ToolInput = string(b)
					}
				}
				// Populate text_content so tool_call events are FTS-searchable.
				evt.TextContent = evt.ToolName

			case "tool_result":
				evt.EventKind = "tool_result"
				evt.ActorRole = "tool"
				evt.ToolName = jsonStr(bm, "name")
				evt.ToolPhase = "result"
				evt.ToolUseID = jsonStr(bm, "tool_use_id")
				// Content can be string or nested
				if c, ok := bm["content"].(string); ok {
					evt.TextContent = c
					evt.ToolOutput = c
				} else if contentArr, ok := bm["content"].([]any); ok {
					for _, item := range contentArr {
						if im, ok := item.(map[string]any); ok {
							if jsonStr(im, "type") == "text" {
								evt.TextContent = jsonStr(im, "text")
								evt.ToolOutput = evt.TextContent
							}
						}
					}
				}
				// Detect tool execution failures via is_error flag
				if isErr, ok := bm["is_error"].(bool); ok && isErr {
					evt.EventKind = "tool_error"
					evt.ErrorCode = "tool_execution_failed"
					msg := evt.TextContent
					// Strip <tool_use_error> wrapper tags for cleaner display
					msg = strings.TrimPrefix(msg, "<tool_use_error>")
					msg = strings.TrimSuffix(msg, "</tool_use_error>")
					evt.ErrorMessage = msg
				}

			case "thinking":
				evt.EventKind = "reasoning"
				evt.ActorRole = "assistant"
				evt.TextContent = jsonStr(bm, "thinking")

			default:
				evt.EventKind = "message"
				evt.ActorRole = role
				evt.PayloadType = blockType
			}

			// Assign token counts to the first event per line
			if !tokensAssigned {
				evt.InputTokens = inputTokens
				evt.OutputTokens = outputTokens
				evt.CacheReadTokens = cacheRead
				evt.CacheCreateTokens = cacheCreate
				evt.Model = model
				tokensAssigned = true
			}

			events = append(events, evt)
		}
	}

	return events, nil
}

func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{} // zero value — filtered out by views
	}
	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Try ISO 8601 without timezone
	if t, err := time.Parse("2006-01-02T15:04:05.000", s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t
	}
	return time.Time{} // zero value — filtered out by views
}

func jsonStr(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func jsonInt64(m map[string]any, key string) int64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case json.Number:
			i, _ := n.Int64()
			return i
		}
	}
	return 0
}
