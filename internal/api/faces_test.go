package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LinuxSuRen/face-recognition-server/internal/config"
	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
	"github.com/LinuxSuRen/face-recognition-server/internal/store"
)

// fakeEngine feeds canned faces to the handlers so the API layer is testable
// without ONNX Runtime.
type fakeEngine struct {
	faces []engine.ExtractedFace
	err   error
}

func (f *fakeEngine) Extract(_ *engine.Image) ([]engine.ExtractedFace, error) {
	return f.faces, f.err
}

func (f *fakeEngine) ModelVersion() string { return "sface:test" }

func (f *fakeEngine) Close() error { return nil }

func testImagePNG(t *testing.T, size int) []byte {
	t.Helper()
	img := engine.NewImage(size, size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if (x/2+y/2)%2 == 0 {
				img.Set(x, y, 255, 255, 255)
			}
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img.ToGoImage()); err != nil {
		t.Fatalf("encoding png: %v", err)
	}
	return buffer.Bytes()
}

func goodFace() engine.ExtractedFace {
	return engine.ExtractedFace{
		Face: engine.Face{
			BBoxXYXY: [4]float32{50, 50, 350, 350},
			Score:    0.96,
		},
		Quality: engine.Quality{Accepted: true, Score: 0.93, FaceSize: 300, BlurVariance: 500},
		Embedding: []float32{1, 0, 0, 0},
	}
}

func newTestRouter(t *testing.T, faceEngine engine.Engine) (http.Handler, *store.Store) {
	t.Helper()
	faceStore, err := store.Open(filepath.Join(t.TempDir(), "faces.sqlite3"))
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(func() { faceStore.Close() })
	return NewRouter(Deps{
		Config: config.Load(),
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Store:  faceStore,
		Engine: faceEngine,
	}), faceStore
}

func doJSON(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshaling body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func doUpload(t *testing.T, handler http.Handler, method, path, field string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, "upload.png")
	if err != nil {
		t.Fatalf("creating form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("writing form file: %v", err)
	}
	writer.Close()
	request := httptest.NewRequest(method, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decoding response %q: %v", recorder.Body.String(), err)
	}
	return payload
}

func TestEmployeeEndpoints(t *testing.T) {
	handler, _ := newTestRouter(t, nil)

	created := doJSON(t, handler, "POST", "/api/v1/faces/employees",
		map[string]any{"employee_no": " E001 ", "name": "张三", "department": "巡检班"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", created.Code, created.Body.String())
	}
	employee := decodeBody(t, created)
	if employee["employee_no"] != "E001" {
		t.Fatalf("employee_no must be trimmed: %+v", employee)
	}
	id := fmt.Sprintf("%.0f", employee["id"])

	duplicate := doJSON(t, handler, "POST", "/api/v1/faces/employees",
		map[string]any{"employee_no": "E001", "name": "重复"})
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d", duplicate.Code)
	}

	blank := doJSON(t, handler, "POST", "/api/v1/faces/employees",
		map[string]any{"employee_no": "  ", "name": "x"})
	if blank.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank status = %d", blank.Code)
	}

	unknownField := doJSON(t, handler, "POST", "/api/v1/faces/employees",
		map[string]any{"employee_no": "E9", "name": "x", "hobby": "cycling"})
	if unknownField.Code != http.StatusBadRequest {
		t.Fatalf("unknown field must be 400, got %d", unknownField.Code)
	}

	list := doJSON(t, handler, "GET", "/api/v1/faces/employees?page=1&page_size=5", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d", list.Code)
	}
	page := decodeBody(t, list)
	if page["page"].(float64) != 1 || page["page_size"].(float64) != 5 ||
		page["total"].(float64) != 1 || len(page["data"].([]any)) != 1 {
		t.Fatalf("pagination contract violated: %+v", page)
	}

	patched := doJSON(t, handler, "PATCH", "/api/v1/faces/employees/"+id,
		map[string]any{"department": ""})
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status = %d", patched.Code)
	}
	if decodeBody(t, patched)["department"] != "" {
		t.Fatalf("department must be clearable")
	}

	missing := doJSON(t, handler, "GET", "/api/v1/faces/employees/999", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing employee status = %d", missing.Code)
	}

	deleted := doJSON(t, handler, "DELETE", "/api/v1/faces/employees/"+id, nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleted.Code)
	}
}

