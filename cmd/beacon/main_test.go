package main

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestRootCommandShowsHelpWithoutSubcommand(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Available Commands:") {
		t.Fatalf("expected help output, got %q", out.String())
	}
}

func TestRootCommandExposesCanonicalSubcommands(t *testing.T) {
	cmd := newRootCmd()
	want := []string{"up", "down", "watch", "mcp", "status", "db"}

	var got []string
	for _, sub := range cmd.Commands() {
		if sub.Hidden {
			continue
		}
		got = append(got, sub.Name())
	}

	for _, name := range want {
		if !slices.Contains(got, name) {
			t.Fatalf("missing canonical command %q; got %v", name, got)
		}
	}
	for _, removed := range []string{"serve", "stop", "run"} {
		if slices.Contains(got, removed) {
			t.Fatalf("removed duplicate command %q is still exposed; got %v", removed, got)
		}
	}
}

func TestRemovedDuplicateCommandsReturnErrors(t *testing.T) {
	tests := [][]string{
		{"serve"},
		{"stop"},
		{"run"},
		{"run", "web"},
		{"run", "capture"},
		{"run", "mcp"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(args)

			if err := cmd.Execute(); err == nil {
				t.Fatalf("expected %q to be unavailable", strings.Join(args, " "))
			}
		})
	}
}
