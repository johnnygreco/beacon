package web

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/ingest"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
)

type IngestBatchCommitter interface {
	CommitIngestBatch(context.Context, store.IngestBatchMeta, store.RowBatch) (store.IngestBatchAck, error)
}

type IngestHeartbeatRecorder interface {
	InsertCaptureHeartbeats(context.Context, []models.CaptureHeartbeat) error
}

type IngestHandlers struct {
	control           *controlplane.Store
	committer         IngestBatchCommitter
	heartbeatRecorder IngestHeartbeatRecorder
	defaultInput      float64
	defaultOutput     float64
	notify            func([]string)
	logger            *slog.Logger
}

func NewIngestHandlers(control *controlplane.Store, committer IngestBatchCommitter, defaultInput, defaultOutput float64, notify func([]string), logger *slog.Logger) *IngestHandlers {
	handlers := &IngestHandlers{
		control:       control,
		committer:     committer,
		defaultInput:  defaultInput,
		defaultOutput: defaultOutput,
		notify:        notify,
		logger:        logger,
	}
	if recorder, ok := committer.(IngestHeartbeatRecorder); ok {
		handlers.heartbeatRecorder = recorder
	}
	return handlers
}

func (h *IngestHandlers) Enroll(w http.ResponseWriter, r *http.Request) {
	if h.control == nil {
		h.internalError(w, "ingest is not configured", errors.New("missing ingest dependencies"))
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		h.jsonError(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	if _, err := h.control.AuthenticateToken(r.Context(), controlplane.AuthenticateTokenRequest{
		Plaintext:      token,
		AllowedTypes:   []string{controlplane.TokenTypeEnroll},
		RequiredScopes: []string{controlplane.ScopeEnroll},
	}); err != nil {
		h.authError(w, err)
		return
	}
	var req ingest.EnrollRequest
	if !h.decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Schema) != ingest.SchemaV1 {
		h.jsonError(w, "invalid ingest schema", http.StatusBadRequest)
		return
	}
	result, err := h.control.CompleteRemoteEnrollment(r.Context(), token, controlplaneBootstrapFromEnroll(req.Bootstrap))
	if err != nil {
		h.authError(w, err)
		return
	}
	h.jsonResponse(w, ingest.EnrollResponse{
		Schema: ingest.SchemaV1,
		Assignment: ingest.EnrollAssignment{
			NodeID:            result.IngestToken.Record.NodeID,
			CollectorID:       result.IngestToken.Record.CollectorID,
			SourceIDs:         result.IngestToken.Record.SourceIDs,
			ControlPlaneEpoch: result.Snapshot.SchemaEpoch,
		},
		IngestToken: result.IngestToken.Plaintext,
	})
}

func (h *IngestHandlers) Batch(w http.ResponseWriter, r *http.Request) {
	if !h.requireIngestBearer(w, r) {
		return
	}
	var req ingest.BatchRequest
	if !h.decode(w, r, &req) {
		return
	}
	req = ingest.NormalizeBatch(req)
	if err := ingest.ValidateBatch(req); err != nil {
		h.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.authenticateIngestToken(r.Context(), r, req.NodeID, req.CollectorID, req.SourceIDs); err != nil {
		h.authError(w, err)
		return
	}
	snapshot, err := h.control.Snapshot(r.Context())
	if err != nil {
		h.internalError(w, "read control-plane metadata", err)
		return
	}
	if req.ControlPlaneEpoch != snapshot.SchemaEpoch {
		h.jsonError(w, "control_plane_epoch mismatch", http.StatusConflict)
		return
	}
	sourceByName, sourceByID, err := validateCollectorBindings(snapshot, req.NodeID, req.CollectorID, req.SourceIDs)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusForbidden)
		return
	}

	identity := capture.FleetIdentity{
		NodeID:            req.NodeID,
		CollectorID:       req.CollectorID,
		ControlPlaneEpoch: req.ControlPlaneEpoch,
		Sources:           make(map[string]capture.FleetSourceIdentity, len(sourceByName)),
	}
	for name, source := range sourceByName {
		identity.Sources[name] = capture.FleetSourceIdentity{SourceID: source.ID}
	}
	if err := validateBatchEvents(req.Events, sourceByName); err != nil {
		h.jsonError(w, err.Error(), http.StatusForbidden)
		return
	}

	rows := capture.BuildRowBatch(req.Events, h.defaultInput, h.defaultOutput, identity, capture.RowBatchMetadata{
		BatchID:          req.BatchID,
		RedactionStatus:  "redacted",
		RedactionVersion: req.RedactionVersion,
	})
	enrichedErrors, err := enrichCaptureErrors(req.CaptureErrors, req, sourceByName)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusForbidden)
		return
	}
	rows.CaptureErrors = enrichedErrors
	enrichedCheckpoints, err := enrichCheckpoints(req.Checkpoints, req.NodeID, req.CollectorID, sourceByName, sourceByID)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusForbidden)
		return
	}
	rows.Checkpoints = enrichedCheckpoints

	ack, err := h.committer.CommitIngestBatch(r.Context(), store.IngestBatchMeta{
		CollectorID:       req.CollectorID,
		BatchID:           req.BatchID,
		NodeID:            req.NodeID,
		Sequence:          req.Sequence,
		ControlPlaneEpoch: req.ControlPlaneEpoch,
		PayloadDigest:     req.PayloadDigest,
		RedactionVersion:  req.RedactionVersion,
		CreatedAt:         req.CreatedAt,
	}, rows)
	if err != nil {
		h.commitError(w, err)
		return
	}
	if h.notify != nil && len(rows.ActivityEvents) > 0 {
		h.notify(sessionIDsFromStoreEvents(rows.ActivityEvents))
	}
	h.jsonResponse(w, ingest.BatchAck{
		Status:            ingest.StatusCommitted,
		BatchID:           ack.BatchID,
		PayloadDigest:     ack.PayloadDigest,
		EventsWritten:     ack.EventsWritten,
		RawRecordsWritten: ack.RawRecordsWritten,
		NextSequence:      ack.NextSequence,
		ControlPlaneEpoch: ack.ControlPlaneEpoch,
	})
}

