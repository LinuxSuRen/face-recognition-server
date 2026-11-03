package rtsp

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
)

func TestFrameScannerHandlesSplitFramesAndGarbage(t *testing.T) {
	frameA := []byte{0xFF, 0xD8, 1, 2, 3, 0xFF, 0xD9}
	frameB := []byte{0xFF, 0xD8, 9, 8, 7, 6, 0xFF, 0xD9}
	scanner := &frameScanner{}

	frames := scanner.feed([]byte{0x00, 0x01})
	if len(frames) != 0 {
		t.Fatalf("garbage alone must yield no frames, got %d", len(frames))
	}
	frames = scanner.feed(frameA[:3])
	if len(frames) != 0 {
		t.Fatalf("partial frame must not be emitted, got %d", len(frames))
	}
	frames = scanner.feed(frameA[3:])
	if len(frames) != 1 || !bytes.Equal(frames[0], frameA) {
		t.Fatalf("frame A not recovered: %v", frames)
	}
	frames = scanner.feed(append(append([]byte{}, frameB...), frameA...))
	if len(frames) != 2 || !bytes.Equal(frames[0], frameB) || !bytes.Equal(frames[1], frameA) {
		t.Fatalf("back-to-back frames not recovered: %v", frames)
	}
}

func testJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encoding jpeg: %v", err)
	}
	return buffer.Bytes()
}

func TestSessionStreamsFramesAndAnalysis(t *testing.T) {
	frame := testJPEG(t, 32, 24)
	var analyzed atomic.Int64
	opts := SessionOptions{
		URL:           "rtsp://example/test",
		Analyzer: func(_ context.Context, img *engine.Image, _ []byte) (any, error) {
			analyzed.Add(1)
			return map[string]any{"width": img.Width, "height": img.Height}, nil
		},
		AnalysisEvery: 20 * time.Millisecond,
		IdleTimeout:   time.Hour,
		sourceFactory: func(ctx context.Context) (<-chan []byte, error) {
			return (&fakeSource{frames: [][]byte{frame}, interval: 30 * time.Millisecond}).start(ctx)
		},
	}
	session := newSession(opts)
	subscriber, detach := session.Subscribe()
	session.start(context.Background())
	defer func() {
		detach()
		session.Stop()
	}()

	deadline := time.After(5 * time.Second)
	for {
		snapshot := session.Snapshot()
		if snapshot.Status == StatusLive && analyzed.Load() > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("session never delivered analysis: %+v analyzed=%d", snapshot, analyzed.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}

	// state events arrive first; wait for an actual frame event
	for {
		var event Event
		select {
		case event = <-subscriber:
		case <-time.After(2 * time.Second):
			t.Fatalf("no frame event arrived")
		}
		if event.Kind == "state" {
			continue
		}
		if event.Seq == 0 || len(event.JPEG) == 0 {
			t.Fatalf("unexpected frame event: %+v", event)
		}
		result := event.Analysis.(map[string]any)
		if result["width"].(int) != 32 || result["height"].(int) != 24 {
			t.Fatalf("analyzer got wrong image: %v", result)
		}
		break
	}
}

func TestSessionExpiresWhenIdle(t *testing.T) {
	opts := SessionOptions{
		URL:           "rtsp://example/test",
		Analyzer:      nil,
		AnalysisEvery: time.Hour, // no analysis activity
		IdleTimeout:   120 * time.Millisecond,
		sourceFactory: func(ctx context.Context) (<-chan []byte, error) {
			return (&fakeSource{frames: [][]byte{testJPEG(t, 8, 8)}, interval: time.Hour}).start(ctx)
		},
	}
	session := newSession(opts)
	session.start(context.Background())
	defer session.Stop()

	deadline := time.After(3 * time.Second)
	for {
		if session.Snapshot().Status == StatusExpired {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("session never expired: %+v", session.Snapshot())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestManagerLifecycle(t *testing.T) {
	manager := NewManager(1)
	opts := SessionOptions{
		URL:           "rtsp://example/test",
		AnalysisEvery: time.Hour,
		IdleTimeout:   time.Hour,
		sourceFactory: func(ctx context.Context) (<-chan []byte, error) {
			return (&fakeSource{frames: [][]byte{testJPEG(t, 8, 8)}}).start(ctx)
		},
	}
	session, err := manager.Create(context.Background(), opts)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if manager.Count() != 1 {
		t.Fatalf("count = %d, want 1", manager.Count())
	}

	if _, err := manager.Create(context.Background(), opts); err != ErrTooManySessions {
		t.Fatalf("second session must hit the cap, got %v", err)
	}

	fetched, err := manager.Get(session.ID())
	if err != nil || fetched.ID() != session.ID() {
		t.Fatalf("get: %v", err)
	}

	if err := manager.Stop(session.ID()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := manager.Get(session.ID()); err != ErrSessionNotFound {
		t.Fatalf("stopped session must be gone, got %v", err)
	}
	if err := manager.Stop(session.ID()); err != ErrSessionNotFound {
		t.Fatalf("double stop must be ErrSessionNotFound, got %v", err)
	}
}
