package capture

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/johnnygreco/beacon/internal/models"
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
	cwd               string
	billingProvider   string
	costUSD           float64
	title             string
}

type hermesMessageReasoning struct {
	reasoningContent    string
	reasoning           string
	reasoningDetails    string
	codexReasoningItems string
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
	costExpr := hermesFirstOptionalColumnSelectExpr(db, "sessions", []string{"actual_cost_usd", "estimated_cost_usd"}, "0")
	rows, err := db.Query(fmt.Sprintf(`SELECT id, %s, %s, %s, started_at, %s,
	       %s, %s, %s, %s,
	       %s, %s, %s, %s,
	       %s, %s
		FROM sessions
		ORDER BY started_at, id`,
		hermesOptionalColumnSelectExpr(db, "sessions", "source"),
		hermesOptionalColumnSelectExpr(db, "sessions", "model"),
		hermesOptionalColumnSelectExpr(db, "sessions", "parent_session_id"),
		hermesOptionalColumnSelectExpr(db, "sessions", "ended_at"),
		hermesOptionalColumnSelectExpr(db, "sessions", "end_reason"),
		hermesOptionalColumnSelectExpr(db, "sessions", "input_tokens"),
		hermesOptionalColumnSelectExpr(db, "sessions", "output_tokens"),
		hermesOptionalColumnSelectExpr(db, "sessions", "cache_read_tokens"),
		hermesOptionalColumnSelectExpr(db, "sessions", "cache_write_tokens"),
		hermesOptionalColumnSelectExpr(db, "sessions", "reasoning_tokens"),
		hermesOptionalColumnSelectExpr(db, "sessions", "cwd"),
		hermesOptionalColumnSelectExpr(db, "sessions", "billing_provider"),
		costExpr,
		hermesOptionalColumnSelectExpr(db, "sessions", "title"),
	))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]hermesSessionRow)
	for rows.Next() {
		var row hermesSessionRow
		var source, model, parent, endReason, cwd, provider, title sql.NullString
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
			&cwd,
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
		row.cwd = cwd.String
		row.billingProvider = provider.String
		row.costUSD = cost.Float64
		row.title = title.String
		result[row.id] = row
	}
	return result, rows.Err()
}

