// Package config loads the server runtime configuration from the environment.
package config

import (
	"os"
	"strconv"
)

// Config carries every runtime setting of the server. All values have safe
// defaults so a bare `server` binary boots with the UI and a health endpoint.
type Config struct {
	// Addr is the HTTP listen address.
	Addr string
	// DBPath is the SQLite database file for employees and face templates.
	DBPath string
	// ModelsDir holds the YuNet / SFace ONNX files.
	ModelsDir string
	// KnownThreshold is the cosine similarity above which a face matches an
	// employee (OpenCV SFace convention: 0.36 cosine is a solid match).
	KnownThreshold float64
	// UnknownThreshold is the similarity below which a face is reported as
	// unknown instead of being associated with the best candidate.
	UnknownThreshold float64
	// MaxImageBytes bounds uploaded recognition/enrollment images.
	MaxImageBytes int64
}

// Load reads the configuration from environment variables, applying defaults
// for anything missing or malformed. Invalid numbers never abort startup:
// the default wins and the misconfiguration is visible in /healthz.
func Load() Config {
	return Config{
		Addr:             envString("FACE_RECOGNITION_ADDR", ":8080"),
		DBPath:           envString("FACE_RECOGNITION_DB", "data/faces.sqlite3"),
		ModelsDir:        envString("FACE_RECOGNITION_MODELS", "models"),
		KnownThreshold:   envFloat("FACE_RECOGNITION_KNOWN_THRESHOLD", 0.45),
		UnknownThreshold: envFloat("FACE_RECOGNITION_UNKNOWN_THRESHOLD", 0.30),
		MaxImageBytes:    int64(envFloat("FACE_RECOGNITION_MAX_IMAGE_MB", 20) * 1024 * 1024),
	}
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil {
		return fallback
	}
	return value
}
