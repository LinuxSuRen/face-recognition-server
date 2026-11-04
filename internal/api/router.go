// Package api exposes the HTTP surface of the face recognition server.
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/LinuxSuRen/face-recognition-server/internal/auth"
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
	// AuthEnabled gates browser-session auth on the management API; the
	// external recognition endpoints always require bearer tokens.
	AuthEnabled bool
	// OIDC configures federated login; empty disables SSO.
	OIDC auth.OIDCConfig
	// SessionUser resolves the current user from the request cookie.
	SessionUser func(r *http.Request) *store.User
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

// NewRouter builds the complete HTTP surface — /healthz, the versioned API
// and the embedded Vue UI — wrapped with session authentication.
func NewRouter(deps Deps) http.Handler {
	if deps.RTSP == nil {
		deps.RTSP = rtsp.NewManager(4)
	}
	authService := &auth.Service{Store: deps.Store}
	if deps.SessionUser == nil {
		deps.SessionUser = func(r *http.Request) *store.User {
			cookie, err := r.Cookie(auth.SessionCookie)
			if err != nil {
				return nil
			}
			user, err := authService.SessionUser(r.Context(), cookie.Value)
			if err != nil {
				return nil
			}
			return user
		}
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
	registerAuthRoutes(mux, deps)
	mux.HandleFunc("/", spaHandler(deps))
	return sessionAuth(deps, mux)
}

// sessionAuth wraps the mux: management endpoints under /api/v1 require a
// logged-in session (and admin-only routes an admin) when auth is enabled;
// the login endpoints, the bearer-token external endpoints, healthz and
// the UI stay public.
func sessionAuth(deps Deps, next *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !deps.AuthEnabled || isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		user := deps.SessionUser(r)
		if user == nil {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		if isAdminOnlyPath(r.URL.Path) && user.Role != auth.AdminRole {
			writeError(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isPublicPath(path string) bool {
	prefixes := []string{
		"/healthz",
		"/api/v1/auth/",
		"/api/v1/verify",
		"/api/v1/identify",
		"/api/v1/enroll",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return !strings.HasPrefix(path, "/api/")
}

func isAdminOnlyPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/users") ||
		strings.HasPrefix(path, "/api/v1/tokens") ||
		strings.HasPrefix(path, "/api/v1/settings") ||
		strings.HasPrefix(path, "/api/v1/login-events")
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
