package capture

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParsePiSessionFile parses Pi coding-agent JSONL session files.
//
// Pi stores sessions under ~/.pi/agent/sessions/<encoded-cwd>/ as JSONL files.
// The first line is a session header; subsequent entries carry tree ids and
// parentId links, with AgentMessage payloads under message entries.
func ParsePiSessionFile(file string) ([]NormalizedEvent, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sessionID := piSessionIDFromFile(file)
	var cwd, parentSession string
	var events []NormalizedEvent

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4<<20), 4<<20)
	var offset int64
	var lineNo int
	for scanner.Scan() {
		lineNo++
		line := append([]byte(nil), scanner.Bytes()...)
		lineLen := int64(len(line)) + 1
		if len(strings.TrimSpace(string(line))) == 0 {
			offset += lineLen
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil, err
		}

		entryType := jsonStr(raw, "type")
		if entryType == "session" {
			if id := jsonStr(raw, "id"); id != "" {
				sessionID = id
			}
			cwd = jsonStr(raw, "cwd")
			parentSession = jsonStr(raw, "parentSession")
			evt := NormalizedEvent{
				SessionID:       sessionID,
				SourceName:      "pi",
				Runtime:         "pi-coding-agent",
				Provider:        "multi",
				Format:          "jsonl",
				EventKind:       "session_meta",
				ActorRole:       "system",
				PayloadType:     "session",
				Timestamp:       parseTimestamp(jsonStr(raw, "timestamp")),
				CWD:             cwd,
				ParentSessionID: parentSession,
				SourceFile:      file,
				SourceLineNo:    lineNo,
				SourceOffset:    offset,
				RawPayload:      piStableRaw(lineNo, "session"),
			}
			events = append(events, evt)
			offset += lineLen
			continue
		}

		base := NormalizedEvent{
			SessionID:       sessionID,
			SourceName:      "pi",
			Runtime:         "pi-coding-agent",
			Provider:        "multi",
			Format:          "jsonl",
			Timestamp:       parseTimestamp(jsonStr(raw, "timestamp")),
			ParentUUID:      jsonStr(raw, "parentId"),
			MessageUUID:     jsonStr(raw, "id"),
			CWD:             cwd,
			ParentSessionID: parentSession,
			SourceFile:      file,
			SourceLineNo:    lineNo,
			SourceOffset:    offset,
		}

		events = append(events, piEntryEvents(base, lineNo, raw)...)
		offset += lineLen
	}
	return events, scanner.Err()
}

func piEntryEvents(base NormalizedEvent, lineNo int, raw map[string]any) []NormalizedEvent {
	entryType := jsonStr(raw, "type")
	switch entryType {
	case "message":
		msg := mapFromAny(raw["message"])
		return piMessageEvents(base, lineNo, msg)
	case "model_change":
		evt := base
		evt.EventKind = "turn_context"
		evt.ActorRole = "system"
		evt.PayloadType = "model_change"
		evt.Provider = firstNonEmpty(jsonStr(raw, "provider"), "multi")
		evt.Model = jsonStr(raw, "modelId")
		evt.TextContent = evt.Model
		evt.RawPayload = piStableRaw(lineNo, "model_change")
		return []NormalizedEvent{evt}
	case "thinking_level_change":
		evt := base
		evt.EventKind = "event_msg"
		evt.ActorRole = "system"
		evt.PayloadType = "thinking_level_change"
		evt.TextContent = jsonStr(raw, "thinkingLevel")
		evt.RawPayload = piStableRaw(lineNo, "thinking_level_change")
		return []NormalizedEvent{evt}
	case "compaction", "branch_summary":
		evt := base
		evt.EventKind = "context_snapshot"
		evt.ActorRole = "system"
		evt.PayloadType = entryType
		evt.TextContent = jsonStr(raw, "summary")
		evt.RawPayload = piStableRaw(lineNo, entryType)
		return []NormalizedEvent{evt}
	case "custom_message":
		evt := base
		evt.EventKind = "message"
		evt.ActorRole = "system"
		evt.PayloadType = jsonStr(raw, "customType")
		evt.TextContent = textFromHarnessContent(raw["content"])
		evt.RawPayload = piStableRaw(lineNo, "custom_message")
		return []NormalizedEvent{evt}
	case "session_info":
		evt := base
		evt.EventKind = "session_meta"
		evt.ActorRole = "system"
		evt.PayloadType = "session_info"
		evt.TextContent = jsonStr(raw, "name")
		evt.RawPayload = piStableRaw(lineNo, "session_info")
		return []NormalizedEvent{evt}
	default:
		evt := base
		evt.EventKind = "event_msg"
		evt.ActorRole = "system"
		evt.PayloadType = entryType
		evt.TextContent = textFromHarnessContent(raw)
		evt.RawPayload = piStableRaw(lineNo, "event")
		return []NormalizedEvent{evt}
	}
}

