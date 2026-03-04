package ingestion

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// OTLPHandler handles incoming OTLP log/event exports.
type OTLPHandler struct {
	eventCh      chan<- BatchEvent
	maxBodyBytes int64
	logger       *slog.Logger
}

// NewOTLPHandler creates a new OTLP handler.
func NewOTLPHandler(eventCh chan<- BatchEvent, maxBodyBytes int64, logger *slog.Logger) *OTLPHandler {
	return &OTLPHandler{
		eventCh:      eventCh,
		maxBodyBytes: maxBodyBytes,
		logger:       logger,
	}
}

// HandleLogs handles POST /v1/logs
func (h *OTLPHandler) HandleLogs(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	var req collogspb.ExportLogsServiceRequest

	contentType := r.Header.Get("Content-Type")
	isJSON := contentType == "application/json" || contentType == ""

	switch {
	case contentType == "application/x-protobuf" || contentType == "application/protobuf":
		if err := proto.Unmarshal(data, &req); err != nil {
			h.logger.Error("failed to unmarshal protobuf", "error", err)
			http.Error(w, "invalid protobuf", http.StatusBadRequest)
			return
		}
	default:
		// JSON (including empty content type)
		if err := protojson.Unmarshal(data, &req); err != nil {
			// Fall back to standard JSON for compatibility
			if err2 := json.Unmarshal(data, &req); err2 != nil {
				h.logger.Error("failed to unmarshal json", "error", err, "fallback_error", err2)
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
		}
	}

	// Process each resource logs
	for _, rl := range req.GetResourceLogs() {
		resource := rl.GetResource()
		source := ""
		for _, attr := range resource.GetAttributes() {
			if attr.GetKey() == "service.name" {
				source = attr.GetValue().GetStringValue()
			}
		}

		for _, sl := range rl.GetScopeLogs() {
			for _, lr := range sl.GetLogRecords() {
				var events []NormalizedEvent
				switch {
				case source == "claude-code" || source == "claude_code":
					events = ParseClaudeCodeEvent(lr, source)
				case source == "codex" || source == "openai-codex":
					events = ParseCodexEvent(lr, source)
				default:
					// Try Claude Code parser as default
					events = ParseClaudeCodeEvent(lr, source)
				}

				for _, evt := range events {
					evt.RawPayload = string(data)
					h.eventCh <- BatchEvent{Insert: &InsertEvent{Normalized: evt}}
				}
			}
		}
	}

	// Return success per OTLP spec, matching request format
	resp := collogspb.ExportLogsServiceResponse{}
	if isJSON {
		respData, _ := protojson.Marshal(&resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respData)
	} else {
		respData, _ := proto.Marshal(&resp)
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
		w.Write(respData)
	}
}

// HandleMetrics handles POST /v1/metrics (accept and store as raw events)
func (h *OTLPHandler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Store raw metric data as a raw event
	evt := NormalizedEvent{
		EventType:  "raw_metric",
		Source:     "otlp",
		RawPayload: string(data),
	}
	h.eventCh <- BatchEvent{Insert: &InsertEvent{Normalized: evt}}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "{}")
}
