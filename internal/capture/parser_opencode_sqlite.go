package capture

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type openCodeSessionRow struct {
	id          string
	parentID    string
	directory   string
	title       string
	model       string
	provider    string
	agent       string
	timeCreated int64
	timeUpdated int64
}

// ParseOpenCodeSQLite parses OpenCode's SQLite session database.
//
// Current OpenCode stores sessions in ~/.local/share/opencode/opencode.db
// (or opencode-<channel>.db). The durable transcript projection is the
// session_message table; older message/part rows are left as implementation
// detail and are not required for current sessions.
func ParseOpenCodeSQLite(file string) ([]NormalizedEvent, error) {
	db, err := openSQLiteReadOnly(file)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if !sqliteHasTable(db, "session") || !sqliteHasTable(db, "session_message") {
		return nil, fmt.Errorf("opencode database missing session/session_message tables")
	}

	sessions, err := loadOpenCodeSessions(db)
	if err != nil {
		return nil, err
	}

	var events []NormalizedEvent
	for _, sess := range sessions {
		events = append(events, openCodeSessionMeta(file, sess))
	}

	messageEvents, err := loadOpenCodeMessages(db, file, sessions)
	if err != nil {
		return nil, err
	}
	events = append(events, messageEvents...)
	return events, nil
}

func loadOpenCodeSessions(db *sql.DB) (map[string]openCodeSessionRow, error) {
	rows, err := db.Query(`SELECT id, parent_id, directory, title, model, agent,
	       time_created, time_updated
		FROM session
		ORDER BY time_created, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]openCodeSessionRow)
	for rows.Next() {
		var row openCodeSessionRow
		var parent, modelRaw, agent sql.NullString
		if err := rows.Scan(
			&row.id,
			&parent,
			&row.directory,
			&row.title,
			&modelRaw,
			&agent,
			&row.timeCreated,
			&row.timeUpdated,
		); err != nil {
			return nil, err
		}
		row.parentID = parent.String
		row.agent = agent.String
		if model := parseJSONMap(modelRaw.String); model != nil {
			row.model = firstNonEmpty(stringFromAny(model["id"]), stringFromAny(model["modelID"]))
			row.provider = stringFromAny(model["providerID"])
		}
		result[row.id] = row
	}
	return result, rows.Err()
}

func openCodeSessionMeta(file string, sess openCodeSessionRow) NormalizedEvent {
	return NormalizedEvent{
		SessionID:       sess.id,
		SourceName:      "opencode",
		Runtime:         "opencode",
		Provider:        firstNonEmpty(sess.provider, "multi"),
		Format:          "sqlite",
		EventKind:       "session_meta",
		ActorRole:       "system",
		PayloadType:     firstNonEmpty(sess.agent, "session"),
		Timestamp:       timeFromUnixMillis(sess.timeCreated),
		TextContent:     sess.title,
		Model:           sess.model,
		ParentSessionID: sess.parentID,
		CWD:             sess.directory,
		SourceFile:      file,
		SourceLineNo:    stableLineNo("opencode", "session", sess.id),
		SourceOffset:    stableOffset("opencode", "session", sess.id),
		RawPayload:      sqliteStableRaw("opencode", "session", sess.id, "session_meta"),
	}
}

func loadOpenCodeMessages(db *sql.DB, file string, sessions map[string]openCodeSessionRow) ([]NormalizedEvent, error) {
	rows, err := db.Query(`SELECT id, session_id, type, time_created, time_updated, data
		FROM session_message
		ORDER BY time_created, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []NormalizedEvent
	for rows.Next() {
		var id, sessionID, msgType, dataRaw string
		var created, updated int64
		if err := rows.Scan(&id, &sessionID, &msgType, &created, &updated, &dataRaw); err != nil {
			return nil, err
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(dataRaw), &data); err != nil {
			return nil, fmt.Errorf("decode opencode session_message %s: %w", id, err)
		}
		sess := sessions[sessionID]
		base := NormalizedEvent{
			SessionID:       sessionID,
			SourceName:      "opencode",
			Runtime:         "opencode",
			Provider:        firstNonEmpty(sess.provider, "multi"),
			Format:          "sqlite",
			Timestamp:       openCodeDataTime(data, created),
			Model:           sess.model,
			ParentSessionID: sess.parentID,
			CWD:             sess.directory,
			SourceFile:      file,
			SourceLineNo:    stableLineNo("opencode", "session_message", id),
			SourceOffset:    stableOffset("opencode", "session_message", id),
			MessageUUID:     id,
		}
		events = append(events, openCodeMessageEvents(base, id, msgType, data)...)
	}
	return events, rows.Err()
}

func openCodeDataTime(data map[string]any, fallback int64) time.Time {
	if tm := mapFromAny(data["time"]); tm != nil {
		if created := numberFromAny(tm["created"]); created > 0 {
			return timeFromUnixMillis(created)
		}
	}
	return timeFromUnixMillis(fallback)
}

func openCodeMessageEvents(base NormalizedEvent, rowID, msgType string, data map[string]any) []NormalizedEvent {
	switch msgType {
	case "user":
		evt := base
		evt.EventKind = "message"
		evt.ActorRole = "user"
		evt.TextContent = stringFromAny(data["text"])
		evt.SourceOffset = stableOffset("opencode", "session_message", rowID, "message")
		evt.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "message")
		return []NormalizedEvent{evt}
	case "synthetic":
		evt := base
		evt.EventKind = "message"
		evt.ActorRole = "system"
		evt.PayloadType = "synthetic"
		evt.TextContent = stringFromAny(data["text"])
		evt.SourceOffset = stableOffset("opencode", "session_message", rowID, "synthetic")
		evt.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "synthetic")
		return []NormalizedEvent{evt}
	case "assistant":
		return openCodeAssistantEvents(base, rowID, data)
	case "shell":
		return openCodeShellEvents(base, rowID, data)
	case "compaction":
		evt := base
		evt.EventKind = "context_snapshot"
		evt.ActorRole = "system"
		evt.PayloadType = "compaction"
		evt.TextContent = stringFromAny(data["summary"])
		evt.SourceOffset = stableOffset("opencode", "session_message", rowID, "compaction")
		evt.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "compaction")
		return []NormalizedEvent{evt}
	case "model-switched":
		evt := base
		evt.EventKind = "turn_context"
		evt.ActorRole = "system"
		evt.PayloadType = "model-switched"
		if model := mapFromAny(data["model"]); model != nil {
			evt.Model = firstNonEmpty(stringFromAny(model["id"]), stringFromAny(model["modelID"]))
			evt.Provider = firstNonEmpty(stringFromAny(model["providerID"]), evt.Provider)
			evt.TextContent = evt.Model
		}
		evt.SourceOffset = stableOffset("opencode", "session_message", rowID, "model-switched")
		evt.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "model-switched")
		return []NormalizedEvent{evt}
	case "agent-switched":
		evt := base
		evt.EventKind = "event_msg"
		evt.ActorRole = "system"
		evt.PayloadType = "agent-switched"
		evt.TextContent = stringFromAny(data["agent"])
		evt.SourceOffset = stableOffset("opencode", "session_message", rowID, "agent-switched")
		evt.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "agent-switched")
		return []NormalizedEvent{evt}
	default:
		evt := base
		evt.EventKind = "event_msg"
		evt.ActorRole = "system"
		evt.PayloadType = msgType
		evt.TextContent = textFromHarnessContent(data)
		evt.SourceOffset = stableOffset("opencode", "session_message", rowID, "event")
		evt.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "event")
		return []NormalizedEvent{evt}
	}
}

