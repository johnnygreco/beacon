package mcp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type RemoteProxy struct {
	Endpoint string
	Token    string
	Client   *http.Client
}

func NewRemoteProxy(endpoint, token string) *RemoteProxy {
	return &RemoteProxy{
		Endpoint: strings.TrimSpace(endpoint),
		Token:    strings.TrimSpace(token),
		Client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *RemoteProxy) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if strings.TrimSpace(p.Endpoint) == "" {
		return fmt.Errorf("remote MCP endpoint is required")
	}
	if strings.TrimSpace(p.Token) == "" {
		return fmt.Errorf("remote MCP read token is required")
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

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
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		response, err := p.forward(ctx, client, line)
		if err != nil {
			return err
		}
		if len(response) == 0 {
			continue
		}
		if _, err := writer.Write(response); err != nil {
			return fmt.Errorf("write remote MCP response: %w", err)
		}
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("flush remote MCP response: %w", err)
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

func (p *RemoteProxy) forward(ctx context.Context, client *http.Client, line []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Endpoint, bytes.NewReader(line))
	if err != nil {
		return nil, fmt.Errorf("create remote MCP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.Token))
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote MCP request failed: %w", err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxHTTPRequestBytes+1))
	if readErr != nil {
		return nil, fmt.Errorf("read remote MCP response: %w", readErr)
	}
	if len(body) > maxHTTPRequestBytes {
		return nil, fmt.Errorf("remote MCP response exceeded %d bytes", maxHTTPRequestBytes)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return append(bytes.TrimRight(body, "\r\n"), '\n'), nil
	case http.StatusNoContent:
		return nil, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("remote MCP authorization failed: %s", resp.Status)
	default:
		if len(body) > 0 {
			return nil, fmt.Errorf("remote MCP endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return nil, fmt.Errorf("remote MCP endpoint returned %s", resp.Status)
	}
}
