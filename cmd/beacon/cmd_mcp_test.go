package main

import (
	"strings"
	"testing"
)

func TestNormalizeRemoteMCPEndpoint(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "root https appends mcp path",
			raw:  "https://beacon.example",
			want: "https://beacon.example/api/mcp",
		},
		{
			name: "explicit path is preserved",
			raw:  "https://beacon.example/custom/mcp",
			want: "https://beacon.example/custom/mcp",
		},
		{
			name: "loopback http is allowed for development",
			raw:  "http://127.0.0.1:4600",
			want: "http://127.0.0.1:4600/api/mcp",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeRemoteMCPEndpoint(tt.raw)
			if err != nil {
				t.Fatalf("normalizeRemoteMCPEndpoint: %v", err)
			}
			if got != tt.want {
				t.Fatalf("endpoint = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeRemoteMCPEndpointRejectsNonLoopbackHTTP(t *testing.T) {
	_, err := normalizeRemoteMCPEndpoint("http://beacon.example")
	if err == nil || !strings.Contains(err.Error(), "https for non-loopback") {
		t.Fatalf("non-loopback http error = %v", err)
	}
}

func TestNormalizeRemoteMCPEndpointRejectsMalformedAuthority(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "userinfo", raw: "https://user:pass@beacon.example/custom/mcp", wantErr: "remote MCP URL must not include userinfo"},
		{name: "empty host with port", raw: "https://:443/custom/mcp", wantErr: "remote MCP URL host is required"},
		{name: "empty explicit port", raw: "https://beacon.example:/custom/mcp", wantErr: "remote MCP URL port must be numeric"},
		{name: "zero port", raw: "https://beacon.example:0/custom/mcp", wantErr: "remote MCP URL port must be between 1 and 65535"},
		{name: "out of range port", raw: "https://beacon.example:99999/custom/mcp", wantErr: "remote MCP URL port must be between 1 and 65535"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeRemoteMCPEndpoint(tt.raw)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("normalizeRemoteMCPEndpoint error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
