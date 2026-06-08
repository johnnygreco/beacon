package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/views"
)

// QuerySessionConversation returns the conversation trace for a session.
func QuerySessionConversation(ctx context.Context, db *sql.DB, id string) ([]views.ChatTurn, []views.TurnDetail) {
	return QuerySessionConversationScoped(ctx, db, id, APIScopeFilters{})
}

func QuerySessionConversationScoped(ctx context.Context, db *sql.DB, id string, scope APIScopeFilters) ([]views.ChatTurn, []views.TurnDetail) {
	sessionScope := scope
	sessionScope.ProjectKeys = nil
	sessionScopeClause, sessionScopeArgs := sessionScope.sqlAndClause("")
	eventScopeClause, eventScopeArgs := scope.eventAndSessionProjectSQLAndClause("e", "e.cwd", "s")
	args := []any{id}
	args = append(args, sessionScopeArgs...)
	args = append(args, eventScopeArgs...)
	traceRows, err := db.QueryContext(ctx,
		`WITH scoped_session AS (
			SELECT session_id
			FROM session_projection FINAL
			WHERE session_id = ?`+sessionScopeClause+`
			LIMIT 1
		),
		trace AS (
			SELECT e.*,
			       row_number() OVER (PARTITION BY session_id ORDER BY timestamp, event_uid) AS event_order,
			       sum(if(event_kind = 'message' AND actor_role = 'user', 1, 0))
			         OVER (PARTITION BY session_id ORDER BY timestamp, event_uid) AS turn_seq
			FROM `+latestActivityEventsSubquery("ae.session_id IN (SELECT session_id FROM scoped_session)")+` e
			LEFT JOIN (
				SELECT session_id, project_key
				FROM session_projection FINAL
			) AS s ON s.session_id = e.session_id
			WHERE 1 = 1`+eventScopeClause+`
		),
		payload_previews AS (
			SELECT event_uid,
			       argMax(input_preview, captured_at) AS input_preview,
			       argMax(output_preview, captured_at) AS output_preview
			FROM tool_payloads
			WHERE event_uid IN (SELECT event_uid FROM trace)
			GROUP BY event_uid
		)
		 SELECT e.event_uid, e.event_kind, COALESCE(e.payload_type, ''), COALESCE(e.actor_role, ''),
		        COALESCE(e.text_content, ''), COALESCE(e.text_preview, ''),
		        COALESCE(e.tool_name, ''), COALESCE(e.tool_use_id, ''), COALESCE(e.model, ''),
		        e.input_tokens + e.output_tokens, e.duration_ms, e.timestamp, turn_seq,
		        COALESCE(tio.input_preview, ''), COALESCE(tio.output_preview, ''),
		        '' AS input_json, '' AS output_json
		 FROM trace e
		 LEFT JOIN payload_previews tio ON e.event_uid = tio.event_uid
		 ORDER BY event_order`, args...)
	if err != nil {
		logQueryError("session conversation", err)
		return nil, nil
	}
	defer traceRows.Close()

	turnMap := make(map[int]*views.TurnDetail)
	var turnOrder []int

	for traceRows.Next() {
		var es views.EventSummary
		var turnSeq int
		if err := traceRows.Scan(&es.EventUID, &es.EventKind, &es.PayloadType, &es.ActorRole,
			&es.TextContent, &es.TextPreview,
			&es.ToolName, &es.ToolUseID, &es.Model, &es.Tokens, &es.DurationMs, &es.Timestamp, &turnSeq,
			&es.InputPreview, &es.OutputPreview, &es.InputJSON, &es.OutputJSON); err != nil {
			logQueryScanError("session conversation", err)
			continue
		}

		td, ok := turnMap[turnSeq]
		if !ok {
			td = &views.TurnDetail{
				TurnSeq:   turnSeq,
				StartedAt: es.Timestamp,
			}
			turnMap[turnSeq] = td
			turnOrder = append(turnOrder, turnSeq)
		}

		td.Events = append(td.Events, es)
		td.TotalTokens += es.Tokens
	}
	if err := traceRows.Err(); err != nil {
		logQueryError("session conversation rows", err)
		return nil, nil
	}

	var turns []views.TurnDetail
	for _, seq := range turnOrder {
		turns = append(turns, *turnMap[seq])
	}

	chatTurns := buildChatTurns(deduplicateTurns(turns))
	return chatTurns, turns
}

// parseToolParams extracts structured parameters from tool input JSON.
// Returns nil if the JSON is empty or unparseable.
func parseToolParams(inputJSON string) *views.ToolCallParams {
	if inputJSON == "" {
		return nil
	}
	var params views.ToolCallParams
	if err := json.Unmarshal([]byte(inputJSON), &params); err != nil {
		return nil
	}
	return &params
}

