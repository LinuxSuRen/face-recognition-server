package engine

// blurReferenceSize normalizes face crops to a fixed resolution before the
// Laplacian variance, so a sharp 4K photo is not penalized merely because
// its crop is large (same value as the reference pipeline).
const blurReferenceSize = 224

// embeddingBlockingReasons fail hard gates that alignment cannot rescue.
var embeddingBlockingReasons = map[string]bool{
	"face_too_small":      true,
	"face_area_too_small": true,
}

// assessQuality mirrors the reference enrollment gates.
func assessQuality(img *Image, face Face, opts Options) Quality {
	x1, y1, x2, y2 := face.BBoxXYXY[0], face.BBoxXYXY[1], face.BBoxXYXY[2], face.BBoxXYXY[3]
	cropX1 := clampI(int(x1), 0, img.Width-1)
	cropY1 := clampI(int(y1), 0, img.Height-1)
	cropX2 := max(cropX1+1, clampI(int(x2), 0, img.Width))
	cropY2 := max(cropY1+1, clampI(int(y2), 0, img.Height))

	var blurVariance float64
	crop := img.Crop(cropX1, cropY1, cropX2, cropY2)
	if crop.Width > 0 && crop.Height > 0 {
		normalized := crop
		if crop.Width != blurReferenceSize || crop.Height != blurReferenceSize {
			if max(crop.Width, crop.Height) > blurReferenceSize {
				normalized = resizeArea(crop, blurReferenceSize, blurReferenceSize)
			} else {
				normalized = resizeBicubic(crop, blurReferenceSize, blurReferenceSize)
			}
		}
		blurVariance = laplacianVariance(normalized)
	}

	faceSize := min(cropX2-cropX1, cropY2-cropY1)
	faceRatio := float64((x2-x1)*(y2-y1)) / float64(img.Width*img.Height)

	var reasons []string
	if faceSize < opts.MinFaceSize {
		reasons = append(reasons, "face_too_small")
	}
	if faceRatio < opts.MinFaceRatio {
		reasons = append(reasons, "face_area_too_small")
	}
	if blurVariance < opts.MinBlurVariance {
		reasons = append(reasons, "image_too_blurry")
	}

	sizeScore := min(1.0, float64(faceSize)/max(float64(opts.MinFaceSize), 1.0))
	blurScore := min(1.0, blurVariance/max(opts.MinBlurVariance, 1.0))
	confidence := float64(face.Score)
	return Quality{
		Accepted:           len(reasons) == 0,
		Score:              round4((confidence + sizeScore + blurScore) / 3.0),
		Reasons:            reasons,
		DetectorConfidence: round4(confidence),
		BlurVariance:       round2(blurVariance),
		FaceSize:           faceSize,
	}
}

// embeddingAllowed reports whether an embedding should be extracted despite
// soft quality failures: blur alone does not block recognition because SFace
// stays discriminative; size failures do.
func (q Quality) embeddingAllowed() bool {
	for _, reason := range q.Reasons {
		if embeddingBlockingReasons[reason] {
			return false
		}
	}
	return true
}

// laplacianVariance applies the 4-neighbour Laplacian kernel with reflected
// borders (OpenCV BORDER_REFLECT_101) and returns the variance of the
// response in float64, like cv2.Laplacian(gray, cv2.CV_64F).var().
func laplacianVariance(img *Image) float64 {
	width, height := img.Width, img.Height
	if width < 3 || height < 3 {
		return 0
	}
	var sum, sumSquares float64
	n := float64(width * height)
	for y := 0; y < height; y++ {
		up, down := reflect101(y-1, height), reflect101(y+1, height)
		for x := 0; x < width; x++ {
			left, right := reflect101(x-1, width), reflect101(x+1, width)
			value := grayAt(img, x, up) + grayAt(img, x, down) +
				grayAt(img, left, y) + grayAt(img, right, y) -
				4*grayAt(img, x, y)
			sum += value
			sumSquares += value * value
		}
	}
	mean := sum / n
	return sumSquares/n - mean*mean
}

func reflect101(index, length int) int {
	if index < 0 {
		return -index
	}
	if index >= length {
		return 2*length - 2 - index
	}
	return index
}

func clampI(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func round4(v float64) float64 { return float64(int64(v*10000+sign(v)*0.5)) / 10000 }

func round2(v float64) float64 { return float64(int64(v*100+sign(v)*0.5)) / 100 }

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}
