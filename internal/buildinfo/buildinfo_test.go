package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestVersionUsesLinkerOverride(t *testing.T) {
	oldVersion := version
	version = "1.2.3-test"
	t.Cleanup(func() {
		version = oldVersion
	})

	if got := Version(); got != "1.2.3-test" {
		t.Fatalf("Version() = %q, want %q", got, "1.2.3-test")
	}
}

func TestVersionDefaultsToDevForLocalBuild(t *testing.T) {
	oldVersion := version
	version = "dev"
	t.Cleanup(func() {
		version = oldVersion
	})

	if got := Version(); got != "dev" {
		t.Fatalf("Version() = %q, want %q", got, "dev")
	}
}

func TestVersionFromBuildInfoUsesModuleVersion(t *testing.T) {
	info := &debug.BuildInfo{}
	info.Main.Version = "v1.2.3"

	if got := versionFromBuildInfo(info); got != "v1.2.3" {
		t.Fatalf("versionFromBuildInfo() = %q, want %q", got, "v1.2.3")
	}
}

func TestVersionFromBuildInfoIgnoresLocalVCSVersion(t *testing.T) {
	info := &debug.BuildInfo{}
	info.Main.Version = "v1.2.3+dirty"
	info.Settings = []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}

	if got := versionFromBuildInfo(info); got != "" {
		t.Fatalf("versionFromBuildInfo() = %q, want empty local VCS fallback", got)
	}
}
