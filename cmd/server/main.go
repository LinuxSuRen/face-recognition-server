// Command server runs the face recognition HTTP service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/LinuxSuRen/face-recognition-server/internal/api"
	authpkg "github.com/LinuxSuRen/face-recognition-server/internal/auth"
	"github.com/LinuxSuRen/face-recognition-server/internal/config"
	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
	_ "github.com/LinuxSuRen/face-recognition-server/internal/engine/onnxrt" // registers the CGO backend when built with cgo
	"github.com/LinuxSuRen/face-recognition-server/internal/rtsp"
	"github.com/LinuxSuRen/face-recognition-server/internal/store"
)

// version is injected at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	faceStore, err := store.Open(cfg.DBPath)
	if err != nil {
		logger.Error("[server] opening database failed", "error", err)
		os.Exit(1)
	}
	defer faceStore.Close()

	faceEngine, err := engine.New(engine.Options{
		DetectorPath:   filepath.Join(cfg.ModelsDir, "face_detection_yunet.onnx"),
		RecognizerPath: filepath.Join(cfg.ModelsDir, "face_recognition_sface.onnx"),
	})
	engineReason := ""
	if err != nil {
		// Degraded mode: the UI and profile management stay available.
		engineReason = classifyEngineError(err)
		logger.Warn("[server] inference engine unavailable",
			"reason", engineReason, "error", err)
	} else {
		defer faceEngine.Close()
		logger.Info("[server] inference engine ready",
			"model_version", faceEngine.ModelVersion())
	}

	rtspManager := rtsp.NewManager(4)
	defer rtspManager.StopAll()

	// platform auth: bootstrap the admin account on first boot
	authEnabled := config.LoadAuthEnabled()
	if authEnabled {
		if count, err := faceStore.CountUsers(context.Background()); err == nil && count == 0 {
			password := config.LoadAdminPassword() // env or generated
			if hash, hashErr := authpkg.HashPassword(password); hashErr == nil {
				if _, createErr := faceStore.CreateUser(context.Background(),
					"admin", "管理员", hash, authpkg.AdminRole, true); createErr == nil {
					logger.Info("[server] bootstrap admin account created",
						"username", "admin", "password", password,
						"hint", "可用 FACE_RECOGNITION_ADMIN_PASSWORD 指定")
				}
			}
		}
	}

	server := &http.Server{
		Addr: cfg.Addr,
		Handler: api.NewRouter(api.Deps{
			Config:      cfg,
			Logger:      logger,
			Store:       faceStore,
			Engine:      faceEngine,
			EngineError: engineReason,
			RTSP:        rtspManager,
			AuthEnabled: authEnabled,
			OIDC:        config.LoadOIDC(),
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	api.SetVersion(version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := listenWithDrift(cfg.Addr, logger)
	if err != nil {
		logger.Error("[server] cannot listen", "addr", cfg.Addr, "error", err)
		os.Exit(1)
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("[server] listening", "addr", listener.Addr().String())
		errCh <- server.Serve(listener)
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("[server] exited with failure", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("[server] graceful shutdown failed", "error", err)
	}
	logger.Info("[server] stopped")
}

// classifyEngineError maps an engine startup failure to the stable reason
// codes surfaced on /healthz and in the UI.
func classifyEngineError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, engine.ErrEngineUnavailable):
		return "no_cgo_build"
	case errors.Is(err, os.ErrNotExist):
		return "models_missing"
	case strings.Contains(err.Error(), "initializing onnxruntime"):
		return "ort_library_failed"
	default:
		return "engine_start_failed"
	}
}

// listenWithDrift binds addr. When exactly its port is already in use the
// listener drifts to an OS-assigned free port on the same host instead of
// aborting, and the fallback is logged; any other bind error (permission,
// bad address) is returned unchanged so misconfiguration stays visible.
func listenWithDrift(addr string, logger *slog.Logger) (net.Listener, error) {
	listener, err := net.Listen("tcp", addr)
	if err == nil {
		return listener, nil
	}
	if !isAddrInUse(err) {
		return nil, err
	}
	host, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		host = ""
	}
	fallback, fallbackErr := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if fallbackErr != nil {
		return nil, err // the conflict is the interesting failure
	}
	logger.Warn("[server] configured port is busy, drifting to a free port",
		"configured", addr, "actual", fallback.Addr().String())
	return fallback, nil
}

// isAddrInUse reports a "port already taken" bind error. On Unix this is
// EADDRINUSE; Windows WinSock reports WSAEADDRINUSE (10048), whose numeric
// value differs from syscall.EADDRINUSE on that platform.
func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return runtime.GOOS == "windows" && errno == 10048 // WSAEADDRINUSE
	}
	return false
}
