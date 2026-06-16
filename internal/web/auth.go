package web

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

const HostGuardRejectedHeader = "X-Beacon-Host-Guard"

func LoopbackHostMiddleware(configuredHost string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isAllowedLoopbackHost(r.Host, configuredHost) {
				w.Header().Set(HostGuardRejectedHeader, "rejected")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("beacon host guard rejected\n"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isAllowedLoopbackHost(hostHeader, configuredHost string) bool {
	host := normalizeHostOnly(hostHeader)
	if host == "" {
		return false
	}
	configured := normalizeHostOnly(configuredHost)
	if configured != "" && strings.EqualFold(host, configured) && isLoopbackLiteral(host) {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if isLoopbackLiteral(host) {
		return true
	}
	return false
}

func normalizeHostOnly(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err == nil && parsed.Host != "" {
			value = parsed.Host
		}
	}
	if strings.HasPrefix(value, "[") {
		if end := strings.Index(value, "]"); end > 0 {
			return strings.TrimSpace(value[1:end])
		}
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return strings.Trim(strings.TrimSpace(host), "[]")
	}
	if strings.Count(value, ":") > 1 {
		return strings.Trim(value, "[]")
	}
	if idx := strings.LastIndex(value, ":"); idx > -1 {
		return strings.TrimSpace(value[:idx])
	}
	return value
}

func isLoopbackLiteral(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
