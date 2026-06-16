package redaction

import (
	"strings"
	"testing"
)

func TestPolicyRedactsCommonCredentials(t *testing.T) {
	input := strings.Join([]string{
		`{"api_key":"sk-test-fixture-that-is-long-enough"}`,
		"Authorization: Bearer abcdefghijklmnop",
		"aws=AKIA1234567890ABCDEF",
		"gh=ghp_abcdefghijklmnopqrstuvwxyz123456",
		"pem=-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----",
		"url=https://user:pass@example.test/path",
	}, "\n")

	got := DefaultPolicy().Redact(input)
	for _, leaked := range []string{
		"sk-test-fixture-that-is-long-enough",
		"abcdefghijklmnop",
		"AKIA1234567890ABCDEF",
		"ghp_abcdefghijklmnopqrstuvwxyz123456",
		"-----BEGIN PRIVATE KEY-----",
		"user:pass",
	} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted output leaked %q: %s", leaked, got)
		}
	}
	for _, marker := range []string{SecretMarker, PrivateKeyMarker, CredentialMarker} {
		if !strings.Contains(got, marker) {
			t.Fatalf("redacted output missing marker %q: %s", marker, got)
		}
	}
}

func TestPolicyRedactsConfiguredPathEnvAndLiteralMasks(t *testing.T) {
	t.Setenv("BEACON_TEST_REDACT_ME", "env-secret-fixture-value")
	policy := NewPolicy(Config{
		PathMasks:    []string{"/Users/example/private"},
		EnvMasks:     []string{"BEACON_TEST_REDACT_ME"},
		LiteralMasks: []string{"literal-secret-fixture"},
	})

	got := policy.Redact("cd /Users/example/private/project && echo env-secret-fixture-value literal-secret-fixture")
	for _, leaked := range []string{"/Users/example/private", "env-secret-fixture-value", "literal-secret-fixture"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted output leaked %q: %s", leaked, got)
		}
	}
	for _, marker := range []string{PathMarker, EnvMarker, LiteralMarker} {
		if !strings.Contains(got, marker) {
			t.Fatalf("redacted output missing marker %q: %s", marker, got)
		}
	}
}

func TestPolicyDoesNotClaimArbitrarySecretDetection(t *testing.T) {
	got := DefaultPolicy().Redact("the launch phrase is regular words only")
	if got != "the launch phrase is regular words only" {
		t.Fatalf("unexpected arbitrary text redaction: %q", got)
	}
}