func hermesSessionEvents(file string, sess hermesSessionRow) []NormalizedEvent {
	base := NormalizedEvent{
		SessionID:          sess.id,
		RawSessionID:       sess.id,
		RawParentSessionID: sess.parentSessionID,
		SourceName:         "hermes",
		Runtime:            models.RuntimeHermesAgent,
		Provider:           firstNonEmpty(sess.billingProvider, models.ProviderMulti),
		Format:             models.FormatSQLite,
		Timestamp:          timeFromUnixSeconds(sess.startedAt),
		Model:              sess.model,
		ParentSessionID:    sess.parentSessionID,
		CWD:                sess.cwd,
		SourceFile:         file,
		SourceLineNo:       stableLineNo("hermes", "sessions", sess.id),
		SourceOffset:       stableOffset("hermes", "sessions", sess.id),
		RawEventID:         "sessions:" + sess.id,
	}

	meta := base
	meta.EventKind = models.EventKindSessionMeta
	meta.ActorRole = models.ActorRoleSystem
	meta.PayloadType = firstNonEmpty(sess.source, "session")
	meta.TextContent = sess.title
	meta.RawPayload = sqliteStableRaw("hermes", "sessions", sess.id, "session_meta")

	events := []NormalizedEvent{meta}

	if sess.inputTokens > 0 || sess.outputTokens > 0 || sess.cacheReadTokens > 0 || sess.cacheCreateTokens > 0 || sess.costUSD > 0 {
		usage := base
		usage.EventKind = models.EventKindEventMsg
		usage.ActorRole = models.ActorRoleSystem
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
		end.EventKind = models.EventKindSessionEnd
		end.ActorRole = models.ActorRoleSystem
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
	reasoningExprs := hermesReasoningSelectExprs(db)
	activeFilter := hermesMessagesActiveFilter(db)
	rows, err := db.Query(fmt.Sprintf(`SELECT id, session_id, role, %s, %s, %s,
	       %s, timestamp, %s, %s, %s, %s, %s
		FROM messages
		%s
		ORDER BY session_id, id`,
		hermesOptionalColumnSelectExpr(db, "messages", "content"),
		hermesOptionalColumnSelectExpr(db, "messages", "tool_call_id"),
		hermesOptionalColumnSelectExpr(db, "messages", "tool_calls"),
		hermesOptionalColumnSelectExpr(db, "messages", "tool_name"),
		hermesOptionalColumnSelectExpr(db, "messages", "finish_reason"),
		reasoningExprs[0],
		reasoningExprs[1],
		reasoningExprs[2],
		reasoningExprs[3],
		activeFilter,
	))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []NormalizedEvent
	lastTimestampBySession := make(map[string]float64)
	for rows.Next() {
		var id int64
		var sessionID, role string
		var content, toolCallID, toolCalls, toolName, finishReason sql.NullString
		var reasoningContent, reasoning, reasoningDetails, codexReasoningItems sql.NullString
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
			&reasoning,
			&reasoningDetails,
			&codexReasoningItems,
		); err != nil {
			return nil, err
		}

		sess := sessions[sessionID]
		timestampValue := ts.Float64
		if ts.Valid && timestampValue > 0 {
			if last := lastTimestampBySession[sessionID]; last > 0 && timestampValue <= last {
				timestampValue = last + 0.001
			}
			lastTimestampBySession[sessionID] = timestampValue
		}
		base := NormalizedEvent{
			SessionID:          sessionID,
			RawSessionID:       sessionID,
			RawParentSessionID: sess.parentSessionID,
			SourceName:         "hermes",
			Runtime:            models.RuntimeHermesAgent,
			Provider:           firstNonEmpty(sess.billingProvider, models.ProviderMulti),
			Format:             models.FormatSQLite,
			Timestamp:          timeFromUnixSeconds(timestampValue),
			Model:              sess.model,
			ParentSessionID:    sess.parentSessionID,
			CWD:                sess.cwd,
			SourceFile:         file,
			SourceLineNo:       stableLineNo("hermes", "messages", fmt.Sprint(id)),
			SourceOffset:       stableOffset("hermes", "messages", fmt.Sprint(id)),
			MessageUUID:        fmt.Sprint(id),
			RawEventID:         fmt.Sprint(id),
		}

		reasoningFields := hermesMessageReasoning{
			reasoningContent:    reasoningContent.String,
			reasoning:           reasoning.String,
			reasoningDetails:    reasoningDetails.String,
			codexReasoningItems: codexReasoningItems.String,
		}
		rowEvents := hermesMessageEvents(base, id, role, content.String, toolCallID.String, toolCalls.String, toolName.String, finishReason.String, reasoningFields)
		events = append(events, rowEvents...)
	}
	return events, rows.Err()
}

func hermesMessagesActiveFilter(db *sql.DB) string {
	if !sqliteHasColumn(db, "messages", "active") {
		return ""
	}
	return "WHERE COALESCE(active, 1) != 0"
}

func hermesOptionalColumnSelectExpr(db *sql.DB, table, column string) string {
	if !sqliteHasColumn(db, table, column) {
		return "NULL"
	}
	return hermesQuotedIdentifier(column)
}

func hermesFirstOptionalColumnSelectExpr(db *sql.DB, table string, columns []string, fallback string) string {
	expressions := make([]string, 0, len(columns))
	for _, column := range columns {
		if sqliteHasColumn(db, table, column) {
			expressions = append(expressions, hermesQuotedIdentifier(column))
		}
	}
	if len(expressions) == 0 {
		return fallback
	}
	return "COALESCE(" + strings.Join(append(expressions, fallback), ", ") + ")"
}

func hermesReasoningSelectExprs(db *sql.DB) []string {
	candidates := []string{
		"reasoning_content",
		"reasoning",
		"reasoning_details",
		"codex_reasoning_items",
	}
	expressions := make([]string, 0, len(candidates))
	for _, column := range candidates {
		expressions = append(expressions, hermesOptionalColumnSelectExpr(db, "messages", column))
	}
	return expressions
}

func hermesQuotedIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func hermesMessageEvents(base NormalizedEvent, rowID int64, role, rawContent, toolCallID, toolCalls, toolName, finishReason string, reasoning hermesMessageReasoning) []NormalizedEvent {
	rowKey := fmt.Sprint(rowID)
	var events []NormalizedEvent
	content := textFromHarnessContent(decodeHarnessJSON(rawContent))

	if reasoningText, payloadType := hermesReasoningText(reasoning); reasoningText != "" || payloadType != "" {
		evt := base
		evt.EventKind = models.EventKindReasoning
		evt.ActorRole = models.ActorRoleAssistant
		evt.PayloadType = payloadType
		evt.TextContent = reasoningText
		evt.SourceOffset = stableOffset("hermes", "messages", rowKey, "reasoning")
		evt.RawPayload = sqliteStableRaw("hermes", "messages", rowKey, "reasoning")
		events = append(events, evt)
	}

	if finishReason == "error" {
		evt := base
		evt.EventKind = models.EventKindError
		evt.ActorRole = models.ActorRoleAssistant
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
			evt.EventKind = models.EventKindMessage
			evt.ActorRole = models.ActorRoleAssistant
			evt.TextContent = content
			evt.SourceOffset = stableOffset("hermes", "messages", rowKey, "message")
			evt.RawPayload = sqliteStableRaw("hermes", "messages", rowKey, "message")
			events = append(events, evt)
		}
		for i, call := range hermesToolCalls(toolCalls) {
			evt := base
			evt.EventKind = models.EventKindToolCall
			evt.ActorRole = models.ActorRoleAssistant
			evt.ToolPhase = models.ToolPhaseCall
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
		evt.EventKind = models.EventKindToolResult
		evt.ActorRole = models.ActorRoleTool
		evt.ToolPhase = models.ToolPhaseResult
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
			evt.EventKind = models.EventKindMessage
			evt.ActorRole = role
			evt.TextContent = content
			evt.SourceOffset = stableOffset("hermes", "messages", rowKey, "message")
			evt.RawPayload = sqliteStableRaw("hermes", "messages", rowKey, "message")
			events = append(events, evt)
		}
	default:
		evt := base
		evt.EventKind = models.EventKindEventMsg
		evt.ActorRole = models.ActorRoleSystem
		evt.PayloadType = role
		evt.TextContent = content
		evt.SourceOffset = stableOffset("hermes", "messages", rowKey, "event")
		evt.RawPayload = sqliteStableRaw("hermes", "messages", rowKey, "event")
		events = append(events, evt)
	}

	return events
}

func hermesReasoningText(reasoning hermesMessageReasoning) (string, string) {
	for _, raw := range []string{
		reasoning.reasoningContent,
		reasoning.reasoning,
		reasoning.reasoningDetails,
	} {
		if text := textFromHarnessContent(decodeHarnessJSON(raw)); text != "" {
			return text, ""
		}
	}
	return hermesCodexReasoningText(reasoning.codexReasoningItems)
}

func hermesCodexReasoningText(raw string) (string, string) {
	decoded := decodeHarnessJSON(raw)
	if text := hermesCodexReasoningTextFromAny(decoded); text != "" {
		return text, ""
	}
	if hermesHasEncryptedReasoning(decoded) {
		return "", "encrypted"
	}
	return "", ""
}

func hermesCodexReasoningTextFromAny(v any) string {
	switch c := v.(type) {
	case []any:
		var texts []string
		for _, item := range c {
			if text := hermesCodexReasoningTextFromAny(item); text != "" {
				texts = append(texts, text)
			}
		}
		return strings.Join(texts, "")
	case map[string]any:
		if summary := stringFromAny(c["summary"]); summary != "" {
			return summary
		}
		var texts []string
		for _, item := range arrayFromAny(c["summary"]) {
			if text := stringField(objectFromAny(item), "text"); text != "" {
				texts = append(texts, text)
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "")
		}
	}
	return textFromHarnessContent(v)
}

func hermesHasEncryptedReasoning(v any) bool {
	switch c := v.(type) {
	case []any:
		for _, item := range c {
			if hermesHasEncryptedReasoning(item) {
				return true
			}
		}
	case map[string]any:
		if _, ok := c["encrypted_content"]; ok {
			return true
		}
	}
	return false
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
