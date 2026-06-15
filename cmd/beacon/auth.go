package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/mcp"
	"github.com/johnnygreco/beacon/internal/web"
)

func dashboardAuthOptions(ctx context.Context, cfg *config.Config, store *controlplane.Store) ([]web.RouterOption, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	options := []web.RouterOption{}
	if isLoopbackHost(cfg.Server.Host) {
		options = append(options, web.WithGlobalMiddleware(web.LoopbackHostMiddleware(cfg.Server.Host, publicURLHost(cfg.Server.PublicURL))))
	}
	if !isLoopbackHost(cfg.Server.Host) {
		switch cfg.Auth.Mode {
		case config.AuthModeReverseProxy:
			options = append(options, web.WithMCPAuthMiddleware(readTokenAPIMiddleware(store, cfg.Auth.CookieName)))
			return options, nil
		case config.AuthModeOwnerToken:
			if !cfg.Auth.AllowInsecureOwnerHTTP {
				return nil, fmt.Errorf("auth.mode %q on non-loopback HTTP requires auth.allow_insecure_owner_http = true or a trusted TLS reverse proxy with auth.mode %q",
					cfg.Auth.Mode,
					config.AuthModeReverseProxy,
				)
			}
			hasOwnerToken, err := store.HasActiveOwnerToken(ctx)
			if err != nil {
				return nil, err
			}
			if !hasOwnerToken {
				return nil, fmt.Errorf("auth.mode %q requires an active owner/admin token; run beacon init before binding %s", cfg.Auth.Mode, cfg.Server.Host)
			}
		default:
			return nil, fmt.Errorf("server.host %q is not loopback; set auth.mode to %q with an owner token or %q behind a trusted proxy",
				cfg.Server.Host,
				config.AuthModeOwnerToken,
				config.AuthModeReverseProxy,
			)
		}
	}
	if cfg.Auth.Mode != config.AuthModeOwnerToken {
		return options, nil
	}
	authenticator := func(ctx context.Context, plaintext string) bool {
		_, err := store.AuthenticateToken(ctx, controlplane.AuthenticateTokenRequest{
			Plaintext:      plaintext,
			AllowedTypes:   []string{controlplane.TokenTypeOwner, controlplane.TokenTypeAdmin},
			RequiredScopes: []string{controlplane.ScopeRead},
		})
		return err == nil
	}
	options = append(options, web.WithAuthMiddleware(web.OwnerTokenMiddleware(authenticator, cfg.Auth.CookieName)))
	options = append(options, web.WithAPIAuthMiddleware(readTokenAPIMiddleware(store, cfg.Auth.CookieName)))
	return options, nil
}

func publicURLHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return parsed.Host
}

func readTokenAPIMiddleware(store *controlplane.Store, cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, token := range web.RequestAuthTokens(r, cookieName) {
				if record, ok := authenticateOwnerOrReadToken(r.Context(), store, token); ok {
					ctx := r.Context()
					if record.Type == controlplane.TokenTypeRead {
						apiScope := web.APIScopeFilters{
							NodeIDs:      singletonScopeValue(record.NodeID),
							CollectorIDs: singletonScopeValue(record.CollectorID),
							SourceIDs:    append([]string(nil), record.SourceIDs...),
						}
						ctx = web.ContextWithAPIScope(ctx, apiScope)
						ctx = mcp.ContextWithAuthScope(ctx, mcp.ScopeFilters{
							NodeIDs:      singletonScopeValue(record.NodeID),
							CollectorIDs: singletonScopeValue(record.CollectorID),
							SourceIDs:    append([]string(nil), record.SourceIDs...),
						})
					}
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			w.WriteHeader(http.StatusUnauthorized)
		})
	}
}

func authenticateOwnerOrReadToken(ctx context.Context, store *controlplane.Store, plaintext string) (*controlplane.TokenRecord, bool) {
	if store == nil {
		return nil, false
	}
	if record, err := store.AuthenticateToken(ctx, controlplane.AuthenticateTokenRequest{
		Plaintext:      plaintext,
		AllowedTypes:   []string{controlplane.TokenTypeOwner, controlplane.TokenTypeAdmin},
		RequiredScopes: []string{controlplane.ScopeRead},
	}); err == nil {
		return record, true
	}
	if record, err := store.AuthenticateToken(ctx, controlplane.AuthenticateTokenRequest{
		Plaintext:      plaintext,
		AllowedTypes:   []string{controlplane.TokenTypeRead},
		RequiredScopes: []string{controlplane.ScopeRead},
	}); err == nil {
		return record, true
	}
	return nil, false
}

func singletonScopeValue(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{value}
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
