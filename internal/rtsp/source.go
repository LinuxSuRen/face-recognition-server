package rtsp

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// frameFeed couples a producing source with diagnostics access.
type frameFeed struct {
	frames <-chan []byte
	// stderrTail returns the last bytes the source wrote to stderr so
	// failures carry a reason (ffmpeg is run at warning log level).
	stderrTail func() string
}

// tailBuffer keeps the last keep bytes written to it.
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
	keep int
}

func newTailBuffer(keep int) *tailBuffer {
	return &tailBuffer{keep: keep}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.data = append(t.data, p...)
	if len(t.data) > t.keep {
		t.data = t.data[len(t.data)-t.keep:]
	}
	t.mu.Unlock()
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.data)
}

// ffmpegSource pulls an RTSP stream with ffmpeg and remuxes it to a
// concatenated JPEG stream on stdout. ffmpeg is the only process
// dependency; no linking or codec work happens in Go.
type ffmpegSource struct {
	url       string
	transport string
	binary    string
}

func newFFmpegSource(url, transport, binary string) *ffmpegSource {
	if transport == "" {
		transport = "tcp"
	}
	if binary == "" {
		binary = "ffmpeg"
	}
	return &ffmpegSource{url: url, transport: transport, binary: binary}
}

func (s *ffmpegSource) start(ctx context.Context) (frameFeed, error) {
	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-rtsp_transport", s.transport,
		"-i", s.url,
		"-an",
		"-vf", "fps=10",
		"-f", "mjpeg", "-",
	}
	command := exec.CommandContext(ctx, s.binary, args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return frameFeed{}, fmt.Errorf("creating ffmpeg pipe: %w", err)
	}
	stderr := newTailBuffer(2048)
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return frameFeed{}, fmt.Errorf("starting ffmpeg: %w (is %s installed?)", err, s.binary)
	}

	frames := make(chan []byte, 4)
	go func() {
		defer close(frames)
		defer stdout.Close()
		scanner := &frameScanner{}
		buffer := make([]byte, 64*1024)
		for {
			read, err := stdout.Read(buffer)
			if read > 0 {
				for _, frame := range scanner.feed(buffer[:read]) {
					select {
					case frames <- frame:
					case <-ctx.Done():
						command.Wait()
						return
					}
				}
			}
			if err != nil {
				command.Wait()
				return
			}
		}
	}()
	return frameFeed{frames: frames, stderrTail: stderr.String}, nil
}

// fakeSource yields canned frames for tests, either once or on a ticker.
type fakeSource struct {
	frames   [][]byte
	interval time.Duration
}

func (s *fakeSource) start(ctx context.Context) (frameFeed, error) {
	out := make(chan []byte, len(s.frames)+1)
	go func() {
		defer close(out)
		var ticker *time.Ticker
		if s.interval > 0 {
			ticker = time.NewTicker(s.interval)
			defer ticker.Stop()
		}
		for index := 0; index < len(s.frames); index++ {
			select {
			case out <- s.frames[index]:
			case <-ctx.Done():
				return
			}
			if ticker != nil {
				select {
				case <-ticker.C:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return frameFeed{frames: out, stderrTail: func() string { return "" }}, nil
}

// framesOnly adapts a bare channel (test helper).
func framesOnly(frames <-chan []byte) frameFeed {
	return frameFeed{frames: frames, stderrTail: func() string { return "" }}
}
