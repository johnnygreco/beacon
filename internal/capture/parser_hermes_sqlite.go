package capture

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

type hermesSessionRow struct {
	id                string
	source            string
	model             string
	parentSessionID   string
	startedAt         float64
	endedAt           float64
	endReason         string
	inputTokens       int64
	outputTokens      int64
	cacheReadTokens   int64
	cacheCreateTokens int64
	reasoningTokens   int64
	billingProvider   string
	costUSD           float64
	title             string
}

// ParseHermesSQLite parses Hermes Agent's SQLite state store.
//
// Current Hermes stores normal CLI/gateway sessions in ~/.hermes/state.db.
// Session metadata and cumulative usage live in the sessions table; message
// history lives in messages. Batch/RL trajectories are separate systems and
// are intentionally not parsed here.
func ParseHermesSQLite(file string) ([]NormalizedEvent, error) {
	db, err := openSQLiteReadOnly(file)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if !sqliteHasTable(db, "sessions") || !sqliteHasTable(db, "messages") {
		return nil, fmt.Errorf("hermes state database missing sessions/messages tables")
	}
	if !sqliteHasColumn(db, "messages", "reasoning_content") {
		return nil, fmt.Errorf("unsupported Hermes state schema: messages.reasoning_content column is required")
	}

	sessions, err := loadHermesSessions(db)
	if err != nil {
		return nil, err
	}

	var events []NormalizedEvent
	for _, sess := range sessions {
		events = append(events, hermesSessionEvents(file, sess)...)
	}

	messageEvents, err := loadHermesMessages(db, file, sessions)
	if err != nil {
		return nil, err
	}
	events = append(events, messageEvents...)
	return events, nil
}

