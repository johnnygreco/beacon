package main

import (
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/redaction"
)

func TestRedactStringsAppliesConfiguredPolicy(t *testing.T) {
	policy := redaction.NewPolicy(redaction.Config{
		PathMasks:    []string{"/Users/example/private"},
		LiteralMasks: []string{"literal-fixture-secret"},
	})

	got := strings.Join(redactStrings(policy, []string{
		"/Users/example/private/**/*.jsonl",
		"literal-fixture-secret",
	}), "\n")
	for _, leaked := range []string{"/Users/example/private", "literal-fixture-secret"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted strings leaked %q: %s", leaked, got)
		}
	}
	for _, marker := range []string{redaction.PathMarker, redaction.LiteralMarker} {
		if !strings.Contains(got, marker) {
			t.Fatalf("redacted strings missing marker %q: %s", marker, got)
		}
	}
}

func TestCollectorStartupLogAttrsApplyConfiguredPolicy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Fleet.ControlPlaneURL = "https://user:literal-fixture-secret@beacon.example/private"
	cfg.Fleet.SpoolDir = "/Users/example/private/spool"
	policy := redaction.NewPolicy(redaction.Config{
		PathMasks:    []string{"/Users/example/private"},
		LiteralMasks: []string{"literal-fixture-secret"},
	})

	attrs := collectorStartupLogAttrs(policy, cfg)
	got := strings.Join([]string{attrs[1].(string), attrs[3].(string)}, "\n")
	for _, leaked := range []string{"/Users/example/private", "literal-fixture-secret"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("collector startup attrs leaked %q: %s", leaked, got)
		}
	}
	for _, marker := range []string{redaction.CredentialMarker, redaction.PathMarker} {
		if !strings.Contains(got, marker) {
			t.Fatalf("collector startup attrs missing marker %q: %s", marker, got)
		}
	}
}
