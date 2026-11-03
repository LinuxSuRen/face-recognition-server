// Package rtsp manages live RTSP sessions: an ffmpeg subprocess pulls the
// stream, the session keeps only the latest JPEG frame (bounded memory),
// runs the analysis callback at its own pace and pushes results to SSE
// subscribers. Reconnects and idle expiry are handled internally.
package rtsp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"

	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
)

// Status of a session over its lifetime.
type Status string

const (
	StatusConnecting   Status = "connecting"
	StatusLive         Status = "live"
	StatusReconnecting Status = "reconnecting"
	StatusFailed       Status = "failed"
	StatusExpired      Status = "expired"
	StatusStopped      Status = "stopped"
)

// Analyzer processes one decoded frame together with the original JPEG
// bytes; the returned value is passed to subscribers untouched (the API
// layer defines the JSON shape).
type Analyzer func(ctx context.Context, img *engine.Image, frameJPEG []byte) (any, error)

// Event is one SSE-worthy occurrence of a session.
type Event struct {
	// Kind is "state" or "frame".
	Kind string
	// Seq identifies the frame for kind == "frame".
	Seq uint64
	// JPEG is the raw latest frame for kind == "frame".
	JPEG []byte
	// Analysis is the analyzer result for kind == "frame".
	Analysis any
	// Status and Detail describe the session for kind == "state".
	Status Status
	Detail string
}

// SessionOptions tunes one session.
type SessionOptions struct {
	URL           string
	Transport     string
	Analyzer      Analyzer
	Logger        *slog.Logger
	IdleTimeout   time.Duration
	ReconnectMax  int
	AnalysisEvery time.Duration
	// sourceFactory starts the frame producer (overridden in tests).
	sourceFactory func(ctx context.Context) (<-chan []byte, error)
}

// Session is one live analysis view of an RTSP source.
type Session struct {
	id   string
	opts SessionOptions

	mu           sync.Mutex
	status       Status
	detail       string
	latestJPEG   []byte
	latestSeq    uint64
	lastAnalysis any
	analyzedSeq  uint64
	subscribers  map[chan Event]struct{}
	lastActivity time.Time
	startedAt    time.Time

	cancel context.CancelFunc
	done   chan struct{}
}

func newSession(opts SessionOptions) *Session {
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = 10 * time.Minute
	}
	if opts.ReconnectMax <= 0 {
		opts.ReconnectMax = 5
	}
	if opts.AnalysisEvery <= 0 {
		opts.AnalysisEvery = 500 * time.Millisecond
	}
	if opts.sourceFactory == nil {
		opts.sourceFactory = newFFmpegSource(opts.URL, opts.Transport, "").start
	}
	id := make([]byte, 8)
	rand.Read(id)
	return &Session{
		id:           hex.EncodeToString(id),
		opts:         opts,
		status:       StatusConnecting,
		detail:       "正在连接 RTSP 摄像机",
		subscribers:  map[chan Event]struct{}{},
		lastActivity: time.Now(),
		startedAt:    time.Now(),
		done:         make(chan struct{}),
	}
}

// ID returns the session identifier.
func (s *Session) ID() string { return s.id }

// Snapshot is a consistent view for API responses.
type Snapshot struct {
	ID          string `json:"session_id"`
	Status      Status `json:"status"`
	Detail      string `json:"detail"`
	Seq         uint64 `json:"seq"`
	StartedAt   string `json:"started_at"`
	LastEventAt string `json:"last_event_at"`
	// Enrollment carries the enrollment controller state when present.
	Enrollment map[string]any `json:"enrollment,omitempty"`
}

// Snapshot returns the current state.
func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{
		ID:          s.id,
		Status:      s.status,
		Detail:      s.detail,
		Seq:         s.latestSeq,
		StartedAt:   s.startedAt.Format(time.RFC3339),
		LastEventAt: s.lastActivity.Format(time.RFC3339),
	}
}

// Subscribe registers for events; the returned channel is buffered and the
// cancel function detaches it.
func (s *Session) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 8)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subscribers, ch)
		s.mu.Unlock()
	}
}