func piMessageEvents(base NormalizedEvent, lineNo int, msg map[string]any) []NormalizedEvent {
	role := jsonStr(msg, "role")
	switch role {
	case "user":
		evt := base
		evt.EventKind = "message"
		evt.ActorRole = "user"
		evt.TextContent = textFromHarnessContent(msg["content"])
		evt.MessageUUID = scopedMessageUUID(base.MessageUUID, "message")
		evt.RawPayload = piStableRaw(lineNo, "message")
		return []NormalizedEvent{evt}
	case "assistant":
		return piAssistantEvents(base, lineNo, msg)
	case "toolResult":
		evt := base
		evt.EventKind = "tool_result"
		if isErr, ok := msg["isError"].(bool); ok && isErr {
			evt.EventKind = "tool_error"
			evt.ErrorCode = "tool_execution_failed"
		}
		evt.ActorRole = "tool"
		evt.ToolPhase = "result"
		evt.ToolUseID = jsonStr(msg, "toolCallId")
		evt.ToolName = jsonStr(msg, "toolName")
		evt.ToolOutput = textFromHarnessContent(msg["content"])
		evt.TextContent = evt.ToolOutput
		if evt.EventKind == "tool_error" {
			evt.ErrorMessage = evt.ToolOutput
		}
		evt.MessageUUID = scopedMessageUUID(base.MessageUUID, "tool_result")
		evt.RawPayload = piStableRaw(lineNo, "tool_result")
		return []NormalizedEvent{evt}
	case "bashExecution":
		return piBashExecutionEvents(base, lineNo, msg)
	case "custom":
		evt := base
		evt.EventKind = "message"
		evt.ActorRole = "system"
		evt.PayloadType = jsonStr(msg, "customType")
		evt.TextContent = textFromHarnessContent(msg["content"])
		evt.MessageUUID = scopedMessageUUID(base.MessageUUID, "custom")
		evt.RawPayload = piStableRaw(lineNo, "custom")
		return []NormalizedEvent{evt}
	case "branchSummary":
		evt := base
		evt.EventKind = "context_snapshot"
		evt.ActorRole = "system"
		evt.PayloadType = "branchSummary"
		evt.TextContent = jsonStr(msg, "summary")
		evt.MessageUUID = scopedMessageUUID(base.MessageUUID, "branch_summary")
		evt.RawPayload = piStableRaw(lineNo, "branch_summary")
		return []NormalizedEvent{evt}
	case "compactionSummary":
		evt := base
		evt.EventKind = "context_snapshot"
		evt.ActorRole = "system"
		evt.PayloadType = "compactionSummary"
		evt.TextContent = jsonStr(msg, "summary")
		evt.MessageUUID = scopedMessageUUID(base.MessageUUID, "compaction_summary")
		evt.RawPayload = piStableRaw(lineNo, "compaction_summary")
		return []NormalizedEvent{evt}
	default:
		evt := base
		evt.EventKind = "event_msg"
		evt.ActorRole = "system"
		evt.PayloadType = role
		evt.TextContent = textFromHarnessContent(msg)
		evt.MessageUUID = scopedMessageUUID(base.MessageUUID, "event")
		evt.RawPayload = piStableRaw(lineNo, "event")
		return []NormalizedEvent{evt}
	}
}