// buildChatTurns converts flat TurnDetail slices into structured ChatTurns
// by grouping consecutive tool_call/tool_result events into tool chains.
func buildChatTurns(turns []views.TurnDetail) []views.ChatTurn {
	var chatTurns []views.ChatTurn
	for _, t := range turns {
		ct := views.ChatTurn{
			TurnSeq:     t.TurnSeq,
			TotalTokens: t.TotalTokens,
			StartedAt:   t.StartedAt,
		}

		var pendingToolChain []views.ToolChainItem
		var pendingReasoning []views.EventSummary

		flushToolChain := func() {
			if len(pendingToolChain) > 0 {
				// Separate Agent tool calls into their own top-level blocks
				var regularTools []views.ToolChainItem
				for _, item := range pendingToolChain {
					if item.ToolName == "Agent" {
						// Flush any accumulated regular tools first
						if len(regularTools) > 0 {
							ct.Blocks = append(ct.Blocks, views.ChatBlock{
								Kind:      views.ChatBlockToolChain,
								ToolChain: regularTools,
							})
							regularTools = nil
						}
						ct.Blocks = append(ct.Blocks, views.ChatBlock{
							Kind:      views.ChatBlockSubagentDispatch,
							ToolChain: []views.ToolChainItem{item},
						})
					} else {
						regularTools = append(regularTools, item)
					}
				}
				if len(regularTools) > 0 {
					ct.Blocks = append(ct.Blocks, views.ChatBlock{
						Kind:      views.ChatBlockToolChain,
						ToolChain: regularTools,
					})
				}
				pendingToolChain = nil
			}
		}

		flushReasoning := func() {
			if len(pendingReasoning) > 0 {
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:     views.ChatBlockReasoning,
					Messages: pendingReasoning,
				})
				pendingReasoning = nil
			}
		}

		// Pre-build a map of tool_use_id → tool_result for call_id-based matching.
		// This handles Codex's pattern of batched calls then batched results.
		resultByCallID := make(map[string]int) // tool_use_id → event index
		consumedResults := make(map[int]bool)
		for idx, e := range t.Events {
			if e.EventKind == models.EventKindToolResult && e.ToolUseID != "" {
				resultByCallID[e.ToolUseID] = idx
			}
		}

		for i := 0; i < len(t.Events); i++ {
			e := t.Events[i]

			switch e.EventKind {
			case models.EventKindToolCall:
				inputForParams := e.InputJSON
				if inputForParams == "" {
					inputForParams = e.InputPreview
				}
				item := views.ToolChainItem{
					CallEvent:    e,
					ToolName:     e.ToolName,
					InputPreview: e.InputPreview,
					InputJSON:    e.InputJSON,
				}
				item.Params = parseToolParams(inputForParams)

				// Try call_id-based matching first
				if e.ToolUseID != "" {
					if ridx, ok := resultByCallID[e.ToolUseID]; ok {
						result := t.Events[ridx]
						// Copy tool name to result if missing
						if result.ToolName == "" {
							result.ToolName = e.ToolName
						}
						item.ResultEvent = &result
						item.OutputPreview = result.OutputPreview
						if item.OutputPreview == "" {
							item.OutputPreview = result.TextPreview
						}
						item.OutputJSON = result.OutputJSON
						consumedResults[ridx] = true
					}
				} else {
					// Fallback: sequential look-ahead for sources without call_id
					for j := i + 1; j < len(t.Events); j++ {
						if t.Events[j].EventKind == models.EventKindToolResult && !consumedResults[j] {
							consumedResults[j] = true
							i = j
							result := t.Events[j]
							item.ResultEvent = &result
							item.OutputPreview = result.OutputPreview
							if item.OutputPreview == "" {
								item.OutputPreview = result.TextPreview
							}
							item.OutputJSON = result.OutputJSON
							break
						} else if t.Events[j].EventKind == models.EventKindEventMsg {
							continue // skip intermediate log events
						} else {
							break // something else — don't consume it
						}
					}
				}
				pendingToolChain = append(pendingToolChain, item)

			case models.EventKindToolResult:
				if consumedResults[i] {
					break // Already paired with a tool_call
				}
				// Orphan tool_result (no preceding tool_call)
				item := views.ToolChainItem{
					CallEvent:     e,
					ToolName:      e.ToolName,
					OutputPreview: e.OutputPreview,
					OutputJSON:    e.OutputJSON,
				}
				if item.OutputPreview == "" {
					item.OutputPreview = e.TextPreview
				}
				pendingToolChain = append(pendingToolChain, item)

			case models.EventKindMessage:
				// Don't let empty assistant messages break a tool chain
				text := e.TextContent
				if text == "" {
					text = e.TextPreview
				}
				if e.ActorRole == models.ActorRoleUser || strings.TrimSpace(text) != "" {
					flushReasoning()
					flushToolChain()
					eCopy := e
					kind := views.ChatBlockAssistantMessage
					if e.ActorRole == models.ActorRoleUser {
						kind = views.ChatBlockUserMessage
					}
					ct.Blocks = append(ct.Blocks, views.ChatBlock{
						Kind:    kind,
						Message: &eCopy,
					})
				}

			case models.EventKindReasoning:
				// Accumulate consecutive reasoning events into a group
				pendingReasoning = append(pendingReasoning, e)

			case models.EventKindError:
				flushReasoning()
				flushToolChain()
				eCopy := e
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:    views.ChatBlockError,
					Message: &eCopy,
				})

			case models.EventKindToolError:
				flushReasoning()
				flushToolChain()
				eCopy := e
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:    views.ChatBlockToolError,
					Message: &eCopy,
				})

			case models.EventKindEventMsg:
				// Intermediate log events — skip without breaking tool chains

			default:
				flushReasoning()
				flushToolChain()
				eCopy := e
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:    views.ChatBlockAssistantMessage,
					Message: &eCopy,
				})
			}
		}

		flushReasoning()
		flushToolChain()

		// Compute per-turn tool stats
		statsMap := make(map[string]int)
		for _, block := range ct.Blocks {
			if block.Kind == views.ChatBlockToolChain {
				for _, item := range block.ToolChain {
					statsMap[item.ToolName]++
				}
			}
		}
		if len(statsMap) > 0 {
			stats := make([]views.ToolStatEntry, 0, len(statsMap))
			for name, count := range statsMap {
				stats = append(stats, views.ToolStatEntry{Name: name, Count: count})
			}
			sort.Slice(stats, func(i, j int) bool {
				if stats[i].Count != stats[j].Count {
					return stats[i].Count > stats[j].Count
				}
				return stats[i].Name < stats[j].Name
			})
			ct.ToolStats = stats
		}

		chatTurns = append(chatTurns, ct)
	}
	return chatTurns
}

