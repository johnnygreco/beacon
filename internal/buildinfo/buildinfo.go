package buildinfo

import "runtime/debug"

var version = "dev"

func Version() string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if buildVersion := versionFromBuildInfo(info); buildVersion != "" {
			return buildVersion
		}
	}
	return version
}

func versionFromBuildInfo(info *debug.BuildInfo) string {
	if info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs" || setting.Key == "vcs.revision" || setting.Key == "vcs.time" || setting.Key == "vcs.modified" {
			return ""
		}
	}
	return info.Main.Version
}