func openCodeAssistantEvents(base NormalizedEvent, rowID string, data map[string]any) []NormalizedEvent {
	if model := mapFromAny(data["model"]); model != nil {
		base.Model = firstNonEmpty(stringFromAny(model["modelID"]), stringFromAny(model["id"]))
		base.Provider = firstNonEmpty(stringFromAny(model["providerID"]), base.Provider)
	}
	tokens := mapFromAny(data["tokens"])
	cost := floatFromAny(data["cost"])
	tokensAssigned := false
	assignUsage := func(evt *NormalizedEvent) {
		if tokensAssigned {
			return
		}
		if tokens != nil {
			evt.InputTokens = int64(floatFromAny(tokens["input"]))
			evt.OutputTokens = int64(floatFromAny(tokens["output"]))
			evt.CacheReadTokens = int64(floatFromAny(mapFromAny(tokens["cache"])["read"]))
			evt.CacheCreateTokens = int64(floatFromAny(mapFromAny(tokens["cache"])["write"]))
		}
		evt.CostUSD = cost
		tokensAssigned = true
	}

	var events []NormalizedEvent
	for i, part := range arrayFromAny(data["content"]) {
		pm := mapFromAny(part)
		if pm == nil {
			continue
		}
		partType := stringFromAny(pm["type"])
		switch partType {
		case "text":
			evt := base
			evt.EventKind = "message"
			evt.ActorRole = "assistant"
			evt.TextContent = stringFromAny(pm["text"])
			evt.SourceOffset = stableOffset("opencode", "session_message", rowID, "text", fmt.Sprint(i))
			evt.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "text:"+fmt.Sprint(i))
			assignUsage(&evt)
			events = append(events, evt)
		case "reasoning":
			evt := base
			evt.EventKind = "reasoning"
			evt.ActorRole = "assistant"
			evt.TextContent = stringFromAny(pm["text"])
			evt.SourceOffset = stableOffset("opencode", "session_message", rowID, "reasoning", fmt.Sprint(i))
			evt.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "reasoning:"+fmt.Sprint(i))
			assignUsage(&evt)
			events = append(events, evt)
		case "tool":
			events = append(events, openCodeToolEvents(base, rowID, i, pm)...)
		}
	}

	if errData := mapFromAny(data["error"]); errData != nil {
		evt := base
		evt.EventKind = "error"
		evt.ActorRole = "assistant"
		evt.ErrorCode = firstNonEmpty(stringFromAny(errData["type"]), "error")
		evt.ErrorMessage = stringFromAny(errData["message"])
		evt.TextContent = evt.ErrorMessage
		evt.SourceOffset = stableOffset("opencode", "session_message", rowID, "error")
		evt.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "error")
		assignUsage(&evt)
		events = append(events, evt)
	}

	if len(events) == 0 {
		evt := base
		evt.EventKind = "message"
		evt.ActorRole = "assistant"
		evt.SourceOffset = stableOffset("opencode", "session_message", rowID, "message")
		evt.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "message")
		assignUsage(&evt)
		events = append(events, evt)
	}
	return events
}