func TestSampleUploadAndRecognition(t *testing.T) {
	fake := &fakeEngine{faces: []engine.ExtractedFace{goodFace()}}
	handler, _ := newTestRouter(t, fake)

	created := doJSON(t, handler, "POST", "/api/v1/faces/employees",
		map[string]any{"employee_no": "E001", "name": "张三"})
	employee := decodeBody(t, created)
	id := fmt.Sprintf("%.0f", employee["id"])

	upload := doUpload(t, handler, "POST", "/api/v1/faces/employees/"+id+"/samples",
		"file", testImagePNG(t, 64))
	if upload.Code != http.StatusCreated {
		t.Fatalf("sample upload status = %d, body %s", upload.Code, upload.Body.String())
	}
	sample := decodeBody(t, upload)
	if sample["model_version"] != "sface:test" || sample["has_image"] != true {
		t.Fatalf("unexpected sample payload: %+v", sample)
	}

	// an inconsistent second sample (orthogonal embedding) must be rejected
	fake.faces = []engine.ExtractedFace{{
		Face:      goodFace().Face,
		Quality:   goodFace().Quality,
		Embedding: []float32{0, 1, 0, 0},
	}}
	rejected := doUpload(t, handler, "POST", "/api/v1/faces/employees/"+id+"/samples",
		"file", testImagePNG(t, 64))
	if rejected.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rejected.Body.String(), "does not match") {
		t.Fatalf("inconsistent sample must be 422, got %d %s", rejected.Code, rejected.Body.String())
	}

	// recognition: known face + unknown face
	fake.faces = []engine.ExtractedFace{
		goodFace(),
		{Face: goodFace().Face, Quality: goodFace().Quality, Embedding: []float32{0, 0, 0.2, 0.98}},
	}
	recognized := doUpload(t, handler, "POST", "/api/v1/faces/recognize",
		"image", testImagePNG(t, 64))
	if recognized.Code != http.StatusOK {
		t.Fatalf("recognize status = %d, body %s", recognized.Code, recognized.Body.String())
	}
	payload := decodeBody(t, recognized)
	faces := payload["faces"].([]any)
	if len(faces) != 2 {
		t.Fatalf("expected 2 faces, got %d", len(faces))
	}
	first := faces[0].(map[string]any)
	if first["recognition_status"] != "known" || first["name"] != "张三" {
		t.Fatalf("first face must be known 张三: %+v", first)
	}
	second := faces[1].(map[string]any)
	if second["recognition_status"] != "unknown" || second["reason"] != "below_threshold" {
		t.Fatalf("second face must be below_threshold: %+v", second)
	}
	summary := payload["summary"].(map[string]any)
	if summary["count"].(float64) != 2 || summary["known"].(float64) != 1 || summary["unknown"].(float64) != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	// sample image endpoint serves the stored bytes
	imagePath := fmt.Sprintf("/api/v1/faces/employees/%s/samples/%s/image", id,
		fmt.Sprintf("%.0f", sample["id"]))
	imageResponse := httptest.NewRecorder()
	request := httptest.NewRequest("GET", imagePath, nil)
	handler.ServeHTTP(imageResponse, request)
	if imageResponse.Code != http.StatusOK || imageResponse.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("sample image status = %d type = %s", imageResponse.Code,
			imageResponse.Header().Get("Content-Type"))
	}

	// deletion
	deleteRequest := doJSON(t, handler, "DELETE",
		fmt.Sprintf("/api/v1/faces/employees/%s/samples/%s", id, fmt.Sprintf("%.0f", sample["id"])), nil)
	if deleteRequest.Code != http.StatusNoContent {
		t.Fatalf("sample delete status = %d", deleteRequest.Code)
	}
}

func TestDegradedModeReportsUnavailable(t *testing.T) {
	handler, _ := newTestRouter(t, nil)

	created := doJSON(t, handler, "POST", "/api/v1/faces/employees",
		map[string]any{"employee_no": "E001", "name": "张三"})
	id := fmt.Sprintf("%.0f", decodeBody(t, created)["id"])

	upload := doUpload(t, handler, "POST", "/api/v1/faces/employees/"+id+"/samples",
		"file", testImagePNG(t, 64))
	if upload.Code != http.StatusServiceUnavailable {
		t.Fatalf("enrollment without engine must be 503, got %d", upload.Code)
	}

	recognized := doUpload(t, handler, "POST", "/api/v1/faces/recognize",
		"image", testImagePNG(t, 64))
	if recognized.Code != http.StatusServiceUnavailable {
		t.Fatalf("recognition without engine must be 503, got %d", recognized.Code)
	}

	health := doJSON(t, handler, "GET", "/healthz", nil)
	payload := decodeBody(t, health)
	if payload["status"] != "degraded" {
		t.Fatalf("healthz must report degraded: %+v", payload)
	}
}

func TestUploadValidation(t *testing.T) {
	fake := &fakeEngine{faces: []engine.ExtractedFace{goodFace()}}
	handler, _ := newTestRouter(t, fake)

	missingEmployee := doUpload(t, handler, "POST", "/api/v1/faces/employees/999/samples",
		"file", testImagePNG(t, 64))
	if missingEmployee.Code != http.StatusNotFound {
		t.Fatalf("upload for missing employee must be 404, got %d", missingEmployee.Code)
	}

	garbage := doUpload(t, handler, "POST", "/api/v1/faces/recognize",
		"image", []byte("definitely not an image"))
	if garbage.Code != http.StatusBadRequest {
		t.Fatalf("garbage upload must be 400, got %d", garbage.Code)
	}

	// no face detected maps to 422
	fake.faces = nil
	noFace := doUpload(t, handler, "POST", "/api/v1/faces/recognize", "image", testImagePNG(t, 64))
	if noFace.Code != http.StatusOK {
		t.Fatalf("recognize without faces must still be 200 with an empty list, got %d", noFace.Code)
	}
	if faces := decodeBody(t, noFace)["faces"].([]any); len(faces) != 0 {
		t.Fatalf("expected empty faces list")
	}
}
