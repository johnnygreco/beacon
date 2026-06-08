package capture

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/johnnygreco/beacon/internal/models"
)

// ParseCodexJSONL parses a single JSONL line from Codex logs.
// Codex writes lines with: type, payload, timestamp, etc.
func ParseCodexJSONL(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, err
	}

	// Session ID: prefer top-level session_id, then extract from filename.
	// Codex filenames follow "rollout-{date}-{uuid}.jsonl" format.
	sessionID := stringField(raw, "session_id")
	if sessionID == "" {
		sessionID = codexSessionIDFromFile(file)
	}

	ts := parseTimestamp(stringField(raw, "timestamp"))

	eventType := stringField(raw, "type")
	payload := objectFromAny(raw["payload"])

	base := NormalizedEvent{
		SessionID:    sessionID,
		RawSessionID: sessionID,
		SourceName:   "codex",
		Provider:     models.ProviderOpenAI,
		Timestamp:    ts,
		RawEventID:   firstNonEmptyString(stringField(raw, "id"), stringField(payload, "id"), stringField(payload, "call_id")),
		SourceFile:   file,
		SourceLineNo: lineNo,
		SourceOffset: offset,
		RawPayload:   string(line),
	}

	// Extract CWD from payload (present in session_meta and turn_context)
	if cwd := stringField(payload, "cwd"); cwd != "" {
		base.CWD = cwd
	}

	var events []NormalizedEvent

	// Extract parent session ID from forked_from_id (Codex subagent linking)
	if forkedFrom := stringField(payload, "forked_from_id"); forkedFrom != "" {
		base.ParentSessionID = forkedFrom
		base.RawParentSessionID = forkedFrom
		base.RawLinkedSessionID = forkedFrom
	}

	switch eventType {
	case "session_meta":
		evt := base
		evt.EventKind = models.EventKindSessionMeta
		evt.ActorRole = models.ActorRoleSystem
		evt.TextContent = stringField(payload, "description")
		events = append(events, evt)

	case "turn_context":
		evt := base
		evt.EventKind = models.EventKindTurnContext
		evt.ActorRole = models.ActorRoleSystem
		// Extract model from turn_context payload (Codex puts it here)
		if m := stringField(payload, "model"); m != "" {
			evt.Model = m
		}
		events = append(events, evt)

	case "response_item":
		payloadType := stringField(payload, "type")
		switch payloadType {
		case "message":
			evt := base
			evt.EventKind = models.EventKindMessage
			evt.ActorRole = stringField(payload, "role")
			if evt.ActorRole == "" {
				evt.ActorRole = models.ActorRoleAssistant
			}
			// Skip developer/system setup messages that don't contain user-relevant content
			if evt.ActorRole == "developer" {
				break
			}
			// Extract text from content array
			var texts []string
			for _, c := range arrayFromAny(payload["content"]) {
				cm := objectFromAny(c)
				if cm == nil {
					continue
				}
				ct := stringField(cm, "type")
				if ct == "output_text" || ct == "text" || ct == "input_text" {
					if t := stringField(cm, "text"); t != "" {
						texts = append(texts, t)
					}
				}
			}
			if len(texts) > 0 {
				evt.TextContent = texts[len(texts)-1] // use last text block (most relevant)
			}
			events = append(events, evt)

		case "function_call":
			evt := base
			evt.EventKind = models.EventKindToolCall
			evt.ActorRole = models.ActorRoleAssistant
			evt.ToolName = stringField(payload, "name")
			evt.ToolPhase = models.ToolPhaseCall
			evt.ToolInput = stringField(payload, "arguments")
			evt.ToolUseID = stringField(payload, "call_id")
			evt.RawEventID = firstNonEmpty(evt.RawEventID, evt.ToolUseID)
			evt.TextContent = evt.ToolName
			// Map spawn_agent to Agent for UI subagent dispatch rendering
			if evt.ToolName == "spawn_agent" {
				evt.ToolName = "Agent"
			}
			events = append(events, evt)

		case "function_call_output":
			evt := base
			evt.EventKind = models.EventKindToolResult
			evt.ActorRole = models.ActorRoleTool
			evt.ToolName = stringField(payload, "name")
			evt.ToolPhase = models.ToolPhaseResult
			evt.ToolOutput = stringField(payload, "output")
			evt.ToolUseID = stringField(payload, "call_id")
			evt.RawEventID = firstNonEmpty(evt.RawEventID, evt.ToolUseID)
			evt.TextContent = stringField(payload, "output")
			// For spawn_agent results, reformat output so agentID() can extract
			// the session ID. Codex output: {"agent_id":"xxx","nickname":"yyy"}
			if evt.ToolName == "spawn_agent" || evt.ToolName == "" {
				if aid := extractCodexAgentID(evt.ToolOutput); aid != "" {
					evt.ToolName = "Agent"
					evt.TextContent = "agentId: " + aid
				}
			}
			if isCodexToolError(evt.ToolOutput) {
				evt.EventKind = models.EventKindToolError
				evt.ErrorCode = "tool_execution_failed"
				evt.ErrorMessage = evt.ToolOutput
			}
			events = append(events, evt)

		case "function_call_output_summary":
			// Codex sometimes emits a summary of tool output — treat as tool_result
			evt := base
			evt.EventKind = models.EventKindToolResult
			evt.ActorRole = models.ActorRoleTool
			evt.ToolName = stringField(payload, "name")
			evt.ToolPhase = models.ToolPhaseResult
			evt.ToolOutput = stringField(payload, "output")
			evt.ToolUseID = stringField(payload, "call_id")
			evt.RawEventID = firstNonEmpty(evt.RawEventID, evt.ToolUseID)
			evt.TextContent = stringField(payload, "output")
			events = append(events, evt)

		case "custom_tool_call":
			// Codex built-in tools (apply_patch, etc.)
			evt := base
			evt.EventKind = models.EventKindToolCall
			evt.ActorRole = models.ActorRoleAssistant
			evt.ToolName = stringField(payload, "name")
			evt.ToolPhase = models.ToolPhaseCall
			evt.ToolInput = stringField(payload, "input")
			evt.ToolUseID = stringField(payload, "call_id")
			evt.RawEventID = firstNonEmpty(evt.RawEventID, evt.ToolUseID)
			evt.TextContent = evt.ToolName
			events = append(events, evt)

		case "custom_tool_call_output":
			// Codex built-in tool results
			evt := base
			evt.EventKind = models.EventKindToolResult
			evt.ActorRole = models.ActorRoleTool
			evt.ToolPhase = models.ToolPhaseResult
			evt.ToolOutput = stringField(payload, "output")
			evt.ToolUseID = stringField(payload, "call_id")
			evt.RawEventID = firstNonEmpty(evt.RawEventID, evt.ToolUseID)
			evt.TextContent = stringField(payload, "output")
			if isCodexToolError(evt.ToolOutput) {
				evt.EventKind = models.EventKindToolError
				evt.ErrorCode = "tool_execution_failed"
				evt.ErrorMessage = evt.ToolOutput
			}
			events = append(events, evt)

		case "reasoning":
			evt := base
			evt.EventKind = models.EventKindReasoning
			evt.ActorRole = models.ActorRoleAssistant
			for _, s := range arrayFromAny(payload["summary"]) {
				sm := objectFromAny(s)
				if sm == nil {
					continue
				}
				evt.TextContent += stringField(sm, "text")
			}
			// Codex reasoning blocks often have encrypted_content with empty summary.
			// Mark as encrypted so the UI can show appropriate messaging.
			if evt.TextContent == "" {
				if _, hasEncrypted := payload["encrypted_content"]; hasEncrypted {
					evt.PayloadType = "encrypted"
				}
			}
			events = append(events, evt)

		default:
			evt := base
			evt.EventKind = models.EventKindEventMsg
			evt.PayloadType = payloadType
			events = append(events, evt)
		}

	case "event_msg":
		payloadType := stringField(payload, "type")
		switch payloadType {
		case "task_complete":
			// Codex emits task_complete at turn boundaries, not only when the
			// session is fully over.
			evt := base
			evt.EventKind = models.EventKindEventMsg
			evt.ActorRole = models.ActorRoleAssistant
			evt.PayloadType = "task_complete"
			evt.TextContent = stringField(payload, "last_agent_message")
			events = append(events, evt)

		case "agent_message":
			// Codex status/commentary messages from the agent
			evt := base
			evt.EventKind = models.EventKindMessage
			evt.ActorRole = models.ActorRoleAssistant
			evt.TextContent = stringField(payload, "message")
			events = append(events, evt)

		case "token_count":
			// Token usage event — store as event_msg but ensure tokens are captured
			evt := base
			evt.EventKind = models.EventKindEventMsg
			evt.PayloadType = payloadType
			events = append(events, evt)

		case "task_started":
			evt := base
			evt.EventKind = models.EventKindSessionMeta
			evt.ActorRole = models.ActorRoleSystem
			evt.TextContent = "Task started"
			events = append(events, evt)

		case "user_message":
			// User message echoed back by Codex
			evt := base
			evt.EventKind = models.EventKindMessage
			evt.ActorRole = models.ActorRoleUser
			evt.TextContent = stringField(payload, "message")
			events = append(events, evt)

		default:
			evt := base
			evt.EventKind = models.EventKindEventMsg
			evt.PayloadType = payloadType
			evt.TextContent = stringField(payload, "message")
			events = append(events, evt)
		}

	case "compacted":
		evt := base
		evt.EventKind = models.EventKindContextSnapshot
		evt.ActorRole = models.ActorRoleSystem
		events = append(events, evt)

	case "error":
		evt := base
		evt.EventKind = models.EventKindError
		evt.ActorRole = models.ActorRoleSystem
		evt.ErrorCode = stringField(payload, "code")
		evt.ErrorMessage = stringField(payload, "message")
		evt.TextContent = evt.ErrorMessage
		events = append(events, evt)

	default:
		evt := base
		evt.EventKind = models.EventKindEventMsg
		evt.PayloadType = eventType
		events = append(events, evt)
	}

	// Extract token usage from payload.info.last_token_usage
	info := objectFromAny(payload["info"])
	if usage := objectFromAny(info["last_token_usage"]); usage != nil {
		grossInput := int64Field(usage, "input_tokens")
		cacheRead := int64Field(usage, "cached_input_tokens")
		input := grossInput - cacheRead
		if input < 0 {
			input = 0
		}
		output := int64Field(usage, "output_tokens")
		for i := range events {
			// OpenAI reports cached tokens as a subset of input_tokens, so we
			// normalize input to uncached prompt tokens for cross-provider totals.
			events[i].InputTokens = input
			events[i].OutputTokens = output
			events[i].CacheReadTokens = cacheRead
		}
	}
	if usage := objectFromAny(info["total_token_usage"]); usage != nil {
		key := codexUsageTotalKey(usage)
		for i := range events {
			events[i].TokenUsageTotalKey = key
		}
	}

	// Extract model from payload
	model := stringField(payload, "model")
	if model == "" {
		model = stringField(info, "model")
	}
	for i := range events {
		if events[i].Model == "" {
			events[i].Model = model
		}
	}

	return events, nil
}

