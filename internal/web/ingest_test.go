package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/ingest"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/redaction"
	"github.com/johnnygreco/beacon/internal/store"
)

func TestIngestBatchAuthenticatesWritesRowsAndReturnsAck(t *testing.T) {
	control, token, sourceID := testIngestControlPlane(t)
	committer := &fakeIngestCommitter{}
	handler := NewIngestHandlers(control, committer, 0, 0, nil, nil)
	req := testIngestBatchRequest(t, sourceID)
	req.CaptureErrors = []models.CaptureError{{
		ID:              "capture-error-web",
		SourceName:      "codex",
		SourceFile:      "session.jsonl",
		SourceLineNo:    2,
		SourceOffset:    40,
		ErrorClass:      "parse_error",
		ErrorMessage:    "redacted parse error",
		ContextFragment: "redacted context",
	}}
	req.PayloadDigest = computeTestBatchDigest(t, req)

	rec := postIngestJSON(t, handler.Batch, req, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var ack ingest.BatchAck
	if err := json.Unmarshal(rec.Body.Bytes(), &ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ack.Status != ingest.StatusCommitted || ack.NextSequence != 2 {
		t.Fatalf("ack = %#v, want committed next sequence 2", ack)
	}
	if committer.calls != 1 {
		t.Fatalf("committer calls = %d, want 1", committer.calls)
	}
	if len(committer.rows.ActivityEvents) != 1 || committer.rows.ActivityEvents[0].CollectorID != "collector-web" {
		t.Fatalf("activity rows = %#v", committer.rows.ActivityEvents)
	}
	if committer.rows.ActivityEvents[0].RedactionStatus != "redacted" {
		t.Fatalf("redaction status = %q, want redacted", committer.rows.ActivityEvents[0].RedactionStatus)
	}
	if len(committer.rows.Checkpoints) != 1 || committer.rows.Checkpoints[0].CollectorID != "collector-web" || committer.rows.Checkpoints[0].SourceID != sourceID {
		t.Fatalf("checkpoint rows = %#v", committer.rows.Checkpoints)
	}
	if len(committer.rows.CaptureErrors) != 1 ||
		committer.rows.CaptureErrors[0].CollectorID != "collector-web" ||
		committer.rows.CaptureErrors[0].SourceID != sourceID ||
		committer.rows.CaptureErrors[0].BatchID != req.BatchID {
		t.Fatalf("capture error rows = %#v", committer.rows.CaptureErrors)
	}
}

func TestIngestBatchRedactsBeforeCommit(t *testing.T) {
	control, token, sourceID := testIngestControlPlane(t)
	committer := &fakeIngestCommitter{}
	policy := redaction.NewPolicy(redaction.Config{
		PathMasks:    []string{"/Users/example/private"},
		LiteralMasks: []string{"literal-fixture-secret"},
	})
	handler := NewIngestHandlers(control, committer, 0, 0, nil, nil, WithIngestRedactionPolicy(policy))
	req := testIngestBatchRequest(t, sourceID)
	req.Events[0].TextContent = "token bcn_owner_fixture_0123456789abcdef literal-fixture-secret"
	req.Events[0].ToolPhase = models.ToolPhaseCall
	req.Events[0].ToolName = "bash"
	req.Events[0].ToolInput = `{"api_key":"tool-input-secret"}`
	req.Events[0].ToolOutput = "Authorization: Bearer tool-output-secret"
	req.Events[0].ErrorMessage = "password=event-error-secret"
	req.Events[0].RawPayload = `{"client_secret":"raw-payload-secret","cwd":"/Users/example/private/project"}`
	req.Events[0].CWD = "/Users/example/private/project"
	req.Events[0].SourceFile = "/Users/example/private/session.jsonl"
	req.CaptureErrors = []models.CaptureError{{
		ID:              "capture-error-redaction",
		SourceName:      "codex",
		SourceFile:      "/Users/example/private/session.jsonl",
		ErrorClass:      "parse_error",
		ErrorMessage:    `{"password":"capture-error-secret"}`,
		ContextFragment: "literal-fixture-secret",
	}}
	req.Checkpoints[0].SourceFile = "/Users/example/private/session.jsonl"
	req.Checkpoints[0].StateJSON = `{"api_key":"checkpoint-secret","path":"/Users/example/private/session.jsonl"}`
	submittedDigest := computeTestBatchDigest(t, req)
	req.PayloadDigest = submittedDigest

	rec := postIngestJSON(t, handler.Batch, req, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if committer.calls != 1 {
		t.Fatalf("committer calls = %d, want 1", committer.calls)
	}
	if committer.meta.PayloadDigest != submittedDigest {
		t.Fatalf("payload digest = %q, want submitted digest %q", committer.meta.PayloadDigest, submittedDigest)
	}
	got := joinedRowBatchForLeakCheck(committer.rows)
	for _, leaked := range []string{
		"0123456789abcdef",
		"literal-fixture-secret",
		"tool-input-secret",
		"tool-output-secret",
		"event-error-secret",
		"raw-payload-secret",
		"capture-error-secret",
		"checkpoint-secret",
		"/Users/example/private",
	} {
		if strings.Contains(got, leaked) {
			t.Fatalf("committed rows leaked %q: %s", leaked, got)
		}
	}
	for _, marker := range []string{redaction.TokenMarker, redaction.SecretMarker, redaction.PathMarker, redaction.LiteralMarker} {
		if !strings.Contains(got, marker) {
			t.Fatalf("committed rows missing marker %q: %s", marker, got)
		}
	}
	for _, event := range committer.rows.ActivityEvents {
		if event.RedactionStatus != "redacted" || event.RedactionVersion != redaction.Version {
			t.Fatalf("activity event redaction metadata = %#v", event)
		}
	}
}

func TestIngestBatchRejectsBindingEpochAndDigestConflicts(t *testing.T) {
	control, token, sourceID := testIngestControlPlane(t)
	tests := []struct {
		name     string
		mutate   func(*ingest.BatchRequest, *fakeIngestCommitter)
		wantCode int
	}{
		{
			name: "source binding mismatch",
			mutate: func(req *ingest.BatchRequest, _ *fakeIngestCommitter) {
				req.SourceIDs = []string{"source-wrong"}
			},
			wantCode: http.StatusForbidden,
		},
		{
			name: "epoch mismatch",
			mutate: func(req *ingest.BatchRequest, _ *fakeIngestCommitter) {
				req.ControlPlaneEpoch = "old"
			},
			wantCode: http.StatusConflict,
		},
		{
			name: "committer digest conflict",
			mutate: func(_ *ingest.BatchRequest, committer *fakeIngestCommitter) {
				committer.err = store.ErrIngestBatchDigestMismatch
			},
			wantCode: http.StatusConflict,
		},
		{
			name: "capture error source binding mismatch",
			mutate: func(req *ingest.BatchRequest, _ *fakeIngestCommitter) {
				req.CaptureErrors = []models.CaptureError{{
					ID:         "capture-error-wrong-source",
					SourceName: "wrong",
					SourceFile: "session.jsonl",
				}}
			},
			wantCode: http.StatusForbidden,
		},
		{
			name: "unsupported redaction version",
			mutate: func(req *ingest.BatchRequest, _ *fakeIngestCommitter) {
				req.RedactionVersion = ""
			},
			wantCode: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			committer := &fakeIngestCommitter{}
			handler := NewIngestHandlers(control, committer, 0, 0, nil, nil)
			req := testIngestBatchRequest(t, sourceID)
			tt.mutate(&req, committer)
			req.PayloadDigest = computeTestBatchDigest(t, req)
			rec := postIngestJSON(t, handler.Batch, req, token)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), tt.wantCode)
			}
		})
	}
}

