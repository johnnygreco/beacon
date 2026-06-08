package web

import (
	"context"
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
