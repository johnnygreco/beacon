package web

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type TokenAuthenticator func(context.Context, string) bool

func OwnerTokenMiddleware(auth TokenAuthenticator, cookieName string) func(http.Handler) http.Handler {
	cookieName = strings.TrimSpace(cookieName)
	if cookieName == "" {
		cookieName = "beacon_owner_token"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, token := range requestOwnerTokens(r, cookieName) {
				if auth != nil && auth(r.Context(), token) {
					next.ServeHTTP(w, r)
					return
				}
			}
			w.WriteHeader(http.StatusUnauthorized)
		})
	}
}

func LoopbackHostMiddleware(configuredHost string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isAllowedLoopbackHost(r.Host, configuredHost) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestOwnerTokens(r *http.Request, cookieName string) []string {
	var tokens []string
	if token := bearerToken(r.Header.Get("Authorization")); token != "" {
		tokens = append(tokens, token)
	}
	if cookie, err := r.Cookie(cookieName); err == nil {
		if token := strings.TrimSpace(cookie.Value); token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if len(header) <= len("Bearer") || !strings.EqualFold(header[:len("Bearer")], "Bearer") {
		return ""
	}
	if header[len("Bearer")] != ' ' && header[len("Bearer")] != '\t' {
		return ""
	}
	value := strings.TrimSpace(header[len("Bearer"):])
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return ""
	}
	return value
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
	return isLoopbackLiteral(host)
}

func normalizeHostOnly(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
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
