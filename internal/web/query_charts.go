package web

import (
	"context"
	"database/sql"
	"time"

	"github.com/johnnygreco/beacon/internal/views"
)

// QueryChartData returns token time-series data with breakdown by type.
func QueryChartData(ctx context.Context, db *sql.DB) views.MultiSeriesChart {
	chart := views.MultiSeriesChart{
		Datasets: []views.ChartDataset{
			{Label: "Input"},
			{Label: "Output"},
			{Label: "Cache Read"},
		},
	}

	rows, err := db.QueryContext(ctx,
		`SELECT minute, total_input, total_output, total_cache_read FROM (
		   SELECT minute,
		          sum(input_tokens) AS total_input,
		          sum(output_tokens) AS total_output,
		          sum(cache_read_tokens) AS total_cache_read
		   FROM `+analyticsProjectionSQL+`
		   GROUP BY minute
		   ORDER BY minute DESC
		   LIMIT 60
		 ) ORDER BY minute ASC`)
	if err != nil {
		logQueryError("chart data", err)
		return chart
	}
	defer rows.Close()

	for rows.Next() {
		var minute time.Time
		var input, output, cacheRead int64
		if err := rows.Scan(&minute, &input, &output, &cacheRead); err != nil {
			logQueryScanError("chart data", err)
			continue
		}
		label := minute.Local().Format(time.RFC3339)
		chart.Labels = append(chart.Labels, label)
		chart.Datasets[0].Values = append(chart.Datasets[0].Values, float64(input))
		chart.Datasets[1].Values = append(chart.Datasets[1].Values, float64(output))
		chart.Datasets[2].Values = append(chart.Datasets[2].Values, float64(cacheRead))
	}
	if err := rows.Err(); err != nil {
		logQueryError("chart data rows", err)
		return chart
	}
	return chart
}

// QueryTotalTokensTimeSeries returns a single-curve time series of total tokens
// (all models combined) for the dashboard line chart.
func QueryTotalTokensTimeSeries(ctx context.Context, db *sql.DB) views.MultiSeriesChart {
	chart := views.MultiSeriesChart{
		Datasets: []views.ChartDataset{{Label: "Total Tokens"}},
	}
	rows, err := db.QueryContext(ctx,
		`SELECT minute, tokens_total FROM (
		   SELECT minute,
		          sum(total_tokens) AS tokens_total
		   FROM `+analyticsProjectionSQL+`
		   GROUP BY minute
		   ORDER BY minute DESC
		   LIMIT 60
		 ) ORDER BY minute ASC`)
	if err != nil {
		logQueryError("total tokens time series", err)
		return chart
	}
	defer rows.Close()
	for rows.Next() {
		var minute time.Time
		var total int64
		if err := rows.Scan(&minute, &total); err != nil {
			logQueryScanError("total tokens time series", err)
			continue
		}
		chart.Labels = append(chart.Labels, minute.Local().Format(time.RFC3339))
		chart.Datasets[0].Values = append(chart.Datasets[0].Values, float64(total))
	}
	if err := rows.Err(); err != nil {
		logQueryError("total tokens time series rows", err)
		return chart
	}
	return chart
}
