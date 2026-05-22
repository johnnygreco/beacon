package web

import (
	"encoding/json"
	"strings"
)

// formatToolCallSnippet extracts a human-readable preview from raw tool input JSON.
func formatToolCallSnippet(toolName, rawJSON string) string {
	if rawJSON == "" || rawJSON == toolName {
		return ""
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &params); err != nil {
		return extractJSONField(rawJSON)
	}
	switch toolName {
	case "Bash":
		if cmd, ok := params["command"].(string); ok {
			if desc, ok := params["description"].(string); ok && desc != "" {
				return desc + " - " + truncateStr(cmd, 120)
			}
			return truncateStr(cmd, 200)
		}
	case "Read", "Edit", "Write":
		if fp, ok := params["file_path"].(string); ok {
			return fp
		}
	case "Glob", "Grep":
		if p, ok := params["pattern"].(string); ok {
			return p
		}
	case "Agent":
		if p, ok := params["prompt"].(string); ok {
			return truncateStr(p, 200)
		}
	}
	for _, key := range []string{"file_path", "command", "pattern", "prompt", "description"} {
		if v, ok := params[key].(string); ok && v != "" {
			return truncateStr(v, 200)
		}
	}
	return truncateStr(rawJSON, 200)
}

func extractJSONField(raw string) string {
	for _, key := range []string{"file_path", "command", "pattern", "prompt", "description"} {
		prefix := `"` + key + `":"`
		idx := strings.Index(raw, prefix)
		if idx < 0 {
			continue
		}
		start := idx + len(prefix)
		end := start
		for end < len(raw) {
			if raw[end] == '\\' {
				end += 2
				continue
			}
			if raw[end] == '"' {
				break
			}
			end++
		}
		if end > start {
			val := raw[start:end]
			val = strings.ReplaceAll(val, `\"`, `"`)
			val = strings.ReplaceAll(val, `\\`, `\`)
			return truncateStr(val, 200)
		}
	}
	return truncateStr(raw, 200)
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
