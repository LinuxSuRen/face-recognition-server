package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
	"github.com/LinuxSuRen/face-recognition-server/internal/rtsp"
)

// rtspAPI exposes RTSP configuration, sessions and the SSE event stream.
type rtspAPI struct {
	manager *rtsp.Manager
	faces   *facesAPI
	logger  *slog.Logger
}

func registerRTSPRoutes(mux *http.ServeMux, manager *rtsp.Manager, faces *facesAPI) {
	api := &rtspAPI{manager: manager, faces: faces, logger: faces.logger}
	mux.HandleFunc("GET /api/v1/rtsp/configuration", api.getConfiguration)
	mux.HandleFunc("PUT /api/v1/rtsp/configuration", api.updateConfiguration)
	mux.HandleFunc("POST /api/v1/rtsp/sessions", api.createSession)
	mux.HandleFunc("GET /api/v1/rtsp/sessions/{id}", api.sessionSnapshot)
	mux.HandleFunc("GET /api/v1/rtsp/sessions/{id}/events", api.sessionEvents)
	mux.HandleFunc("DELETE /api/v1/rtsp/sessions/{id}", api.stopSession)
}

type rtspConfigurationPayload struct {
	URL       string `json:"url"`
	Transport string `json:"transport"`
}

func (api *rtspAPI) getConfiguration(w http.ResponseWriter, r *http.Request) {
	sourceURL, transport, err := api.manager.Source()
	if errors.Is(err, rtsp.ErrSourceNotConfigured) {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 RTSP 配置失败")
		return
	}
	writeJSON(w, http.StatusOK, rtspConfigurationPayload{URL: sourceURL, Transport: transport})
}

func (api *rtspAPI) updateConfiguration(w http.ResponseWriter, r *http.Request) {
	var body rtspConfigurationPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体必须是包含 url 的 JSON 对象")
		return
	}
	parsed, err := url.Parse(body.URL)
	if err != nil || (parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps") || parsed.Host == "" {
		writeError(w, http.StatusUnprocessableEntity, "RTSP 地址无效，需以 rtsp:// 开头并包含主机名")
		return
	}
	transport := body.Transport
	if transport == "" {
		transport = "tcp"
	}
	if transport != "tcp" && transport != "udp" {
		writeError(w, http.StatusUnprocessableEntity, "transport 只支持 tcp 或 udp")
		return
	}
	api.manager.Configure(body.URL, transport)
	writeJSON(w, http.StatusOK, rtspConfigurationPayload{URL: body.URL, Transport: transport})
}

func (api *rtspAPI) createSession(w http.ResponseWriter, r *http.Request) {
	if api.faces.engine == nil {
		writeError(w, http.StatusServiceUnavailable, engineUnavailableDetail)
		return
	}
	session, err := api.manager.Create(r.Context(), rtsp.SessionOptions{
		Logger: api.faces.logger,
		Analyzer: func(ctx context.Context, img *engine.Image) (any, error) {
			return api.faces.analyze(ctx, img)
		},
	})
	if err != nil {
		switch {
		case errors.Is(err, rtsp.ErrSourceNotConfigured):
			writeError(w, http.StatusConflict, "请先保存 RTSP 视频源配置")
		case errors.Is(err, rtsp.ErrTooManySessions):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "创建会话失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, session.Snapshot())
}

func (api *rtspAPI) sessionSnapshot(w http.ResponseWriter, r *http.Request) {
	session, err := api.manager.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "RTSP 会话不存在")
		return
	}
	writeJSON(w, http.StatusOK, session.Snapshot())
}

func (api *rtspAPI) stopSession(w http.ResponseWriter, r *http.Request) {
	if err := api.manager.Stop(r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "RTSP 会话不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sessionEvents streams the SSE feed of one session: state transitions and
// analyzed frames (JPEG as a data URL plus faces payload).
func (api *rtspAPI) sessionEvents(w http.ResponseWriter, r *http.Request) {
	session, err := api.manager.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "RTSP 会话不存在")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, detach := session.Subscribe()
	defer detach()

	// initial state so a reconnecting client renders immediately
	writeSSE(w, flusher, "state", map[string]any{
		"type": "state", "status": session.Snapshot().Status, "detail": session.Snapshot().Detail,
	})

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, open := <-events:
			if !open {
				return
			}
			var payload map[string]any
			switch event.Kind {
			case "state":
				payload = map[string]any{
					"type":   "state",
					"status": string(event.Status),
					"detail": event.Detail,
				}
			case "frame":
				payload = map[string]any{
					"type":  "frame",
					"seq":   event.Seq,
					"image": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(event.JPEG),
				}
				for key, value := range event.Analysis.(map[string]any) {
					payload[key] = value
				}
			}
			writeSSE(w, flusher, "message", payload)
			if event.Kind == "state" && isTerminal(event.Status) {
				return
			}
		}
	}
}

func isTerminal(status rtsp.Status) bool {
	switch status {
	case rtsp.StatusStopped, rtsp.StatusExpired, rtsp.StatusFailed:
		return true
	}
	return false
}

func writeSSE(w io.Writer, flusher http.Flusher, eventName string, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if _, err := io.WriteString(w, "event: "+eventName+"\ndata: "+string(encoded)+"\n\n"); err != nil {
		return
	}
	flusher.Flush()
}