func TestIngestBatchRejectsResetPendingBeforeCommit(t *testing.T) {
	control, token, sourceID := testIngestControlPlane(t)
	if _, err := control.BeginReset(context.Background()); err != nil {
		t.Fatalf("BeginReset: %v", err)
	}
	committer := &fakeIngestCommitter{}
	handler := NewIngestHandlers(control, committer, 0, 0, nil, nil)
	req := testIngestBatchRequest(t, sourceID)

	rec := postIngestJSON(t, handler.Batch, req, token)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", rec.Code, rec.Body.String())
	}
	if committer.calls != 0 {
		t.Fatalf("committer calls = %d, want no commit during reset", committer.calls)
	}
}

func TestIngestBatchRejectsOldEpochAfterResetCompletion(t *testing.T) {
	control, token, sourceID := testIngestControlPlane(t)
	if _, err := control.BeginReset(context.Background()); err != nil {
		t.Fatalf("BeginReset: %v", err)
	}
	completed, err := control.CompleteReset(context.Background())
	if err != nil {
		t.Fatalf("CompleteReset: %v", err)
	}
	if completed.SchemaEpoch == controlplane.InitialSchemaEpoch {
		t.Fatalf("schema epoch did not advance after reset: %#v", completed)
	}
	committer := &fakeIngestCommitter{}
	handler := NewIngestHandlers(control, committer, 0, 0, nil, nil)
	req := testIngestBatchRequest(t, sourceID)

	rec := postIngestJSON(t, handler.Batch, req, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
	}
	if committer.calls != 0 {
		t.Fatalf("committer calls = %d, want no commit for old epoch", committer.calls)
	}

	req.ControlPlaneEpoch = completed.SchemaEpoch
	req.PayloadDigest = computeTestBatchDigest(t, req)
	rec = postIngestJSON(t, handler.Batch, req, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status with stale token and new epoch = %d body=%s, want 409", rec.Code, rec.Body.String())
	}
	if committer.calls != 0 {
		t.Fatalf("committer calls = %d, want no commit for stale token", committer.calls)
	}
}