// isCodexToolError detects tool execution failures in Codex output.
// Codex embeds exit code info as "Process exited with code N" in tool output.
func isCodexToolError(output string) bool {
	const prefix = "Process exited with code "
	idx := strings.Index(output, prefix)
	if idx < 0 {
		return false
	}
	after := output[idx+len(prefix):]
	// Non-zero exit code indicates failure
	return len(after) > 0 && after[0] != '0'
}

func codexUsageTotalKey(usage map[string]any) string {
	return fmt.Sprintf("%d:%d:%d:%d",
		int64Field(usage, "input_tokens"),
		int64Field(usage, "cached_input_tokens"),
		int64Field(usage, "output_tokens"),
		int64Field(usage, "total_tokens"),
	)
}

// codexSessionIDFromFile extracts a session ID from a Codex filename.
// Codex filenames follow "rollout-YYYY-MM-DDTHH-MM-SS-{uuid}.jsonl".
// We extract the UUID portion for a cleaner session ID.
func codexSessionIDFromFile(file string) string {
	base := filepath.Base(file)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	// Try to find a UUID pattern (8-4-4-4-12 hex) in the filename
	parts := strings.Split(name, "-")
	// Walk backwards through parts looking for a UUID start
	// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx (5 groups)
	for i := 0; i+4 < len(parts); i++ {
		if len(parts[i]) == 8 && isHex(parts[i]) &&
			len(parts[i+1]) == 4 && isHex(parts[i+1]) &&
			len(parts[i+2]) == 4 && isHex(parts[i+2]) &&
			len(parts[i+3]) == 4 && isHex(parts[i+3]) &&
			len(parts[i+4]) >= 12 && isHex(parts[i+4][:12]) {
			return strings.Join(parts[i:i+5], "-")
		}
	}
	return name
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

// extractCodexAgentID extracts agent_id from Codex spawn_agent output JSON.
// Output format: {"agent_id":"<uuid>","nickname":"<name>"}
func extractCodexAgentID(output string) string {
	if output == "" {
		return ""
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return ""
	}
	if aid, ok := result["agent_id"].(string); ok && aid != "" {
		return aid
	}
	return ""
}
