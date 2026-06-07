package usage

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeDefaultsAndRelativeWindow(t *testing.T) {
	now := time.Date(2026, 6, 7, 17, 0, 0, 0, time.UTC)
	got, err := normalize(Request{
		Since:      "now-24h",
		Until:      "now",
		SourceName: " codex ",
		GroupBy:    []string{"source_name", "source_name", "session_id"},
		Limit:      500,
	}, now)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got.Window.Since != now.Add(-24*time.Hour) || got.Window.Until != now {
		t.Fatalf("window = %s..%s, want last 24h ending now", got.Window.Since, got.Window.Until)
	}
	if got.Window.Mode != DefaultWindowMode || got.TokenMode != TokenModeIOOnly {
		t.Fatalf("mode/token = %q/%q", got.Window.Mode, got.TokenMode)
	}
	if got.Filters.SourceName != "codex" {
		t.Fatalf("source filter = %q", got.Filters.SourceName)
	}
	if got.Limit != MaxLimit {
		t.Fatalf("limit = %d, want %d", got.Limit, MaxLimit)
	}
	if len(got.GroupBy) != 2 || got.GroupBy[0] != "source_name" || got.GroupBy[1] != "session_id" {
		t.Fatalf("group_by = %#v", got.GroupBy)
	}
}

func TestNormalizeRejectsInvalidInputs(t *testing.T) {
	now := time.Date(2026, 6, 7, 17, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{name: "window mode", req: Request{WindowMode: "session"}, want: "unsupported window_mode"},
		{name: "token mode", req: Request{TokenMode: "cache_only"}, want: "unsupported token_mode"},
		{name: "group field", req: Request{GroupBy: []string{"source_name; DROP TABLE activity_events"}}, want: "unsupported group_by field"},
		{name: "since", req: Request{Since: "yesterday"}, want: "invalid since timestamp"},
		{name: "until", req: Request{Until: "tomorrow"}, want: "invalid until timestamp"},
		{name: "inverted window", req: Request{Since: "2026-06-08T00:00:00Z", Until: "2026-06-07T00:00:00Z"}, want: "since must be before"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalize(tt.req, now)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !IsUserError(err) {
				t.Fatalf("normalize error = %v, want user error containing %q", err, tt.want)
			}
		})
	}
}

func TestUsageGroupSQLUsesWhitelistedExpressions(t *testing.T) {
	now := time.Date(2026, 6, 7, 17, 0, 0, 0, time.UTC)
	req, err := normalize(Request{
		Since:      "now-1h",
		Until:      "now",
		TokenMode:  TokenModeAll,
		SourceName: "codex",
		WorkingDir: "/work/project",
		GroupBy:    []string{"model", "session_id"},
		Limit:      3,
	}, now)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	query, args := usageGroupSQL(req)
	for _, want := range []string{
		"argMax(session_id, captured_at) AS session_id",
		"GROUP BY event_uid",
		"session_working_dirs AS",
		"argMaxIf(cwd, timestamp, cwd != '') AS working_dir",
		"COALESCE(e.session_working_dir, '') = ?",
		"COALESCE(NULLIF(e.model, ''), 'unknown') AS model",
		"e.session_id AS session_id",
		"GROUP BY COALESCE(NULLIF(e.model, ''), 'unknown'), e.session_id",
		"sum(e.input_tokens + e.output_tokens + e.cache_read_tokens + e.cache_create_tokens) AS selected_total_tokens",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
	if len(args) != 5 || args[2] != "codex" || args[3] != "/work/project" || args[4] != 3 {
		t.Fatalf("args = %#v", args)
	}
}
