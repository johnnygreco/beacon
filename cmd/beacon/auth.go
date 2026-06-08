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
	if !isLoopbackHost(cfg.Server.Host) {
		switch cfg.Auth.Mode {
		case config.AuthModeReverseProxy:
			return nil, nil
		case config.AuthModeOwnerToken:
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
		return nil, nil
	}
	authenticator := func(ctx context.Context, plaintext string) bool {
		_, err := store.AuthenticateToken(ctx, controlplane.AuthenticateTokenRequest{
			Plaintext:      plaintext,
			AllowedTypes:   []string{controlplane.TokenTypeOwner, controlplane.TokenTypeAdmin},
			RequiredScopes: []string{controlplane.ScopeRead},
		})
		return err == nil
	}
	return []web.RouterOption{web.WithAuthMiddleware(web.OwnerTokenMiddleware(authenticator, cfg.Auth.CookieName))}, nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
