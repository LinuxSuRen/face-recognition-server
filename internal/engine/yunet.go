package engine

import (
	"math"
	"sort"
)

// YunetRowStride is the number of float32 values per decoded face row
// (x, y, w, h, five landmark pairs, score) — the shape downstream layers
// and the reference pipeline exchange.
const YunetRowStride = 15

// yunetStrides are the anchor strides of the YuNet head (cls/obj/bbox/kps
// outputs exist per stride), mirroring OpenCV's FaceDetectorYN.
var yunetStrides = []int{8, 16, 32}

// YuNetHeads holds the raw multi-head detector output of the opencv_zoo
// YuNet export: per stride, per-anchor classification and objectness
// scores, four box deltas and ten landmark deltas, all in grid units.
type YuNetHeads struct {
	// Strides lists the anchor strides present (8, 16, 32).
	Strides []int
	// Cls and Obj hold one score per anchor per stride.
	Cls map[int][]float32
	Obj map[int][]float32
	// Bbox holds 4 values per anchor per stride: center offsets in grid
	// cells plus log-scale width/height.
	Bbox map[int][]float32
	// Kps holds 10 values per anchor per stride: landmark offsets in grid
	// cells.
	Kps map[int][]float32
}

// NewYuNetHeads allocates the head container.
func NewYuNetHeads() *YuNetHeads {
	return &YuNetHeads{
		Strides: yunetStrides,
		Cls:     map[int][]float32{},
		Obj:     map[int][]float32{},
		Bbox:    map[int][]float32{},
		Kps:     map[int][]float32{},
	}
}

// decodeYuNetHeads turns the raw heads into faces in detector-input
// coordinates, mirroring OpenCV's FaceDetectorYN post-processing exactly:
//
//	score = sqrt(clamp(cls) * clamp(obj))
//	cx    = (column + bbox[0]) * stride
//	w     = exp(bbox[2]) * stride        (same for y/h)
//	kps   = (kps + column/row) * stride
//
// followed by the confidence gate and NMS. paddedWidth/paddedHeight are the
// padded detector input dims (multiples of 32).
func decodeYuNetHeads(heads *YuNetHeads, paddedWidth, paddedHeight int, scoreThreshold, nmsThreshold float64, maxDetections int) []Face {
	var candidates []Face
	for _, stride := range heads.Strides {
		cls := heads.Cls[stride]
		obj := heads.Obj[stride]
		bbox := heads.Bbox[stride]
		kps := heads.Kps[stride]
		if len(cls) == 0 || len(obj) == 0 || len(bbox) < 4*len(cls) {
			continue
		}
		gridWidth := (paddedWidth + stride - 1) / stride
		for anchor := 0; anchor < len(cls); anchor++ {
			clsScore := clamp01(cls[anchor])
			objScore := clamp01(obj[anchor])
			if !isFinite(cls[anchor]) || !isFinite(obj[anchor]) {
				continue
			}
			score := float32(math.Sqrt(float64(clsScore) * float64(objScore)))
			if score < float32(scoreThreshold) {
				continue
			}
			row := anchor / gridWidth
			column := anchor % gridWidth
			deltas := bbox[anchor*4 : anchor*4+4]
			if !allFinite(deltas) {
				continue
			}
			strideF := float64(stride)
			cx := (float64(column) + float64(deltas[0])) * strideF
			cy := (float64(row) + float64(deltas[1])) * strideF
			w := math.Exp(float64(deltas[2])) * strideF
			h := math.Exp(float64(deltas[3])) * strideF
			x1, y1 := cx-w/2, cy-h/2
			if !isFinite(float32(x1)) || !isFinite(float32(y1)) || w <= 0 || h <= 0 {
				continue
			}
			face := Face{Score: score}
			face.BBoxXYXY = [4]float32{float32(x1), float32(y1), float32(x1 + w), float32(y1 + h)}
			if len(kps) >= 10*len(cls) {
				points := kps[anchor*10 : anchor*10+10]
				if allFinite(points) {
					for i := 0; i < 5; i++ {
						face.Landmarks[i] = [2]float32{
							float32((float64(points[2*i]) + float64(column)) * strideF),
							float32((float64(points[2*i+1]) + float64(row)) * strideF),
						}
					}
				}
			}
			candidates = append(candidates, face)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	kept := nms(candidates, nmsThreshold)
	if len(kept) > maxDetections {
		kept = kept[:maxDetections]
	}
	return kept
}

// scaleFaces maps detector-input coordinates onto the original image with
// independent per-axis factors.
func scaleFaces(faces []Face, scaleX, scaleY float64) []Face {
	for index := range faces {
		faces[index].BBoxXYXY = [4]float32{
			faces[index].BBoxXYXY[0] * float32(scaleX),
			faces[index].BBoxXYXY[1] * float32(scaleY),
			faces[index].BBoxXYXY[2] * float32(scaleX),
			faces[index].BBoxXYXY[3] * float32(scaleY),
		}
		for i := 0; i < 5; i++ {
			faces[index].Landmarks[i] = [2]float32{
				faces[index].Landmarks[i][0] * float32(scaleX),
				faces[index].Landmarks[i][1] * float32(scaleY),
			}
		}
	}
	return faces
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// SqrtScore combines the per-anchor classification and objectness scores
// the way OpenCV's FaceDetectorYN does (after clamping).
func SqrtScore(cls, obj float32) float32 {
	return float32(math.Sqrt(float64(clamp01(cls)) * float64(clamp01(obj))))
}

// DebugDecode exposes head decoding with a caller-chosen threshold for
// diagnostics and integration tests.
func DebugDecode(heads *YuNetHeads, paddedWidth, paddedHeight int, scoreThreshold float64) []Face {
	return decodeYuNetHeads(heads, paddedWidth, paddedHeight, scoreThreshold, 0.3, 50)
}

func allFinite(values []float32) bool {
	for _, v := range values {
		if !isFinite(v) {
			return false
		}
	}
	return true
}

func isFinite(v float32) bool {
	return !(v != v || v > 3.4e38 || v < -3.4e38)
}
