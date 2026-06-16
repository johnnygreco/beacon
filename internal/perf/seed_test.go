package perf

import "testing"

func TestProfileForStressMatchesIssueShape(t *testing.T) {
	profile := ProfileFor(SizeStress)
	if !profile.Heavy {
		t.Fatal("stress profile should be marked heavy")
	}
	if profile.Sources != 5 || profile.Runtimes != 5 || profile.Sessions != 100000 {
		t.Fatalf("stress profile dimensions = sources %d runtimes %d sessions %d, want 5/5/100000", profile.Sources, profile.Runtimes, profile.Sessions)
	}
	if profile.ActiveSessions != 2500 || profile.IdleSessions != 2500 {
		t.Fatalf("stress active/idle = %d/%d, want 2500/2500", profile.ActiveSessions, profile.IdleSessions)
	}
	if profile.TargetEvents < 14000000 || profile.TargetEvents > 16000000 {
		t.Fatalf("stress target events = %d, want approximately 15M", profile.TargetEvents)
	}
	if profile.TargetSearchPostings < 90000000 {
		t.Fatalf("stress target search postings = %d, want approximately 100M", profile.TargetSearchPostings)
	}
	if profile.TargetPayloads < 900000 || profile.TargetPayloads > 1100000 {
		t.Fatalf("stress target payloads = %d, want approximately 1M", profile.TargetPayloads)
	}
	if profile.CommonSearchToken == "" || profile.ScopedSourceName == "" || profile.ScopedProjectKey == "" {
		t.Fatalf("stress scoped search metadata incomplete: %#v", profile)
	}
}

func TestParseSeedSizeAcceptsStressProfile(t *testing.T) {
	if got := ParseSeedSize(" STRESS "); got != SizeStress {
		t.Fatalf("ParseSeedSize stress = %q, want %q", got, SizeStress)
	}
	if got := ParseSeedSize("unknown"); got != SizeSmall {
		t.Fatalf("ParseSeedSize unknown = %q, want %q", got, SizeSmall)
	}
}

func TestSeedSourceForSessionFansOutAcrossRuntimes(t *testing.T) {
	cfg := configFor(SizeStress)
	seenSources := map[string]struct{}{}
	seenRuntimes := map[string]struct{}{}
	for session := 0; session < len(seedRuntimeProfiles); session++ {
		source := seedSourceForSession(session, cfg)
		seenSources[source.profile.SourceName] = struct{}{}
		seenRuntimes[source.profile.Runtime] = struct{}{}
	}
	if len(seenSources) != len(seedRuntimeProfiles) {
		t.Fatalf("sources = %d, want %d", len(seenSources), len(seedRuntimeProfiles))
	}
	if len(seenRuntimes) != len(seedRuntimeProfiles) {
		t.Fatalf("runtimes = %d, want %d", len(seenRuntimes), len(seedRuntimeProfiles))
	}
}

func TestSeedSessionRangesReserveActiveAndIdleAtEnd(t *testing.T) {
	cfg := configFor(SizeSmall)
	if got, want := activeSeedStart(cfg), cfg.sessions-cfg.activeSessions; got != want {
		t.Fatalf("activeSeedStart = %d, want %d", got, want)
	}
	if got, want := idleSeedStart(cfg), cfg.sessions-cfg.activeSessions-cfg.idleSessions; got != want {
		t.Fatalf("idleSeedStart = %d, want %d", got, want)
	}
	if isActiveSeedSession(idleSeedStart(cfg), cfg) {
		t.Fatal("idle start should not be active")
	}
	if !isIdleSeedSession(idleSeedStart(cfg), cfg) {
		t.Fatal("idle start should be idle")
	}
	if !isActiveSeedSession(activeSeedStart(cfg), cfg) {
		t.Fatal("active start should be active")
	}
}

func TestStressToolPayloadsAreDownsampled(t *testing.T) {
	stress := configFor(SizeStress)
	if !shouldSeedToolPayload(seedConfig{}, 0, 1) {
		t.Fatal("default profile should keep tool payloads")
	}
	kept := 0
	for event := 0; event < 900; event++ {
		if shouldSeedToolPayload(stress, 0, event) {
			kept++
		}
	}
	if kept < 95 || kept > 105 {
		t.Fatalf("stress kept %d/900 payload slots, want approximately 100", kept)
	}
}

func TestSeedRuntimeProfilesUseSupportedFormatPairs(t *testing.T) {
	want := map[string]string{
		"claude-code":     "jsonl",
		"codex":           "jsonl",
		"hermes-agent":    "sqlite",
		"opencode":        "sqlite",
		"pi-coding-agent": "jsonl",
	}
	for _, profile := range seedRuntimeProfiles {
		if got := profile.Format; got != want[profile.Runtime] {
			t.Fatalf("runtime %s format = %s, want %s", profile.Runtime, got, want[profile.Runtime])
		}
		if got := profile.Extension; got != want[profile.Runtime] {
			t.Fatalf("runtime %s extension = %s, want %s", profile.Runtime, got, want[profile.Runtime])
		}
	}
}
