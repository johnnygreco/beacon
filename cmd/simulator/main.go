package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

// Simulator generates fake JSONL files that mimic Claude Code output.
// This tests the JSONL watcher pipeline end-to-end.
func main() {
	home, _ := os.UserHomeDir()
	outDir := filepath.Join(home, ".claude", "projects", "simulator")
	if v := os.Getenv("SIMULATOR_DIR"); v != "" {
		outDir = v
	}

	os.MkdirAll(outDir, 0755)

	sessionID := fmt.Sprintf("sim-%d", time.Now().Unix())
	outFile := filepath.Join(outDir, sessionID+".jsonl")

	f, err := os.Create(outFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	fmt.Printf("Writing simulated JSONL to %s\n", outFile)

	models := []string{"claude-sonnet-4", "claude-opus-4", "claude-haiku-4"}
	tools := []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebSearch"}
	encoder := json.NewEncoder(f)

	turnNum := 0
	for {
		turnNum++
		parentUUID := fmt.Sprintf("uuid-turn-%d", turnNum)
		ts := time.Now()

		// User message — real Claude Code uses a plain string for user content
		writeLine(encoder, map[string]any{
			"sessionId":  sessionID,
			"uuid":       fmt.Sprintf("uuid-%d-user", turnNum),
			"parentUuid": "",
			"timestamp":  ts.Format(time.RFC3339Nano),
			"type":       "user",
			"message": map[string]any{
				"role":    "user",
				"content": fmt.Sprintf("Simulated prompt #%d: Please help me with task %d", turnNum, turnNum),
			},
		})
		time.Sleep(300 * time.Millisecond)

		// Assistant response with model call
		model := models[rand.Intn(len(models))]
		inputTokens := rand.Intn(50000) + 1000
		outputTokens := rand.Intn(4000) + 100
		cacheRead := rand.Intn(30000)

		contentBlocks := []map[string]any{
			{"type": "text", "text": fmt.Sprintf("I'll help you with task %d. Let me work on that.", turnNum)},
		}

		// Add tool calls
		numTools := rand.Intn(3) + 1
		for i := 0; i < numTools; i++ {
			tool := tools[rand.Intn(len(tools))]
			toolUseID := fmt.Sprintf("toolu_%d_%d", turnNum, i)
			contentBlocks = append(contentBlocks, map[string]any{
				"type":  "tool_use",
				"id":    toolUseID,
				"name":  tool,
				"input": map[string]any{"path": fmt.Sprintf("/src/file%d.go", i)},
			})
		}

		writeLine(encoder, map[string]any{
			"sessionId":  sessionID,
			"uuid":       fmt.Sprintf("uuid-%d-assistant", turnNum),
			"parentUuid": parentUUID,
			"timestamp":  time.Now().Format(time.RFC3339Nano),
			"type":       "assistant",
			"message": map[string]any{
				"role":    "assistant",
				"model":   model,
				"content": contentBlocks,
				"usage": map[string]any{
					"input_tokens":                inputTokens,
					"output_tokens":               outputTokens,
					"cache_read_input_tokens":      cacheRead,
					"cache_creation_input_tokens":  0,
				},
			},
		})
		time.Sleep(200 * time.Millisecond)

		// Tool results
		for i := 0; i < numTools; i++ {
			tool := tools[rand.Intn(len(tools))]
			toolUseID := fmt.Sprintf("toolu_%d_%d", turnNum, i)
			writeLine(encoder, map[string]any{
				"sessionId":  sessionID,
				"uuid":       fmt.Sprintf("uuid-%d-result-%d", turnNum, i),
				"parentUuid": fmt.Sprintf("uuid-%d-assistant", turnNum),
				"timestamp":  time.Now().Format(time.RFC3339Nano),
				"type":       "tool_result",
				"message": map[string]any{
					"role": "tool",
					"content": []map[string]any{
						{
							"type":        "tool_result",
							"tool_use_id": toolUseID,
							"name":        tool,
							"content":     fmt.Sprintf("Output from %s for turn %d", tool, turnNum),
						},
					},
				},
			})
			time.Sleep(100 * time.Millisecond)
		}

		// Occasional error
		if rand.Float32() < 0.1 {
			writeLine(encoder, map[string]any{
				"sessionId": sessionID,
				"uuid":      fmt.Sprintf("uuid-%d-error", turnNum),
				"timestamp": time.Now().Format(time.RFC3339Nano),
				"type":      "error",
				"message": map[string]any{
					"role": "system",
					"content": []map[string]any{
						{"type": "text", "text": "Rate limit exceeded"},
					},
				},
			})
		}

		fmt.Printf("  Turn %d: model=%s input=%d output=%d tools=%d\n", turnNum, model, inputTokens, outputTokens, numTools)
		time.Sleep(time.Duration(rand.Intn(3)+1) * time.Second)
	}
}

func writeLine(enc *json.Encoder, data map[string]any) {
	enc.Encode(data)
}
