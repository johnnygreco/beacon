package beaconcli

import "testing"

func withConfigFile(t *testing.T, path string) {
	t.Helper()
	oldCfgFile := cfgFile
	cfgFile = path
	t.Cleanup(func() {
		cfgFile = oldCfgFile
	})
}
