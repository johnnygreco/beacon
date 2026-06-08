package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const InitialSchemaEpoch = "1"

type Store struct {
	db   *sql.DB
	path string
}

type Bootstrap struct {
	NodeID        string
	NodeName      string
	CollectorID   string
	CollectorName string
	Sources       []SourceRegistration
}

type SourceRegistration struct {
	Name      string
	Runtime   string
	Provider  string
	Format    string
	WatchRoot string
}

type Snapshot struct {
	Path              string
	OwnerInstanceID   string
	SchemaEpoch       string
	ResetPending      bool
	ResetPendingEpoch string
	ResetPendingAt    *time.Time
	LocalNodeID       string
	LocalCollectorID  string
	Nodes             []Node
	Collectors        []Collector
	Sources           []Source
}

type Node struct {
	ID          string
	DisplayName string
	Hostname    string
	Platform    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Collector struct {
	ID          string
	NodeID      string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Source struct {
	ID          string
	CollectorID string
	Name        string
	Runtime     string
	Provider    string
	Format      string
	WatchRoot   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("control-plane metadata path is required")
	}
	if isSQLiteSpecialPath(path) {
		return nil, fmt.Errorf("control-plane metadata path must be a durable filesystem path, not a SQLite DSN")
	}
	if err := prepareMetadataFile(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open control-plane metadata: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	if err := s.configure(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := restrictMetadataFiles(path); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func Exists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) EnsureLocal(ctx context.Context, boot Bootstrap) (*Snapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("control-plane metadata store is nil")
	}
	boot = normalizeBootstrap(boot)
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin control-plane metadata transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := ensureLocalTx(ctx, tx, boot, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit control-plane metadata: %w", err)
	}
	if err := restrictMetadataFiles(s.path); err != nil {
		return nil, err
	}
	return s.Snapshot(ctx)
}

func (s *Store) EnsureControlPlane(ctx context.Context) (*Snapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("control-plane metadata store is nil")
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin control-plane metadata transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := ensureMetadataValue(ctx, tx, "owner_instance_id", generatedID("owner"), now); err != nil {
		return nil, err
	}
	if _, err := ensureMetadataValue(ctx, tx, "schema_epoch", InitialSchemaEpoch, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit control-plane metadata: %w", err)
	}
	if err := restrictMetadataFiles(s.path); err != nil {
		return nil, err
	}
	return s.Snapshot(ctx)
}

func (s *Store) BeginReset(ctx context.Context) (*Snapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("control-plane metadata store is nil")
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin control-plane metadata transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := ensureMetadataValue(ctx, tx, "owner_instance_id", generatedID("owner"), now); err != nil {
		return nil, err
	}
	epoch, err := ensureMetadataValue(ctx, tx, "schema_epoch", InitialSchemaEpoch, now)
	if err != nil {
		return nil, err
	}
	metadata, err := readMetadata(ctx, tx)
	if err != nil {
		return nil, err
	}
	if metadata["reset_pending"] != "true" {
		if err := setMetadataValue(ctx, tx, "reset_pending", "true", now); err != nil {
			return nil, err
		}
		if err := setMetadataValue(ctx, tx, "reset_pending_epoch", epoch, now); err != nil {
			return nil, err
		}
		if err := setMetadataValue(ctx, tx, "reset_pending_at", formatTime(now), now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reset-pending metadata: %w", err)
	}
	return s.Snapshot(ctx)
}

func (s *Store) CompleteReset(ctx context.Context) (*Snapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("control-plane metadata store is nil")
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin control-plane metadata transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := ensureMetadataValue(ctx, tx, "owner_instance_id", generatedID("owner"), now); err != nil {
		return nil, err
	}
	epoch, err := ensureMetadataValue(ctx, tx, "schema_epoch", InitialSchemaEpoch, now)
	if err != nil {
		return nil, err
	}
	metadata, err := readMetadata(ctx, tx)
	if err != nil {
		return nil, err
	}
	if metadata["reset_pending"] == "true" {
		pendingEpoch := metadata["reset_pending_epoch"]
		if pendingEpoch == "" || pendingEpoch == epoch {
			next, err := incrementSchemaEpoch(epoch)
			if err != nil {
				return nil, err
			}
			if err := setMetadataValue(ctx, tx, "schema_epoch", next, now); err != nil {
				return nil, err
			}
		}
		if err := setMetadataValue(ctx, tx, "reset_pending", "false", now); err != nil {
			return nil, err
		}
		if err := setMetadataValue(ctx, tx, "reset_pending_epoch", "", now); err != nil {
			return nil, err
		}
		if err := setMetadataValue(ctx, tx, "reset_pending_at", "", now); err != nil {
			return nil, err
		}
		if err := setMetadataValue(ctx, tx, "reset_completed_at", formatTime(now), now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reset completion metadata: %w", err)
	}
	return s.Snapshot(ctx)
}

func (s *Store) SetSchemaEpoch(ctx context.Context, epoch string) (*Snapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("control-plane metadata store is nil")
	}
	epoch = strings.TrimSpace(epoch)
	if epoch == "" {
		return nil, fmt.Errorf("schema_epoch is required")
	}
	if _, err := parseSchemaEpoch(epoch); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin schema epoch metadata transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := ensureMetadataValue(ctx, tx, "owner_instance_id", generatedID("owner"), now); err != nil {
		return nil, err
	}
	if err := setMetadataValue(ctx, tx, "schema_epoch", epoch, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit schema epoch metadata: %w", err)
	}
	return s.Snapshot(ctx)
}

func ensureLocalTx(ctx context.Context, tx *sql.Tx, boot Bootstrap, now time.Time) (Bootstrap, error) {
	if _, err := ensureMetadataValue(ctx, tx, "owner_instance_id", generatedID("owner"), now); err != nil {
		return Bootstrap{}, err
	}
	if _, err := ensureMetadataValue(ctx, tx, "schema_epoch", InitialSchemaEpoch, now); err != nil {
		return Bootstrap{}, err
	}
	nodeID, err := ensureLocalMetadataValue(ctx, tx, "local_node_id", boot.NodeID, generatedID("node"), now)
	if err != nil {
		return Bootstrap{}, err
	}
	boot.NodeID = nodeID
	collectorID, err := ensureLocalMetadataValue(ctx, tx, "local_collector_id", boot.CollectorID, generatedID("collector"), now)
	if err != nil {
		return Bootstrap{}, err
	}
	boot.CollectorID = collectorID
	if err := upsertNode(ctx, tx, boot, now); err != nil {
		return Bootstrap{}, err
	}
	if err := upsertCollector(ctx, tx, boot, now); err != nil {
		return Bootstrap{}, err
	}
	for _, source := range boot.Sources {
		if err := upsertSource(ctx, tx, boot.CollectorID, source, now); err != nil {
			return Bootstrap{}, err
		}
	}
	if err := reconcileSources(ctx, tx, boot.CollectorID, boot.Sources); err != nil {
		return Bootstrap{}, err
	}
	return boot, nil
}

func ensureRemoteRegistrationTx(ctx context.Context, tx *sql.Tx, boot Bootstrap, now time.Time) (Bootstrap, bool, error) {
	if _, err := ensureMetadataValue(ctx, tx, "owner_instance_id", generatedID("owner"), now); err != nil {
		return Bootstrap{}, false, err
	}
	if _, err := ensureMetadataValue(ctx, tx, "schema_epoch", InitialSchemaEpoch, now); err != nil {
		return Bootstrap{}, false, err
	}
	if err := validateRemoteBootstrap(boot); err != nil {
		return Bootstrap{}, false, err
	}
	existing, err := existingRemoteCollector(ctx, tx, boot)
	if err != nil {
		return Bootstrap{}, false, err
	}
	if !existing {
		boot.NodeID = generatedID("node")
		boot.CollectorID = generatedID("collector")
	}
	if err := upsertNode(ctx, tx, boot, now); err != nil {
		return Bootstrap{}, false, err
	}
	if err := upsertCollector(ctx, tx, boot, now); err != nil {
		return Bootstrap{}, false, err
	}
	for _, source := range boot.Sources {
		if err := upsertSource(ctx, tx, boot.CollectorID, source, now); err != nil {
			return Bootstrap{}, false, err
		}
	}
	if err := reconcileSources(ctx, tx, boot.CollectorID, boot.Sources); err != nil {
		return Bootstrap{}, false, err
	}
	return boot, existing, nil
}

func validateRemoteBootstrap(boot Bootstrap) error {
	if len(boot.Sources) == 0 {
		return fmt.Errorf("%w: at least one source is required", ErrEnrollmentInvalid)
	}
	for i, source := range boot.Sources {
		prefix := fmt.Sprintf("source %d", i+1)
		if source.Name == "" {
			return fmt.Errorf("%w: %s name is required", ErrEnrollmentInvalid, prefix)
		}
		if source.Runtime == "" {
			return fmt.Errorf("%w: %s runtime is required", ErrEnrollmentInvalid, prefix)
		}
		if source.Provider == "" {
			return fmt.Errorf("%w: %s provider is required", ErrEnrollmentInvalid, prefix)
		}
		if source.Format == "" {
			return fmt.Errorf("%w: %s format is required", ErrEnrollmentInvalid, prefix)
		}
		if source.WatchRoot == "" {
			return fmt.Errorf("%w: %s watch_root is required", ErrEnrollmentInvalid, prefix)
		}
	}
	return nil
}

func existingRemoteCollector(ctx context.Context, tx *sql.Tx, boot Bootstrap) (bool, error) {
	if boot.NodeID == "" || boot.CollectorID == "" {
		return false, nil
	}
	var nodeID string
	err := tx.QueryRowContext(ctx,
		`SELECT node_id FROM collectors WHERE collector_id = ?`,
		boot.CollectorID,
	).Scan(&nodeID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read remote collector %q: %w", boot.CollectorID, err)
	case nodeID != boot.NodeID:
		return false, fmt.Errorf("%w: collector %q is bound to node %q, not %q", ErrEnrollmentInvalid, boot.CollectorID, nodeID, boot.NodeID)
	default:
		return true, nil
	}
}

func (s *Store) Snapshot(ctx context.Context) (*Snapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("control-plane metadata store is nil")
	}
	return snapshotFromQueryer(ctx, s.path, s.db)
}

func snapshotFromQueryer(ctx context.Context, path string, q tokenQueryer) (*Snapshot, error) {
	snap := &Snapshot{Path: path}
	metadata, err := readMetadata(ctx, q)
	if err != nil {
		return nil, err
	}
	snap.OwnerInstanceID = metadata["owner_instance_id"]
	snap.SchemaEpoch = metadata["schema_epoch"]
	snap.ResetPending = metadata["reset_pending"] == "true"
	snap.ResetPendingEpoch = metadata["reset_pending_epoch"]
	if resetPendingAt := parseTime(metadata["reset_pending_at"]); !resetPendingAt.IsZero() {
		snap.ResetPendingAt = &resetPendingAt
	}
	snap.LocalNodeID = metadata["local_node_id"]
	snap.LocalCollectorID = metadata["local_collector_id"]
	if snap.Nodes, err = readNodes(ctx, q); err != nil {
		return nil, err
	}
	if snap.Collectors, err = readCollectors(ctx, q); err != nil {
		return nil, err
	}
	if snap.Sources, err = readSources(ctx, q); err != nil {
		return nil, err
	}
	return snap, nil
}

func (s *Store) configure(ctx context.Context) error {
	for _, stmt := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("configure control-plane metadata sqlite: %w", err)
		}
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS nodes (
			node_id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			hostname TEXT NOT NULL,
			platform TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS collectors (
			collector_id TEXT PRIMARY KEY,
			node_id TEXT NOT NULL,
			display_name TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(node_id) REFERENCES nodes(node_id)
		)`,
		`CREATE TABLE IF NOT EXISTS sources (
			source_id TEXT PRIMARY KEY,
			collector_id TEXT NOT NULL,
			name TEXT NOT NULL,
			runtime TEXT NOT NULL,
			provider TEXT NOT NULL,
			format TEXT NOT NULL,
			watch_root TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(collector_id) REFERENCES collectors(collector_id)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_sources_collector_name ON sources(collector_id, name)`,
		`CREATE TABLE IF NOT EXISTS tokens (
			token_id TEXT PRIMARY KEY,
			token_type TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			token_prefix TEXT NOT NULL,
			status TEXT NOT NULL,
			scopes TEXT NOT NULL,
			node_id TEXT,
			collector_id TEXT,
			source_ids TEXT NOT NULL,
			expires_at TEXT,
			used_at TEXT,
			revoked_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tokens_prefix ON tokens(token_prefix)`,
		`CREATE INDEX IF NOT EXISTS idx_tokens_status_type ON tokens(status, token_type)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate control-plane metadata: %w", err)
		}
	}
	return nil
}

func normalizeBootstrap(boot Bootstrap) Bootstrap {
	hostname := localHostname()
	boot.NodeName = firstNonEmpty(boot.NodeName, hostname, "local")
	boot.NodeID = strings.TrimSpace(boot.NodeID)
	boot.CollectorName = firstNonEmpty(boot.CollectorName, boot.NodeName)
	boot.CollectorID = strings.TrimSpace(boot.CollectorID)
	for i := range boot.Sources {
		boot.Sources[i].Name = strings.TrimSpace(boot.Sources[i].Name)
		boot.Sources[i].Runtime = strings.TrimSpace(boot.Sources[i].Runtime)
		boot.Sources[i].Provider = strings.TrimSpace(boot.Sources[i].Provider)
		boot.Sources[i].Format = strings.TrimSpace(boot.Sources[i].Format)
		boot.Sources[i].WatchRoot = strings.TrimSpace(boot.Sources[i].WatchRoot)
	}
	return boot
}

func prepareMetadataFile(path string) error {
	dir := filepath.Dir(path)
	dirExisted := true
	if info, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		dirExisted = false
	} else if err != nil {
		return fmt.Errorf("stat control-plane metadata directory: %w", err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("control-plane metadata directory %q must not be a symlink", dir)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create control-plane metadata directory: %w", err)
	}
	if shouldSecureMetadataDir(dir, dirExisted) {
		if err := os.Chmod(dir, 0700); err != nil {
			return fmt.Errorf("secure control-plane metadata directory: %w", err)
		}
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("control-plane metadata file %q must not be a symlink", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("control-plane metadata file %q must be a regular file", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect control-plane metadata file %q: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("create control-plane metadata file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close control-plane metadata file: %w", err)
	}
	return restrictMetadataFiles(path)
}

func shouldSecureMetadataDir(dir string, dirExisted bool) bool {
	return !dirExisted || filepath.Base(dir) == ".beacon"
}

func restrictMetadataFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect control-plane metadata file %q: %w", candidate, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("control-plane metadata file %q must not be a symlink", candidate)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("control-plane metadata file %q must be a regular file", candidate)
		}
		if err := os.Chmod(candidate, 0600); err != nil {
			return fmt.Errorf("secure control-plane metadata file %q: %w", candidate, err)
		}
	}
	return nil
}

func isSQLiteSpecialPath(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	return lower == ":memory:" || strings.HasPrefix(lower, "file:") || strings.Contains(lower, "?")
}

func ensureMetadataValue(ctx context.Context, tx *sql.Tx, key, value string, now time.Time) (string, error) {
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO metadata (key, value, updated_at) VALUES (?, ?, ?)`,
		key,
		value,
		formatTime(now),
	); err != nil {
		return "", fmt.Errorf("ensure metadata %q: %w", key, err)
	}
	var stored string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key = ?`, key).Scan(&stored); err != nil {
		return "", fmt.Errorf("read metadata %q: %w", key, err)
	}
	return stored, nil
}

