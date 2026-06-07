package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/johnnygreco/beacon/internal/usage"
	"github.com/spf13/cobra"
)

type usageCLIOptions struct {
	clickHouseAddr string
	since          string
	until          string
	sourceName     string
	model          string
	provider       string
	workingDir     string
	groupBy        []string
	timezone       string
	limit          int
	today          bool
	json           bool
	includeCache   bool
}

var (
	usageNow       = time.Now
	usageOpenStore = store.OpenReadOnly
	usageSummarize = usage.Summarize
)

func newUsageCmd() *cobra.Command {
	opts := usageCLIOptions{limit: usage.DefaultLimit}
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Summarize captured token usage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsage(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.clickHouseAddr, "clickhouse", "", "ClickHouse address (overrides config)")
	cmd.Flags().StringVar(&opts.since, "since", "", "window start as RFC3339, now, or now-duration (default: now-24h)")
	cmd.Flags().StringVar(&opts.until, "until", "", "window end as RFC3339, now, or now-duration (default: now)")
	cmd.Flags().BoolVar(&opts.today, "today", false, "summarize the current calendar day; requires --timezone")
	cmd.Flags().StringVar(&opts.timezone, "timezone", "", "IANA timezone for --today, such as UTC or America/New_York")
	cmd.Flags().StringVar(&opts.sourceName, "source", "", "filter by Beacon source name")
	cmd.Flags().StringVar(&opts.model, "model", "", "filter by model")
	cmd.Flags().StringVar(&opts.provider, "provider", "", "filter by provider")
	cmd.Flags().StringVar(&opts.workingDir, "working-dir", "", "filter by session working directory")
	cmd.Flags().StringSliceVar(&opts.groupBy, "group-by", nil, "group top contributors by source_name, provider, model, session_id, or working_dir")
	cmd.Flags().IntVar(&opts.limit, "limit", usage.DefaultLimit, "maximum grouped rows to return")
	cmd.Flags().BoolVar(&opts.includeCache, "include-cache", false, "include cache read/create tokens in the selected total")
	cmd.Flags().BoolVar(&opts.json, "json", false, "emit JSON")

	return cmd
}

