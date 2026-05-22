package store

import (
	"crypto/tls"
	"testing"
	"time"
)

func TestNormalizeOptionsFillsDefaultsAndPreservesExplicitSettings(t *testing.T) {
	opts := normalizeOptions(Options{
		Password:    "secret",
		Secure:      true,
		ReadTimeout: 7 * time.Second,
	})
	defaults := DefaultOptions()

	if len(opts.Addrs) != len(defaults.Addrs) || opts.Addrs[0] != defaults.Addrs[0] {
		t.Fatalf("Addrs = %v, want %v", opts.Addrs, defaults.Addrs)
	}
	if opts.Database != defaults.Database {
		t.Fatalf("Database = %q, want %q", opts.Database, defaults.Database)
	}
	if opts.Username != defaults.Username {
		t.Fatalf("Username = %q, want %q", opts.Username, defaults.Username)
	}
	if opts.ReadPoolSize != defaults.ReadPoolSize {
		t.Fatalf("ReadPoolSize = %d, want %d", opts.ReadPoolSize, defaults.ReadPoolSize)
	}
	if opts.DialTimeout != defaults.DialTimeout {
		t.Fatalf("DialTimeout = %s, want %s", opts.DialTimeout, defaults.DialTimeout)
	}
	if opts.ReadTimeout != 7*time.Second {
		t.Fatalf("ReadTimeout = %s, want explicit 7s", opts.ReadTimeout)
	}
	if opts.Password != "secret" || !opts.Secure {
		t.Fatalf("explicit password/secure not preserved: %#v", opts)
	}
}

func TestClickHouseOptionsMapStoreOptions(t *testing.T) {
	opts := Options{
		Addrs:        []string{"clickhouse.example:9440"},
		Database:     "beacon",
		Username:     "writer",
		Password:     "secret",
		Secure:       true,
		ReadPoolSize: 11,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  9 * time.Second,
	}

	chOpts := clickhouseOptions(opts, "beacon_test")
	if len(chOpts.Addr) != 1 || chOpts.Addr[0] != "clickhouse.example:9440" {
		t.Fatalf("Addr = %v", chOpts.Addr)
	}
	if chOpts.Auth.Database != "beacon_test" || chOpts.Auth.Username != opts.Username || chOpts.Auth.Password != opts.Password {
		t.Fatalf("Auth = %#v", chOpts.Auth)
	}
	if chOpts.MaxOpenConns != opts.ReadPoolSize || chOpts.MaxIdleConns != opts.ReadPoolSize {
		t.Fatalf("pool = open %d idle %d, want %d", chOpts.MaxOpenConns, chOpts.MaxIdleConns, opts.ReadPoolSize)
	}
	if chOpts.DialTimeout != opts.DialTimeout || chOpts.ReadTimeout != opts.ReadTimeout {
		t.Fatalf("timeouts = %s/%s, want %s/%s", chOpts.DialTimeout, chOpts.ReadTimeout, opts.DialTimeout, opts.ReadTimeout)
	}
	if chOpts.TLS == nil || chOpts.TLS.MinVersion != tls.VersionTLS12 {
		t.Fatalf("TLS = %#v, want TLS 1.2 minimum", chOpts.TLS)
	}
	if chOpts.Settings["max_execution_time"] != 60 {
		t.Fatalf("max_execution_time = %#v, want 60", chOpts.Settings["max_execution_time"])
	}
}
