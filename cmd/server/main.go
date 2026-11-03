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
	"syscall"
	"time"

	"github.com/LinuxSuRen/face-recognition-server/internal/api"
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
	if err != nil {
		// Degraded mode: the UI and profile management stay available.
		logger.Warn("[server] inference engine unavailable", "error", err)
	} else {
		defer faceEngine.Close()
		logger.Info("[server] inference engine ready",
			"model_version", faceEngine.ModelVersion())
	}

	rtspManager := rtsp.NewManager(4)
	defer rtspManager.StopAll()

	server := &http.Server{
		Addr: cfg.Addr,
		Handler: api.NewRouter(api.Deps{
			Config: cfg,
			Logger: logger,
			Store:  faceStore,
			Engine: faceEngine,
			RTSP:   rtspManager,
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