func (h *IngestHandlers) Heartbeat(w http.ResponseWriter, r *http.Request) {
	if !h.requireIngestBearer(w, r) {
		return
	}
	var req ingest.HeartbeatRequest
	if !h.decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Schema) != ingest.SchemaV1 {
		h.jsonError(w, "invalid ingest schema", http.StatusBadRequest)
		return
	}
	sourceIDs := make([]string, 0, len(req.Sources))
	for _, source := range req.Sources {
		sourceIDs = append(sourceIDs, source.SourceID)
	}
	if err := h.authenticateIngestToken(r.Context(), r, req.NodeID, req.CollectorID, sourceIDs); err != nil {
		h.authError(w, err)
		return
	}
	snapshot, err := h.control.Snapshot(r.Context())
	if err != nil {
		h.internalError(w, "read control-plane metadata", err)
		return
	}
	if req.ControlPlaneEpoch != snapshot.SchemaEpoch {
		h.jsonError(w, "control_plane_epoch mismatch", http.StatusConflict)
		return
	}
	_, sourceByID, err := validateCollectorBindings(snapshot, req.NodeID, req.CollectorID, sourceIDs)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusForbidden)
		return
	}
	if h.heartbeatRecorder == nil {
		h.internalError(w, "heartbeat storage is not configured", errors.New("missing heartbeat recorder"))
		return
	}
	if err := h.heartbeatRecorder.InsertCaptureHeartbeats(r.Context(), heartbeatRows(req, sourceByID)); err != nil {
		h.internalError(w, "write collector heartbeat", err)
		return
	}
	h.jsonResponse(w, ingest.HeartbeatResponse{
		Schema:            ingest.SchemaV1,
		Status:            "ok",
		ControlPlaneEpoch: snapshot.SchemaEpoch,
	})
}

func (h *IngestHandlers) authenticateIngestToken(ctx context.Context, r *http.Request, nodeID, collectorID string, sourceIDs []string) error {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return controlplane.ErrTokenInvalid
	}
	_, err := h.control.AuthenticateToken(ctx, controlplane.AuthenticateTokenRequest{
		Plaintext:      token,
		AllowedTypes:   []string{controlplane.TokenTypeIngest},
		RequiredScopes: []string{controlplane.ScopeIngest},
		NodeID:         nodeID,
		CollectorID:    collectorID,
		SourceIDs:      sourceIDs,
	})
	return err
}

func controlplaneBootstrapFromEnroll(boot ingest.EnrollBootstrap) controlplane.Bootstrap {
	out := controlplane.Bootstrap{
		NodeID:        strings.TrimSpace(boot.NodeID),
		NodeName:      strings.TrimSpace(boot.NodeName),
		CollectorID:   strings.TrimSpace(boot.CollectorID),
		CollectorName: strings.TrimSpace(boot.CollectorName),
		Sources:       make([]controlplane.SourceRegistration, 0, len(boot.Sources)),
	}
	for _, source := range boot.Sources {
		out.Sources = append(out.Sources, controlplane.SourceRegistration{
			Name:      strings.TrimSpace(source.Name),
			Runtime:   strings.TrimSpace(source.Runtime),
			Provider:  strings.TrimSpace(source.Provider),
			Format:    strings.TrimSpace(source.Format),
			WatchRoot: strings.TrimSpace(source.WatchRoot),
		})
	}
	return out
}

