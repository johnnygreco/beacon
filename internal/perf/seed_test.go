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
