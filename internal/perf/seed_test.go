package perf

import "testing"

func TestProfileForFleetMatchesIssueShape(t *testing.T) {
	profile := ProfileFor(SizeFleet)
	if !profile.Heavy {
		t.Fatal("fleet profile should be marked heavy")
	}
	if profile.Collectors != 25 || profile.Runtimes != 5 || profile.Sessions != 100000 {
		t.Fatalf("fleet profile dimensions = collectors %d runtimes %d sessions %d, want 25/5/100000", profile.Collectors, profile.Runtimes, profile.Sessions)
	}
	if profile.ActiveSessions != 2500 || profile.IdleSessions != 2500 {
		t.Fatalf("fleet active/idle = %d/%d, want 2500/2500", profile.ActiveSessions, profile.IdleSessions)
	}
	if profile.TargetEvents < 14000000 || profile.TargetEvents > 16000000 {
		t.Fatalf("fleet target events = %d, want approximately 15M", profile.TargetEvents)
	}
	if profile.TargetSearchPostings < 90000000 {
		t.Fatalf("fleet target search postings = %d, want approximately 100M", profile.TargetSearchPostings)
	}
	if profile.TargetPayloads < 900000 || profile.TargetPayloads > 1100000 {
		t.Fatalf("fleet target payloads = %d, want approximately 1M", profile.TargetPayloads)
	}
	if profile.CommonSearchToken == "" || profile.ScopedCollectorID == "" || profile.ScopedSourceID == "" || profile.ScopedProjectKey == "" {
		t.Fatalf("fleet scoped search metadata incomplete: %#v", profile)
	}
}

func TestParseSeedSizeAcceptsFleetProfile(t *testing.T) {
	if got := ParseSeedSize(" FLEET "); got != SizeFleet {
		t.Fatalf("ParseSeedSize fleet = %q, want %q", got, SizeFleet)
	}
	if got := ParseSeedSize("unknown"); got != SizeSmall {
		t.Fatalf("ParseSeedSize unknown = %q, want %q", got, SizeSmall)
	}
}

func TestSeedSourceForSessionFansOutAcrossCollectorsAndRuntimes(t *testing.T) {
	cfg := configFor(SizeFleet)
	seenSources := map[string]struct{}{}
	seenCollectors := map[int]struct{}{}
	seenRuntimes := map[string]struct{}{}
	for session := 0; session < cfg.collectorCount*len(seedRuntimeProfiles); session++ {
		source := seedSourceForSession(session, cfg)
		seenSources[source.sourceID] = struct{}{}
		seenCollectors[source.collectorIndex] = struct{}{}
		seenRuntimes[source.profile.Runtime] = struct{}{}
	}
	if len(seenSources) != cfg.collectorCount*len(seedRuntimeProfiles) {
		t.Fatalf("sources = %d, want %d", len(seenSources), cfg.collectorCount*len(seedRuntimeProfiles))
	}
	if len(seenCollectors) != cfg.collectorCount {
		t.Fatalf("collectors = %d, want %d", len(seenCollectors), cfg.collectorCount)
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

func TestFleetToolPayloadsAreDownsampled(t *testing.T) {
	fleet := configFor(SizeFleet)
	if !shouldSeedToolPayload(seedConfig{}, 0, 1) {
		t.Fatal("default profile should keep tool payloads")
	}
	kept := 0
	for event := 0; event < 900; event++ {
		if shouldSeedToolPayload(fleet, 0, event) {
			kept++
		}
	}
	if kept < 95 || kept > 105 {
		t.Fatalf("fleet kept %d/900 payload slots, want approximately 100", kept)
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
