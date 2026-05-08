package mcp

import (
	"encoding/json"
	"time"

	"github.com/johnnygreco/beacon/internal/search"
)

func FormatSearchResults(results []search.SearchResult) string {
	type result struct {
		EventID     string    `json:"event_id"`
		SessionID   string    `json:"session_id"`
		EventKind   string    `json:"event_kind"`
		TextPreview string    `json:"text_preview"`
		Score       float64   `json:"score"`
		Timestamp   time.Time `json:"timestamp"`
		ToolName    string    `json:"tool_name,omitempty"`
		Model       string    `json:"model,omitempty"`
		Provider    string    `json:"provider,omitempty"`
	}

	payload := struct {
		Schema      string         `json:"schema"`
		Tool        string         `json:"tool"`
		Results     []result       `json:"results"`
		Warnings    []string       `json:"warnings"`
		Performance map[string]any `json:"performance"`
	}{
		Schema:      "beacon.mcp.search_sessions.v1",
		Tool:        "search_sessions",
		Results:     []result{},
		Warnings:    []string{},
		Performance: map[string]any{"result_count": len(results)},
	}

	for _, r := range results {
		payload.Results = append(payload.Results, result{
			EventID:     "event:" + r.EventUID,
			SessionID:   "session:" + r.SessionID,
			EventKind:   r.EventKind,
			TextPreview: r.TextPreview,
			Score:       r.Score,
			Timestamp:   r.Timestamp,
			ToolName:    r.ToolName,
			Model:       r.Model,
			Provider:    r.Provider,
		})
	}
	return mustJSON(payload)
}

type contextEvent struct {
	EventUID    string
	EventKind   string
	ActorRole   string
	TextPreview string
	ToolName    string
	Model       string
	Tokens      int64
	Timestamp   time.Time
}

func FormatOpenContext(events []contextEvent, targetIdx int) string {
	type event struct {
		EventID     string    `json:"event_id"`
		EventKind   string    `json:"event_kind"`
		ActorRole   string    `json:"actor_role"`
		TextPreview string    `json:"text_preview"`
		ToolName    string    `json:"tool_name,omitempty"`
		Model       string    `json:"model,omitempty"`
		Tokens      int64     `json:"tokens"`
		Timestamp   time.Time `json:"timestamp"`
		Target      bool      `json:"target"`
	}
	payload := struct {
		Schema   string   `json:"schema"`
		Tool     string   `json:"tool"`
		Events   []event  `json:"events"`
		Warnings []string `json:"warnings"`
	}{
		Schema:   "beacon.mcp.open.v1",
		Tool:     "open",
		Events:   []event{},
		Warnings: []string{},
	}
	for i, e := range events {
		payload.Events = append(payload.Events, event{
			EventID:     "event:" + e.EventUID,
			EventKind:   e.EventKind,
			ActorRole:   e.ActorRole,
			TextPreview: e.TextPreview,
			ToolName:    e.ToolName,
			Model:       e.Model,
			Tokens:      e.Tokens,
			Timestamp:   e.Timestamp,
			Target:      i == targetIdx,
		})
	}
	return mustJSON(payload)
}

type sessionInfo struct {
	SessionID     string
	SourceName    string
	StartedAt     time.Time
	EndedAt       time.Time
	EventCount    int64
	TurnCount     int64
	TotalTokens   int64
	ToolCallCount int64
	MCPCallCount  int64
	ErrorCount    int64
	LastModel     string
}

func FormatSessionList(sessions []sessionInfo) string {
	type session struct {
		SessionID     string    `json:"session_id"`
		SourceName    string    `json:"source_name"`
		StartedAt     time.Time `json:"started_at"`
		EndedAt       time.Time `json:"ended_at"`
		EventCount    int64     `json:"event_count"`
		TurnCount     int64     `json:"turn_count"`
		TotalTokens   int64     `json:"total_tokens"`
		ToolCallCount int64     `json:"tool_call_count"`
		MCPCallCount  int64     `json:"mcp_call_count"`
		ErrorCount    int64     `json:"error_count"`
		LastModel     string    `json:"last_model,omitempty"`
	}
	payload := struct {
		Schema   string    `json:"schema"`
		Tool     string    `json:"tool"`
		Results  []session `json:"results"`
		Warnings []string  `json:"warnings"`
	}{
		Schema:   "beacon.mcp.list_sessions.v1",
		Tool:     "list_sessions",
		Results:  []session{},
		Warnings: []string{},
	}
	for _, s := range sessions {
		payload.Results = append(payload.Results, session{
			SessionID:     "session:" + s.SessionID,
			SourceName:    s.SourceName,
			StartedAt:     s.StartedAt,
			EndedAt:       s.EndedAt,
			EventCount:    s.EventCount,
			TurnCount:     s.TurnCount,
			TotalTokens:   s.TotalTokens,
			ToolCallCount: s.ToolCallCount,
			MCPCallCount:  s.MCPCallCount,
			ErrorCount:    s.ErrorCount,
			LastModel:     s.LastModel,
		})
	}
	return mustJSON(payload)
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{"schema":"beacon.mcp.error.v1","error":"json marshal failed"}`
	}
	return string(data)
}
