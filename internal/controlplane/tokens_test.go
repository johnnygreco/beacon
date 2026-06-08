package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTokenExpiryAndRevocation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	past := time.Now().UTC().Add(-time.Second)
	expired, err := store.CreateToken(context.Background(), CreateTokenRequest{
		Type:      TokenTypeRead,
		ExpiresAt: &past,
	})
	if err != nil {
		t.Fatalf("CreateToken expired: %v", err)
	}
	_, err = store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      expired.Plaintext,
		AllowedTypes:   []string{TokenTypeRead},
		RequiredScopes: []string{ScopeRead},
	})
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expired AuthenticateToken error = %v, want ErrTokenExpired", err)
	}

	active, err := store.CreateToken(context.Background(), CreateTokenRequest{Type: TokenTypeRead})
	if err != nil {
		t.Fatalf("CreateToken active: %v", err)
	}
	if err := store.RevokeToken(context.Background(), active.Record.ID); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	_, err = store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      active.Plaintext,
		AllowedTypes:   []string{TokenTypeRead},
		RequiredScopes: []string{ScopeRead},
	})
	if !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("revoked AuthenticateToken error = %v, want ErrTokenRevoked", err)
	}
}

func TestEnrollmentTokenIsOneUseAndMintsBoundIngestToken(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	expires := time.Now().UTC().Add(time.Hour)
	enroll, err := store.CreateToken(context.Background(), CreateTokenRequest{
		Type:      TokenTypeEnroll,
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("CreateToken enroll: %v", err)
	}
	result, err := store.CompleteEnrollment(context.Background(), enroll.Plaintext, testBootstrap())
	if err != nil {
		t.Fatalf("CompleteEnrollment: %v", err)
	}
	if result.IngestToken.Plaintext == "" {
		t.Fatal("CompleteEnrollment returned empty ingest token")
	}
	if result.IngestToken.Record.NodeID != "node-test" || result.IngestToken.Record.CollectorID != "collector-test" {
		t.Fatalf("ingest bindings = %#v, want node-test/collector-test", result.IngestToken.Record)
	}
	if len(result.IngestToken.Record.SourceIDs) != 2 {
		t.Fatalf("ingest source bindings = %#v, want two sources", result.IngestToken.Record.SourceIDs)
	}
	if _, err := store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      result.IngestToken.Plaintext,
		AllowedTypes:   []string{TokenTypeIngest},
		RequiredScopes: []string{ScopeIngest},
		NodeID:         "node-test",
		CollectorID:    "collector-test",
		SourceID:       result.IngestToken.Record.SourceIDs[0],
	}); err != nil {
		t.Fatalf("AuthenticateToken ingest: %v", err)
	}
	_, err = store.CompleteEnrollment(context.Background(), enroll.Plaintext, testBootstrap())
	if !errors.Is(err, ErrTokenUsed) {
		t.Fatalf("second CompleteEnrollment error = %v, want ErrTokenUsed", err)
	}
}

