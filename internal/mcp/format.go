package mcp

import (
	"encoding/json"
	"time"

	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/usage"
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
	Provider      string
	StartedAt     time.Time
	EndedAt       time.Time
	EventCount    int64
	TurnCount     int64
	TotalTokens   int64
	ToolCallCount int64
	MCPCallCount  int64
	ErrorCount    int64
	LastModel     string
	WorkingDir    string
}

type sessionListMetadata struct {
	ResultCount        int
	TotalMatchingCount int64
	Limit              int
	Cursor             string
	ResultComplete     bool
	NextCursor         string
}

func FormatSessionList(sessions []sessionInfo, metadataOpt ...sessionListMetadata) string {
	type session struct {
		SessionID     string    `json:"session_id"`
		SourceName    string    `json:"source_name"`
		Provider      string    `json:"provider,omitempty"`
		StartedAt     time.Time `json:"started_at"`
		EndedAt       time.Time `json:"ended_at"`
		EventCount    int64     `json:"event_count"`
		TurnCount     int64     `json:"turn_count"`
		TotalTokens   int64     `json:"total_tokens"`
		ToolCallCount int64     `json:"tool_call_count"`
		MCPCallCount  int64     `json:"mcp_call_count"`
		ErrorCount    int64     `json:"error_count"`
		LastModel     string    `json:"last_model,omitempty"`
		WorkingDir    string    `json:"working_dir,omitempty"`
	}
	type metadata struct {
		ResultCount        int    `json:"result_count"`
		TotalMatchingCount int64  `json:"total_matching_count"`
		Limit              int    `json:"limit"`
		Cursor             string `json:"cursor,omitempty"`
		ResultComplete     bool   `json:"result_complete"`
		NextCursor         string `json:"next_cursor"`
	}
	meta := sessionListMetadata{
		ResultCount:        len(sessions),
		TotalMatchingCount: int64(len(sessions)),
		Limit:              len(sessions),
		ResultComplete:     true,
	}
	if len(metadataOpt) > 0 {
		meta = metadataOpt[0]
	}
	payload := struct {
		Schema   string    `json:"schema"`
		Tool     string    `json:"tool"`
		Results  []session `json:"results"`
		Metadata metadata  `json:"metadata"`
		Warnings []string  `json:"warnings"`
	}{
		Schema:   "beacon.mcp.list_sessions.v1",
		Tool:     "list_sessions",
		Results:  []session{},
		Metadata: metadata(meta),
		Warnings: []string{},
	}
	for _, s := range sessions {
		payload.Results = append(payload.Results, session{
			SessionID:     "session:" + s.SessionID,
			SourceName:    s.SourceName,
			Provider:      s.Provider,
			StartedAt:     s.StartedAt,
			EndedAt:       s.EndedAt,
			EventCount:    s.EventCount,
			TurnCount:     s.TurnCount,
			TotalTokens:   s.TotalTokens,
			ToolCallCount: s.ToolCallCount,
			MCPCallCount:  s.MCPCallCount,
			ErrorCount:    s.ErrorCount,
			LastModel:     s.LastModel,
			WorkingDir:    s.WorkingDir,
		})
	}
	return mustJSON(payload)
}

func FormatUsageSummary(result usage.Result) string {
	payload := struct {
		Schema string `json:"schema"`
		Tool   string `json:"tool"`
		usage.Result
	}{
		Schema: "beacon.mcp.usage_summary.v1",
		Tool:   "usage_summary",
		Result: result,
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
