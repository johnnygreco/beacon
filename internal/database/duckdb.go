package database

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/duckdb/duckdb-go/v2"
)

// DB holds the DuckDB connections: a shared pool and a dedicated write connection.
type DB struct {
	ReadPool  *sql.DB
	writeConn *sql.Conn
	path      string
}

// Open creates a new DB with a shared connection pool.
// A dedicated write connection is pinned from the pool for serialized writes.
func Open(path string, readPoolSize int) (*DB, error) {
	dsn := path
	if path == "" {
		dsn = ":memory:"
	}

	// Single connection pool shared by readers and writer.
	// DuckDB handles concurrent access within a single instance.
	pool, err := sql.Open("duckdb", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	pool.SetMaxOpenConns(readPoolSize + 1) // +1 for the dedicated write connection

	// Pin a dedicated write connection from the pool
	writeConn, err := pool.Conn(context.Background())
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("acquire write conn: %w", err)
	}

	db := &DB{
		ReadPool:  pool,
		writeConn: writeConn,
		path:      path,
	}

	if err := db.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

// WriteConn returns the dedicated write connection. All writes must go through the batcher.
func (db *DB) WriteConn() *sql.Conn {
	return db.writeConn
}

// Close closes all connections.
func (db *DB) Close() error {
	if db.writeConn != nil {
		db.writeConn.Close()
	}
	if db.ReadPool != nil {
		db.ReadPool.Close()
	}
	return nil
}
