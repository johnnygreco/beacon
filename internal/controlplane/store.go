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
	Path            string
	OwnerInstanceID string
	SchemaEpoch     string
	Nodes           []Node
	Collectors      []Collector
	Sources         []Source
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
	defer tx.Rollback()

	if _, err := ensureMetadataValue(ctx, tx, "owner_instance_id", generatedID("owner"), now); err != nil {
		return nil, err
	}
	if _, err := ensureMetadataValue(ctx, tx, "schema_epoch", InitialSchemaEpoch, now); err != nil {
		return nil, err
	}
	if boot.NodeID == "" {
		boot.NodeID, err = ensureMetadataValue(ctx, tx, "local_node_id", generatedID("node"), now)
		if err != nil {
			return nil, err
		}
	}
	if boot.CollectorID == "" {
		boot.CollectorID, err = ensureMetadataValue(ctx, tx, "local_collector_id", generatedID("collector"), now)
		if err != nil {
			return nil, err
		}
	}
	if err := upsertNode(ctx, tx, boot, now); err != nil {
		return nil, err
	}
	if err := upsertCollector(ctx, tx, boot, now); err != nil {
		return nil, err
	}
	for _, source := range boot.Sources {
		if err := upsertSource(ctx, tx, boot.CollectorID, source, now); err != nil {
			return nil, err
		}
	}
	if err := reconcileSources(ctx, tx, boot.CollectorID, boot.Sources); err != nil {
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

func (s *Store) Snapshot(ctx context.Context) (*Snapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("control-plane metadata store is nil")
	}
	snap := &Snapshot{Path: s.path}
	metadata, err := readMetadata(ctx, s.db)
	if err != nil {
		return nil, err
	}
	snap.OwnerInstanceID = metadata["owner_instance_id"]
	snap.SchemaEpoch = metadata["schema_epoch"]
	if snap.Nodes, err = readNodes(ctx, s.db); err != nil {
		return nil, err
	}
	if snap.Collectors, err = readCollectors(ctx, s.db); err != nil {
		return nil, err
	}
	if snap.Sources, err = readSources(ctx, s.db); err != nil {
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
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create control-plane metadata directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("secure control-plane metadata directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("control-plane metadata path %q must not be a symlink", path)
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

func restrictMetadataFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(candidate, 0600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("secure control-plane metadata file %q: %w", candidate, err)
		}
	}
	return nil
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

func readMetadata(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM metadata`)
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

func readNodes(ctx context.Context, db *sql.DB) ([]Node, error) {
	rows, err := db.QueryContext(ctx,
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

func readCollectors(ctx context.Context, db *sql.DB) ([]Collector, error) {
	rows, err := db.QueryContext(ctx,
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

func readSources(ctx context.Context, db *sql.DB) ([]Source, error) {
	rows, err := db.QueryContext(ctx,
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