func (h *IngestHandlers) requireIngestBearer(w http.ResponseWriter, r *http.Request) bool {
	if h.control == nil || h.committer == nil {
		h.internalError(w, "ingest is not configured", errors.New("missing ingest dependencies"))
		return false
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		h.jsonError(w, "missing bearer token", http.StatusUnauthorized)
		return false
	}
	if _, err := h.control.AuthenticateToken(r.Context(), controlplane.AuthenticateTokenRequest{
		Plaintext:        token,
		AllowedTypes:     []string{controlplane.TokenTypeIngest},
		RequiredScopes:   []string{controlplane.ScopeIngest},
		SkipBindingCheck: true,
	}); err != nil {
		h.authError(w, err)
		return false
	}
	return true
}

func (h *IngestHandlers) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if h.control == nil || h.committer == nil {
		h.internalError(w, "ingest is not configured", errors.New("missing ingest dependencies"))
		return false
	}
	body := http.MaxBytesReader(w, r.Body, ingest.MaxBodyBytes)
	defer body.Close()
	var reader io.Reader = body
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				h.jsonError(w, "request body too large", http.StatusRequestEntityTooLarge)
				return false
			}
			h.jsonError(w, "invalid gzip body", http.StatusBadRequest)
			return false
		}
		defer gz.Close()
		reader = gz
	}
	limited := &io.LimitedReader{R: reader, N: ingest.MaxBodyBytes + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if limited.N <= 0 || errors.As(err, &maxErr) {
			h.jsonError(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		h.jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return false
	}
	if limited.N <= 0 {
		h.jsonError(w, "request body too large", http.StatusRequestEntityTooLarge)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if limited.N <= 0 {
			h.jsonError(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		h.jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return false
	}
	return true
}

func (h *IngestHandlers) authError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, controlplane.ErrTokenExpired),
		errors.Is(err, controlplane.ErrTokenRevoked),
		errors.Is(err, controlplane.ErrTokenUsed),
		errors.Is(err, controlplane.ErrTokenScopeDenied),
		errors.Is(err, controlplane.ErrTokenBindingMismatch):
		h.jsonError(w, "forbidden", http.StatusForbidden)
	default:
		h.jsonError(w, "unauthorized", http.StatusUnauthorized)
	}
}

func (h *IngestHandlers) commitError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrIngestBatchDigestMismatch):
		h.jsonError(w, "batch_id already exists with a different payload_digest", http.StatusConflict)
	case errors.Is(err, store.ErrIngestBatchSequenceGap), errors.Is(err, store.ErrIngestBatchStaleSequence):
		h.jsonError(w, err.Error(), http.StatusConflict)
	default:
		h.log().Error("ingest batch commit failed", "error", err)
		h.jsonError(w, "batch commit failed; retry later", http.StatusServiceUnavailable)
	}
}

func (h *IngestHandlers) jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.log().Debug("ingest json response write failed", "error", err)
	}
}

func (h *IngestHandlers) jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(apiErrorResponse{Error: msg}); err != nil {
		h.log().Debug("ingest json error response write failed", "error", err)
	}
}

func (h *IngestHandlers) internalError(w http.ResponseWriter, publicMessage string, err error) {
	h.log().Error(publicMessage, "error", err)
	h.jsonError(w, publicMessage, http.StatusInternalServerError)
}

func (h *IngestHandlers) log() *slog.Logger {
	if h != nil && h.logger != nil {
		return h.logger
	}
	return slog.Default()
}

