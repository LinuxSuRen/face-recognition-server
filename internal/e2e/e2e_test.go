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
}

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
	return process
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

func postJSON(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	request, err := http.NewRequest(method, url, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	return doRequest(t, request)
}

func postFile(t *testing.T, url, field string, content []byte) (int, map[string]any) {
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
	request, err := http.NewRequest("POST", url, &body)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return doRequest(t, request)
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

	status, employee := postJSON(t, "POST", server.baseURL+"/api/v1/faces/employees",
		map[string]any{"employee_no": "E2E001", "name": "端到端员工"})
	if status != http.StatusCreated {
		t.Fatalf("create employee status = %d: %v", status, employee)
	}
	employeeID := int64(employee["id"].(float64))

	status, sample := postFile(t,
		fmt.Sprintf("%s/api/v1/faces/employees/%d/samples", server.baseURL, employeeID),
		"file", sampleImg)
	if status != http.StatusCreated {
		t.Fatalf("sample upload status = %d: %v", status, sample)
	}
	if sample["model_version"] == "" || sample["has_image"] != true {
		t.Fatalf("unexpected sample payload: %v", sample)
	}

	status, recognition := postFile(t, server.baseURL+"/api/v1/faces/recognize",
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
	status, employee := postJSON(t, "POST", server.baseURL+"/api/v1/faces/employees",
		map[string]any{"employee_no": "E2E002", "name": "实时员工"})
	if status != http.StatusCreated {
		t.Fatalf("create employee status = %d: %v", status, employee)
	}
	employeeID := int64(employee["id"].(float64))
	if status, _ = postFile(t,
		fmt.Sprintf("%s/api/v1/faces/employees/%d/samples", server.baseURL, employeeID),
		"file", sampleImg); status != http.StatusCreated {
		t.Fatalf("sample upload status = %d", status)
	}

	// a local ffmpeg RTSP server looping the photo
	rtspPort := freePort(t)
	stream := exec.Command("ffmpeg",
		"-loglevel", "warning",
		"-re", "-stream_loop", "-1", "-i", filepath.Join(os.TempDir(),
			"face-recognition-server-testdata", "lena.jpg"),
		"-vf", "fps=5",
		"-c:v", "mjpeg", "-q:v", "5",
		"-f", "rtsp", "-rtsp_flags", "listen",
		"rtsp://127.0.0.1:"+rtspPort+"/e2e",
	)
	var streamErr bytes.Buffer
	stream.Stderr = &streamErr
	if err := stream.Start(); err != nil {
		t.Fatalf("starting rtsp server (ffmpeg installed?): %v", err)
	}
	t.Cleanup(func() {
		stream.Process.Kill()
		stream.Wait()
	})

	status, _ = postJSON(t, "PUT", server.baseURL+"/api/v1/rtsp/configuration",
		map[string]any{"url": "rtsp://127.0.0.1:" + rtspPort + "/e2e", "transport": "tcp"})
	if status != http.StatusOK {
		t.Fatalf("rtsp configuration status = %d", status)
	}

	status, session := postJSON(t, "POST", server.baseURL+"/api/v1/rtsp/sessions", map[string]any{})
	if status != http.StatusCreated {
		t.Fatalf("session create status = %d: %v", status, session)
	}
	sessionID := session["session_id"].(string)
	t.Cleanup(func() {
		request, _ := http.NewRequest("DELETE", server.baseURL+"/api/v1/rtsp/sessions/"+sessionID, nil)
		http.DefaultClient.Do(request)
	})

	recognized := waitForKnownFace(t, server.baseURL+"/api/v1/rtsp/sessions/"+sessionID+"/events", 90*time.Second)
	if !recognized {
		t.Fatalf("live stream never produced a known face; ffmpeg:\n%s", streamErr.String())
	}
}

// waitForKnownFace consumes the SSE feed until a frame reports a known face.
func waitForKnownFace(t *testing.T, eventsURL string, timeout time.Duration) bool {
	t.Helper()
	request, err := http.NewRequest("GET", eventsURL, nil)
	if err != nil {
		t.Fatalf("building SSE request: %v", err)
	}
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
