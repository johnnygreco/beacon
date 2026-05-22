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
	db       *sql.DB
	searcher searcher
	logger   *slog.Logger
}

type searcher interface {
	Search(ctx context.Context, q search.SearchQuery) ([]search.SearchResult, error)
}

func NewServer(db *sql.DB, searcher *search.Searcher, logger *slog.Logger) *Server {
	return &Server{db: db, searcher: searcher, logger: logger}
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
	return s.run(ctx, os.Stdin, os.Stdout)
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

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			if err := s.writeError(writer, nil, -32700, "Parse error"); err != nil {
				return fmt.Errorf("write error: %w", err)
			}
			continue
		}

		resp := s.dispatch(ctx, &req)
		if resp == nil {
			// Notification — no response
			continue
		}
		if req.ID == nil {
			continue
		}

		if err := writeJSONRPC(writer, resp); err != nil {
			return fmt.Errorf("write response: %w", err)
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
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": fmt.Sprintf("Error: %s", err.Error())},
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

func writeJSONRPC(w *bufio.Writer, resp *jsonRPCResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
		return err
	}
	return w.Flush()
}

func (s *Server) writeError(w *bufio.Writer, id json.RawMessage, code int, msg string) error {
	if id == nil {
		id = json.RawMessage("null")
	}
	return writeJSONRPC(w, &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: msg},
	})
}
