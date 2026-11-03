// Command server runs the face recognition HTTP service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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

	errCh := make(chan error, 1)
	go func() {
		logger.Info("[server] listening", "addr", cfg.Addr)
		errCh <- server.ListenAndServe()
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
