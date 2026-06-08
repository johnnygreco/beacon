package mcp

import (
	"io"
	"net/http"
)

const maxHTTPRequestBytes = 4 << 20

func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()
		body, err := io.ReadAll(io.LimitReader(r.Body, maxHTTPRequestBytes+1))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(body) > maxHTTPRequestBytes {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		response, err := s.HandleJSONRPC(r.Context(), body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if len(response) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(response)
	})
}
