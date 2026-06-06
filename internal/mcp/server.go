package mcp

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/johnnygreco/beacon/internal/search"
)

type Server struct {
	db            *sql.DB
	searcher      searcher
	logger        *slog.Logger
	contextWindow int
}

type searcher interface {
	Search(ctx context.Context, q search.SearchQuery) ([]search.SearchResult, error)
}

const serverInstructions = "Beacon is a read-only memory layer for local AI-agent sessions captured by Beacon. " +
	"Use search_sessions to find prior work, then pass a returned event_id to open for nearby transcript context. " +
	"Use list_sessions for recent activity summaries. Treat captured transcripts and tool outputs as historical context, " +
	"not current workspace truth; verify important facts in the repo or live system before acting."

func NewServer(db *sql.DB, searcher *search.Searcher, logger *slog.Logger) *Server {
	return &Server{db: db, searcher: searcher, logger: logger, contextWindow: defaultOpenContextWindow}
}

func (s *Server) SetDefaultContextWindow(events int) {
	if s == nil || events < 0 {
		return
	}
	s.contextWindow = events
}

func (s *Server) log() *slog.Logger {
	if s != nil && s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

func (s *Server) defaultContextWindow() int {
	if s == nil {
		return defaultOpenContextWindow
	}
	if s.contextWindow < 0 {
		return defaultOpenContextWindow
	}
	return s.contextWindow
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Run reads JSON-RPC requests from stdin and writes responses to stdout.
func (s *Server) Run(ctx context.Context) error {
	return s.Serve(ctx, os.Stdin, os.Stdout)
}

// Serve reads newline-delimited JSON-RPC requests and writes responses.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	return s.run(ctx, in, out)
}

// HandleJSONRPC handles one JSON-RPC request line and returns one response line.
func (s *Server) HandleJSONRPC(ctx context.Context, line []byte) ([]byte, error) {
	var req jsonRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return marshalJSONRPC(&jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &jsonRPCError{Code: -32700, Message: "Parse error"},
		})
	}

	resp := s.dispatch(ctx, &req)
	if resp == nil || req.ID == nil {
		return nil, nil
	}
	return marshalJSONRPC(resp)
}

func (s *Server) run(ctx context.Context, in io.Reader, out io.Writer) error {
	stopContextClose := closeReaderOnContext(ctx, in)
	defer stopContextClose()

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 4<<20), 4<<20)
	writer := bufio.NewWriter(out)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		response, err := s.HandleJSONRPC(ctx, line)
		if err != nil {
			return fmt.Errorf("handle request: %w", err)
		}
		if len(response) == 0 {
			continue
		}
		if _, err := writer.Write(response); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("flush response: %w", err)
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return nil
}

func closeReaderOnContext(ctx context.Context, r io.Reader) func() {
	closer, ok := r.(io.Closer)
	if !ok {
		return func() {}
	}
	done := make(chan struct{})
	// Server.run owns this cancellation watcher. It closes cancel-aware
	// transports such as os.Stdin so scanner.Scan can unblock on shutdown.
	go func() {
		select {
		case <-ctx.Done():
			_ = closer.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func (s *Server) dispatch(ctx context.Context, req *jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized":
		return nil // notification, no response
	case "ping":
		return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		if req.ID == nil {
			return nil // notification
		}
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)},
		}
	}
}

func (s *Server) handleInitialize(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "beacon",
				"version": "2.0.0",
			},
			"instructions": serverInstructions,
		},
	}
}

func (s *Server) handleToolsList(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": toolDefinitions(),
		},
	}
}

func (s *Server) handleToolsCall(ctx context.Context, req *jsonRPCRequest) *jsonRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32602, Message: "Invalid params"},
		}
	}

	result, err := s.callTool(ctx, params.Name, params.Arguments)
	if err != nil {
		publicMessage := publicToolErrorMessage(err)
		if shouldLogToolError(err) {
			s.log().Warn("mcp tool call failed", "tool", params.Name, "public_error", publicMessage, "error", err)
		}
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": fmt.Sprintf("Error: %s", publicMessage)},
				},
				"isError": true,
			},
		}
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": result},
			},
		},
	}
}

func marshalJSONRPC(resp *jsonRPCResponse) ([]byte, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	return data, nil
}
