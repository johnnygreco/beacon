package views

import (
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
)

var (
	assetVersionOnce sync.Once
	assetVersion     string
)

// AssetURL appends a build-derived cache key to static asset URLs.
func AssetURL(path string) string {
	version := AssetVersion()
	if strings.TrimSpace(path) == "" || version == "" {
		return path
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + "v=" + url.QueryEscape(version)
}

func AssetVersion() string {
	assetVersionOnce.Do(func() {
		assetVersion = buildAssetVersion()
	})
	return assetVersion
}

func buildAssetVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && strings.TrimSpace(setting.Value) != "" {
			return setting.Value
		}
	}
	if strings.TrimSpace(info.Main.Version) != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
