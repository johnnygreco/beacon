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

type publicURLCheckStage string

const (
	publicURLCheckStageHealth     publicURLCheckStage = "health"
	publicURLCheckStageEnrollment publicURLCheckStage = "enrollment"
	publicURLCheckStageProtected  publicURLCheckStage = "protected"
)

type publicURLCheckError struct {
	stage publicURLCheckStage
	err   error
}

func (e *publicURLCheckError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *publicURLCheckError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

type publicURLHealthStatusError struct {
	status  int
	body    string
	headers http.Header
}

func (e *publicURLHealthStatusError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("public URL health check returned HTTP %d%s", e.status, hostGuardSuffix(e.headers, e.body))
}

func (e *publicURLHealthStatusError) hostGuardRejected() bool {
	return e != nil && publicURLHostGuardRejected(e.headers, e.body)
}

var newPublicURLCheckClient = func() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func newEnrollmentPreflightClient() *http.Client {
	client := newPublicURLCheckClient()
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	clone := *client
	clone.CheckRedirect = rejectHTTPRedirect
	return &clone
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
		return &publicURLCheckError{stage: publicURLCheckStageHealth, err: err}
	}
	if err := checkPublicEnrollmentAuth(ctx, client, rootURL); err != nil {
		return &publicURLCheckError{stage: publicURLCheckStageEnrollment, err: err}
	}
	if !opts.Unsafe {
		if err := checkProtectedPublicRoutes(ctx, client, rootURL); err != nil {
			return &publicURLCheckError{stage: publicURLCheckStageProtected, err: err}
		}
	}
	return nil
}

func preflightEnrollmentRoute(ctx context.Context, rawURL string, opts publicURLCheckOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rootURL, err := config.NormalizeRootURL(rawURL, "control-plane URL")
	if err != nil {
		return err
	}
	client := opts.Client
	if client == nil {
		client = newEnrollmentPreflightClient()
	}
	if err := checkPublicHealth(ctx, client, rootURL); err != nil {
		return err
	}
	return checkPublicEnrollmentAuth(ctx, client, rootURL)
}

func checkPublicHealth(ctx context.Context, client *http.Client, rootURL string) error {
	status, body, headers, err := doPublicURLProbe(ctx, client, http.MethodGet, rootURL+"/health", nil, "")
	if err != nil {
		return fmt.Errorf("public URL health check failed: %w", err)
	}
	if status != http.StatusOK {
		return &publicURLHealthStatusError{status: status, body: body, headers: headers}
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
		if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound {
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
	if publicURLHostGuardRejected(headers, body) {
		return " (Beacon host guard rejected the request)"
	}
	return ""
}

func publicURLHostGuardRejected(headers http.Header, body string) bool {
	if headers != nil && headers.Get(web.HostGuardRejectedHeader) == "rejected" {
		return true
	}
	if strings.Contains(strings.ToLower(body), "host guard") {
		return true
	}
	return false
}