// deduplicateTurns removes duplicate events within and across turns.
// Claude Code JSONL can log the same content twice (e.g. "human"+"result" entries).
// This merges orphan turns (single user msg duplicated in the next turn) and
// removes consecutive duplicate events within each turn.
func deduplicateTurns(turns []views.TurnDetail) []views.TurnDetail {
	if len(turns) <= 1 {
		return turns
	}

	var result []views.TurnDetail
	for i, t := range turns {
		// Merge orphan turn: if this turn has a single user message that's
		// identical to the first user message in the next turn, skip it.
		if i+1 < len(turns) && len(t.Events) == 1 &&
			t.Events[0].EventKind == models.EventKindMessage && t.Events[0].ActorRole == models.ActorRoleUser {
			nextTurn := turns[i+1]
			if len(nextTurn.Events) > 0 && nextTurn.Events[0].EventKind == models.EventKindMessage &&
				nextTurn.Events[0].ActorRole == models.ActorRoleUser &&
				nextTurn.Events[0].TextContent == t.Events[0].TextContent {
				continue // skip this orphan turn
			}
		}

		// Remove duplicate events within the turn.
		// Claude Code JSONL logs the same content in multiple line types
		// with different UIDs. Deduplicate by content for reasoning and
		// message events to avoid showing the same text twice.
		var deduped []views.EventSummary
		seen := make(map[string]bool)
		for _, e := range t.Events {
			var key string
			switch {
			case e.EventKind == models.EventKindReasoning && e.TextContent != "":
				key = e.EventKind + "|" + e.TextContent
			case e.EventKind == models.EventKindReasoning:
				// Empty reasoning (redacted thinking) — use UID to preserve each block
				key = e.EventUID
			case e.EventKind == models.EventKindMessage && e.TextContent != "":
				key = e.EventKind + "|" + e.ActorRole + "|" + e.TextContent
			default:
				key = e.EventUID + "|" + e.EventKind + "|" + e.ActorRole + "|" + e.TextContent + "|" + e.ToolName + "|" + e.InputJSON + "|" + e.InputPreview
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			deduped = append(deduped, e)
		}
		t.Events = deduped
		// Recompute total tokens after dedup
		t.TotalTokens = views.SumTokens(deduped)
		result = append(result, t)
	}
	return result
}
