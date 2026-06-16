package beaconcli

import (
	"slices"
	"testing"

	"github.com/spf13/pflag"
)

func TestMCPCommandExposesLocalFlagsOnly(t *testing.T) {
	cmd := newMCPCmd()
	var got []string
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		got = append(got, flag.Name)
	})
	want := []string{"clickhouse"}
	if !slices.Equal(got, want) {
		t.Fatalf("mcp flags = %v, want %v", got, want)
	}
}
