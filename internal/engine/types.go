// Package engine implements the face recognition pipeline on top of an ONNX
// Runtime backend: YuNet detection decoding, NMS, landmark alignment, SFace
// embedding post-processing and enrollment quality assessment.
//
// Everything in this package is pure Go; the ONNX Runtime binding lives in
// the onnxrt sub-package behind a cgo build tag.
package engine

import "errors"

// ErrEngineUnavailable is returned when the binary was built without CGO or
// the ONNX Runtime shared library could not be loaded. The HTTP server keeps
// running and reports this condition to its clients.
var ErrEngineUnavailable = errors.New("inference engine unavailable: build with CGO and provide the onnxruntime shared library")

// Options configures an Engine.
type Options struct {
	// DetectorPath points to the YuNet ONNX file.
	DetectorPath string
	// RecognizerPath points to the SFace ONNX file.
	RecognizerPath string
	// DetectorThreshold is the YuNet confidence gate (default 0.8).
	DetectorThreshold float64
	// NMSThreshold suppresses overlapping detections (default 0.3).
	NMSThreshold float64
	// MaxDetections caps decoded faces per image.
	MaxDetections int
	// DetectorMaxSide downscales larger images before detection
	// (default 1280; 0 keeps the module default).
	DetectorMaxSide int
	// MinFaceSize is the hard minimum face edge in pixels (default 48).
	MinFaceSize int
	// MinFaceRatio is the minimum face-area / image-area ratio (default 0.015).
	MinFaceRatio float64
	// MinBlurVariance is the Laplacian variance gate (default 40).
	MinBlurVariance float64
}

// withDefaults fills unset options with the reference values used by the
// original detection-server pipeline.
func (o Options) withDefaults() Options {
	if o.DetectorThreshold <= 0 {
		o.DetectorThreshold = 0.8
	}
	if o.NMSThreshold <= 0 {
		o.NMSThreshold = 0.3
	}
	if o.MaxDetections <= 0 {
		o.MaxDetections = 50
	}
	if o.DetectorMaxSide <= 0 {
		o.DetectorMaxSide = 1280
	}
	if o.MinFaceSize <= 0 {
		o.MinFaceSize = 48
	}
	if o.MinFaceRatio <= 0 {
		o.MinFaceRatio = 0.015
	}
	if o.MinBlurVariance <= 0 {
		o.MinBlurVariance = 40.0
	}
	return o
}

// Face is one YuNet detection mapped back to original image coordinates.
type Face struct {
	// BBoxXYXY is the face box as left, top, right, bottom.
	BBoxXYXY [4]float32
	// Landmarks holds right eye, left eye, nose tip, right mouth corner and
	// left mouth corner, in that YuNet order.
	Landmarks [5][2]float32
	// Score is the detector confidence.
	Score float32
}

// ExtractedFace couples a detection with its quality assessment and, when
// quality allowed extraction, its SFace embedding.
type ExtractedFace struct {
	Face      Face
	Quality   Quality
	Embedding []float32
}

// Quality mirrors the enrollment gates of the reference pipeline.
type Quality struct {
	// Accepted is true when no gate rejected the face.
	Accepted bool
	// Score blends detector confidence, size and sharpness into [0,1].
	Score float64
	// Reasons lists failed gates: face_too_small, face_area_too_small,
	// image_too_blurry.
	Reasons []string
	// DetectorConfidence is the raw YuNet score.
	DetectorConfidence float64
	// BlurVariance is the resolution-normalized Laplacian variance.
	BlurVariance float64
	// FaceSize is min(box width, box height) in pixels.
	FaceSize int
}

// Engine runs detection and embedding over whole images.
type Engine interface {
	// Extract returns every usable face of the image, largest first. Faces
	// failing the hard gates (too small) carry no embedding.
	Extract(img *Image) ([]ExtractedFace, error)
	// ModelVersion identifies the recognizer weights, used to invalidate
	// templates enrolled by a different model.
	ModelVersion() string
	// Close releases the ONNX sessions.
	Close() error
}