func TestIngestEnrollCompletesRemoteEnrollment(t *testing.T) {
	control, enrollToken := testEnrollControlPlane(t)
	committer := &fakeIngestCommitter{}
	handler := NewIngestHandlers(control, committer, 0, 0, nil, nil)
	req := ingest.EnrollRequest{
		Schema: ingest.SchemaV1,
		Bootstrap: ingest.EnrollBootstrap{
			NodeID:      "node-claimed",
			NodeName:    "Remote",
			CollectorID: "collector-claimed",
			Sources: []ingest.EnrollSourceRegistration{{
				Name:      "codex",
				Runtime:   models.RuntimeCodex,
				Provider:  models.ProviderOpenAI,
				Format:    models.FormatJSONL,
				WatchRoot: "~/.codex",
			}},
		},
	}
	rec := postIngestJSON(t, handler.Enroll, req, enrollToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var resp ingest.EnrollResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode enrollment: %v", err)
	}
	if resp.IngestToken == "" || resp.Assignment.CollectorID == "collector-claimed" || resp.Assignment.NodeID == "node-claimed" || len(resp.Assignment.SourceIDs) != 1 {
		t.Fatalf("enrollment response = %#v", resp)
	}
	if resp.Assignment.ControlPlaneEpoch == "" {
		t.Fatalf("assignment response = %#v", resp.Assignment)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw enrollment response: %v", err)
	}
	if _, ok := raw["snapshot"]; ok {
		t.Fatalf("enrollment response leaked full snapshot: %s", rec.Body.String())
	}
	if _, ok := raw["token"]; ok {
		t.Fatalf("enrollment response leaked token metadata: %s", rec.Body.String())
	}
}

func TestIngestEnrollRejectsResetPendingWithoutUsingToken(t *testing.T) {
	control, enrollToken := testEnrollControlPlane(t)
	if _, err := control.BeginReset(context.Background()); err != nil {
		t.Fatalf("BeginReset: %v", err)
	}
	handler := NewIngestHandlers(control, &fakeIngestCommitter{}, 0, 0, nil, nil)
	req := ingest.EnrollRequest{
		Schema: ingest.SchemaV1,
		Bootstrap: ingest.EnrollBootstrap{
			NodeName: "Remote",
			Sources: []ingest.EnrollSourceRegistration{{
				Name:      "codex",
				Runtime:   models.RuntimeCodex,
				Provider:  models.ProviderOpenAI,
				Format:    models.FormatJSONL,
				WatchRoot: "~/.codex",
			}},
		},
	}

	rec := postIngestJSON(t, handler.Enroll, req, enrollToken)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", rec.Code, rec.Body.String())
	}
	if _, err := control.AuthenticateToken(context.Background(), controlplane.AuthenticateTokenRequest{
		Plaintext:      enrollToken,
		AllowedTypes:   []string{controlplane.TokenTypeEnroll},
		RequiredScopes: []string{controlplane.ScopeEnroll},
	}); err != nil {
		t.Fatalf("enrollment token after reset-pending rejection = %v, want still active", err)
	}
}

