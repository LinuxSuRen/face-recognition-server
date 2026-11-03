package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LinuxSuRen/face-recognition-server/internal/auth"
	"github.com/LinuxSuRen/face-recognition-server/internal/config"
	"github.com/LinuxSuRen/face-recognition-server/internal/store"
)

// authHarness boots a router with session auth enabled and an admin.
type authHarness struct {
	handler http.Handler
	store   *store.Store
	cookie  string
}

func newAuthHarness(t *testing.T) *authHarness {
	t.Helper()
	faceStore, err := store.Open(filepath.Join(t.TempDir(), "auth.sqlite3"))
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(func() { faceStore.Close() })
	hash, _ := auth.HashPassword("admin-pw")
	if _, err := faceStore.CreateUser(t.Context(), "admin", "管理员", hash, auth.AdminRole, true); err != nil {
		t.Fatalf("creating admin: %v", err)
	}
	handler := NewRouter(Deps{
		Config:      config.Load(),
		Logger:      slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Store:       faceStore,
		AuthEnabled: true,
	})
	return &authHarness{handler: handler, store: faceStore}
}

func (h *authHarness) login(t *testing.T, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	request := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == auth.SessionCookie {
			h.cookie = cookie.Value
		}
	}
	return recorder
}

func (h *authHarness) do(t *testing.T, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if h.cookie != "" {
		request.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: h.cookie})
	}
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	return recorder
}

func TestSessionAuthGatesManagementAPI(t *testing.T) {
	h := newAuthHarness(t)

	// unauthenticated management call -> 401
	if response := h.do(t, "GET", "/api/v1/faces/employees", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.Code)
	}
	// public endpoints stay open
	if response := h.do(t, "GET", "/healthz", nil); response.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", response.Code)
	}
	if response := h.do(t, "GET", "/api/v1/auth/session", nil); response.Code != http.StatusOK {
		t.Fatalf("session status = %d", response.Code)
	}
	// bearer endpoint rejects missing token
	if response := h.do(t, "POST", "/api/v1/verify", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("verify without token status = %d", response.Code)
	}

	// wrong password
	if response := h.login(t, "admin", "nope"); response.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d", response.Code)
	}
	// good password
	if response := h.login(t, "admin", "admin-pw"); response.Code != http.StatusOK {
		t.Fatalf("login status = %d body %s", response.Code, response.Body.String())
	}
	if h.cookie == "" {
		t.Fatalf("login must set the session cookie")
	}
	// management call now passes
	if response := h.do(t, "GET", "/api/v1/faces/employees", nil); response.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d body %s", response.Code, response.Body.String())
	}

	// logout invalidates
	if response := h.do(t, "POST", "/api/v1/auth/logout", nil); response.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", response.Code)
	}
	h.cookie = ""
	if response := h.do(t, "GET", "/api/v1/faces/employees", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout status = %d", response.Code)
	}
}

func TestAdminOnlyRoutesRejectPlainUsers(t *testing.T) {
	h := newAuthHarness(t)
	hash, _ := auth.HashPassword("user-pw")
	if _, err := h.store.CreateUser(t.Context(), "bob", "Bob", hash, "user", true); err != nil {
		t.Fatalf("creating user: %v", err)
	}
	h.login(t, "bob", "user-pw")
	if response := h.do(t, "GET", "/api/v1/users", nil); response.Code != http.StatusForbidden {
		t.Fatalf("plain user users status = %d", response.Code)
	}
	if response := h.do(t, "GET", "/api/v1/tokens", nil); response.Code != http.StatusForbidden {
		t.Fatalf("plain user tokens status = %d", response.Code)
	}
	// faces stay usable for plain users
	if response := h.do(t, "GET", "/api/v1/faces/employees", nil); response.Code != http.StatusOK {
		t.Fatalf("plain user faces status = %d", response.Code)
	}
}

func TestTokenLifecycle(t *testing.T) {
	h := newAuthHarness(t)
	h.login(t, "admin", "admin-pw")

	// create a verify-scope token
	body, _ := json.Marshal(map[string]any{"name": "系统A", "scopes": []string{"verify"}})
	response := h.do(t, "POST", "/api/v1/tokens", body)
	if response.Code != http.StatusCreated {
		t.Fatalf("token create status = %d body %s", response.Code, response.Body.String())
	}
	var created struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.Token == "" {
		t.Fatalf("token not returned: %v %s", err, response.Body.String())
	}

	// list shows metadata only; the raw secret never repeats
	list := h.do(t, "GET", "/api/v1/tokens", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("token list status = %d", list.Code)
	}
	secret := strings.TrimPrefix(created.Token, "face_")
	if strings.Contains(list.Body.String(), secret) {
		t.Fatalf("raw token secret must never appear again")
	}

	// a verify call with the token passes auth (engine is nil here, so the
	// service answers 503 — the point is that auth itself succeeds)
	request := multipartRequest("/api/v1/verify", "image", tinyPNG(t), map[string]string{"subject": "E1"})
	request.Header.Set("Authorization", "Bearer "+created.Token)
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusUnauthorized || recorder.Code == http.StatusForbidden {
		t.Fatalf("valid token rejected: %d %s", recorder.Code, recorder.Body.String())
	}

	// delete revokes
	path := "/api/v1/tokens/" + strconv.FormatInt(created.ID, 10)
	if response := h.do(t, "DELETE", path, nil); response.Code != http.StatusNoContent {
		t.Fatalf("token delete status = %d body %s", response.Code, response.Body.String())
	}
	request = multipartRequest("/api/v1/verify", "image", tinyPNG(t), map[string]string{"subject": "E1"})
	request.Header.Set("Authorization", "Bearer "+created.Token)
	recorder = httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d", recorder.Code)
	}
}

func multipartRequest(path, field string, content []byte, fields map[string]string) *http.Request {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		_ = writer.WriteField(name, value)
	}
	part, _ := writer.CreateFormFile(field, "upload.png")
	_, _ = part.Write(content)
	writer.Close()
	request := httptest.NewRequest("POST", path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encoding png: %v", err)
	}
	return buffer.Bytes()
}
