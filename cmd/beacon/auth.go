package main

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/web"
)

func dashboardAuthOptions(ctx context.Context, cfg *config.Config, store *controlplane.Store) ([]web.RouterOption, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	options := []web.RouterOption{}
	if isLoopbackHost(cfg.Server.Host) {
		options = append(options, web.WithGlobalMiddleware(web.LoopbackHostMiddleware(cfg.Server.Host)))
	}
	if !isLoopbackHost(cfg.Server.Host) {
		switch cfg.Auth.Mode {
		case config.AuthModeReverseProxy:
			return nil, nil
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
	return options, nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