func TestReplacementIngestUseRevokesOlderCollectorToken(t *testing.T) {
	control, enrollToken := testEnrollControlPlane(t)
	handler := NewIngestHandlers(control, &fakeIngestCommitter{}, 0, 0, nil, nil)
	boot := ingest.EnrollBootstrap{
		NodeName: "Remote",
		Sources: []ingest.EnrollSourceRegistration{{
			Name:      "codex",
			Runtime:   models.RuntimeCodex,
			Provider:  models.ProviderOpenAI,
			Format:    models.FormatJSONL,
			WatchRoot: "~/.codex",
		}},
	}
	first := postIngestJSON(t, handler.Enroll, ingest.EnrollRequest{Schema: ingest.SchemaV1, Bootstrap: boot}, enrollToken)
	if first.Code != http.StatusOK {
		t.Fatalf("first enroll status = %d body=%s, want 200", first.Code, first.Body.String())
	}
	var firstResp ingest.EnrollResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("decode first enrollment: %v", err)
	}
	secondEnroll, err := control.CreateToken(context.Background(), controlplane.CreateTokenRequest{Type: controlplane.TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken second enroll: %v", err)
	}
	boot.NodeID = firstResp.Assignment.NodeID
	boot.CollectorID = firstResp.Assignment.CollectorID
	second := postIngestJSON(t, handler.Enroll, ingest.EnrollRequest{
		Schema:              ingest.SchemaV1,
		Bootstrap:           boot,
		ExistingIngestToken: firstResp.IngestToken,
	}, secondEnroll.Plaintext)
	if second.Code != http.StatusOK {
		t.Fatalf("second enroll status = %d body=%s, want 200", second.Code, second.Body.String())
	}
	var secondResp ingest.EnrollResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second enrollment: %v", err)
	}
	if _, err := control.AuthenticateToken(context.Background(), controlplane.AuthenticateTokenRequest{
		Plaintext:      firstResp.IngestToken,
		AllowedTypes:   []string{controlplane.TokenTypeIngest},
		RequiredScopes: []string{controlplane.ScopeIngest},
		NodeID:         firstResp.Assignment.NodeID,
		CollectorID:    firstResp.Assignment.CollectorID,
		SourceID:       firstResp.Assignment.SourceIDs[0],
	}); err != nil {
		t.Fatalf("old ingest token should authenticate before replacement use: %v", err)
	}

	batch := testIngestBatchRequest(t, secondResp.Assignment.SourceIDs[0])
	batch.BatchID = "batch-rotated-token"
	batch.NodeID = secondResp.Assignment.NodeID
	batch.CollectorID = secondResp.Assignment.CollectorID
	batch.SourceIDs = secondResp.Assignment.SourceIDs
	batch.PayloadDigest = computeTestBatchDigest(t, batch)
	rec := postIngestJSON(t, handler.Batch, batch, secondResp.IngestToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	_, err = control.AuthenticateToken(context.Background(), controlplane.AuthenticateTokenRequest{
		Plaintext:      firstResp.IngestToken,
		AllowedTypes:   []string{controlplane.TokenTypeIngest},
		RequiredScopes: []string{controlplane.ScopeIngest},
		NodeID:         firstResp.Assignment.NodeID,
		CollectorID:    firstResp.Assignment.CollectorID,
		SourceID:       firstResp.Assignment.SourceIDs[0],
	})
	if !errors.Is(err, controlplane.ErrTokenRevoked) {
		t.Fatalf("old ingest token after replacement batch = %v, want revoked", err)
	}
}