func TestRemoteReEnrollmentRequiresExistingIngestTokenAndRetiresOldTokenAfterUse(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	firstEnroll, err := store.CreateToken(context.Background(), CreateTokenRequest{Type: TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken first enroll: %v", err)
	}
	first, err := store.CompleteRemoteEnrollment(context.Background(), firstEnroll.Plaintext, "", testBootstrap())
	if err != nil {
		t.Fatalf("CompleteRemoteEnrollment first: %v", err)
	}
	nodeID := first.IngestToken.Record.NodeID
	collectorID := first.IngestToken.Record.CollectorID
	sourceID := first.IngestToken.Record.SourceIDs[0]
	if nodeID == "node-test" || collectorID == "collector-test" {
		t.Fatalf("remote enrollment reused claimed IDs without existing collector: %#v", first.IngestToken.Record)
	}

	secondEnroll, err := store.CreateToken(context.Background(), CreateTokenRequest{Type: TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken second enroll: %v", err)
	}
	claimed := testBootstrap()
	claimed.NodeID = nodeID
	claimed.CollectorID = collectorID
	_, err = store.CompleteRemoteEnrollment(context.Background(), secondEnroll.Plaintext, "", claimed)
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("remote re-enrollment without proof error = %v, want ErrTokenInvalid", err)
	}
	if _, err := store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      secondEnroll.Plaintext,
		AllowedTypes:   []string{TokenTypeEnroll},
		RequiredScopes: []string{ScopeEnroll},
	}); err != nil {
		t.Fatalf("failed re-enrollment consumed enroll token: %v", err)
	}

	second, err := store.CompleteRemoteEnrollment(context.Background(), secondEnroll.Plaintext, first.IngestToken.Plaintext, claimed)
	if err != nil {
		t.Fatalf("CompleteRemoteEnrollment second: %v", err)
	}
	if second.IngestToken.Record.NodeID != nodeID || second.IngestToken.Record.CollectorID != collectorID {
		t.Fatalf("second assignment = %#v, want existing node/collector", second.IngestToken.Record)
	}
	if _, err := store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      first.IngestToken.Plaintext,
		AllowedTypes:   []string{TokenTypeIngest},
		RequiredScopes: []string{ScopeIngest},
		NodeID:         nodeID,
		CollectorID:    collectorID,
		SourceID:       sourceID,
	}); err != nil {
		t.Fatalf("old ingest token before replacement use should still authenticate: %v", err)
	}
	if err := store.RevokeOlderActiveIngestTokensForCollector(context.Background(), second.IngestToken.Record); err != nil {
		t.Fatalf("RevokeOlderActiveIngestTokensForCollector: %v", err)
	}
	_, err = store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      first.IngestToken.Plaintext,
		AllowedTypes:   []string{TokenTypeIngest},
		RequiredScopes: []string{ScopeIngest},
		NodeID:         nodeID,
		CollectorID:    collectorID,
		SourceID:       sourceID,
	})
	if !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("old ingest AuthenticateToken error = %v, want ErrTokenRevoked", err)
	}
	if _, err := store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      second.IngestToken.Plaintext,
		AllowedTypes:   []string{TokenTypeIngest},
		RequiredScopes: []string{ScopeIngest},
		NodeID:         nodeID,
		CollectorID:    collectorID,
		SourceID:       second.IngestToken.Record.SourceIDs[0],
	}); err != nil {
		t.Fatalf("new ingest token should authenticate: %v", err)
	}
}

func TestRevokeOlderActiveIngestTokensUsesChronologicalTimestamps(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	snapshot, err := store.EnsureLocal(context.Background(), testBootstrap())
	if err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	sourceIDs := sourceIDsForCollector(snapshot, "collector-test")
	oldToken, err := store.CreateToken(context.Background(), CreateTokenRequest{
		Type:        TokenTypeIngest,
		NodeID:      "node-test",
		CollectorID: "collector-test",
		SourceIDs:   sourceIDs,
	})
	if err != nil {
		t.Fatalf("CreateToken old: %v", err)
	}
	currentToken, err := store.CreateToken(context.Background(), CreateTokenRequest{
		Type:        TokenTypeIngest,
		NodeID:      "node-test",
		CollectorID: "collector-test",
		SourceIDs:   sourceIDs,
	})
	if err != nil {
		t.Fatalf("CreateToken current: %v", err)
	}
	oldCreated := parseTime("2026-06-08T12:00:00.1Z")
	currentCreated := parseTime("2026-06-08T12:00:00.1001Z")
	if !(formatTime(oldCreated) > formatTime(currentCreated)) {
		t.Fatalf("test setup invalid: old timestamp %q must sort after current %q", formatTime(oldCreated), formatTime(currentCreated))
	}
	if _, err := store.db.ExecContext(context.Background(),
		`UPDATE tokens
		 SET created_at = CASE token_id WHEN ? THEN ? WHEN ? THEN ? ELSE created_at END,
		     updated_at = CASE token_id WHEN ? THEN ? WHEN ? THEN ? ELSE updated_at END
		 WHERE token_id IN (?, ?)`,
		oldToken.Record.ID,
		formatTime(oldCreated),
		currentToken.Record.ID,
		formatTime(currentCreated),
		oldToken.Record.ID,
		formatTime(oldCreated),
		currentToken.Record.ID,
		formatTime(currentCreated),
		oldToken.Record.ID,
		currentToken.Record.ID,
	); err != nil {
		t.Fatalf("update token timestamps: %v", err)
	}
	currentRecord := currentToken.Record
	currentRecord.CreatedAt = currentCreated
	if err := store.RevokeOlderActiveIngestTokensForCollector(context.Background(), currentRecord); err != nil {
		t.Fatalf("RevokeOlderActiveIngestTokensForCollector: %v", err)
	}
	_, err = store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      oldToken.Plaintext,
		AllowedTypes:   []string{TokenTypeIngest},
		RequiredScopes: []string{ScopeIngest},
		NodeID:         "node-test",
		CollectorID:    "collector-test",
		SourceID:       sourceIDs[0],
	})
	if !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("old token auth error = %v, want revoked", err)
	}
	if _, err := store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      currentToken.Plaintext,
		AllowedTypes:   []string{TokenTypeIngest},
		RequiredScopes: []string{ScopeIngest},
		NodeID:         "node-test",
		CollectorID:    "collector-test",
		SourceID:       sourceIDs[0],
	}); err != nil {
		t.Fatalf("current token should remain active: %v", err)
	}
}

