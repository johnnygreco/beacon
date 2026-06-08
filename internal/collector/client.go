package collector

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/ingest"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type SendError struct {
	StatusCode int
	Retryable  bool
	Message    string
}

func (e *SendError) Error() string {
	if e == nil {
		return ""
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("ingest request failed with status %d: %s", e.StatusCode, e.Message)
	}
	return e.Message
}

func NewClient(baseURL, token string, timeout time.Duration) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("control plane URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("control plane URL must be absolute")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("control plane URL must use http or https")
	}
	if parsed.Scheme == "http" && !isLoopbackURLHost(parsed.Host) {
		return nil, fmt.Errorf("control plane URL must use https for non-loopback hosts")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("ingest token is required")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: timeout, CheckRedirect: rejectHTTPRedirect},
	}, nil
}

func rejectHTTPRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func isLoopbackURLHost(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) SendBatch(ctx context.Context, req ingest.BatchRequest) (ingest.BatchAck, error) {
	var ack ingest.BatchAck
	if err := c.post(ctx, "/api/ingest/v1/batches", req, &ack); err != nil {
		return ingest.BatchAck{}, err
	}
	return ack, nil
}

func (c *Client) SendHeartbeat(ctx context.Context, req ingest.HeartbeatRequest) (ingest.HeartbeatResponse, error) {
	var resp ingest.HeartbeatResponse
	if err := c.post(ctx, "/api/ingest/v1/heartbeats", req, &resp); err != nil {
		return ingest.HeartbeatResponse{}, err
	}
	return resp, nil
}

func (c *Client) post(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(body); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, &compressed)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Content-Encoding", "gzip")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return &SendError{Retryable: true, Message: err.Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &SendError{
			StatusCode: resp.StatusCode,
			Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
			Message:    strings.TrimSpace(string(data)),
		}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &SendError{StatusCode: resp.StatusCode, Retryable: true, Message: err.Error()}
	}
	return nil
}