func piAssistantEvents(base NormalizedEvent, lineNo int, msg map[string]any) []NormalizedEvent {
	base.Provider = firstNonEmpty(jsonStr(msg, "provider"), base.Provider)
	base.Model = jsonStr(msg, "model")

	usage := mapFromAny(msg["usage"])
	tokensAssigned := false
	assignUsage := func(evt *NormalizedEvent) {
		if tokensAssigned || usage == nil {
			return
		}
		evt.InputTokens = jsonInt64(usage, "input")
		evt.OutputTokens = jsonInt64(usage, "output")
		evt.CacheReadTokens = jsonInt64(usage, "cacheRead")
		evt.CacheCreateTokens = jsonInt64(usage, "cacheWrite")
		if cost := mapFromAny(usage["cost"]); cost != nil {
			evt.CostUSD = floatFromAny(cost["total"])
		}
		tokensAssigned = true
	}

	var events []NormalizedEvent
	for i, block := range arrayFromAny(msg["content"]) {
		bm := mapFromAny(block)
		if bm == nil {
			continue
		}
		blockType := jsonStr(bm, "type")
		switch blockType {
		case "text":
			evt := base
			evt.EventKind = "message"
			evt.ActorRole = "assistant"
			evt.TextContent = jsonStr(bm, "text")
			evt.MessageUUID = scopedMessageUUID(base.MessageUUID, "text", fmt.Sprint(i))
			evt.RawPayload = piStableRaw(lineNo, "text:"+fmt.Sprint(i))
			assignUsage(&evt)
			events = append(events, evt)
		case "thinking":
			evt := base
			evt.EventKind = "reasoning"
			evt.ActorRole = "assistant"
			evt.TextContent = jsonStr(bm, "thinking")
			evt.MessageUUID = scopedMessageUUID(base.MessageUUID, "thinking", fmt.Sprint(i))
			evt.RawPayload = piStableRaw(lineNo, "thinking:"+fmt.Sprint(i))
			assignUsage(&evt)
			events = append(events, evt)
		case "toolCall":
			evt := base
			evt.EventKind = "tool_call"
			evt.ActorRole = "assistant"
			evt.ToolPhase = "call"
			evt.ToolUseID = jsonStr(bm, "id")
			evt.ToolName = jsonStr(bm, "name")
			evt.ToolInput = jsonPayload(bm["arguments"])
			evt.TextContent = evt.ToolName
			evt.MessageUUID = scopedMessageUUID(base.MessageUUID, "tool_call", fmt.Sprint(i), evt.ToolUseID)
			evt.RawPayload = piStableRaw(lineNo, "tool_call:"+evt.ToolUseID)
			assignUsage(&evt)
			events = append(events, evt)
		}
	}
	if errMsg := jsonStr(msg, "errorMessage"); errMsg != "" {
		evt := base
		evt.EventKind = "error"
		evt.ActorRole = "assistant"
		evt.ErrorCode = firstNonEmpty(jsonStr(msg, "stopReason"), "error")
		evt.ErrorMessage = errMsg
		evt.TextContent = errMsg
		evt.MessageUUID = scopedMessageUUID(base.MessageUUID, "error")
		evt.RawPayload = piStableRaw(lineNo, "error")
		assignUsage(&evt)
		events = append(events, evt)
	}
	if len(events) == 0 {
		evt := base
		evt.EventKind = "message"
		evt.ActorRole = "assistant"
		evt.TextContent = textFromHarnessContent(msg["content"])
		evt.MessageUUID = scopedMessageUUID(base.MessageUUID, "message")
		evt.RawPayload = piStableRaw(lineNo, "message")
		assignUsage(&evt)
		events = append(events, evt)
	}
	return events
}

func piBashExecutionEvents(base NormalizedEvent, lineNo int, msg map[string]any) []NormalizedEvent {
	call := base
	call.EventKind = "tool_call"
	call.ActorRole = "assistant"
	call.ToolPhase = "call"
	call.ToolName = "bash"
	call.ToolUseID = base.MessageUUID
	call.ToolInput = jsonPayload(map[string]any{"command": jsonStr(msg, "command")})
	call.TextContent = "bash"
	call.MessageUUID = scopedMessageUUID(base.MessageUUID, "bash_call")
	call.RawPayload = piStableRaw(lineNo, "bash_call")

	result := base
	result.EventKind = "tool_result"
	if code := numberFromAny(msg["exitCode"]); code != 0 {
		result.EventKind = "tool_error"
		result.ErrorCode = "tool_execution_failed"
	}
	result.ActorRole = "tool"
	result.ToolPhase = "result"
	result.ToolName = "bash"
	result.ToolUseID = base.MessageUUID
	result.ToolOutput = jsonStr(msg, "output")
	result.TextContent = result.ToolOutput
	if result.EventKind == "tool_error" {
		result.ErrorMessage = result.ToolOutput
	}
	result.MessageUUID = scopedMessageUUID(base.MessageUUID, "bash_result")
	result.RawPayload = piStableRaw(lineNo, "bash_result")
	return []NormalizedEvent{call, result}
}

func piStableRaw(lineNo int, kind string) string {
	return sqliteStableRaw("pi", "jsonl", fmt.Sprint(lineNo), kind)
}

func piSessionIDFromFile(file string) string {
	base := filepath.Base(file)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
