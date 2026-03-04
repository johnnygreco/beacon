package models

import "time"

type Actor struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	OrgTeam   string `json:"org_team"`
	MachineID string `json:"machine_id"`
	CreatedAt time.Time `json:"created_at"`
}

type Session struct {
	ID         string    `json:"id"`
	ActorID    string    `json:"actor_id"`
	Source     string    `json:"source"` // claude_code, codex, cursor
	StartedAt  time.Time `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	CWD        string    `json:"cwd"`
	GitRepo    string    `json:"git_repo"`
	TotalCost  float64   `json:"total_cost"`
}

type Turn struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id"`
	TurnNumber   int       `json:"turn_number"`
	UserPrompt   string    `json:"user_prompt"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CacheRead    int64     `json:"cache_read"`
	CacheCreate  int64     `json:"cache_create"`
	CostUSD      float64   `json:"cost_usd"`
}

type ModelCall struct {
	ID            string    `json:"id"`
	SessionID     string    `json:"session_id"`
	TurnID        string    `json:"turn_id"`
	Model         string    `json:"model"`
	Provider      string    `json:"provider"`
	InputTokens   int64     `json:"input_tokens"`
	OutputTokens  int64     `json:"output_tokens"`
	CacheRead     int64     `json:"cache_read"`
	CacheCreate   int64     `json:"cache_create"`
	DurationMs    int64     `json:"duration_ms"`
	StatusCode    int       `json:"status_code"`
	CostUSD       float64   `json:"cost_usd"`
	CreatedAt     time.Time `json:"created_at"`
}

type ToolCall struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	TurnID     string    `json:"turn_id"`
	ToolName   string    `json:"tool_name"`
	Input      string    `json:"input"`
	Output     string    `json:"output"`
	Success    bool      `json:"success"`
	DurationMs int64     `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

type ApiError struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"session_id"`
	TurnID      string    `json:"turn_id"`
	ErrorCode   string    `json:"error_code"`
	ErrorClass  string    `json:"error_class"`
	Message     string    `json:"message"`
	Provider    string    `json:"provider"`
	RetryCount  int       `json:"retry_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type ContextSnapshot struct {
	ID              string    `json:"id"`
	SessionID       string    `json:"session_id"`
	TurnID          string    `json:"turn_id"`
	TokensInContext int64     `json:"tokens_in_context"`
	MaxTokens       int64     `json:"max_tokens"`
	Headroom        int64     `json:"headroom"`
	CompactionEvent bool      `json:"compaction_event"`
	CreatedAt       time.Time `json:"created_at"`
}

type Document struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"session_id"`
	TurnID         string    `json:"turn_id"`
	DocType        string    `json:"doc_type"` // prompt, summary, tool_output, note
	Content        string    `json:"content"`
	Embedding      []float32 `json:"embedding,omitempty"`
	EmbeddingModel string    `json:"embedding_model,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type RawEvent struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Source    string    `json:"source"`
	EventType string    `json:"event_type"`
	Payload   string    `json:"payload"` // JSON blob
	CreatedAt time.Time `json:"created_at"`
}
