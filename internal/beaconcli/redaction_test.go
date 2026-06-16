package beaconcli

import (
	"strings"
	"testing"

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
