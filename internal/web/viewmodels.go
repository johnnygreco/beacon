package web

import "time"

type APIMetricData struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Delta float64 `json:"delta,omitempty"`
}

type APISessionSummary struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	StartedAt   time.Time `json:"started_at"`
	Duration    string    `json:"duration"`
	TurnCount   int64     `json:"turn_count"`
	TotalTokens int64     `json:"total_tokens"`
	TotalCost   float64   `json:"total_cost"`
	ErrorCount  int64     `json:"error_count"`
	LastModel   string    `json:"last_model"`
}
