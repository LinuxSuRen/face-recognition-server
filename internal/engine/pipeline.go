package engine

import (
	"sort"
)

// DetectorRunner feeds one raw-BGR NCHW tensor of the given padded size
// through YuNet and returns the raw multi-head output.
type DetectorRunner func(input []float32, width, height int) (*YuNetHeads, error)

// RecognizerRunner feeds one aligned 112x112 raw-RGB tensor through SFace
// and returns the raw embedding.
type RecognizerRunner func(input []float32) ([]float32, error)

// ExtractWith runs the backend-agnostic pipeline with the given inference
// runners. CGO backends pass their session runners here.
func ExtractWith(img *Image, opts Options, detect DetectorRunner, embed RecognizerRunner) ([]ExtractedFace, error) {
	return extractFaces(img, opts, detect, embed)
}

// extractFaces is the backend-agnostic core of Engine.Extract, mirroring
// OpenCV's FaceDetectorYN/FaceRecognizerSF semantics:
//
//   - detector input is raw BGR (0-255) padded to a multiple of 32 —
//     either the model's fixed size (opencv_zoo 2023mar: 640x640) or the
//     image downscaled so its longer side is DetectorMaxSide
//   - recognizer input is raw RGB (0-255) after 5-point alignment
//
// Faces are returned largest first, mirroring the enrollment preference of
// the reference pipeline.
func extractFaces(img *Image, opts Options, detect DetectorRunner, embed RecognizerRunner) ([]ExtractedFace, error) {
	scaleX, scaleY := 1.0, 1.0
	var detectorInput *Image
	if opts.DetectorFixedWidth > 0 && opts.DetectorFixedHeight > 0 {
		// static-input exports (opencv_zoo 2023mar, 640x640): feed the
		// exact tensor the graph expects and map coordinates back per axis
		width, height := opts.DetectorFixedWidth, opts.DetectorFixedHeight
		detectorInput = resizeAny(img, width, height)
		scaleX = float64(img.Width) / float64(width)
		scaleY = float64(img.Height) / float64(height)
	} else {
		detectorInput = img
		if longest := max(img.Width, img.Height); longest > opts.DetectorMaxSide {
			scale := float64(opts.DetectorMaxSide) / float64(longest)
			width := max(1, int(float64(img.Width)*scale+0.5))
			height := max(1, int(float64(img.Height)*scale+0.5))
			scaleX = float64(img.Width) / float64(width)
			scaleY = float64(img.Height) / float64(height)
			detectorInput = resizeArea(img, width, height)
		}
	}
	padded := padToMultiple(detectorInput, 32)

	heads, err := detect(buildNCHWRawBGR(padded), padded.Width, padded.Height)
	if err != nil {
		return nil, err
	}
	faces := scaleFaces(
		decodeYuNetHeads(heads, padded.Width, padded.Height,
			opts.DetectorThreshold, opts.NMSThreshold, opts.MaxDetections),
		scaleX, scaleY)

	extracted := make([]ExtractedFace, 0, len(faces))
	for _, face := range faces {
		quality := assessQuality(img, face, opts)
		item := ExtractedFace{Face: face, Quality: quality}
		if quality.embeddingAllowed() {
			aligned := alignFace(img, face)
			raw, embedErr := embed(buildNCHWRawRGB(aligned))
			if embedErr == nil {
				if normalized := normalizeEmbedding(raw); normalized != nil {
					item.Embedding = normalized
				}
			}
		}
		extracted = append(extracted, item)
	}
	sort.SliceStable(extracted, func(i, j int) bool {
		return faceArea(extracted[i].Face) > faceArea(extracted[j].Face)
	})
	return extracted, nil
}

// padToMultiple zero-pads the right/bottom edges so both dims are
// multiples of divisor (OpenCV's padWithDivisor).
func padToMultiple(img *Image, divisor int) *Image {
	width := ((img.Width + divisor - 1) / divisor) * divisor
	height := ((img.Height + divisor - 1) / divisor) * divisor
	if width == img.Width && height == img.Height {
		return img
	}
	out := NewImage(width, height)
	for y := 0; y < img.Height; y++ {
		sourceRow := y * img.Width * 3
		targetRow := y * out.Width * 3
		copy(out.Pixels[targetRow:targetRow+img.Width*3], img.Pixels[sourceRow:sourceRow+img.Width*3])
	}
	return out
}

// ResizeForDetector resizes to an exact target (test/diagnostic helper for
// static-input models).
func ResizeForDetector(img *Image, width, height int) *Image {
	return resizeAny(img, width, height)
}

// BuildDetectorInput renders the raw BGR NCHW tensor the detector expects
// (integration tests).
func BuildDetectorInput(img *Image) []float32 {
	return buildNCHWRawBGR(img)
}

// buildNCHWRawBGR interleaves a BGR copy into a CHW tensor of raw values.
func buildNCHWRawBGR(img *Image) []float32 {
	plane := img.Width * img.Height
	tensor := make([]float32, plane*3)
	for i := 0; i < plane; i++ {
		tensor[i] = float32(img.Pixels[i*3+2])
		tensor[plane+i] = float32(img.Pixels[i*3+1])
		tensor[plane*2+i] = float32(img.Pixels[i*3])
	}
	return tensor
}

// buildNCHWRawRGB is the plain CHW layout of the raw RGB values.
func buildNCHWRawRGB(img *Image) []float32 {
	plane := img.Width * img.Height
	tensor := make([]float32, plane*3)
	for i := 0; i < plane; i++ {
		tensor[i] = float32(img.Pixels[i*3])
		tensor[plane+i] = float32(img.Pixels[i*3+1])
		tensor[plane*2+i] = float32(img.Pixels[i*3+2])
	}
	return tensor
}

func faceArea(face Face) float64 {
	w := float64(face.BBoxXYXY[2] - face.BBoxXYXY[0])
	h := float64(face.BBoxXYXY[3] - face.BBoxXYXY[1])
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}