func setMetadataValue(ctx context.Context, tx *sql.Tx, key, value string, now time.Time) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO metadata (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key,
		value,
		formatTime(now),
	); err != nil {
		return fmt.Errorf("set metadata %q: %w", key, err)
	}
	return nil
}

func ensureLocalMetadataValue(ctx context.Context, tx *sql.Tx, key, configuredValue, generatedValue string, now time.Time) (string, error) {
	value := configuredValue
	if value == "" {
		value = generatedValue
	}
	stored, err := ensureMetadataValue(ctx, tx, key, value, now)
	if err != nil {
		return "", err
	}
	if configuredValue != "" && stored != configuredValue {
		return "", fmt.Errorf("configured %s %q does not match existing metadata value %q", key, configuredValue, stored)
	}
	return stored, nil
}

func incrementSchemaEpoch(epoch string) (string, error) {
	epoch = strings.TrimSpace(epoch)
	if epoch == "" {
		epoch = InitialSchemaEpoch
	}
	value, err := parseSchemaEpoch(epoch)
	if err != nil {
		return "", err
	}
	return strconv.FormatUint(value+1, 10), nil
}

func parseSchemaEpoch(epoch string) (uint64, error) {
	value, err := strconv.ParseUint(epoch, 10, 64)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("schema_epoch %q is not a positive integer", epoch)
	}
	return value, nil
}