func TestEnrollmentTokensCannotIngest(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if _, err := store.CreateToken(context.Background(), CreateTokenRequest{
		Type:   TokenTypeEnroll,
		Scopes: []string{ScopeIngest},
	}); err == nil || !strings.Contains(err.Error(), "cannot carry ingest scope") {
		t.Fatalf("CreateToken enroll with ingest scope error = %v, want rejection", err)
	}
	enroll, err := store.CreateToken(context.Background(), CreateTokenRequest{Type: TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken enroll: %v", err)
	}
	_, err = store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      enroll.Plaintext,
		AllowedTypes:   []string{TokenTypeIngest},
		RequiredScopes: []string{ScopeIngest},
	})
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("AuthenticateToken enroll-as-ingest error = %v, want ErrTokenInvalid", err)
	}
}

func TestTokenHashOnlyStorageAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	snapshot, err := store.EnsureLocal(context.Background(), testBootstrap())
	if err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	sourceIDs := sourceIDsForCollector(snapshot, "collector-test")
	expires := time.Now().UTC().Add(time.Hour)
	created, err := store.CreateToken(context.Background(), CreateTokenRequest{
		Type:        TokenTypeIngest,
		NodeID:      "node-test",
		CollectorID: "collector-test",
		SourceIDs:   sourceIDs,
		ExpiresAt:   &expires,
	})
	if err != nil {
		t.Fatalf("CreateToken ingest: %v", err)
	}
	assertPlaintextNotStored(t, store.db, created.Plaintext)
	if err := store.RevokeToken(context.Background(), created.Record.ID); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open reopened: %v", err)
	}
	defer reopened.Close()
	_, err = reopened.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      created.Plaintext,
		AllowedTypes:   []string{TokenTypeIngest},
		RequiredScopes: []string{ScopeIngest},
		NodeID:         "node-test",
		CollectorID:    "collector-test",
		SourceID:       sourceIDs[0],
	})
	if !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("AuthenticateToken after restart error = %v, want ErrTokenRevoked", err)
	}
	var tokenType, status, scopes, sourceBindings, expiresAt string
	if err := reopened.db.QueryRowContext(context.Background(),
		`SELECT token_type, status, scopes, source_ids, COALESCE(expires_at, '')
		 FROM tokens WHERE token_id = ?`,
		created.Record.ID,
	).Scan(&tokenType, &status, &scopes, &sourceBindings, &expiresAt); err != nil {
		t.Fatalf("read token row: %v", err)
	}
	if tokenType != TokenTypeIngest || status != TokenStatusRevoked ||
		!strings.Contains(scopes, ScopeIngest) ||
		!strings.Contains(sourceBindings, sourceIDs[0]) ||
		expiresAt == "" {
		t.Fatalf("persisted token row = type:%q status:%q scopes:%q sources:%q expires:%q", tokenType, status, scopes, sourceBindings, expiresAt)
	}
}

func TestIngestTokenRejectsCollectorBindingMismatch(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	token, err := store.CreateToken(context.Background(), CreateTokenRequest{
		Type:        TokenTypeIngest,
		NodeID:      "node-a",
		CollectorID: "collector-a",
		SourceIDs:   []string{"source-a"},
	})
	if err != nil {
		t.Fatalf("CreateToken ingest: %v", err)
	}
	_, err = store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      token.Plaintext,
		AllowedTypes:   []string{TokenTypeIngest},
		RequiredScopes: []string{ScopeIngest},
		NodeID:         "node-a",
		CollectorID:    "collector-b",
		SourceID:       "source-a",
	})
	if !errors.Is(err, ErrTokenBindingMismatch) {
		t.Fatalf("AuthenticateToken collector mismatch error = %v, want ErrTokenBindingMismatch", err)
	}
}