func loadHermesSessions(db *sql.DB) (map[string]hermesSessionRow, error) {
	rows, err := db.Query(`SELECT id, source, model, parent_session_id, started_at, ended_at,
	       end_reason, input_tokens, output_tokens, cache_read_tokens,
	       cache_write_tokens, reasoning_tokens, billing_provider,
	       COALESCE(actual_cost_usd, estimated_cost_usd, 0), title
		FROM sessions
		ORDER BY started_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]hermesSessionRow)
	for rows.Next() {
		var row hermesSessionRow
		var source, model, parent, endReason, provider, title sql.NullString
		var started, ended, cost sql.NullFloat64
		var input, output, cacheRead, cacheWrite, reasoning sql.NullInt64
		if err := rows.Scan(
			&row.id,
			&source,
			&model,
			&parent,
			&started,
			&ended,
			&endReason,
			&input,
			&output,
			&cacheRead,
			&cacheWrite,
			&reasoning,
			&provider,
			&cost,
			&title,
		); err != nil {
			return nil, err
		}
		row.source = source.String
		row.model = model.String
		row.parentSessionID = parent.String
		row.startedAt = started.Float64
		row.endedAt = ended.Float64
		row.endReason = endReason.String
		row.inputTokens = input.Int64
		row.outputTokens = output.Int64
		row.cacheReadTokens = cacheRead.Int64
		row.cacheCreateTokens = cacheWrite.Int64
		row.reasoningTokens = reasoning.Int64
		row.billingProvider = provider.String
		row.costUSD = cost.Float64
		row.title = title.String
		result[row.id] = row
	}
	return result, rows.Err()
}

func hermesSessionEvents(file string, sess hermesSessionRow) []NormalizedEvent {
	base := NormalizedEvent{
		SessionID:       sess.id,
		SourceName:      "hermes",
		Runtime:         "hermes-agent",
		Provider:        firstNonEmpty(sess.billingProvider, "multi"),
		Format:          "sqlite",
		Timestamp:       timeFromUnixSeconds(sess.startedAt),
		Model:           sess.model,
		ParentSessionID: sess.parentSessionID,
		SourceFile:      file,
		SourceLineNo:    stableLineNo("hermes", "sessions", sess.id),
		SourceOffset:    stableOffset("hermes", "sessions", sess.id),
	}

	meta := base
	meta.EventKind = "session_meta"
	meta.ActorRole = "system"
	meta.PayloadType = firstNonEmpty(sess.source, "session")
	meta.TextContent = sess.title
	meta.RawPayload = sqliteStableRaw("hermes", "sessions", sess.id, "session_meta")

	events := []NormalizedEvent{meta}

	if sess.inputTokens > 0 || sess.outputTokens > 0 || sess.cacheReadTokens > 0 || sess.cacheCreateTokens > 0 || sess.costUSD > 0 {
		usage := base
		usage.EventKind = "event_msg"
		usage.ActorRole = "system"
		usage.PayloadType = "usage"
		usage.TextContent = "usage"
		usage.InputTokens = sess.inputTokens
		usage.OutputTokens = sess.outputTokens
		usage.CacheReadTokens = sess.cacheReadTokens
		usage.CacheCreateTokens = sess.cacheCreateTokens
		usage.CostUSD = sess.costUSD
		usage.SourceOffset = stableOffset("hermes", "sessions", sess.id, "usage")
		usage.RawPayload = sqliteStableRaw("hermes", "sessions", sess.id, "usage")
		events = append(events, usage)
	}

	if sess.endedAt > 0 || sess.endReason != "" {
		end := base
		end.EventKind = "session_end"
		end.ActorRole = "system"
		end.PayloadType = firstNonEmpty(sess.endReason, "ended")
		end.Timestamp = timeFromUnixSeconds(sess.endedAt)
		end.TextContent = sess.endReason
		end.SourceOffset = stableOffset("hermes", "sessions", sess.id, "session_end")
		end.RawPayload = sqliteStableRaw("hermes", "sessions", sess.id, "session_end")
		events = append(events, end)
	}

	return events
}

func loadHermesMessages(db *sql.DB, file string, sessions map[string]hermesSessionRow) ([]NormalizedEvent, error) {
	rows, err := db.Query(`SELECT id, session_id, role, content, tool_call_id, tool_calls,
	       tool_name, timestamp, finish_reason, reasoning_content
		FROM messages
		ORDER BY session_id, timestamp, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []NormalizedEvent
	for rows.Next() {
		var id int64
		var sessionID, role string
		var content, toolCallID, toolCalls, toolName, finishReason, reasoningContent sql.NullString
		var ts sql.NullFloat64
		if err := rows.Scan(
			&id,
			&sessionID,
			&role,
			&content,
			&toolCallID,
			&toolCalls,
			&toolName,
			&ts,
			&finishReason,
			&reasoningContent,
		); err != nil {
			return nil, err
		}

		sess := sessions[sessionID]
		base := NormalizedEvent{
			SessionID:       sessionID,
			SourceName:      "hermes",
			Runtime:         "hermes-agent",
			Provider:        firstNonEmpty(sess.billingProvider, "multi"),
			Format:          "sqlite",
			Timestamp:       timeFromUnixSeconds(ts.Float64),
			Model:           sess.model,
			ParentSessionID: sess.parentSessionID,
			SourceFile:      file,
			SourceLineNo:    stableLineNo("hermes", "messages", fmt.Sprint(id)),
			SourceOffset:    stableOffset("hermes", "messages", fmt.Sprint(id)),
			MessageUUID:     fmt.Sprint(id),
		}

		rowEvents := hermesMessageEvents(base, id, role, content.String, toolCallID.String, toolCalls.String, toolName.String, finishReason.String, reasoningContent.String)
		events = append(events, rowEvents...)
	}
	return events, rows.Err()
}

func hermesMessageEvents(base NormalizedEvent, rowID int64, role, rawContent, toolCallID, toolCalls, toolName, finishReason, reasoningContent string) []NormalizedEvent {
	rowKey := fmt.Sprint(rowID)
	var events []NormalizedEvent
	content := textFromHarnessContent(decodeHarnessJSON(rawContent))

	if reasoningText := textFromHarnessContent(decodeHarnessJSON(reasoningContent)); reasoningText != "" {
		evt := base
		evt.EventKind = "reasoning"
		evt.ActorRole = "assistant"
		evt.TextContent = reasoningText
		evt.SourceOffset = stableOffset("hermes", "messages", rowKey, "reasoning")
		evt.RawPayload = sqliteStableRaw("hermes", "messages", rowKey, "reasoning")
		events = append(events, evt)
	}

	if finishReason == "error" {
		evt := base
		evt.EventKind = "error"
		evt.ActorRole = "assistant"
		evt.ErrorCode = "error"
		evt.ErrorMessage = content
		evt.TextContent = content
		evt.SourceOffset = stableOffset("hermes", "messages", rowKey, "error")
		evt.RawPayload = sqliteStableRaw("hermes", "messages", rowKey, "error")
		events = append(events, evt)
	}

	switch role {
	case "assistant":
		if content != "" {
			evt := base
			evt.EventKind = "message"
			evt.ActorRole = "assistant"
			evt.TextContent = content
			evt.SourceOffset = stableOffset("hermes", "messages", rowKey, "message")
			evt.RawPayload = sqliteStableRaw("hermes", "messages", rowKey, "message")
			events = append(events, evt)
		}
		for i, call := range hermesToolCalls(toolCalls) {
			evt := base
			evt.EventKind = "tool_call"
			evt.ActorRole = "assistant"
			evt.ToolPhase = "call"
			evt.ToolUseID = call.id
			evt.ToolName = call.name
			evt.ToolInput = call.input
			evt.TextContent = call.name
			evt.SourceOffset = stableOffset("hermes", "messages", rowKey, "tool_call", fmt.Sprint(i), call.id)
			evt.RawPayload = sqliteStableRaw("hermes", "messages", rowKey, "tool_call:"+call.id)
			events = append(events, evt)
		}
	case "tool":
		evt := base
		evt.EventKind = "tool_result"
		evt.ActorRole = "tool"
		evt.ToolPhase = "result"
		evt.ToolUseID = toolCallID
		evt.ToolName = toolName
		evt.ToolOutput = content
		evt.TextContent = content
		evt.SourceOffset = stableOffset("hermes", "messages", rowKey, "tool_result")
		evt.RawPayload = sqliteStableRaw("hermes", "messages", rowKey, "tool_result")
		events = append(events, evt)
	case "system", "user":
		if content != "" {
			evt := base
			evt.EventKind = "message"
			evt.ActorRole = role
			evt.TextContent = content
			evt.SourceOffset = stableOffset("hermes", "messages", rowKey, "message")
			evt.RawPayload = sqliteStableRaw("hermes", "messages", rowKey, "message")
			events = append(events, evt)
		}
	default:
		evt := base
		evt.EventKind = "event_msg"
		evt.ActorRole = "system"
		evt.PayloadType = role
		evt.TextContent = content
		evt.SourceOffset = stableOffset("hermes", "messages", rowKey, "event")
		evt.RawPayload = sqliteStableRaw("hermes", "messages", rowKey, "event")
		events = append(events, evt)
	}

	return events
}

type hermesToolCall struct {
	id    string
	name  string
	input string
}

func hermesToolCalls(raw string) []hermesToolCall {
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	var items []any
	switch v := decoded.(type) {
	case []any:
		items = v
	case map[string]any:
		items = []any{v}
	}

	var calls []hermesToolCall
	for _, item := range items {
		m := objectFromAny(item)
		if m == nil {
			continue
		}
		call := hermesToolCall{
			id:   firstNonEmpty(stringFromAny(m["id"]), stringFromAny(m["tool_call_id"])),
			name: stringFromAny(m["name"]),
		}
		if fn := objectFromAny(m["function"]); fn != nil {
			call.name = firstNonEmpty(call.name, stringFromAny(fn["name"]))
			if args, ok := fn["arguments"]; ok {
				call.input = jsonPayload(args)
			}
		}
		if call.input == "" {
			if args, ok := m["arguments"]; ok {
				call.input = jsonPayload(args)
			} else if args, ok := m["input"]; ok {
				call.input = jsonPayload(args)
			}
		}
		if call.name != "" || call.id != "" {
			calls = append(calls, call)
		}
	}
	return calls
}