func upsertNode(ctx context.Context, tx *sql.Tx, boot Bootstrap, now time.Time) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO nodes (node_id, display_name, hostname, platform, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(node_id) DO UPDATE SET
		   display_name = excluded.display_name,
		   hostname = excluded.hostname,
		   platform = excluded.platform,
		   updated_at = excluded.updated_at`,
		boot.NodeID,
		boot.NodeName,
		localHostname(),
		runtime.GOOS+"/"+runtime.GOARCH,
		formatTime(now),
		formatTime(now),
	); err != nil {
		return fmt.Errorf("upsert node %q: %w", boot.NodeID, err)
	}
	return nil
}

func upsertCollector(ctx context.Context, tx *sql.Tx, boot Bootstrap, now time.Time) error {
	var existingNodeID string
	err := tx.QueryRowContext(ctx,
		`SELECT node_id FROM collectors WHERE collector_id = ?`,
		boot.CollectorID,
	).Scan(&existingNodeID)
	switch {
	case err == nil && existingNodeID != boot.NodeID:
		return fmt.Errorf("collector_id %q is already bound to node_id %q, not %q", boot.CollectorID, existingNodeID, boot.NodeID)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("read collector %q: %w", boot.CollectorID, err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO collectors (collector_id, node_id, display_name, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(collector_id) DO UPDATE SET
		   display_name = excluded.display_name,
		   updated_at = excluded.updated_at`,
		boot.CollectorID,
		boot.NodeID,
		boot.CollectorName,
		formatTime(now),
		formatTime(now),
	); err != nil {
		return fmt.Errorf("upsert collector %q: %w", boot.CollectorID, err)
	}
	return nil
}

