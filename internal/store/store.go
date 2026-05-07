package store

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const DefaultDatabase = "beacon"

type Options struct {
	Addrs        []string
	Database     string
	Username     string
	Password     string
	Secure       bool
	ReadPoolSize int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
}

type Store struct {
	DB       *sql.DB
	native   driver.Conn
	database string
}

func DefaultOptions() Options {
	return Options{
		Addrs:        []string{"127.0.0.1:9000"},
		Database:     DefaultDatabase,
		Username:     "default",
		ReadPoolSize: 8,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  30 * time.Second,
	}
}

func Open(ctx context.Context, opts Options) (*Store, error) {
	opts = normalizeOptions(opts)

	adminDB := clickhouse.OpenDB(clickhouseOptions(opts, "default"))
	defer adminDB.Close()
	if err := adminDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("connect clickhouse: %w", err)
	}
	if err := Migrate(ctx, adminDB, opts.Database); err != nil {
		return nil, fmt.Errorf("migrate clickhouse: %w", err)
	}

	db := clickhouse.OpenDB(clickhouseOptions(opts, opts.Database))
	db.SetMaxOpenConns(opts.ReadPoolSize)
	db.SetMaxIdleConns(opts.ReadPoolSize)
	db.SetConnMaxLifetime(time.Hour)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect beacon database: %w", err)
	}

	native, err := clickhouse.Open(clickhouseOptions(opts, opts.Database))
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open native clickhouse client: %w", err)
	}
	if err := native.Ping(ctx); err != nil {
		db.Close()
		native.Close()
		return nil, fmt.Errorf("ping native clickhouse client: %w", err)
	}

	return &Store{DB: db, native: native, database: opts.Database}, nil
}

func (s *Store) Close() error {
	var err error
	if s.native != nil {
		err = s.native.Close()
	}
	if s.DB != nil {
		if dbErr := s.DB.Close(); err == nil {
			err = dbErr
		}
	}
	return err
}

func (s *Store) Native() driver.Conn {
	return s.native
}

func (s *Store) Database() string {
	return s.database
}

func normalizeOptions(opts Options) Options {
	defaults := DefaultOptions()
	if len(opts.Addrs) == 0 {
		opts.Addrs = defaults.Addrs
	}
	if opts.Database == "" {
		opts.Database = defaults.Database
	}
	if opts.Username == "" {
		opts.Username = defaults.Username
	}
	if opts.ReadPoolSize <= 0 {
		opts.ReadPoolSize = defaults.ReadPoolSize
	}
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = defaults.DialTimeout
	}
	if opts.ReadTimeout <= 0 {
		opts.ReadTimeout = defaults.ReadTimeout
	}
	return opts
}

func clickhouseOptions(opts Options, database string) *clickhouse.Options {
	chOpts := &clickhouse.Options{
		Addr: opts.Addrs,
		Auth: clickhouse.Auth{
			Database: database,
			Username: opts.Username,
			Password: opts.Password,
		},
		Compression:  &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
		DialTimeout:  opts.DialTimeout,
		ReadTimeout:  opts.ReadTimeout,
		MaxOpenConns: opts.ReadPoolSize,
		MaxIdleConns: opts.ReadPoolSize,
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
	}
	if opts.Secure {
		chOpts.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return chOpts
}
