package web

import "log/slog"

func logQueryError(operation string, err error) {
	if err != nil {
		slog.Default().Warn("web query failed", "operation", operation, "error", err)
	}
}

func logQueryScanError(operation string, err error) {
	if err != nil {
		slog.Default().Warn("web query scan failed", "operation", operation, "error", err)
	}
}
