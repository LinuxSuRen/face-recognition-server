//go:build e2e

// End-to-end tests: they build and boot the real server binary with the
// real ONNX models, then drive it over HTTP the way the UI does. The RTSP
// test additionally loops a photo through a local ffmpeg RTSP server, so
// the whole chain (ffmpeg pull → frame scan → engine → SSE) is covered.
//
// Required environment (see the ci workflow):
//   - FACE_RECOGNITION_MODELS: directory with the two ONNX models
//   - FACE_RECOGNITION_ORT_LIB: onnxruntime shared library
//   - ffmpeg on PATH
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	serverBin string
	sampleImg []byte
)

func TestMain(m *testing.M) {
	var err error
	serverBin, err = buildServer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "building server: %v\n", err)
		os.Exit(1)
	}
	sampleImg, err = fetchSamplePhoto()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetching sample photo: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	os.Exit(code)
}

func buildServer() (string, error) {
	if cached := os.Getenv("FRS_SERVER_BIN"); cached != "" {
		return cached, nil
	}
	binary := filepath.Join(os.TempDir(), "face-recognition-server-e2e", "server")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(binary); err == nil {
		return binary, nil
	}
	command := exec.Command("go", "build", "-o", binary, "./cmd/server")
	command.Dir = repoRoot()
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return "", err
	}
	return binary, nil
}

func repoRoot() string {
	if root := os.Getenv("FRS_REPO_ROOT"); root != "" {
		return root
	}
	return "."
}

func fetchSamplePhoto() ([]byte, error) {
	cache := filepath.Join(os.TempDir(), "face-recognition-server-testdata", "lena.jpg")
	if content, err := os.ReadFile(cache); err == nil && len(content) > 10_000 {
		return content, nil
	}
	response, err := http.Get("https://raw.githubusercontent.com/opencv/opencv/master/samples/data/lena.jpg")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sample photo HTTP %d", response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if len(content) < 10_000 {
		return nil, fmt.Errorf("sample photo suspiciously small: %d", len(content))
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(cache, content, 0o644); err != nil {
		return nil, err
	}
	return content, nil
}

// serverProcess is one running instance of the server binary.
type serverProcess struct {
	baseURL string
	command *exec.Cmd
	stderr  bytes.Buffer
	cookie  string
}

const e2eAdminPassword = "e2e-admin-pass"

func startServer(t *testing.T) *serverProcess {
	t.Helper()
	port := freePort(t)
	dir := t.TempDir()
	command := exec.Command(serverBin)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	command.Env = append(os.Environ(),
		"FACE_RECOGNITION_ADDR=127.0.0.1:"+port,
		"FACE_RECOGNITION_DB="+filepath.Join(dir, "faces.sqlite3"),
		"FACE_RECOGNITION_ADMIN_PASSWORD="+e2eAdminPassword,
	)
	if err := command.Start(); err != nil {
		t.Fatalf("starting server: %v", err)
	}
	process := &serverProcess{
		baseURL: "http://127.0.0.1:" + port,
		command: command,
		stderr:  stderr,
	}
	t.Cleanup(func() {
		command.Process.Kill()
		command.Wait()
	})
	waitHealthy(t, process)
	loginServer(t, process)
	return process
}

// loginServer authenticates the bootstrap admin so management calls pass
// the session guard.
func loginServer(t *testing.T, process *serverProcess) {
	t.Helper()
	body := fmt.Sprintf(`{"username":"admin","password":%q}`, e2eAdminPassword)
	response, err := http.Post(process.baseURL+"/api/v1/auth/login",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d: session auth blocks the e2e flow", response.StatusCode)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "frs_session" {
			process.cookie = cookie.Value
		}
	}
	if process.cookie == "" {
		t.Fatalf("login did not set the session cookie")
	}
}

func waitHealthy(t *testing.T, process *serverProcess) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(process.baseURL + "/healthz")
		if err == nil {
			raw, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			var payload struct {
				Status string `json:"status"`
				Engine struct {
					Ready  bool   `json:"ready"`
					Reason string `json:"reason"`
				} `json:"engine"`
			}
			err = json.Unmarshal(raw, &payload)
			if err == nil && payload.Engine.Ready {
				return
			}
			if err == nil && payload.Status != "" && !payload.Engine.Ready {
				t.Fatalf("engine not ready (healthz: %s):\nserver stderr:\n%s",
					string(raw), process.stderr.String())
			}
			if readErr != nil {
				t.Fatalf("reading healthz: %v", readErr)
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("server never became healthy:\n%s", process.stderr.String())
}

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving port: %v", err)
	}
	defer listener.Close()
	return fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
}