func heartbeatRows(req ingest.HeartbeatRequest, sourceByID map[string]controlplane.Source) []models.CaptureHeartbeat {
	now := time.Now().UTC()
	rows := make([]models.CaptureHeartbeat, 0, max(1, len(req.Sources)))
	for _, source := range req.Sources {
		registered := sourceByID[source.SourceID]
		rows = append(rows, models.CaptureHeartbeat{
			NodeID:            req.NodeID,
			CollectorID:       req.CollectorID,
			SourceID:          registered.ID,
			SourceName:        registered.Name,
			ControlPlaneEpoch: req.ControlPlaneEpoch,
			Status:            strings.TrimSpace(source.Status),
			QueueDepth:        req.QueueDepth,
			SpoolBytes:        req.SpoolBytes,
			ActiveFiles:       req.ActiveFiles,
			ErrorCount:        source.ErrorCount,
			LastEventAt:       source.LastEventAt,
			CreatedAt:         now,
		})
	}
	if len(rows) == 0 {
		rows = append(rows, models.CaptureHeartbeat{
			NodeID:            req.NodeID,
			CollectorID:       req.CollectorID,
			ControlPlaneEpoch: req.ControlPlaneEpoch,
			Status:            "healthy",
			QueueDepth:        req.QueueDepth,
			SpoolBytes:        req.SpoolBytes,
			ActiveFiles:       req.ActiveFiles,
			CreatedAt:         now,
		})
	}
	for i := range rows {
		if rows[i].Status == "" {
			rows[i].Status = "healthy"
		}
	}
	return rows
}

func validateCollectorBindings(snapshot *controlplane.Snapshot, nodeID, collectorID string, sourceIDs []string) (map[string]controlplane.Source, map[string]controlplane.Source, error) {
	if snapshot == nil {
		return nil, nil, fmt.Errorf("control-plane metadata unavailable")
	}
	foundCollector := false
	for _, collector := range snapshot.Collectors {
		if collector.ID == collectorID {
			if collector.NodeID != nodeID {
				return nil, nil, fmt.Errorf("collector is not bound to node")
			}
			foundCollector = true
			break
		}
	}
	if !foundCollector {
		return nil, nil, fmt.Errorf("collector is not registered")
	}
	wantSources := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		wantSources[strings.TrimSpace(sourceID)] = struct{}{}
	}
	sourceByName := make(map[string]controlplane.Source, len(wantSources))
	sourceByID := make(map[string]controlplane.Source, len(wantSources))
	for _, source := range snapshot.Sources {
		if source.CollectorID != collectorID {
			continue
		}
		if _, ok := wantSources[source.ID]; !ok {
			continue
		}
		sourceByName[source.Name] = source
		sourceByID[source.ID] = source
		delete(wantSources, source.ID)
	}
	if len(wantSources) > 0 {
		return nil, nil, fmt.Errorf("source is not bound to collector")
	}
	return sourceByName, sourceByID, nil
}

func validateBatchEvents(events []capture.NormalizedEvent, sourceByName map[string]controlplane.Source) error {
	for _, event := range events {
		source, ok := sourceByName[event.SourceName]
		if !ok || source.ID == "" {
			return fmt.Errorf("event source %q is not bound to collector", event.SourceName)
		}
	}
	return nil
}

func enrichCaptureErrors(errorsIn []models.CaptureError, req ingest.BatchRequest, sourceByName map[string]controlplane.Source) ([]models.CaptureError, error) {
	out := make([]models.CaptureError, 0, len(errorsIn))
	for _, captureErr := range errorsIn {
		source, ok := sourceByName[captureErr.SourceName]
		if !ok {
			return nil, fmt.Errorf("capture error source %q is not bound to collector", captureErr.SourceName)
		}
		captureErr.NodeID = req.NodeID
		captureErr.CollectorID = req.CollectorID
		captureErr.SourceID = source.ID
		captureErr.SourceName = source.Name
		captureErr.BatchID = req.BatchID
		captureErr.ControlPlaneEpoch = req.ControlPlaneEpoch
		out = append(out, captureErr)
	}
	return out, nil
}

func enrichCheckpoints(checkpoints []models.Checkpoint, nodeID, collectorID string, sourceByName, sourceByID map[string]controlplane.Source) ([]models.Checkpoint, error) {
	out := make([]models.Checkpoint, 0, len(checkpoints))
	for _, checkpoint := range checkpoints {
		source, ok := sourceByName[checkpoint.SourceName]
		if checkpoint.SourceID != "" {
			source, ok = sourceByID[checkpoint.SourceID]
		}
		if !ok {
			return nil, fmt.Errorf("checkpoint source %q is not bound to collector", checkpoint.SourceName)
		}
		checkpoint.NodeID = nodeID
		checkpoint.CollectorID = collectorID
		checkpoint.SourceID = source.ID
		checkpoint.SourceName = source.Name
		out = append(out, checkpoint)
	}
	return out, nil
}

func sessionIDsFromStoreEvents(events []models.Event) []string {
	seen := make(map[string]struct{}, len(events))
	out := make([]string, 0, len(events))
	for _, event := range events {
		if event.SessionID == "" {
			continue
		}
		if _, ok := seen[event.SessionID]; ok {
			continue
		}
		seen[event.SessionID] = struct{}{}
		out = append(out, event.SessionID)
	}
	return out
}