func TestReplacementBatchWithInvalidCheckpointDoesNotRevokeOlderToken(t *testing.T) {
	control, enrollToken := testEnrollControlPlane(t)
	handler := NewIngestHandlers(control, &fakeIngestCommitter{}, 0, 0, nil, nil)
	boot := ingest.EnrollBootstrap{
		NodeName: "Remote",
		Sources: []ingest.EnrollSourceRegistration{{
			Name:      "codex",
			Runtime:   models.RuntimeCodex,
			Provider:  models.ProviderOpenAI,
			Format:    models.FormatJSONL,
			WatchRoot: "~/.codex",
		}},
	}
	first := postIngestJSON(t, handler.Enroll, ingest.EnrollRequest{Schema: ingest.SchemaV1, Bootstrap: boot}, enrollToken)
	if first.Code != http.StatusOK {
		t.Fatalf("first enroll status = %d body=%s, want 200", first.Code, first.Body.String())
	}
	var firstResp ingest.EnrollResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("decode first enrollment: %v", err)
	}
	secondEnroll, err := control.CreateToken(context.Background(), controlplane.CreateTokenRequest{Type: controlplane.TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken second enroll: %v", err)
	}
	boot.NodeID = firstResp.Assignment.NodeID
	boot.CollectorID = firstResp.Assignment.CollectorID
	second := postIngestJSON(t, handler.Enroll, ingest.EnrollRequest{
		Schema:              ingest.SchemaV1,
		Bootstrap:           boot,
		ExistingIngestToken: firstResp.IngestToken,
	}, secondEnroll.Plaintext)
	if second.Code != http.StatusOK {
		t.Fatalf("second enroll status = %d body=%s, want 200", second.Code, second.Body.String())
	}
	var secondResp ingest.EnrollResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second enrollment: %v", err)
	}

	batch := testIngestBatchRequest(t, secondResp.Assignment.SourceIDs[0])
	batch.BatchID = "batch-invalid-checkpoint"
	batch.NodeID = secondResp.Assignment.NodeID
	batch.CollectorID = secondResp.Assignment.CollectorID
	batch.SourceIDs = secondResp.Assignment.SourceIDs
	batch.Checkpoints = []models.Checkpoint{{
		SourceName: "unknown",
		SourceFile: "session.jsonl",
		LastOffset: 32,
		LastLineNo: 1,
	}}
	batch.PayloadDigest = computeTestBatchDigest(t, batch)
	rec := postIngestJSON(t, handler.Batch, batch, secondResp.IngestToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("batch status = %d body=%s, want 403", rec.Code, rec.Body.String())
	}
	if _, err := control.AuthenticateToken(context.Background(), controlplane.AuthenticateTokenRequest{
		Plaintext:      firstResp.IngestToken,
		AllowedTypes:   []string{controlplane.TokenTypeIngest},
		RequiredScopes: []string{controlplane.ScopeIngest},
		NodeID:         firstResp.Assignment.NodeID,
		CollectorID:    firstResp.Assignment.CollectorID,
		SourceID:       firstResp.Assignment.SourceIDs[0],
	}); err != nil {
		t.Fatalf("old ingest token after rejected replacement batch = %v, want still active", err)
	}
}

func TestBatchCommitStillSucceedsWhenTokenCleanupFails(t *testing.T) {
	control, token, sourceID := testIngestControlPlane(t)
	committer := &fakeIngestCommitter{
		afterCommit: func() {
			if err := control.Close(); err != nil {
				t.Fatalf("Close control-plane after commit: %v", err)
			}
		},
	}
	handler := NewIngestHandlers(control, committer, 0, 0, nil, nil)
	req := testIngestBatchRequest(t, sourceID)

	rec := postIngestJSON(t, handler.Batch, req, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200 despite cleanup failure after commit", rec.Code, rec.Body.String())
	}
	if committer.calls != 1 {
		t.Fatalf("committer calls = %d, want committed once", committer.calls)
	}
}

func TestReplacementHeartbeatWithBlankSourceDoesNotRevokeOlderToken(t *testing.T) {
	control, enrollToken := testEnrollControlPlane(t)
	handler := NewIngestHandlers(control, &fakeIngestCommitter{}, 0, 0, nil, nil)
	boot := ingest.EnrollBootstrap{
		NodeName: "Remote",
		Sources: []ingest.EnrollSourceRegistration{{
			Name:      "codex",
			Runtime:   models.RuntimeCodex,
			Provider:  models.ProviderOpenAI,
			Format:    models.FormatJSONL,
			WatchRoot: "~/.codex",
		}},
	}
	first := postIngestJSON(t, handler.Enroll, ingest.EnrollRequest{Schema: ingest.SchemaV1, Bootstrap: boot}, enrollToken)
	if first.Code != http.StatusOK {
		t.Fatalf("first enroll status = %d body=%s, want 200", first.Code, first.Body.String())
	}
	var firstResp ingest.EnrollResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("decode first enrollment: %v", err)
	}
	secondEnroll, err := control.CreateToken(context.Background(), controlplane.CreateTokenRequest{Type: controlplane.TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken second enroll: %v", err)
	}
	boot.NodeID = firstResp.Assignment.NodeID
	boot.CollectorID = firstResp.Assignment.CollectorID
	second := postIngestJSON(t, handler.Enroll, ingest.EnrollRequest{
		Schema:              ingest.SchemaV1,
		Bootstrap:           boot,
		ExistingIngestToken: firstResp.IngestToken,
	}, secondEnroll.Plaintext)
	if second.Code != http.StatusOK {
		t.Fatalf("second enroll status = %d body=%s, want 200", second.Code, second.Body.String())
	}
	var secondResp ingest.EnrollResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second enrollment: %v", err)
	}

	req := ingest.HeartbeatRequest{
		Schema:            ingest.SchemaV1,
		CollectorID:       secondResp.Assignment.CollectorID,
		NodeID:            secondResp.Assignment.NodeID,
		ControlPlaneEpoch: secondResp.Assignment.ControlPlaneEpoch,
		Sources: []ingest.HeartbeatSource{
			{
				SourceID: secondResp.Assignment.SourceIDs[0],
				Status:   "healthy",
			},
			{
				SourceID: " ",
				Status:   "healthy",
			},
		},
	}
	rec := postIngestJSON(t, handler.Heartbeat, req, secondResp.IngestToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("heartbeat status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if _, err := control.AuthenticateToken(context.Background(), controlplane.AuthenticateTokenRequest{
		Plaintext:      firstResp.IngestToken,
		AllowedTypes:   []string{controlplane.TokenTypeIngest},
		RequiredScopes: []string{controlplane.ScopeIngest},
		NodeID:         firstResp.Assignment.NodeID,
		CollectorID:    firstResp.Assignment.CollectorID,
		SourceID:       firstResp.Assignment.SourceIDs[0],
	}); err != nil {
		t.Fatalf("old ingest token after malformed heartbeat = %v, want still active", err)
	}
}

