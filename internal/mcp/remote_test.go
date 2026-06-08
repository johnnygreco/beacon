package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteProxyForwardsJSONRPCWithBearerToken(t *testing.T) {
	var gotAuth string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer server.Close()

	proxy := NewRemoteProxy(server.URL, "read-token")
	var out strings.Builder
	if err := proxy.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if gotAuth != "Bearer read-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"method":"ping"`) {
		t.Fatalf("body = %q", gotBody)
	}
	if out.String() != `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`+"\n" {
		t.Fatalf("proxy output = %q", out.String())
	}
}

func TestRemoteProxySkipsNotifications(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	proxy := NewRemoteProxy(server.URL, "read-token")
	var out strings.Builder
	if err := proxy.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","method":"initialized"}`+"\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if out.String() != "" {
		t.Fatalf("notification output = %q", out.String())
	}
}
