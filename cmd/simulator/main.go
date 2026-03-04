package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"
)

// Simulator sends fake OTLP log events to the Technodrome server for testing.
func main() {
	target := "http://localhost:4600"
	if v := os.Getenv("TECHNODROME_URL"); v != "" {
		target = v
	}

	fmt.Printf("Sending simulated events to %s/v1/logs\n", target)

	sessionID := fmt.Sprintf("sim-session-%d", time.Now().Unix())
	models := []string{"claude-sonnet-4", "claude-opus-4", "claude-haiku-4"}
	tools := []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebSearch"}

	turnNum := 0
	for {
		turnNum++
		turnID := fmt.Sprintf("turn-%s-%d", sessionID, turnNum)

		// User prompt event
		sendEvent(target, sessionID, turnID, "user_prompt", map[string]any{
			"prompt":      fmt.Sprintf("Simulated prompt #%d: Please help me with task %d", turnNum, turnNum),
			"turn_number": turnNum,
		})
		time.Sleep(500 * time.Millisecond)

		// API request event
		model := models[rand.Intn(len(models))]
		inputTokens := rand.Intn(50000) + 1000
		outputTokens := rand.Intn(4000) + 100
		cacheRead := rand.Intn(30000)
		sendEvent(target, sessionID, turnID, "api_request", map[string]any{
			"model":                      model,
			"input_tokens":               inputTokens,
			"output_tokens":              outputTokens,
			"cache_read_input_tokens":    cacheRead,
			"cache_creation_input_tokens": 0,
			"duration_ms":                rand.Intn(5000) + 500,
			"status_code":                200,
		})
		time.Sleep(300 * time.Millisecond)

		// Tool calls (1-3 per turn)
		numTools := rand.Intn(3) + 1
		for i := 0; i < numTools; i++ {
			tool := tools[rand.Intn(len(tools))]
			sendEvent(target, sessionID, turnID, "tool_result", map[string]any{
				"tool_name":   tool,
				"tool_output": fmt.Sprintf("Output from %s for turn %d", tool, turnNum),
				"success":     rand.Float32() > 0.05, // 95% success rate
				"duration_ms": rand.Intn(2000) + 50,
			})
			time.Sleep(200 * time.Millisecond)
		}

		// Occasional errors (10% chance)
		if rand.Float32() < 0.1 {
			sendEvent(target, sessionID, turnID, "api_error", map[string]any{
				"error_code":    "rate_limit_exceeded",
				"error_class":   "rate_limit",
				"error_message": "Too many requests",
				"retry_count":   1,
			})
		}

		// Context snapshot
		contextTokens := rand.Intn(150000) + 10000
		sendEvent(target, sessionID, turnID, "context_snapshot", map[string]any{
			"tokens_in_context": contextTokens,
			"max_tokens":        200000,
			"compaction_event":  contextTokens > 180000,
		})

		fmt.Printf("  Turn %d: model=%s input=%d output=%d tools=%d\n", turnNum, model, inputTokens, outputTokens, numTools)
		time.Sleep(time.Duration(rand.Intn(3)+1) * time.Second)
	}
}

func sendEvent(target, sessionID, turnID, eventName string, attrs map[string]any) {
	// Build a simplified OTLP-like JSON payload
	attrs["event.name"] = eventName
	attrs["session_id"] = sessionID
	attrs["turn_id"] = turnID

	// Convert to OTLP JSON format
	logRecord := map[string]any{
		"timeUnixNano": fmt.Sprintf("%d", time.Now().UnixNano()),
		"attributes":   toOTLPAttrs(attrs),
		"body": map[string]any{
			"stringValue": eventName,
		},
	}

	payload := map[string]any{
		"resourceLogs": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": toOTLPAttrs(map[string]any{
						"service.name": "claude_code",
					}),
				},
				"scopeLogs": []any{
					map[string]any{
						"logRecords": []any{logRecord},
					},
				},
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal error: %v\n", err)
		return
	}

	resp, err := http.Post(target+"/v1/logs", "application/json", bytes.NewReader(data))
	if err != nil {
		fmt.Fprintf(os.Stderr, "send error: %v\n", err)
		return
	}
	resp.Body.Close()
}

func toOTLPAttrs(m map[string]any) []map[string]any {
	var attrs []map[string]any
	for k, v := range m {
		attr := map[string]any{"key": k}
		switch val := v.(type) {
		case string:
			attr["value"] = map[string]any{"stringValue": val}
		case int:
			attr["value"] = map[string]any{"intValue": fmt.Sprintf("%d", val)}
		case int64:
			attr["value"] = map[string]any{"intValue": fmt.Sprintf("%d", val)}
		case float64:
			attr["value"] = map[string]any{"doubleValue": val}
		case bool:
			attr["value"] = map[string]any{"boolValue": val}
		default:
			attr["value"] = map[string]any{"stringValue": fmt.Sprintf("%v", val)}
		}
		attrs = append(attrs, attr)
	}
	return attrs
}
