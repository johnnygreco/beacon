package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/store"
)

type Backend struct {
	DB       *sql.DB
	Searcher searcher
}

type BackendProvider interface {
	Backend(ctx context.Context) (Backend, error)
}

type staticBackendProvider struct {
	backend Backend
}

func (p staticBackendProvider) Backend(context.Context) (Backend, error) {
	return p.backend, nil
}

type ClickHouseBackend struct {
	opts       store.Options
	logger     *slog.Logger
	maxResults int
	open       func(context.Context, store.Options) (*store.Store, error)

	mu       sync.Mutex
	store    *store.Store
	searcher *search.Searcher
}

func NewClickHouseBackend(opts store.Options, logger *slog.Logger, maxResults int) *ClickHouseBackend {
	return &ClickHouseBackend{
		opts:       opts,
		logger:     logger,
		maxResults: maxResults,
		open:       store.OpenReadOnly,
	}
}

func (b *ClickHouseBackend) Backend(ctx context.Context) (Backend, error) {
	if b == nil {
		return Backend{}, internalToolError("Beacon database backend is not configured", fmt.Errorf("missing backend provider"))
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.store != nil {
		return Backend{DB: b.store.DB, Searcher: b.searcher}, nil
	}

	open := b.open
	if open == nil {
		open = store.OpenReadOnly
	}
	ch, err := open(ctx, b.opts)
	if err != nil {
		return Backend{}, databaseUnavailableError(b.opts, err)
	}

	maxResults := b.maxResults
	if maxResults <= 0 {
		maxResults = 25
	}
	searcher := search.NewSearcher(ch.DB, b.logger, maxResults, 0)
	searcher.ProbeIndex()

	b.store = ch
	b.searcher = searcher
	return Backend{DB: ch.DB, Searcher: searcher}, nil
}

func (b *ClickHouseBackend) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.store == nil {
		return nil
	}
	err := b.store.Close()
	b.store = nil
	b.searcher = nil
	return err
}

func databaseUnavailableError(opts store.Options, err error) error {
	return internalToolError(databaseUnavailableMessage(opts), err)
}

func databaseUnavailableMessage(opts store.Options) string {
	defaults := store.DefaultOptions()
	addrs := opts.Addrs
	if len(addrs) == 0 {
		addrs = defaults.Addrs
	}
	database := opts.Database
	if database == "" {
		database = defaults.Database
	}
	return fmt.Sprintf("Beacon database is not available at %s (database %s). Start Beacon with `beacon up`, or configure `beacon mcp --clickhouse <host:port>` for a reachable ClickHouse instance.", strings.Join(addrs, ","), database)
}
