package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/ingest"
	"github.com/johnnygreco/beacon/internal/web"
)

type publicURLCheckOptions struct {
	Unsafe bool
	Client *http.Client
}

var newPublicURLCheckClient = func() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func runPublicURLChecks(ctx context.Context, rawURL string, opts publicURLCheckOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rootURL, err := config.NormalizeRootURL(rawURL, "server.public_url")
	if err != nil {
		return err
	}
	client := opts.Client
	if client == nil {
		client = newPublicURLCheckClient()
	}
	if err := checkPublicHealth(ctx, client, rootURL); err != nil {
		return err
	}
	if err := checkPublicEnrollmentAuth(ctx, client, rootURL); err != nil {
		return err
	}
	if !opts.Unsafe {
		if err := checkProtectedPublicRoutes(ctx, client, rootURL); err != nil {
			return err
		}
	}
	return nil
}

func checkPublicHealth(ctx context.Context, client *http.Client, rootURL string) error {
	status, body, headers, err := doPublicURLProbe(ctx, client, http.MethodGet, rootURL+"/health", nil, "")
	if err != nil {
		return fmt.Errorf("public URL health check failed: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("public URL health check returned HTTP %d%s", status, hostGuardSuffix(headers, body))
	}
	return nil
}

func checkPublicEnrollmentAuth(ctx context.Context, client *http.Client, rootURL string) error {
	body := []byte(`{"schema":"` + ingest.SchemaV1 + `","bootstrap":{}}`)
	status, responseBody, headers, err := doPublicURLProbe(ctx, client, http.MethodPost, rootURL+"/api/ingest/v1/enroll", body, "Bearer bcn_enroll_public_probe")
	if err != nil {
		return fmt.Errorf("public URL enrollment auth check failed: %w", err)
	}
	if status != http.StatusUnauthorized {
		return fmt.Errorf("public URL enrollment auth check returned HTTP %d%s", status, hostGuardSuffix(headers, responseBody))
	}
	normalizedBody := strings.ToLower(responseBody)
	if strings.Contains(normalizedBody, "missing bearer token") {
		return fmt.Errorf("public URL enrollment auth check lost the Authorization header")
	}
	if !strings.Contains(normalizedBody, "unauthorized") {
		return fmt.Errorf("public URL enrollment auth check returned an unexpected unauthorized response")
	}
	return nil
}

func checkProtectedPublicRoutes(ctx context.Context, client *http.Client, rootURL string) error {
	checks := []struct {
		method string
		path   string
		body   []byte
	}{
		{method: http.MethodGet, path: "/"},
		{method: http.MethodGet, path: "/api/status"},
		{method: http.MethodPost, path: "/api/mcp", body: []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)},
	}
	for _, check := range checks {
		status, body, headers, err := doPublicURLProbe(ctx, client, check.method, rootURL+check.path, check.body, "")
		if err != nil {
			return fmt.Errorf("public URL protected route check %s failed: %w", check.path, err)
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			continue
		}
		return fmt.Errorf("public URL protected route %s returned HTTP %d without authentication%s; set auth.mode to %q or %q, or pass --unsafe-public-url to acknowledge this exposure",
			check.path,
			status,
			hostGuardSuffix(headers, body),
			config.AuthModeOwnerToken,
			config.AuthModeReverseProxy,
		)
	}
	return nil
}

func doPublicURLProbe(ctx context.Context, client *http.Client, method, target string, body []byte, authorization string) (int, string, http.Header, error) {
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, "", nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", nil, err
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		return 0, "", resp.Header, readErr
	}
	return resp.StatusCode, string(responseBody), resp.Header, nil
}

func hostGuardSuffix(headers http.Header, body string) string {
	if headers != nil && headers.Get(web.HostGuardRejectedHeader) == "rejected" {
		return " (Beacon host guard rejected the request)"
	}
	if strings.Contains(strings.ToLower(body), "host guard") {
		return " (Beacon host guard rejected the request)"
	}
	return ""
}
