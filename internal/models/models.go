package models

import (
	"strings"
	"time"
)

const MCPToolPrefix = "mcp__"

// IsMCPTool returns true if the tool name indicates an MCP tool.
func IsMCPTool(name string) bool {
	return strings.HasPrefix(name, MCPToolPrefix)
}

type Event struct {
	EventUID          string     `json:"event_uid"`
	SessionID         string     `json:"session_id"`
	ParentSessionID   string     `json:"parent_session_id,omitempty"`
	SessionDate       *time.Time `json:"session_date,omitempty"`
	SourceName        string     `json:"source_name"`
	Provider          string     `json:"provider"`
	EventKind         string     `json:"event_kind"`
	PayloadType       string     `json:"payload_type"`
	ActorRole         string     `json:"actor_role"`
	Timestamp         time.Time  `json:"timestamp"`
	TextContent       string     `json:"text_content"`
	TextPreview       string     `json:"text_preview"`
	ToolName          string     `json:"tool_name"`
	Model             string     `json:"model"`
	InputTokens       int64      `json:"input_tokens"`
	OutputTokens      int64      `json:"output_tokens"`
	CacheReadTokens   int64      `json:"cache_read_tokens"`
	CacheCreateTokens int64      `json:"cache_create_tokens"`
	DurationMs        int64      `json:"duration_ms"`
	CostUSD           float64    `json:"cost_usd"`
	ErrorCode         string     `json:"error_code"`
	ErrorMessage      string     `json:"error_message"`
	EventVersion      int        `json:"event_version"`
	PayloadJSON       string     `json:"payload_json"`
	CWD               string     `json:"cwd"`
	SourceFile        string     `json:"source_file"`
	SourceLineNo      int        `json:"source_line_no"`
	SourceOffset      int64      `json:"source_offset"`
	CreatedAt         time.Time  `json:"created_at"`
}

type EventLink struct {
	EventUID       string `json:"event_uid"`
	LinkedEventUID string `json:"linked_event_uid"`
	LinkType       string `json:"link_type"`
}

type ToolIO struct {
	EventUID      string `json:"event_uid"`
	ToolName      string `json:"tool_name"`
	ToolPhase     string `json:"tool_phase"`
	InputJSON     string `json:"input_json"`
	OutputJSON    string `json:"output_json"`
	InputPreview  string `json:"input_preview"`
	OutputPreview string `json:"output_preview"`
}

type IngestError struct {
	ID              string    `json:"id"`
	SourceFile      string    `json:"source_file"`
	SourceLineNo    int       `json:"source_line_no"`
	ErrorClass      string    `json:"error_class"`
	ErrorMessage    string    `json:"error_message"`
	ContextFragment string    `json:"context_fragment"`
	CreatedAt       time.Time `json:"created_at"`
}

type Checkpoint struct {
	SourceName       string `json:"source_name"`
	SourceFile       string `json:"source_file"`
	SourceInode      int64  `json:"source_inode"`
	SourceGeneration int    `json:"source_generation"`
	LastOffset       int64  `json:"last_offset"`
	LastLineNo       int    `json:"last_line_no"`
}
