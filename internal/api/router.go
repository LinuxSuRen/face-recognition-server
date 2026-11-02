// Package api exposes the HTTP surface of the face recognition server.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/LinuxSuRen/face-recognition-server/internal/config"
)

// Deps groups the collaborators every handler may use.
type Deps struct {
	Config config.Config
	Logger *slog.Logger
}

// NewRouter builds the complete HTTP mux: /healthz, versioned API endpoints
// and the embedded Vue UI as the fallback.
func NewRouter(deps Deps) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	// API endpoints are registered in sub-routers as they land; the mux
	// already serves as the single wiring point for main.
	return mux
}

// writeJSON renders a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// writeError renders a stable error envelope: {"detail": "..."} so the UI can
// always show a friendly message from one field.
func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}