func upsertSource(ctx context.Context, tx *sql.Tx, collectorID string, source SourceRegistration, now time.Time) error {
	if source.Name == "" {
		return fmt.Errorf("source name is required")
	}
	sourceID := stableID("source", collectorID, source.Name)
	var existingCollectorID, existingName string
	err := tx.QueryRowContext(ctx,
		`SELECT collector_id, name FROM sources WHERE source_id = ?`,
		sourceID,
	).Scan(&existingCollectorID, &existingName)
	switch {
	case err == nil && (existingCollectorID != collectorID || existingName != source.Name):
		return fmt.Errorf("source_id %q is already bound to collector/name %q/%q, not %q/%q",
			sourceID,
			existingCollectorID,
			existingName,
			collectorID,
			source.Name,
		)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("read source %q: %w", sourceID, err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sources (source_id, collector_id, name, runtime, provider, format, watch_root, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(source_id) DO UPDATE SET
		   runtime = excluded.runtime,
		   provider = excluded.provider,
		   format = excluded.format,
		   watch_root = excluded.watch_root,
		   updated_at = excluded.updated_at`,
		sourceID,
		collectorID,
		source.Name,
		source.Runtime,
		source.Provider,
		source.Format,
		source.WatchRoot,
		formatTime(now),
		formatTime(now),
	); err != nil {
		return fmt.Errorf("upsert source %q: %w", source.Name, err)
	}
	return nil
}

func reconcileSources(ctx context.Context, tx *sql.Tx, collectorID string, sources []SourceRegistration) error {
	if len(sources) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sources WHERE collector_id = ?`, collectorID); err != nil {
			return fmt.Errorf("reconcile sources for collector %q: %w", collectorID, err)
		}
		return nil
	}
	ids := make([]string, 0, len(sources))
	args := make([]any, 0, len(sources)+1)
	args = append(args, collectorID)
	for _, source := range sources {
		ids = append(ids, "?")
		args = append(args, stableID("source", collectorID, source.Name))
	}
	query := fmt.Sprintf(`DELETE FROM sources WHERE collector_id = ? AND source_id NOT IN (%s)`, strings.Join(ids, ","))
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("reconcile sources for collector %q: %w", collectorID, err)
	}
	return nil
}

