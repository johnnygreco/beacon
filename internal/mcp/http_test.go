package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/search"
)

func TestHTTPHandlerServesScopedMCPToolCall(t *testing.T) {
	fake := &fakeMCPSearcher{
		results: []search.SearchResult{{
			EventUID:    "evt-http",
			SessionID:   "session-http",
			SourceName:  "source-a",
			EventKind:   "message",
			TextPreview: "http result",
			Timestamp:   time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
		}},
	}
	srv := testServer()
	srv.searcher = fake
	handler := srv.HTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_sessions","arguments":{"query":"http"}}}`))
	req = req.WithContext(ContextWithAuthScope(req.Context(), ScopeFilters{SourceNames: []string{"source-a"}}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "auth_scope_applied") || !strings.Contains(rec.Body.String(), "source-a") {
		t.Fatalf("response missing scoped metadata: %s", rec.Body.String())
	}
	if strings.Join(fake.query.SourceNames, ",") != "source-a" {
		t.Fatalf("search query source scope = %#v", fake.query.SourceNames)
	}
}