func openCodeToolEvents(base NormalizedEvent, rowID string, index int, part map[string]any) []NormalizedEvent {
	callID := stringFromAny(part["id"])
	toolName := stringFromAny(part["name"])
	state := mapFromAny(part["state"])
	status := stringFromAny(state["status"])

	call := base
	call.EventKind = "tool_call"
	call.ActorRole = "assistant"
	call.ToolPhase = "call"
	call.ToolUseID = callID
	call.ToolName = toolName
	call.ToolInput = jsonPayload(state["input"])
	if call.ToolInput == "" {
		call.ToolInput = stringFromAny(state["raw"])
	}
	call.TextContent = toolName
	call.SourceOffset = stableOffset("opencode", "session_message", rowID, "tool_call", fmt.Sprint(index), callID)
	call.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "tool_call:"+callID)

	events := []NormalizedEvent{call}
	if status == "completed" || status == "error" {
		result := base
		result.EventKind = "tool_result"
		if status == "error" {
			result.EventKind = "tool_error"
			result.ErrorCode = "tool_execution_failed"
			if errData := mapFromAny(state["error"]); errData != nil {
				result.ErrorMessage = stringFromAny(errData["message"])
			}
		}
		result.ActorRole = "tool"
		result.ToolPhase = "result"
		result.ToolUseID = callID
		result.ToolName = toolName
		result.ToolOutput = openCodeToolOutput(state)
		result.TextContent = firstNonEmpty(result.ToolOutput, result.ErrorMessage)
		result.SourceOffset = stableOffset("opencode", "session_message", rowID, "tool_result", fmt.Sprint(index), callID)
		result.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "tool_result:"+callID)
		events = append(events, result)
	}
	return events
}

func openCodeToolOutput(state map[string]any) string {
	if state == nil {
		return ""
	}
	if output := stringFromAny(state["output"]); output != "" {
		return output
	}
	if content := textFromHarnessContent(state["content"]); content != "" {
		return content
	}
	if structured, ok := state["structured"]; ok {
		return jsonPayload(structured)
	}
	return ""
}

func openCodeShellEvents(base NormalizedEvent, rowID string, data map[string]any) []NormalizedEvent {
	callID := stringFromAny(data["callID"])
	call := base
	call.EventKind = "tool_call"
	call.ActorRole = "assistant"
	call.ToolPhase = "call"
	call.ToolUseID = callID
	call.ToolName = "shell"
	call.ToolInput = jsonPayload(map[string]any{"command": stringFromAny(data["command"])})
	call.TextContent = "shell"
	call.SourceOffset = stableOffset("opencode", "session_message", rowID, "shell_call")
	call.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "shell_call")

	result := base
	result.EventKind = "tool_result"
	result.ActorRole = "tool"
	result.ToolPhase = "result"
	result.ToolUseID = callID
	result.ToolName = "shell"
	result.ToolOutput = stringFromAny(data["output"])
	result.TextContent = result.ToolOutput
	result.SourceOffset = stableOffset("opencode", "session_message", rowID, "shell_result")
	result.RawPayload = sqliteStableRaw("opencode", "session_message", rowID, "shell_result")

	return []NormalizedEvent{call, result}
}
