package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	TokenTypeOwner  = "owner"
	TokenTypeEnroll = "enroll"
	TokenTypeIngest = "ingest"
	TokenTypeRead   = "read"
	TokenTypeAdmin  = "admin"

	TokenStatusActive  = "active"
	TokenStatusUsed    = "used"
	TokenStatusRevoked = "revoked"

	ScopeOwner  = "owner"
	ScopeEnroll = "enroll"
	ScopeIngest = "ingest"
	ScopeRead   = "read"
	ScopeAdmin  = "admin"
)

const tokenPrefixLength = 24

var (
	ErrTokenInvalid         = errors.New("token invalid")
	ErrTokenExpired         = errors.New("token expired")
	ErrTokenRevoked         = errors.New("token revoked")
	ErrTokenUsed            = errors.New("token already used")
	ErrTokenScopeDenied     = errors.New("token scope denied")
	ErrTokenBindingMismatch = errors.New("token binding mismatch")
	ErrEnrollmentInvalid    = errors.New("enrollment invalid")
)

type TokenRecord struct {
	ID          string
	Type        string
	Status      string
	Prefix      string
	Scopes      []string
	NodeID      string
	CollectorID string
	SourceIDs   []string
	ExpiresAt   *time.Time
	UsedAt      *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreatedToken struct {
	Record    TokenRecord
	Plaintext string
}

type CreateTokenRequest struct {
	Type        string
	Scopes      []string
	NodeID      string
	CollectorID string
	SourceIDs   []string
	ExpiresAt   *time.Time
}

type AuthenticateTokenRequest struct {
	Plaintext        string
	AllowedTypes     []string
	RequiredScopes   []string
	NodeID           string
	CollectorID      string
	SourceID         string
	SourceIDs        []string
	SkipBindingCheck bool
}

type EnrollmentResult struct {
	Snapshot    *Snapshot
	IngestToken CreatedToken
}

type tokenQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) CreateToken(ctx context.Context, req CreateTokenRequest) (*CreatedToken, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("control-plane metadata store is nil")
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin token transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	created, err := insertToken(ctx, tx, req, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit token transaction: %w", err)
	}
	return created, nil
}

func (s *Store) AuthenticateToken(ctx context.Context, req AuthenticateTokenRequest) (*TokenRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("control-plane metadata store is nil")
	}
	return authenticateToken(ctx, s.db, req, time.Now().UTC())
}

func (s *Store) RevokeToken(ctx context.Context, tokenID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("control-plane metadata store is nil")
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return fmt.Errorf("token id is required")
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE tokens
		 SET status = ?, revoked_at = ?, updated_at = ?
		 WHERE token_id = ? AND status = ?`,
		TokenStatusRevoked,
		formatTime(now),
		formatTime(now),
		tokenID,
		TokenStatusActive,
	)
	if err != nil {
		return fmt.Errorf("revoke token %q: %w", tokenID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke token %q: %w", tokenID, err)
	}
	if affected == 0 {
		return ErrTokenInvalid
	}
	return nil
}

func (s *Store) CompleteEnrollment(ctx context.Context, enrollPlaintext string, boot Bootstrap) (*EnrollmentResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("control-plane metadata store is nil")
	}
	boot = normalizeBootstrap(boot)
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin enrollment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	enrollRecord, err := authenticateToken(ctx, tx, AuthenticateTokenRequest{
		Plaintext:      enrollPlaintext,
		AllowedTypes:   []string{TokenTypeEnroll},
		RequiredScopes: []string{ScopeEnroll},
	}, now)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE tokens
		 SET status = ?, used_at = ?, updated_at = ?
		 WHERE token_id = ? AND status = ?`,
		TokenStatusUsed,
		formatTime(now),
		formatTime(now),
		enrollRecord.ID,
		TokenStatusActive,
	)
	if err != nil {
		return nil, fmt.Errorf("mark enroll token used: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("mark enroll token used: %w", err)
	}
	if affected != 1 {
		return nil, ErrTokenUsed
	}
	assignedBoot, err := ensureLocalTx(ctx, tx, boot, now)
	if err != nil {
		return nil, err
	}
	snapshot, err := snapshotFromQueryer(ctx, s.path, tx)
	if err != nil {
		return nil, err
	}
	nodeID, collectorID, sourceIDs, err := assignmentForEnrollment(snapshot, assignedBoot)
	if err != nil {
		return nil, err
	}
	ingest, err := insertToken(ctx, tx, CreateTokenRequest{
		Type:        TokenTypeIngest,
		Scopes:      []string{ScopeIngest},
		NodeID:      nodeID,
		CollectorID: collectorID,
		SourceIDs:   sourceIDs,
	}, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit enrollment transaction: %w", err)
	}
	return &EnrollmentResult{Snapshot: snapshot, IngestToken: *ingest}, nil
}