func readMetadata(ctx context.Context, q tokenQueryer) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT key, value FROM metadata`)
	if err != nil {
		return nil, fmt.Errorf("read control-plane metadata: %w", err)
	}
	defer rows.Close()

	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan control-plane metadata: %w", err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read control-plane metadata rows: %w", err)
	}
	return values, nil
}

func readNodes(ctx context.Context, q tokenQueryer) ([]Node, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT node_id, display_name, hostname, platform, created_at, updated_at
		 FROM nodes ORDER BY node_id`)
	if err != nil {
		return nil, fmt.Errorf("read nodes: %w", err)
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var node Node
		var created, updated string
		if err := rows.Scan(&node.ID, &node.DisplayName, &node.Hostname, &node.Platform, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		node.CreatedAt = parseTime(created)
		node.UpdatedAt = parseTime(updated)
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read node rows: %w", err)
	}
	return nodes, nil
}

func readCollectors(ctx context.Context, q tokenQueryer) ([]Collector, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT collector_id, node_id, display_name, created_at, updated_at
		 FROM collectors ORDER BY collector_id`)
	if err != nil {
		return nil, fmt.Errorf("read collectors: %w", err)
	}
	defer rows.Close()

	var collectors []Collector
	for rows.Next() {
		var collector Collector
		var created, updated string
		if err := rows.Scan(&collector.ID, &collector.NodeID, &collector.DisplayName, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan collector: %w", err)
		}
		collector.CreatedAt = parseTime(created)
		collector.UpdatedAt = parseTime(updated)
		collectors = append(collectors, collector)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read collector rows: %w", err)
	}
	return collectors, nil
}

func readSources(ctx context.Context, q tokenQueryer) ([]Source, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT source_id, collector_id, name, runtime, provider, format, watch_root, created_at, updated_at
		 FROM sources ORDER BY collector_id, name`)
	if err != nil {
		return nil, fmt.Errorf("read sources: %w", err)
	}
	defer rows.Close()

	var sources []Source
	for rows.Next() {
		var source Source
		var created, updated string
		if err := rows.Scan(
			&source.ID,
			&source.CollectorID,
			&source.Name,
			&source.Runtime,
			&source.Provider,
			&source.Format,
			&source.WatchRoot,
			&created,
			&updated,
		); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		source.CreatedAt = parseTime(created)
		source.UpdatedAt = parseTime(updated)
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read source rows: %w", err)
	}
	return sources, nil
}

func stableID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(strings.TrimSpace(part)))
		h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return prefix + "_" + hex.EncodeToString(sum[:12])
}

func generatedID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return stableID(prefix, time.Now().UTC().Format(time.RFC3339Nano))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func localHostname() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "unknown"
	}
	return strings.TrimSpace(name)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}
