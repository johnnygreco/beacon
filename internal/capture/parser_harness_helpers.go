package capture

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"hash/fnv"
	"net/url"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const hermesJSONContentPrefix = "\x00json:"

func openSQLiteReadOnly(file string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: file}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("cache", "shared")
	u.RawQuery = q.Encode()

	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func sqliteHasTable(db *sql.DB, table string) bool {
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	return err == nil
}

func sqliteStableRaw(source, table, id, kind string) string {
	b, _ := json.Marshal(map[string]string{
		"source": source,
		"table":  table,
		"id":     id,
		"kind":   kind,
	})
	return string(b)
}

func scopedMessageUUID(base string, parts ...string) string {
	if base == "" {
		return ""
	}
	items := []string{base}
	for _, part := range parts {
		if part != "" {
			items = append(items, part)
		}
	}
	return strings.Join(items, ":")
}

func stableLineNo(parts ...string) int {
	key := strings.Join(parts, "|")
	n := int(crc32.ChecksumIEEE([]byte(key)))
	if n == 0 {
		return 1
	}
	return n
}

func stableOffset(parts ...string) int64 {
	h := fnv.New64a()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return int64(h.Sum64() & 0x7fffffffffffffff)
}

func timeFromUnixSeconds(v float64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	sec := int64(v)
	nsec := int64((v - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).UTC()
}

func timeFromUnixMillis(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

func numberFromAny(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

func floatFromAny(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

func stringFromAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func mapFromAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func arrayFromAny(v any) []any {
	a, _ := v.([]any)
	return a
}

func decodeHarnessJSON(raw string) any {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, hermesJSONContentPrefix) {
		raw = strings.TrimPrefix(raw, hermesJSONContentPrefix)
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return raw
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return raw
	}
	return v
}

func textFromHarnessContent(v any) string {
	switch c := v.(type) {
	case nil:
		return ""
	case string:
		if decoded := decodeHarnessJSON(c); decoded != c {
			return textFromHarnessContent(decoded)
		}
		return c
	case []any:
		var texts []string
		for _, item := range c {
			if t := textFromHarnessContent(item); t != "" {
				texts = append(texts, t)
			}
		}
		return strings.Join(texts, "\n")
	case map[string]any:
		if t := stringFromAny(c["text"]); t != "" {
			return t
		}
		if t := stringFromAny(c["thinking"]); t != "" {
			return t
		}
		if t := stringFromAny(c["output"]); t != "" {
			return t
		}
		if content, ok := c["content"]; ok {
			return textFromHarnessContent(content)
		}
	}
	return ""
}

func jsonPayload(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		trimmed := strings.TrimSpace(s)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			return s
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func parseJSONMap(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}

func parseJSONArray(raw string) []any {
	if raw == "" {
		return nil
	}
	var a []any
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return nil
	}
	return a
}