func runUsage(cmd *cobra.Command, opts usageCLIOptions) error {
	req, now, err := usageRequestFromCLI(cmd, opts)
	if err != nil {
		return err
	}
	if err := usage.Validate(req, now); err != nil {
		return err
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	storeOpts := storeOptionsFromConfig(cfg)
	if opts.clickHouseAddr != "" {
		storeOpts.Addrs = []string{opts.clickHouseAddr}
	}

	ch, err := usageOpenStore(cmd.Context(), storeOpts)
	if err != nil {
		return fmt.Errorf("open ClickHouse read-only: %w", err)
	}
	defer ch.Close()

	result, err := usageSummarize(cmd.Context(), ch.DB, req, now)
	if err != nil {
		if usage.IsUserError(err) {
			return err
		}
		return fmt.Errorf("summarize usage: %w", err)
	}

	if opts.json {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	return printUsageText(cmd.OutOrStdout(), result)
}

func usageRequestFromCLI(cmd *cobra.Command, opts usageCLIOptions) (usage.Request, time.Time, error) {
	if opts.limit <= 0 {
		return usage.Request{}, time.Time{}, fmt.Errorf("--limit must be positive")
	}
	if opts.today {
		if cmd.Flags().Changed("since") || cmd.Flags().Changed("until") {
			return usage.Request{}, time.Time{}, fmt.Errorf("cannot combine --today with --since or --until")
		}
		if !cmd.Flags().Changed("timezone") || strings.TrimSpace(opts.timezone) == "" {
			return usage.Request{}, time.Time{}, fmt.Errorf("--timezone is required with --today")
		}
	} else if cmd.Flags().Changed("timezone") {
		return usage.Request{}, time.Time{}, fmt.Errorf("--timezone requires --today")
	}

	now := usageNow()
	req := usage.Request{
		Since:      opts.since,
		Until:      opts.until,
		WindowMode: usage.DefaultWindowMode,
		TokenMode:  usage.TokenModeIOOnly,
		SourceName: opts.sourceName,
		Model:      opts.model,
		Provider:   opts.provider,
		WorkingDir: opts.workingDir,
		GroupBy:    opts.groupBy,
		Limit:      opts.limit,
	}
	if opts.includeCache {
		req.TokenMode = usage.TokenModeAll
	}
	if opts.today {
		since, until, err := todayWindow(now, opts.timezone)
		if err != nil {
			return usage.Request{}, time.Time{}, err
		}
		req.Since = since.Format(time.RFC3339)
		req.Until = until.Format(time.RFC3339)
	}

	return req, now, nil
}

func todayWindow(now time.Time, timezone string) (time.Time, time.Time, error) {
	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --timezone: %w", err)
	}
	localNow := now.In(loc)
	since := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	until := since.AddDate(0, 0, 1)
	return since.UTC(), until.UTC(), nil
}

func printUsageText(out io.Writer, result usage.Result) error {
	if _, err := fmt.Fprintln(out, "Usage Summary"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "============="); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Window: %s to %s (%s)\n",
		formatUsageTime(result.Window.Since),
		formatUsageTime(result.Window.Until),
		result.Window.Mode,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Filters: %s\n", formatUsageFilters(result.Filters)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Token mode: %s\n", result.TokenMode); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Selected total: %d (%s)\n",
		result.Summary.SelectedTotalTokens,
		result.SelectedTotalDefinition,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "I/O total: %d (%s)\n", result.Summary.TotalTokens, result.TotalDefinition); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Input tokens: %d\n", result.Summary.InputTokens); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Output tokens: %d\n", result.Summary.OutputTokens); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Cache read tokens: %d\n", result.Summary.CacheReadTokens); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Cache create tokens: %d\n", result.Summary.CacheCreateTokens); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Sessions: %d\n", result.Summary.SessionCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Events: %d\n", result.Summary.EventCount); err != nil {
		return err
	}
	if len(result.Groups) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(out, "\nGroups: top %d of %d by selected total\n", result.Metadata.ResultCount, result.Metadata.TotalMatchingCount); err != nil {
		return err
	}
	for _, group := range result.Groups {
		if _, err := fmt.Fprintf(out, "  %s: selected=%d sessions=%d events=%d input=%d output=%d cache_read=%d cache_create=%d\n",
			formatUsageGroupKeys(result.GroupBy, group.Keys),
			group.Totals.SelectedTotalTokens,
			group.Totals.SessionCount,
			group.Totals.EventCount,
			group.Totals.InputTokens,
			group.Totals.OutputTokens,
			group.Totals.CacheReadTokens,
			group.Totals.CacheCreateTokens,
		); err != nil {
			return err
		}
	}
	if result.Metadata.TruncatedByLimit {
		_, err := fmt.Fprintln(out, "Groups truncated by --limit.")
		return err
	}
	return nil
}

func formatUsageTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func formatUsageFilters(filters usage.Filters) string {
	parts := make([]string, 0, 4)
	if filters.SourceName != "" {
		parts = append(parts, "source_name="+filters.SourceName)
	}
	if filters.Model != "" {
		parts = append(parts, "model="+filters.Model)
	}
	if filters.Provider != "" {
		parts = append(parts, "provider="+filters.Provider)
	}
	if filters.WorkingDir != "" {
		parts = append(parts, "working_dir="+filters.WorkingDir)
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func formatUsageGroupKeys(groupBy []string, keys map[string]string) string {
	parts := make([]string, 0, len(groupBy))
	for _, field := range groupBy {
		parts = append(parts, field+"="+keys[field])
	}
	return strings.Join(parts, ", ")
}
