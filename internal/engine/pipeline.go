package engine

import "sort"

// DetectorRunner feeds one RGB NCHW tensor of the given size through YuNet
// and returns the raw output values plus the number of candidate rows.
type DetectorRunner func(input []float32, width, height int) ([]float32, int, error)

// RecognizerRunner feeds one aligned 112x112 tensor through SFace and
// returns the raw embedding.
type RecognizerRunner func(input []float32) ([]float32, error)

// ExtractWith runs the backend-agnostic pipeline with the given inference
// runners. CGO backends pass their session runners here.
func ExtractWith(img *Image, opts Options, detect DetectorRunner, embed RecognizerRunner) ([]ExtractedFace, error) {
	return extractFaces(img, opts, detect, embed)
}

// extractFaces is the backend-agnostic core of Engine.Extract: downscale for
// detection, decode + NMS, then per-face quality gating, alignment and
// embedding. Faces are returned largest first, mirroring the enrollment
// preference of the reference pipeline.
func extractFaces(img *Image, opts Options, detect DetectorRunner, embed RecognizerRunner) ([]ExtractedFace, error) {
	scale := 1.0
	detectorImage := img
	if longest := max(img.Width, img.Height); longest > opts.DetectorMaxSide {
		scale = float64(opts.DetectorMaxSide) / float64(longest)
		width := max(1, int(float64(img.Width)*scale+0.5))
		height := max(1, int(float64(img.Height)*scale+0.5))
		// recompute the exact scale from the rounded size so boxes map back
		// onto the original image without drift
		scale = min(float64(width)/float64(img.Width), float64(height)/float64(img.Height))
		detectorImage = resizeArea(img, width, height)
	}

	output, rows, err := detect(buildNCHW(detectorImage, 0, 1.0/255.0), detectorImage.Width, detectorImage.Height)
	if err != nil {
		return nil, err
	}
	faces := decodeYuNet(output[:rows*YunetRowStride], scale, opts.DetectorThreshold, opts.NMSThreshold, opts.MaxDetections)

	extracted := make([]ExtractedFace, 0, len(faces))
	for _, face := range faces {
		quality := assessQuality(img, face, opts)
		item := ExtractedFace{Face: face, Quality: quality}
		if quality.embeddingAllowed() {
			aligned := alignFace(img, face)
			raw, embedErr := embed(buildNCHW(aligned, 127.5, 1.0/128.0))
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

func faceArea(face Face) float64 {
	w := float64(face.BBoxXYXY[2] - face.BBoxXYXY[0])
	h := float64(face.BBoxXYXY[3] - face.BBoxXYXY[1])
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}
