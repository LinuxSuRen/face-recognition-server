//go:build cgo && integration

package onnxrt

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// samplePhotoURL is the classic OpenCV sample photo (a portrait with one
// clear frontal face); lena.jpg ships in the opencv repository forever.
const samplePhotoURL = "https://raw.githubusercontent.com/opencv/opencv/master/samples/data/lena.jpg"

// samplePhotoSHA lets the test detect truncated downloads.
const samplePhotoSHA = ""

// fetchSamplePhoto downloads the photo once into the local cache directory.
func fetchSamplePhoto(t *testing.T) ([]byte, error) {
	t.Helper()
	cacheDir := filepath.Join(os.TempDir(), "face-recognition-server-testdata")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	cache := filepath.Join(cacheDir, "lena.jpg")
	if content, err := os.ReadFile(cache); err == nil && len(content) > 10_000 {
		return content, nil
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Get(samplePhotoURL)
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
	if samplePhotoSHA != "" {
		sum := fmt.Sprintf("%x", sha256.Sum256(content))
		if sum != samplePhotoSHA {
			return nil, fmt.Errorf("sample photo checksum mismatch")
		}
	}
	if err := os.WriteFile(cache, content, 0o644); err != nil {
		return nil, err
	}
	return content, nil
}
