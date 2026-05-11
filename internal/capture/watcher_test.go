package capture

import (
	"path/filepath"
	"testing"
)

func TestWatcherFindSourceExpandsHomeGlobs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	w := &Watcher{
		sources: []WatchSource{
			{
				Name:  "hermes",
				Globs: []string{"~/.hermes/state.db"},
			},
		},
	}

	file := filepath.Join(home, ".hermes", "state.db")
	src := w.findSource(file)
	if src == nil {
		t.Fatalf("findSource(%q) = nil, want hermes source", file)
	} else if src.Name != "hermes" {
		t.Fatalf("findSource(%q).Name = %q, want hermes", file, src.Name)
	}
}