func (s *Store) CompleteRemoteEnrollment(ctx context.Context, enrollPlaintext, existingIngestPlaintext string, boot Bootstrap) (*EnrollmentResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("control-plane metadata store is nil")
	}
	boot = normalizeBootstrap(boot)
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin remote enrollment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	enrollRecord, err := authenticateToken(ctx, tx, AuthenticateTokenRequest{
		Plaintext:      enrollPlaintext,
		AllowedTypes:   []string{TokenTypeEnroll},
		RequiredScopes: []string{ScopeEnroll},
	}, now)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE tokens
		 SET status = ?, used_at = ?, updated_at = ?
		 WHERE token_id = ? AND status = ?`,
		TokenStatusUsed,
		formatTime(now),
		formatTime(now),
		enrollRecord.ID,
		TokenStatusActive,
	)
	if err != nil {
		return nil, fmt.Errorf("mark enroll token used: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("mark enroll token used: %w", err)
	}
	if affected != 1 {
		return nil, ErrTokenUsed
	}
	assignedBoot, existingCollector, err := ensureRemoteRegistrationTx(ctx, tx, boot, now)
	if err != nil {
		return nil, err
	}
	if existingCollector {
		if _, err := authenticateToken(ctx, tx, AuthenticateTokenRequest{
			Plaintext:        existingIngestPlaintext,
			AllowedTypes:     []string{TokenTypeIngest},
			RequiredScopes:   []string{ScopeIngest},
			NodeID:           assignedBoot.NodeID,
			CollectorID:      assignedBoot.CollectorID,
			SkipBindingCheck: true,
		}, now); err != nil {
			return nil, err
		}
	}
	snapshot, err := snapshotFromQueryer(ctx, s.path, tx)
	if err != nil {
		return nil, err
	}
	nodeID, collectorID, sourceIDs, err := assignmentForEnrollment(snapshot, assignedBoot)
	if err != nil {
		return nil, err
	}
	if existingCollector {
		if err := revokeActiveIngestTokensForCollector(ctx, tx, collectorID, now); err != nil {
			return nil, err
		}
	}
	ingest, err := insertToken(ctx, tx, CreateTokenRequest{
		Type:        TokenTypeIngest,
		Scopes:      []string{ScopeIngest},
		NodeID:      nodeID,
		CollectorID: collectorID,
		SourceIDs:   sourceIDs,
	}, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit remote enrollment transaction: %w", err)
	}
	return &EnrollmentResult{Snapshot: snapshot, IngestToken: *ingest}, nil
}

func (s *Store) HasActiveOwnerToken(ctx context.Context) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("control-plane metadata store is nil")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT scopes, expires_at FROM tokens
		 WHERE status = ? AND token_type IN (?, ?)
		 ORDER BY created_at DESC`,
		TokenStatusActive,
		TokenTypeOwner,
		TokenTypeAdmin,
	)
	if err != nil {
		return false, fmt.Errorf("read owner tokens: %w", err)
	}
	defer rows.Close()

	now := time.Now().UTC()
	for rows.Next() {
		var scopesJSON string
		var expires sql.NullString
		if err := rows.Scan(&scopesJSON, &expires); err != nil {
			return false, fmt.Errorf("scan owner token: %w", err)
		}
		scopes, err := decodeStringList(scopesJSON)
		if err != nil {
			return false, fmt.Errorf("decode owner token scopes: %w", err)
		}
		if !containsString(scopes, ScopeRead) {
			continue
		}
		if !expires.Valid {
			return true, nil
		}
		expiresAt := parseTime(expires.String)
		if !expiresAt.IsZero() && now.Before(expiresAt) {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read owner token rows: %w", err)
	}
	return false, nil
}