func TestIngestEnrollClassifiesInvalidBootstrapAsBadRequest(t *testing.T) {
	control, enrollToken := testEnrollControlPlane(t)
	handler := NewIngestHandlers(control, &fakeIngestCommitter{}, 0, 0, nil, nil)
	req := ingest.EnrollRequest{
		Schema: ingest.SchemaV1,
		Bootstrap: ingest.EnrollBootstrap{
			Sources: []ingest.EnrollSourceRegistration{{
				Runtime:   models.RuntimeCodex,
				Provider:  models.ProviderOpenAI,
				Format:    models.FormatJSONL,
				WatchRoot: "~/.codex",
			}},
		},
	}
	rec := postIngestJSON(t, handler.Enroll, req, enrollToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestIngestGzipRequiresBearerBeforeDecodeAndCapsDecompressedBody(t *testing.T) {
	control, token, _ := testIngestControlPlane(t)
	handler := NewIngestHandlers(control, &fakeIngestCommitter{}, 0, 0, nil, nil)
	hugeCompressed := gzipBytes(t, []byte(strings.Repeat(" ", ingest.MaxBodyBytes+1)))

	missingAuth := httptest.NewRequest(http.MethodPost, "/api/ingest/v1/batches", bytes.NewReader(hugeCompressed))
	missingAuth.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.Batch(rec, missingAuth)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d body=%s, want 401", rec.Code, rec.Body.String())
	}

	withBearer := httptest.NewRequest(http.MethodPost, "/api/ingest/v1/batches", bytes.NewReader(hugeCompressed))
	withBearer.Header.Set("Content-Encoding", "gzip")
	withBearer.Header.Set("Authorization", "Bearer invalid")
	rec = httptest.NewRecorder()
	handler.Batch(rec, withBearer)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid auth status = %d body=%s, want 401", rec.Code, rec.Body.String())
	}

	validBearer := httptest.NewRequest(http.MethodPost, "/api/ingest/v1/batches", bytes.NewReader(hugeCompressed))
	validBearer.Header.Set("Content-Encoding", "gzip")
	validBearer.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.Batch(rec, validBearer)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("valid auth oversized gzip status = %d body=%s, want 413", rec.Code, rec.Body.String())
	}
}

