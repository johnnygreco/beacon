package ingest

import (
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

func TestNormalizeBatchCanonicalizesEnvelope(t *testing.T) {
	local := time.FixedZone("local", -5*60*60)
	createdAt := time.Date(2026, 6, 8, 9, 30, 0, 0, local)
	req := BatchRequest{
		Schema:            "  " + SchemaV1 + "  ",
		BatchID:           " batch-1 ",
		CollectorID:       " collector-1 ",
		NodeID:            " node-1 ",
		ControlPlaneEpoch: " epoch-1 ",
		CreatedAt:         createdAt,
		Sequence:          7,
		PayloadDigest:     " sha256:abc ",
		RedactionVersion:  " " + RedactionVersionV1 + " ",
		SourceIDs:         []string{" source-b ", "", "source-a", "source-b"},
	}

	got := NormalizeBatch(req)

	if got.Schema != SchemaV1 {
		t.Fatalf("Schema = %q, want %q", got.Schema, SchemaV1)
	}
	for name, value := range map[string]string{
		"BatchID":           got.BatchID,
		"CollectorID":       got.CollectorID,
		"NodeID":            got.NodeID,
		"ControlPlaneEpoch": got.ControlPlaneEpoch,
		"PayloadDigest":     got.PayloadDigest,
		"RedactionVersion":  got.RedactionVersion,
	} {
		if strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ") {
			t.Fatalf("%s was not trimmed: %q", name, value)
		}
	}
	if got.PayloadDigest != "sha256:abc" {
		t.Fatalf("PayloadDigest = %q, want sha256:abc", got.PayloadDigest)
	}
	if got.RedactionVersion != RedactionVersionV1 {
		t.Fatalf("RedactionVersion = %q, want %q", got.RedactionVersion, RedactionVersionV1)
	}
	if want := []string{"source-a", "source-b"}; !sameStrings(got.SourceIDs, want) {
		t.Fatalf("SourceIDs = %#v, want %#v", got.SourceIDs, want)
	}
	if !got.CreatedAt.Equal(createdAt.UTC()) {
		t.Fatalf("CreatedAt = %s, want %s", got.CreatedAt, createdAt.UTC())
	}

	defaulted := NormalizeBatch(BatchRequest{})
	if !defaulted.CreatedAt.IsZero() {
		t.Fatalf("zero CreatedAt was defaulted to %s; created_at must be explicit for stable digests", defaulted.CreatedAt)
	}
}

func TestComputeBatchDigestIsStableAndContentSensitive(t *testing.T) {
	req := validBatchRequest(t)
	req.PayloadDigest = "sha256:ignored"

	first, err := ComputeBatchDigest(req)
	if err != nil {
		t.Fatalf("ComputeBatchDigest first: %v", err)
	}
	req.PayloadDigest = "sha256:also-ignored"
	second, err := ComputeBatchDigest(req)
	if err != nil {
		t.Fatalf("ComputeBatchDigest second: %v", err)
	}
	if first != second {
		t.Fatalf("digest changed when only PayloadDigest changed: %q != %q", first, second)
	}

	req.CaptureErrors[0].ErrorMessage = "different"
	changed, err := ComputeBatchDigest(req)
	if err != nil {
		t.Fatalf("ComputeBatchDigest changed: %v", err)
	}
	if changed == first {
		t.Fatalf("digest did not change after content mutation: %q", changed)
	}

	req.CreatedAt = time.Time{}
	if _, err := ComputeBatchDigest(req); err == nil || !strings.Contains(err.Error(), "created_at is required") {
		t.Fatalf("ComputeBatchDigest zero CreatedAt error = %v, want created_at required", err)
	}
}