func insertToken(ctx context.Context, tx *sql.Tx, req CreateTokenRequest, now time.Time) (*CreatedToken, error) {
	req = normalizeCreateTokenRequest(req)
	if err := validateCreateTokenRequest(req); err != nil {
		return nil, err
	}
	tokenID := generatedID("token")
	secret, err := generatedSecretHex(32)
	if err != nil {
		return nil, err
	}
	plain := fmt.Sprintf("bcn_%s_%s_%s", req.Type, tokenID, secret)
	record := TokenRecord{
		ID:          tokenID,
		Type:        req.Type,
		Status:      TokenStatusActive,
		Prefix:      tokenPrefix(plain),
		Scopes:      req.Scopes,
		NodeID:      req.NodeID,
		CollectorID: req.CollectorID,
		SourceIDs:   req.SourceIDs,
		ExpiresAt:   cloneTimePtr(req.ExpiresAt),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	scopesJSON, err := encodeStringList(record.Scopes)
	if err != nil {
		return nil, err
	}
	sourceIDsJSON, err := encodeStringList(record.SourceIDs)
	if err != nil {
		return nil, err
	}
	var expiresAt any
	if record.ExpiresAt != nil {
		expiresAt = formatTime(*record.ExpiresAt)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tokens (
			token_id, token_type, token_hash, token_prefix, status, scopes,
			node_id, collector_id, source_ids, expires_at, used_at, revoked_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
		record.ID,
		record.Type,
		tokenHash(plain),
		record.Prefix,
		record.Status,
		scopesJSON,
		nullableString(record.NodeID),
		nullableString(record.CollectorID),
		sourceIDsJSON,
		expiresAt,
		formatTime(record.CreatedAt),
		formatTime(record.UpdatedAt),
	); err != nil {
		return nil, fmt.Errorf("insert token %q: %w", record.ID, err)
	}
	return &CreatedToken{Record: record, Plaintext: plain}, nil
}

func revokeActiveIngestTokensForCollector(ctx context.Context, tx *sql.Tx, collectorID string, now time.Time) error {
	collectorID = strings.TrimSpace(collectorID)
	if collectorID == "" {
		return fmt.Errorf("collector id is required")
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tokens
		 SET status = ?, revoked_at = ?, updated_at = ?
		 WHERE token_type = ? AND collector_id = ? AND status = ?`,
		TokenStatusRevoked,
		formatTime(now),
		formatTime(now),
		TokenTypeIngest,
		collectorID,
		TokenStatusActive,
	); err != nil {
		return fmt.Errorf("revoke active ingest tokens for collector %q: %w", collectorID, err)
	}
	return nil
}

func authenticateToken(ctx context.Context, q tokenQueryer, req AuthenticateTokenRequest, now time.Time) (*TokenRecord, error) {
	plain := strings.TrimSpace(req.Plaintext)
	if plain == "" {
		return nil, ErrTokenInvalid
	}
	rows, err := q.QueryContext(ctx,
		`SELECT token_id, token_type, token_hash, token_prefix, status, scopes,
		        COALESCE(node_id, ''), COALESCE(collector_id, ''), source_ids,
		        expires_at, used_at, revoked_at, created_at, updated_at
		 FROM tokens WHERE token_prefix = ?`,
		tokenPrefix(plain),
	)
	if err != nil {
		return nil, fmt.Errorf("read token candidates: %w", err)
	}
	defer rows.Close()

	wantHash := tokenHash(plain)
	for rows.Next() {
		record, storedHash, err := scanTokenRecord(rows)
		if err != nil {
			return nil, err
		}
		if subtle.ConstantTimeCompare([]byte(storedHash), []byte(wantHash)) != 1 {
			continue
		}
		if err := validateAuthenticatedToken(record, req, now); err != nil {
			return nil, err
		}
		return &record, nil
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read token candidate rows: %w", err)
	}
	return nil, ErrTokenInvalid
}

func validateAuthenticatedToken(record TokenRecord, req AuthenticateTokenRequest, now time.Time) error {
	switch record.Status {
	case TokenStatusActive:
	case TokenStatusUsed:
		return ErrTokenUsed
	case TokenStatusRevoked:
		return ErrTokenRevoked
	default:
		return ErrTokenInvalid
	}
	if record.ExpiresAt != nil && !now.Before(*record.ExpiresAt) {
		return ErrTokenExpired
	}
	if len(req.AllowedTypes) > 0 && !containsString(req.AllowedTypes, record.Type) {
		return ErrTokenInvalid
	}
	for _, scope := range req.RequiredScopes {
		if !containsString(record.Scopes, scope) {
			return ErrTokenScopeDenied
		}
	}
	if !req.SkipBindingCheck && tokenAuthRequiresIngestBindings(record, req) &&
		(req.NodeID == "" || req.CollectorID == "" || (req.SourceID == "" && len(req.SourceIDs) == 0) ||
			record.NodeID == "" || record.CollectorID == "" || len(record.SourceIDs) == 0) {
		return ErrTokenBindingMismatch
	}
	if req.NodeID != "" && req.NodeID != record.NodeID {
		return ErrTokenBindingMismatch
	}
	if req.CollectorID != "" && req.CollectorID != record.CollectorID {
		return ErrTokenBindingMismatch
	}
	if req.SourceID != "" && !containsString(record.SourceIDs, req.SourceID) {
		return ErrTokenBindingMismatch
	}
	for _, sourceID := range normalizeStringList(req.SourceIDs) {
		if !containsString(record.SourceIDs, sourceID) {
			return ErrTokenBindingMismatch
		}
	}
	return nil
}

func tokenAuthRequiresIngestBindings(record TokenRecord, req AuthenticateTokenRequest) bool {
	return record.Type == TokenTypeIngest ||
		containsString(req.AllowedTypes, TokenTypeIngest) ||
		containsString(req.RequiredScopes, ScopeIngest)
}

func scanTokenRecord(rows *sql.Rows) (TokenRecord, string, error) {
	var record TokenRecord
	var hash string
	var scopesJSON string
	var sourceIDsJSON string
	var expiresAt, usedAt, revokedAt sql.NullString
	var createdAt, updatedAt string
	if err := rows.Scan(
		&record.ID,
		&record.Type,
		&hash,
		&record.Prefix,
		&record.Status,
		&scopesJSON,
		&record.NodeID,
		&record.CollectorID,
		&sourceIDsJSON,
		&expiresAt,
		&usedAt,
		&revokedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return TokenRecord{}, "", fmt.Errorf("scan token: %w", err)
	}
	var err error
	record.Scopes, err = decodeStringList(scopesJSON)
	if err != nil {
		return TokenRecord{}, "", fmt.Errorf("decode token scopes: %w", err)
	}
	record.SourceIDs, err = decodeStringList(sourceIDsJSON)
	if err != nil {
		return TokenRecord{}, "", fmt.Errorf("decode token source bindings: %w", err)
	}
	record.ExpiresAt = parseOptionalTime(expiresAt)
	record.UsedAt = parseOptionalTime(usedAt)
	record.RevokedAt = parseOptionalTime(revokedAt)
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, hash, nil
}

func normalizeCreateTokenRequest(req CreateTokenRequest) CreateTokenRequest {
	req.Type = strings.TrimSpace(req.Type)
	req.NodeID = strings.TrimSpace(req.NodeID)
	req.CollectorID = strings.TrimSpace(req.CollectorID)
	req.Scopes = normalizeStringList(req.Scopes)
	req.SourceIDs = normalizeStringList(req.SourceIDs)
	if len(req.Scopes) == 0 {
		req.Scopes = defaultScopesForTokenType(req.Type)
	}
	if req.ExpiresAt != nil {
		expires := req.ExpiresAt.UTC()
		req.ExpiresAt = &expires
	}
	return req
}

func validateCreateTokenRequest(req CreateTokenRequest) error {
	switch req.Type {
	case TokenTypeOwner, TokenTypeEnroll, TokenTypeIngest, TokenTypeRead, TokenTypeAdmin:
	default:
		return fmt.Errorf("token type %q is unsupported", req.Type)
	}
	if len(req.Scopes) == 0 {
		return fmt.Errorf("token scopes are required")
	}
	if req.Type == TokenTypeEnroll && containsString(req.Scopes, ScopeIngest) {
		return fmt.Errorf("enrollment tokens cannot carry ingest scope")
	}
	if req.Type == TokenTypeIngest {
		if req.NodeID == "" {
			return fmt.Errorf("ingest token requires node binding")
		}
		if req.CollectorID == "" {
			return fmt.Errorf("ingest token requires collector binding")
		}
		if len(req.SourceIDs) == 0 {
			return fmt.Errorf("ingest token requires at least one source binding")
		}
	}
	return nil
}

func defaultScopesForTokenType(tokenType string) []string {
	switch tokenType {
	case TokenTypeOwner:
		return []string{ScopeOwner, ScopeAdmin, ScopeRead}
	case TokenTypeEnroll:
		return []string{ScopeEnroll}
	case TokenTypeIngest:
		return []string{ScopeIngest}
	case TokenTypeRead:
		return []string{ScopeRead}
	case TokenTypeAdmin:
		return []string{ScopeAdmin, ScopeRead}
	default:
		return nil
	}
}

func assignmentForEnrollment(snapshot *Snapshot, boot Bootstrap) (string, string, []string, error) {
	if snapshot == nil {
		return "", "", nil, fmt.Errorf("enrollment snapshot is nil")
	}
	boot = normalizeBootstrap(boot)
	collectorID := firstNonEmpty(boot.CollectorID, snapshot.LocalCollectorID)
	if collectorID == "" && len(snapshot.Collectors) == 1 {
		collectorID = snapshot.Collectors[0].ID
	}
	var collector Collector
	for _, candidate := range snapshot.Collectors {
		if candidate.ID == collectorID {
			collector = candidate
			break
		}
	}
	if collector.ID == "" {
		return "", "", nil, fmt.Errorf("enrollment collector assignment is ambiguous")
	}
	nodeID := firstNonEmpty(boot.NodeID, snapshot.LocalNodeID, collector.NodeID)
	if nodeID != collector.NodeID {
		return "", "", nil, fmt.Errorf("collector %q is bound to node %q, not %q", collector.ID, collector.NodeID, nodeID)
	}
	var sourceIDs []string
	for _, source := range snapshot.Sources {
		if source.CollectorID == collector.ID {
			sourceIDs = append(sourceIDs, source.ID)
		}
	}
	if len(sourceIDs) == 0 {
		return "", "", nil, fmt.Errorf("enrollment collector %q has no source assignments", collector.ID)
	}
	sort.Strings(sourceIDs)
	return nodeID, collector.ID, sourceIDs, nil
}

func generatedSecretHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func tokenHash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func tokenPrefix(plain string) string {
	plain = strings.TrimSpace(plain)
	if len(plain) <= tokenPrefixLength {
		return plain
	}
	return plain[:tokenPrefixLength]
}

func encodeStringList(values []string) (string, error) {
	data, err := json.Marshal(normalizeStringList(values))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeStringList(value string) ([]string, error) {
	var values []string
	if err := json.Unmarshal([]byte(value), &values); err != nil {
		return nil, err
	}
	return normalizeStringList(values), nil
}

func normalizeStringList(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	t := parseTime(value.String)
	if t.IsZero() {
		return nil
	}
	return &t
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	t := value.UTC()
	return &t
}
