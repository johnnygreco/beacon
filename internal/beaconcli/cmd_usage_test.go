package beaconcli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/store"
	"github.com/johnnygreco/beacon/internal/usage"
)

func TestUsageCommandBuildsRequestAndPrintsHumanOutput(t *testing.T) {
	fixedNow := time.Date(2026, 6, 7, 17, 0, 0, 0, time.UTC)
	var gotReq usage.Request
	var gotNow time.Time
	var gotAddrs []string
	withUsageCommandFakes(t,
		func(ctx context.Context, opts store.Options) (*store.Store, error) {
			gotAddrs = opts.Addrs
			return &store.Store{}, nil
		},
		func(ctx context.Context, db *sql.DB, req usage.Request, now time.Time) (usage.Result, error) {
			gotReq = req
			gotNow = now
			return sampleUsageResult(req), nil
		},
		func() time.Time { return fixedNow },
	)

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"usage",
		"--source", "codex",
		"--since", "now-24h",
		"--group-by", "session_id",
		"--limit", "2",
		"--clickhouse", "clickhouse.example:9440",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if gotNow != fixedNow {
		t.Fatalf("now = %s, want %s", gotNow, fixedNow)
	}
	if len(gotAddrs) != 1 || gotAddrs[0] != "clickhouse.example:9440" {
		t.Fatalf("ClickHouse addrs = %v", gotAddrs)
	}
	if gotReq.SourceName != "codex" || gotReq.Since != "now-24h" || gotReq.Until != "" {
		t.Fatalf("request window/filter = %#v", gotReq)
	}
	if gotReq.TokenMode != usage.TokenModeIOOnly || gotReq.Limit != 2 {
		t.Fatalf("request token/limit = %q/%d", gotReq.TokenMode, gotReq.Limit)
	}
	if len(gotReq.GroupBy) != 1 || gotReq.GroupBy[0] != "session_id" {
		t.Fatalf("group_by = %v", gotReq.GroupBy)
	}

	text := out.String()
	for _, want := range []string{
		"Usage Summary",
		"Window: 2026-06-06T17:00:00Z to 2026-06-07T17:00:00Z (event_timestamp)",
		"Filters: source_name=codex",
		"Selected total: 300 (input_tokens + output_tokens)",
		"I/O total: 300 (input_tokens + output_tokens)",
		"session_id=s1: selected=300 sessions=1 events=7",
		"Groups truncated by --limit.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}

func TestUsageCommandTodayTimezoneJSON(t *testing.T) {
	fixedNow := time.Date(2026, 6, 7, 17, 0, 0, 0, time.UTC)
	var gotReq usage.Request
	withUsageCommandFakes(t,
		func(ctx context.Context, opts store.Options) (*store.Store, error) {
			return &store.Store{}, nil
		},
		func(ctx context.Context, db *sql.DB, req usage.Request, now time.Time) (usage.Result, error) {
			gotReq = req
			return sampleUsageResult(req), nil
		},
		func() time.Time { return fixedNow },
	)

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"usage", "--today", "--timezone", "UTC", "--include-cache", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if gotReq.Since != "2026-06-07T00:00:00Z" || gotReq.Until != "2026-06-08T00:00:00Z" {
		t.Fatalf("today window = %q..%q", gotReq.Since, gotReq.Until)
	}
	if gotReq.TokenMode != usage.TokenModeAll {
		t.Fatalf("token mode = %q, want %q", gotReq.TokenMode, usage.TokenModeAll)
	}

	var decoded usage.Result
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("json output did not decode: %v\n%s", err, out.String())
	}
	if decoded.SelectedTotalDefinition != usage.TotalDefinitionAll {
		t.Fatalf("selected definition = %q, want %q", decoded.SelectedTotalDefinition, usage.TotalDefinitionAll)
	}
	if decoded.Summary.SelectedTotalTokens != 315 {
		t.Fatalf("selected total = %d, want 315", decoded.Summary.SelectedTotalTokens)
	}
}

func TestUsageCommandRejectsInvalidArgumentsBeforeOpeningStore(t *testing.T) {
	withUsageCommandFakes(t,
		func(ctx context.Context, opts store.Options) (*store.Store, error) {
			t.Fatal("usage command opened store for invalid arguments")
			return nil, nil
		},
		func(ctx context.Context, db *sql.DB, req usage.Request, now time.Time) (usage.Result, error) {
			t.Fatal("usage command queried for invalid arguments")
			return usage.Result{}, nil
		},
		func() time.Time { return time.Date(2026, 6, 7, 17, 0, 0, 0, time.UTC) },
	)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "today requires timezone", args: []string{"usage", "--today"}, want: "--timezone is required with --today"},
		{name: "today conflicts with since", args: []string{"usage", "--today", "--timezone", "UTC", "--since", "now-1h"}, want: "cannot combine --today with --since or --until"},
		{name: "timezone requires today", args: []string{"usage", "--timezone", "UTC"}, want: "--timezone requires --today"},
		{name: "limit positive", args: []string{"usage", "--limit", "0"}, want: "--limit must be positive"},
		{name: "invalid since", args: []string{"usage", "--since", "bad-time"}, want: "invalid since timestamp"},
		{name: "invalid until", args: []string{"usage", "--until", "bad-time"}, want: "invalid until timestamp"},
		{name: "inverted window", args: []string{"usage", "--since", "2026-06-08T00:00:00Z", "--until", "2026-06-07T00:00:00Z"}, want: "since must be before until"},
		{name: "bad group field", args: []string{"usage", "--group-by", "session_id;DROP TABLE activity_events"}, want: "unsupported group_by field"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Execute() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func withUsageCommandFakes(
	t *testing.T,
	openStore func(context.Context, store.Options) (*store.Store, error),
	summarize func(context.Context, *sql.DB, usage.Request, time.Time) (usage.Result, error),
	now func() time.Time,
) {
	t.Helper()
	oldCfgFile := cfgFile
	oldOpenStore := usageOpenStore
	oldSummarize := usageSummarize
	oldNow := usageNow
	cfgFile = ""
	usageOpenStore = openStore
	usageSummarize = summarize
	usageNow = now
	t.Cleanup(func() {
		cfgFile = oldCfgFile
		usageOpenStore = oldOpenStore
		usageSummarize = oldSummarize
		usageNow = oldNow
	})
}

func sampleUsageResult(req usage.Request) usage.Result {
	tokenMode := req.TokenMode
	selectedDefinition := usage.TotalDefinitionIOOnly
	selectedTotal := int64(300)
	if tokenMode == usage.TokenModeAll {
		selectedDefinition = usage.TotalDefinitionAll
		selectedTotal = 315
	}
	return usage.Result{
		Window: usage.Window{
			Since: sampleTimeOr(req.Since, time.Date(2026, 6, 6, 17, 0, 0, 0, time.UTC)),
			Until: sampleTimeOr(req.Until, time.Date(2026, 6, 7, 17, 0, 0, 0, time.UTC)),
			Mode:  usage.DefaultWindowMode,
		},
		Filters: usage.Filters{
			SourceName: req.SourceName,
			Model:      req.Model,
			Provider:   req.Provider,
			WorkingDir: req.WorkingDir,
		},
		GroupBy:                 req.GroupBy,
		TokenMode:               tokenMode,
		TotalDefinition:         usage.TotalDefinitionIOOnly,
		SelectedTotalDefinition: selectedDefinition,
		Summary: usage.Totals{
			SessionCount:        1,
			EventCount:          7,
			InputTokens:         100,
			OutputTokens:        200,
			TotalTokens:         300,
			CacheReadTokens:     10,
			CacheCreateTokens:   5,
			SelectedTotalTokens: selectedTotal,
		},
		Groups: []usage.Group{
			{
				Keys: map[string]string{"session_id": "s1"},
				Totals: usage.Totals{
					SessionCount:        1,
					EventCount:          7,
					InputTokens:         100,
					OutputTokens:        200,
					TotalTokens:         300,
					CacheReadTokens:     10,
					CacheCreateTokens:   5,
					SelectedTotalTokens: selectedTotal,
				},
			},
		},
		Metadata: usage.Metadata{
			ResultCount:        1,
			TotalMatchingCount: 2,
			Limit:              req.Limit,
			ResultComplete:     false,
			TruncatedByLimit:   true,
		},
	}
}

func sampleTimeOr(raw string, fallback time.Time) time.Time {
	if raw == "" || raw == "now-24h" || raw == "now" {
		return fallback
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		panic(fmt.Sprintf("bad test time %q: %v", raw, err))
	}
	return t
}