func postJSON(t *testing.T, server *serverProcess, method, path string, body any) (int, map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	request, err := http.NewRequest(method, server.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	attachSession(request, server)
	return doRequest(t, request)
}

func postFile(t *testing.T, server *serverProcess, path, field string, content []byte) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, "sample.jpg")
	if err != nil {
		t.Fatalf("creating form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("writing form file: %v", err)
	}
	writer.Close()
	request, err := http.NewRequest("POST", server.baseURL+path, &body)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	attachSession(request, server)
	return doRequest(t, request)
}

// attachSession carries the admin cookie on management calls.
func attachSession(request *http.Request, server *serverProcess) {
	if server.cookie != "" {
		request.AddCookie(&http.Cookie{Name: "frs_session", Value: server.cookie})
	}
}

func doRequest(t *testing.T, request *http.Request) (int, map[string]any) {
	t.Helper()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request %s failed: %v", request.URL, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	var payload map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decoding response %q: %v", string(raw), err)
		}
	}
	return response.StatusCode, payload
}

func TestE2EFacesFlow(t *testing.T) {
	server := startServer(t)

	status, employee := postJSON(t, server, "POST", "/api/v1/faces/employees",
		map[string]any{"employee_no": "E2E001", "name": "端到端员工"})
	if status != http.StatusCreated {
		t.Fatalf("create employee status = %d: %v", status, employee)
	}
	employeeID := int64(employee["id"].(float64))

	status, sample := postFile(t, server,
		fmt.Sprintf("/api/v1/faces/employees/%d/samples", employeeID),
		"file", sampleImg)
	if status != http.StatusCreated {
		t.Fatalf("sample upload status = %d: %v", status, sample)
	}
	if sample["model_version"] == "" || sample["has_image"] != true {
		t.Fatalf("unexpected sample payload: %v", sample)
	}

	status, recognition := postFile(t, server, "/api/v1/faces/recognize",
		"image", sampleImg)
	if status != http.StatusOK {
		t.Fatalf("recognize status = %d: %v", status, recognition)
	}
	faces := recognition["faces"].([]any)
	if len(faces) == 0 {
		t.Fatalf("no faces recognized: %v", recognition)
	}
	first := faces[0].(map[string]any)
	if first["recognition_status"] != "known" || first["name"] != "端到端员工" {
		t.Fatalf("expected known 端到端员工, got %v", first)
	}
	summary := recognition["summary"].(map[string]any)
	if summary["known"].(float64) != 1 {
		t.Fatalf("unexpected summary: %v", summary)
	}
}