func TestIngestHeartbeatAuthenticatesAndPersists(t *testing.T) {
	control, token, sourceID := testIngestControlPlane(t)
	committer := &fakeIngestCommitter{}
	handler := NewIngestHandlers(control, committer, 0, 0, nil, nil)
	req := ingest.HeartbeatRequest{
		Schema:            ingest.SchemaV1,
		CollectorID:       "collector-web",
		NodeID:            "node-web",
		ControlPlaneEpoch: controlplane.InitialSchemaEpoch,
		QueueDepth:        3,
		SpoolBytes:        2048,
		ActiveFiles:       2,
		Sources: []ingest.HeartbeatSource{{
			SourceID:   sourceID,
			Status:     "degraded",
			ErrorCount: 1,
		}},
	}

	rec := postIngestJSON(t, handler.Heartbeat, req, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if len(committer.heartbeats) != 1 {
		t.Fatalf("heartbeats = %#v, want one row", committer.heartbeats)
	}
	row := committer.heartbeats[0]
	if row.CollectorID != "collector-web" || row.NodeID != "node-web" || row.SourceID != sourceID ||
		row.SourceName != "codex" || row.Status != "degraded" || row.QueueDepth != 3 || row.SpoolBytes != 2048 ||
		row.ActiveFiles != 2 || row.ErrorCount != 1 {
		t.Fatalf("heartbeat row = %#v", row)
	}
}

func TestIngestHeartbeatRejectsResetPending(t *testing.T) {
	control, token, sourceID := testIngestControlPlane(t)
	if _, err := control.BeginReset(context.Background()); err != nil {
		t.Fatalf("BeginReset: %v", err)
	}
	committer := &fakeIngestCommitter{}
	handler := NewIngestHandlers(control, committer, 0, 0, nil, nil)
	req := ingest.HeartbeatRequest{
		Schema:            ingest.SchemaV1,
		CollectorID:       "collector-web",
		NodeID:            "node-web",
		ControlPlaneEpoch: controlplane.InitialSchemaEpoch,
		Sources:           []ingest.HeartbeatSource{{SourceID: sourceID, Status: "healthy"}},
	}

	rec := postIngestJSON(t, handler.Heartbeat, req, token)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", rec.Code, rec.Body.String())
	}
	if len(committer.heartbeats) != 0 {
		t.Fatalf("heartbeats = %#v, want none during reset", committer.heartbeats)
	}
}

func TestIngestHeartbeatRejectsStaleTokenAfterResetCompletion(t *testing.T) {
	control, token, sourceID := testIngestControlPlane(t)
	if _, err := control.BeginReset(context.Background()); err != nil {
		t.Fatalf("BeginReset: %v", err)
	}
	completed, err := control.CompleteReset(context.Background())
	if err != nil {
		t.Fatalf("CompleteReset: %v", err)
	}
	committer := &fakeIngestCommitter{}
	handler := NewIngestHandlers(control, committer, 0, 0, nil, nil)
	req := ingest.HeartbeatRequest{
		Schema:            ingest.SchemaV1,
		CollectorID:       "collector-web",
		NodeID:            "node-web",
		ControlPlaneEpoch: completed.SchemaEpoch,
		Sources:           []ingest.HeartbeatSource{{SourceID: sourceID, Status: "healthy"}},
	}

	rec := postIngestJSON(t, handler.Heartbeat, req, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
	}
	if len(committer.heartbeats) != 0 {
		t.Fatalf("heartbeats = %#v, want none for stale token", committer.heartbeats)
	}
}

type fakeIngestCommitter struct {
	calls       int
	meta        store.IngestBatchMeta
	rows        store.RowBatch
	heartbeats  []models.CaptureHeartbeat
	err         error
	afterCommit func()
}

func (f *fakeIngestCommitter) CommitIngestBatch(_ context.Context, meta store.IngestBatchMeta, rows store.RowBatch) (store.IngestBatchAck, error) {
	f.calls++
	f.meta = meta
	f.rows = rows
	if f.err != nil {
		return store.IngestBatchAck{}, f.err
	}
	if f.afterCommit != nil {
		f.afterCommit()
	}
	return store.IngestBatchAck{
		BatchID:           meta.BatchID,
		PayloadDigest:     meta.PayloadDigest,
		EventsWritten:     len(rows.ActivityEvents),
		RawRecordsWritten: len(rows.RawRecords),
		NextSequence:      meta.Sequence + 1,
		ControlPlaneEpoch: meta.ControlPlaneEpoch,
	}, nil
}

func (f *fakeIngestCommitter) InsertCaptureHeartbeats(_ context.Context, heartbeats []models.CaptureHeartbeat) error {
	f.heartbeats = append(f.heartbeats, heartbeats...)
	return nil
}

func joinedRowBatchForLeakCheck(rows store.RowBatch) string {
	var b strings.Builder
	for _, event := range rows.ActivityEvents {
		b.WriteString(event.TextContent)
		b.WriteString(event.TextPreview)
		b.WriteString(event.ErrorMessage)
		b.WriteString(event.PayloadJSON)
		b.WriteString(event.CWD)
		b.WriteString(event.SourceFile)
	}
	for _, raw := range rows.RawRecords {
		b.WriteString(raw.SourceFile)
		b.WriteString(raw.PayloadJSON)
	}
	for _, payload := range rows.ToolPayloads {
		b.WriteString(payload.InputJSON)
		b.WriteString(payload.OutputJSON)
		b.WriteString(payload.InputPreview)
		b.WriteString(payload.OutputPreview)
	}
	for _, captureErr := range rows.CaptureErrors {
		b.WriteString(captureErr.SourceFile)
		b.WriteString(captureErr.ErrorMessage)
		b.WriteString(captureErr.ContextFragment)
	}
	for _, checkpoint := range rows.Checkpoints {
		b.WriteString(checkpoint.SourceFile)
		b.WriteString(checkpoint.StateJSON)
	}
	return b.String()
}