func TestValidateBatchAcceptsCanonicalBatch(t *testing.T) {
	req := validBatchRequest(t)
	req.Schema = " " + req.Schema + " "
	req.BatchID = " " + req.BatchID + " "
	req.CollectorID = " " + req.CollectorID + " "
	req.NodeID = " " + req.NodeID + " "
	req.ControlPlaneEpoch = " " + req.ControlPlaneEpoch + " "
	req.RedactionVersion = " " + req.RedactionVersion + " "
	req.SourceIDs = []string{" source-b ", "source-a", "source-a"}
	digest, err := ComputeBatchDigest(req)
	if err != nil {
		t.Fatalf("ComputeBatchDigest: %v", err)
	}
	req.PayloadDigest = digest

	if err := ValidateBatch(req); err != nil {
		t.Fatalf("ValidateBatch: %v", err)
	}
}

func TestValidateBatchRejectsInvalidEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*BatchRequest)
		wantErr string
	}{
		{name: "schema", mutate: func(req *BatchRequest) { req.Schema = "wrong" }, wantErr: "schema must be"},
		{name: "batch id", mutate: func(req *BatchRequest) { req.BatchID = "" }, wantErr: "batch_id is required"},
		{name: "collector id", mutate: func(req *BatchRequest) { req.CollectorID = "" }, wantErr: "collector_id is required"},
		{name: "node id", mutate: func(req *BatchRequest) { req.NodeID = "" }, wantErr: "node_id is required"},
		{name: "epoch", mutate: func(req *BatchRequest) { req.ControlPlaneEpoch = "" }, wantErr: "control_plane_epoch is required"},
		{name: "created at", mutate: func(req *BatchRequest) { req.CreatedAt = time.Time{} }, wantErr: "created_at is required"},
		{name: "sequence", mutate: func(req *BatchRequest) { req.Sequence = 0 }, wantErr: "sequence must be positive"},
		{name: "sources", mutate: func(req *BatchRequest) { req.SourceIDs = nil }, wantErr: "source_ids must contain at least one source"},
		{name: "content", mutate: func(req *BatchRequest) { req.CaptureErrors = nil }, wantErr: "batch must contain events, capture_errors, or checkpoints"},
		{name: "redaction", mutate: func(req *BatchRequest) { req.RedactionVersion = "redact-v0" }, wantErr: "redaction_version must be"},
		{name: "digest required", mutate: func(req *BatchRequest) { req.PayloadDigest = "" }, wantErr: "payload_digest is required"},
		{name: "digest mismatch", mutate: func(req *BatchRequest) { req.PayloadDigest = "sha256:bad" }, wantErr: "payload_digest mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validBatchRequest(t)
			tt.mutate(&req)
			if tt.name != "digest required" && tt.name != "digest mismatch" && tt.name != "created at" {
				digest, err := ComputeBatchDigest(req)
				if err != nil {
					t.Fatalf("ComputeBatchDigest: %v", err)
				}
				req.PayloadDigest = digest
			}

			err := ValidateBatch(req)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateBatch error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func validBatchRequest(t *testing.T) BatchRequest {
	t.Helper()
	req := BatchRequest{
		Schema:            SchemaV1,
		BatchID:           "batch-1",
		CollectorID:       "collector-1",
		NodeID:            "node-1",
		ControlPlaneEpoch: "epoch-1",
		CreatedAt:         time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
		Sequence:          1,
		RedactionVersion:  RedactionVersionV1,
		SourceIDs:         []string{"source-a"},
		CaptureErrors: []models.CaptureError{{
			ID:              "capture-error-1",
			SourceID:        "source-a",
			SourceName:      "codex",
			SourceFile:      "/tmp/session.jsonl",
			ErrorClass:      "parse",
			ErrorMessage:    "bad payload",
			ContextFragment: "fragment",
			CreatedAt:       time.Date(2026, 6, 8, 12, 1, 0, 0, time.UTC),
		}},
	}
	digest, err := ComputeBatchDigest(req)
	if err != nil {
		t.Fatalf("ComputeBatchDigest: %v", err)
	}
	req.PayloadDigest = digest
	return req
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