func TestE2ERTSPRecognize(t *testing.T) {
	server := startServer(t)

	// enroll the sample face first so the live stream can be recognized
	status, employee := postJSON(t, server, "POST", "/api/v1/faces/employees",
		map[string]any{"employee_no": "E2E002", "name": "实时员工"})
	if status != http.StatusCreated {
		t.Fatalf("create employee status = %d: %v", status, employee)
	}
	employeeID := int64(employee["id"].(float64))
	if status, _ = postFile(t, server,
		fmt.Sprintf("/api/v1/faces/employees/%d/samples", employeeID),
		"file", sampleImg); status != http.StatusCreated {
		t.Fatalf("sample upload status = %d", status)
	}

	// a MediaMTX instance serves the looped photo as an H.264 RTSP stream;
	// ffmpeg listen-mode is unreliable and MJPEG-over-RTSP gets torn down,
	// so the test uses the same media gateway real deployments use
	mediamtx := findMediaMTX(t)
	rtspPort := freePort(t)
	// Windows reserved-port ranges can reject UDP binds; retry with fresh
	// ports until the RTSP listener actually opens
	var gateway *exec.Cmd
	var gatewayLog *syncBuffer
	var stream *exec.Cmd
	var streamErr *bytes.Buffer
	for attempt := 0; attempt < 10; attempt++ {
		candidateRTP := mustPort(t)
		candidateRTP -= candidateRTP % 2
		config := fmt.Sprintf("rtspAddress: 127.0.0.1:%s\nrtpAddress: 127.0.0.1:%d\n"+
			"rtcpAddress: 127.0.0.1:%d\nrtmp: no\nhls: no\nwebrtc: no\n"+
			"paths:\n  all:\n    source: publisher\n",
			rtspPort, candidateRTP, candidateRTP+1)
		configPath := filepath.Join(t.TempDir(), "mediamtx.yml")
		if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
			t.Fatalf("writing mediamtx config: %v", err)
		}
		candidate := exec.Command(mediamtx, configPath)
		candidateLog := &syncBuffer{}
		candidate.Stdout, candidate.Stderr = candidateLog, candidateLog
		if err := candidate.Start(); err != nil {
			t.Fatalf("starting mediamtx: %v", err)
		}
		if candidateLog.waitFor("RTSP] listener opened", 5*time.Second) {
			gateway, gatewayLog = candidate, candidateLog
			t.Cleanup(func() {
				gateway.Process.Kill()
				gateway.Wait()
			})
			break
		}
		candidate.Process.Kill()
		candidate.Wait()
	}
	if gateway == nil {
		t.Skip("mediamtx could not bind (UDP port ranges exhausted)")
	}

	stream = exec.Command("ffmpeg",
		"-loglevel", "warning",
		"-re", "-loop", "1", "-i", filepath.Join(os.TempDir(),
			"face-recognition-server-testdata", "lena.jpg"),
		"-vf", "fps=5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-preset", "ultrafast", "-tune", "zerolatency", "-g", "10",
		"-f", "rtsp", "-t", "600",
		"rtsp://127.0.0.1:"+rtspPort+"/e2e",
	)
	streamErr = &bytes.Buffer{}
	stream.Stderr = streamErr
	if err := stream.Start(); err != nil {
		t.Fatalf("starting rtsp push (ffmpeg installed?): %v", err)
	}
	t.Cleanup(func() {
		stream.Process.Kill()
		stream.Wait()
	})
	if !gatewayLog.waitFor("is publishing to path", 20*time.Second) {
		t.Fatalf("rtsp push never published; mediamtx:\n%s\nffmpeg:\n%s",
			gatewayLog.String(), streamErr.String())
	}

	status, _ = postJSON(t, server, "PUT", "/api/v1/rtsp/configuration",
		map[string]any{"url": "rtsp://127.0.0.1:" + rtspPort + "/e2e", "transport": "tcp"})
	if status != http.StatusOK {
		t.Fatalf("rtsp configuration status = %d", status)
	}

	status, session := postJSON(t, server, "POST", "/api/v1/rtsp/sessions", map[string]any{})
	if status != http.StatusCreated {
		t.Fatalf("session create status = %d: %v", status, session)
	}
	sessionID := session["session_id"].(string)
	t.Cleanup(func() {
		request, _ := http.NewRequest("DELETE", server.baseURL+"/api/v1/rtsp/sessions/"+sessionID, nil)
		attachSession(request, server)
		http.DefaultClient.Do(request)
	})

	recognized := waitForKnownFace(t, server,
		"/api/v1/rtsp/sessions/"+sessionID+"/events", 90*time.Second)
	if !recognized {
		t.Fatalf("live stream never produced a known face; mediamtx:\n%s\nffmpeg:\n%s",
			gatewayLog.String(), streamErr.String())
	}
}

// findMediaMTX resolves the mediamtx binary or skips the test.
func findMediaMTX(t *testing.T) string {
	t.Helper()
	if custom := os.Getenv("FRS_MEDIAMTX_BIN"); custom != "" {
		if _, err := os.Stat(custom); err == nil {
			return custom
		}
	}
	if path, err := exec.LookPath("mediamtx"); err == nil {
		return path
	}
	t.Skip("mediamtx not found; set FRS_MEDIAMTX_BIN or add it to PATH")
	return ""
}

// mustPort reserves a free port (numbers only; no listener kept).
func mustPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// syncBuffer is a mutex-guarded buffer that can be awaited for a pattern.
type syncBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

func (b *syncBuffer) waitFor(pattern string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(b.String(), pattern) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return strings.Contains(b.String(), pattern)
}

// waitForKnownFace consumes the SSE feed until a frame reports a known face.
func waitForKnownFace(t *testing.T, server *serverProcess, path string, timeout time.Duration) bool {
	t.Helper()
	request, err := http.NewRequest("GET", server.baseURL+path, nil)
	if err != nil {
		t.Fatalf("building SSE request: %v", err)
	}
	attachSession(request, server)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	request = request.WithContext(ctx)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("opening SSE stream: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d", response.StatusCode)
	}

	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return false
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line[len("data: "):]), &payload); err != nil {
			continue
		}
		if payload["type"] != "frame" {
			continue
		}
		faces, ok := payload["faces"].([]any)
		if !ok {
			continue
		}
		for _, item := range faces {
			face, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if face["recognition_status"] == "known" {
				return true
			}
		}
	}
}
