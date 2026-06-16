//go:build e2e

package main

import (
	"io"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/johnnygreco/beacon/internal/assets"
	"github.com/johnnygreco/beacon/internal/sse"
	"github.com/johnnygreco/beacon/internal/web"
)

func main() {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	staticFS, err := fs.Sub(assets.StaticFS, "static")
	if err != nil {
		log.Fatalf("prepare static fs: %v", err)
	}

	handlers := web.NewHandlersForE2E(nil, logger)
	apiHandlers := web.NewAPIHandlers(nil, nil, logger)
	router := web.NewRouter(staticFS, sse.NewBroker(16, logger), handlers, apiHandlers)

	addr := os.Getenv("BEACON_E2E_ADDR")
	if addr == "" {
		addr = "127.0.0.1:4610"
	}
	log.Printf("e2e server listening on http://%s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}
