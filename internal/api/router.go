// Package api exposes the HTTP surface of the face recognition server.
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/LinuxSuRen/face-recognition-server/internal/config"
	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
	"github.com/LinuxSuRen/face-recognition-server/internal/httpui"
	"github.com/LinuxSuRen/face-recognition-server/internal/rtsp"
	"github.com/LinuxSuRen/face-recognition-server/internal/store"
)

// Deps groups the collaborators every handler may use. Engine may be nil
// (CGO-free build or missing models): the server keeps serving the UI and
// reports the engine as unavailable on /healthz and inference endpoints.
type Deps struct {
	Config config.Config
	Logger *slog.Logger
	Store  *store.Store
	Engine engine.Engine
	// EngineError is the classified reason the engine is unavailable:
	// no_cgo_build, models_missing, ort_library_failed or
	// engine_start_failed. Empty means "not attempted / unknown".
	EngineError string
	// RTSP is optional; a fresh manager is created when nil.
	RTSP *rtsp.Manager
}

// version is reported on /healthz; main overrides it via SetVersion at
// startup with the value injected at build time.
var version = "dev"

// SetVersion records the build version for /healthz.
func SetVersion(value string) {
	if value != "" {
		version = value
	}
}

// NewRouter builds the complete HTTP mux: /healthz, the versioned API and
// the embedded Vue UI as the fallback.
func NewRouter(deps Deps) *http.ServeMux {
	if deps.RTSP == nil {
		deps.RTSP = rtsp.NewManager(4)
	}
	faces := &facesAPI{
		config: deps.Config,
		logger: deps.Logger,
		store:  deps.Store,
		engine: deps.Engine,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(deps))
	registerFacesRoutes(mux, faces)
	registerRTSPRoutes(mux, deps.RTSP, faces)
	mux.HandleFunc("/", spaHandler(deps))
	return mux
}

func healthz(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		engineState := map[string]any{"ready": deps.Engine != nil}
		if deps.Engine != nil {
			engineState["model_version"] = deps.Engine.ModelVersion()
		} else {
			status = "degraded"
			reason := deps.EngineError
			if reason == "" {
				reason = "no_cgo_build"
			}
			engineState["reason"] = reason
		}
		dbState := map[string]any{"ok": deps.Store != nil}
		if deps.Store == nil {
			status = "degraded"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  status,
			"version": version,
			"engine":  engineState,
			"db":      dbState,
		})
	}
}

// spaHandler serves the embedded Vue build; unknown extension-less paths
// fall back to index.html so client-side routing survives refreshes.
func spaHandler(deps Deps) http.HandlerFunc {
	fileSystem := httpui.FS()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if fileSystem == nil {
			writeError(w, http.StatusNotFound, "web UI is not bundled in this build")
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if _, err := fs.Stat(fileSystem, name); err != nil && !strings.Contains(name, ".") {
				index, indexErr := httpui.Index()
				if indexErr == nil {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(index)
					return
				}
			}
		}
		http.FileServerFS(fileSystem).ServeHTTP(w, r)
	}
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