func testIngestControlPlane(t *testing.T) (*controlplane.Store, string, string) {
	t.Helper()
	control, err := controlplane.Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open control-plane: %v", err)
	}
	t.Cleanup(func() { _ = control.Close() })
	snapshot, err := control.EnsureLocal(context.Background(), controlplane.Bootstrap{
		NodeID:      "node-web",
		NodeName:    "Web",
		CollectorID: "collector-web",
		Sources: []controlplane.SourceRegistration{{
			Name:      "codex",
			Runtime:   models.RuntimeCodex,
			Provider:  models.ProviderOpenAI,
			Format:    models.FormatJSONL,
			WatchRoot: "~/.codex",
		}},
	})
	if err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	sourceID := snapshot.Sources[0].ID
	token, err := control.CreateToken(context.Background(), controlplane.CreateTokenRequest{
		Type:        controlplane.TokenTypeIngest,
		NodeID:      "node-web",
		CollectorID: "collector-web",
		SourceIDs:   []string{sourceID},
	})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	return control, token.Plaintext, sourceID
}

func testEnrollControlPlane(t *testing.T) (*controlplane.Store, string) {
	t.Helper()
	control, err := controlplane.Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open control-plane: %v", err)
	}
	t.Cleanup(func() { _ = control.Close() })
	if _, err := control.EnsureLocal(context.Background(), controlplane.Bootstrap{
		NodeID:      "node-local",
		NodeName:    "Local",
		CollectorID: "collector-local",
		Sources: []controlplane.SourceRegistration{{
			Name:      "local-codex",
			Runtime:   models.RuntimeCodex,
			Provider:  models.ProviderOpenAI,
			Format:    models.FormatJSONL,
			WatchRoot: "~/.codex",
		}},
	}); err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	enroll, err := control.CreateToken(context.Background(), controlplane.CreateTokenRequest{Type: controlplane.TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	return control, enroll.Plaintext
}

func testIngestBatchRequest(t *testing.T, sourceID string) ingest.BatchRequest {
	t.Helper()
	req := ingest.BatchRequest{
		Schema:            ingest.SchemaV1,
		BatchID:           "batch-web",
		CollectorID:       "collector-web",
		NodeID:            "node-web",
		ControlPlaneEpoch: controlplane.InitialSchemaEpoch,
		CreatedAt:         time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		Sequence:          1,
		RedactionVersion:  "redact-v1",
		SourceIDs:         []string{sourceID},
		Events: []capture.NormalizedEvent{{
			SessionID:    "session-web",
			SourceName:   "codex",
			Runtime:      models.RuntimeCodex,
			Provider:     models.ProviderOpenAI,
			Format:       models.FormatJSONL,
			EventKind:    models.EventKindMessage,
			ActorRole:    models.ActorRoleAssistant,
			Timestamp:    time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
			TextContent:  "hello",
			RawPayload:   `{"message":"hello"}`,
			SourceFile:   "session.jsonl",
			SourceLineNo: 1,
		}},
		Checkpoints: []models.Checkpoint{{
			SourceName: "codex",
			SourceFile: "session.jsonl",
			LastOffset: 32,
			LastLineNo: 1,
		}},
	}
	req.PayloadDigest = computeTestBatchDigest(t, req)
	return req
}

func computeTestBatchDigest(t *testing.T, req ingest.BatchRequest) string {
	t.Helper()
	digest, err := ingest.ComputeBatchDigest(req)
	if err != nil {
		t.Fatalf("ComputeBatchDigest: %v", err)
	}
	return digest
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func postIngestJSON(t *testing.T, handler http.HandlerFunc, payload any, token string) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/ingest/v1/batches", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestIngestPayloadDigestMismatchRejectsBeforeCommit(t *testing.T) {
	control, token, sourceID := testIngestControlPlane(t)
	committer := &fakeIngestCommitter{}
	handler := NewIngestHandlers(control, committer, 0, 0, nil, nil)
	req := testIngestBatchRequest(t, sourceID)
	req.PayloadDigest = "sha256:bad"

	rec := postIngestJSON(t, handler.Batch, req, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if committer.calls != 0 {
		t.Fatalf("committer calls = %d, want 0", committer.calls)
	}
}
