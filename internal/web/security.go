package web

import (
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const dashboardCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; worker-src 'none'; manifest-src 'self'"

// SecurityHeadersMiddleware sets personal-production browser hardening headers
// for the dashboard, JSON API, and local MCP endpoint.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", dashboardCSP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func MutationRequestGuardMiddleware(requireJSON bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !sameOriginMutationRequest(r) {
				http.Error(w, "cross-site mutation rejected", http.StatusForbidden)
				return
			}
			if requireJSON && !requestContentTypeIsJSON(r) {
				http.Error(w, "content-type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func sameOriginMutationRequest(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "", "same-origin", "same-site", "none":
	case "cross-site":
		return false
	default:
		return false
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return equalHost(parsed.Host, r.Host)
}

func equalHost(a, b string) bool {
	aHost, aPort, errA := net.SplitHostPort(a)
	bHost, bPort, errB := net.SplitHostPort(b)
	if errA != nil {
		aHost = a
		aPort = ""
	}
	if errB != nil {
		bHost = b
		bPort = ""
	}
	return strings.EqualFold(aHost, bHost) && aPort == bPort
}

func requestContentTypeIsJSON(r *http.Request) bool {
	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}