func TestIngestTokenRejectsMissingBindingRequests(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	token, err := store.CreateToken(context.Background(), CreateTokenRequest{
		Type:        TokenTypeIngest,
		NodeID:      "node-a",
		CollectorID: "collector-a",
		SourceIDs:   []string{"source-a"},
	})
	if err != nil {
		t.Fatalf("CreateToken ingest: %v", err)
	}
	tests := []struct {
		name        string
		nodeID      string
		collectorID string
		sourceID    string
	}{
		{name: "missing node", collectorID: "collector-a", sourceID: "source-a"},
		{name: "missing collector", nodeID: "node-a", sourceID: "source-a"},
		{name: "missing source", nodeID: "node-a", collectorID: "collector-a"},
		{name: "missing all"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
				Plaintext:      token.Plaintext,
				AllowedTypes:   []string{TokenTypeIngest},
				RequiredScopes: []string{ScopeIngest},
				NodeID:         tt.nodeID,
				CollectorID:    tt.collectorID,
				SourceID:       tt.sourceID,
			})
			if !errors.Is(err, ErrTokenBindingMismatch) {
				t.Fatalf("AuthenticateToken error = %v, want ErrTokenBindingMismatch", err)
			}
		})
	}
}

func TestInvalidEnrollmentTokenDoesNotMutateMetadata(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	_, err = store.CompleteEnrollment(context.Background(), "not-a-real-token", testBootstrap())
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("CompleteEnrollment invalid token error = %v, want ErrTokenInvalid", err)
	}
	snapshot, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snapshot.Nodes) != 0 || len(snapshot.Collectors) != 0 || len(snapshot.Sources) != 0 {
		t.Fatalf("invalid enrollment mutated metadata: %#v", snapshot)
	}
}

func TestEnrollmentAssignmentFailureRollsBackMetadataAndTokenUse(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	enroll, err := store.CreateToken(context.Background(), CreateTokenRequest{Type: TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken enroll: %v", err)
	}
	boot := Bootstrap{
		NodeID:      "node-no-source",
		NodeName:    "No Source",
		CollectorID: "collector-no-source",
	}
	_, err = store.CompleteEnrollment(context.Background(), enroll.Plaintext, boot)
	if err == nil || !strings.Contains(err.Error(), "no source assignments") {
		t.Fatalf("CompleteEnrollment error = %v, want source assignment failure", err)
	}
	if _, err := store.AuthenticateToken(context.Background(), AuthenticateTokenRequest{
		Plaintext:      enroll.Plaintext,
		AllowedTypes:   []string{TokenTypeEnroll},
		RequiredScopes: []string{ScopeEnroll},
	}); err != nil {
		t.Fatalf("enroll token should remain active after rollback: %v", err)
	}
	snapshot, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snapshot.Nodes) != 0 || len(snapshot.Collectors) != 0 || len(snapshot.Sources) != 0 {
		t.Fatalf("failed enrollment mutated metadata: %#v", snapshot)
	}
}

func assertPlaintextNotStored(t *testing.T, db *sql.DB, plaintext string) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT token_id, token_type, token_hash, token_prefix, status, scopes,
		        COALESCE(node_id, ''), COALESCE(collector_id, ''), source_ids,
		        COALESCE(expires_at, ''), COALESCE(used_at, ''), COALESCE(revoked_at, '')
		 FROM tokens`,
	)
	if err != nil {
		t.Fatalf("read token rows: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		values := make([]string, 12)
		dest := make([]any, len(values))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatalf("scan token row: %v", err)
		}
		for _, value := range values {
			if value == plaintext || strings.Contains(value, plaintext) {
				t.Fatalf("plaintext token leaked into token row value %q", value)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read token rows: %v", err)
	}
}

func sourceIDsForCollector(snapshot *Snapshot, collectorID string) []string {
	var ids []string
	for _, source := range snapshot.Sources {
		if source.CollectorID == collectorID {
			ids = append(ids, source.ID)
		}
	}
	return ids
}

func testBootstrap() Bootstrap {
	return Bootstrap{
		NodeID:      "node-test",
		NodeName:    "Test Node",
		CollectorID: "collector-test",
		Sources: []SourceRegistration{
			{Name: "codex", Runtime: "codex", Provider: "openai", Format: "jsonl", WatchRoot: "/tmp/codex"},
			{Name: "claude", Runtime: "claude-code", Provider: "anthropic", Format: "jsonl", WatchRoot: "/tmp/claude"},
		},
	}
}