func (s *Session) setStatus(status Status, detail string) {
	s.mu.Lock()
	s.status = status
	s.detail = detail
	s.mu.Unlock()
	s.broadcast(Event{Kind: "state", Status: status, Detail: detail})
}

func (s *Session) broadcast(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActivity = time.Now()
	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
			// slow consumer: drop events, the next snapshot will resync it
		}
	}
}

// SetAnalyzer swaps the analysis callback (enrollment sessions install
// their controller after creation).
func (s *Session) SetAnalyzer(analyzer Analyzer) {
	s.mu.Lock()
	s.opts.Analyzer = analyzer
	s.mu.Unlock()
}

// analyzer returns the current callback under lock.
func (s *Session) analyzer() Analyzer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opts.Analyzer
}

// start runs the reader and analysis loops until Stop/expire.
func (s *Session) start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	go s.readerLoop(ctx)
	go s.analysisLoop(ctx)
	go s.idleLoop(ctx)
}

// Stop terminates the session; safe to call multiple times.
func (s *Session) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	status, detail := s.status, s.detail
	if status != StatusFailed && status != StatusExpired {
		status, detail = StatusStopped, "会话已停止"
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	<-s.done
	s.setStatus(status, detail)
}

func (s *Session) readerLoop(ctx context.Context) {
	defer close(s.done)
	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		attempt++
		frames, err := s.opts.sourceFactory(ctx)
		if err != nil {
			if attempt > s.opts.ReconnectMax {
				s.setStatus(StatusFailed, "RTSP 连接失败，已停止重试")
				return
			}
			s.setStatus(StatusReconnecting, "RTSP 连接失败，正在重连")
			if !sleep(ctx, time.Duration(attempt)*time.Second) {
				return
			}
			continue
		}
		s.setStatus(StatusLive, "RTSP 已连接，正在等待画面")
		for frame := range frames {
			s.mu.Lock()
			s.latestJPEG = frame
			s.latestSeq++
			s.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		if attempt > s.opts.ReconnectMax {
			s.setStatus(StatusFailed, "RTSP 流中断，已停止重试")
			return
		}
		s.setStatus(StatusReconnecting, "RTSP 流中断，正在重连")
		if !sleep(ctx, time.Duration(attempt)*time.Second) {
			return
		}
	}
}

func (s *Session) analysisLoop(ctx context.Context) {
	ticker := time.NewTicker(s.opts.AnalysisEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		if s.status != StatusLive || s.latestJPEG == nil || s.analyzedSeq == s.latestSeq {
			s.mu.Unlock()
			continue
		}
		frame := s.latestJPEG
		seq := s.latestSeq
		s.mu.Unlock()

		var analysis any
		var err error
		if analyzer := s.analyzer(); analyzer != nil {
			var img *engine.Image
			img, err = engine.DecodeImage(frame)
			if err == nil {
				analysis, err = analyzer(ctx, img, frame)
			}
		}
		s.mu.Lock()
		s.analyzedSeq = seq
		s.lastAnalysis = analysis
		s.mu.Unlock()
		if err != nil {
			s.warn("analysis failed", err)
			continue
		}
		s.broadcast(Event{Kind: "frame", Seq: seq, JPEG: frame, Analysis: analysis})
	}
}

func (s *Session) idleLoop(ctx context.Context) {
	// check at half the timeout so expiry lands close to the deadline;
	// small timeouts keep tests fast
	interval := s.opts.IdleTimeout / 2
	if interval > 15*time.Second {
		interval = 15 * time.Second
	}
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		idle := time.Since(s.lastActivity) > s.opts.IdleTimeout
		s.mu.Unlock()
		if idle {
			s.setStatus(StatusExpired, "会话空闲超时，已自动停止")
			s.Stop()
			return
		}
	}
}

func (s *Session) warn(message string, err error) {
	if s.opts.Logger != nil {
		s.opts.Logger.Warn("[rtsp] "+message, "session", s.id, "error", err)
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
