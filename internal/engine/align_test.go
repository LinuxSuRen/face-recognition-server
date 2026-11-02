package engine

import (
	"math"
	"testing"
)

func applyTransform(m [6]float64, p [2]float64) [2]float64 {
	return [2]float64{m[0]*p[0] + m[1]*p[1] + m[2], m[3]*p[0] + m[4]*p[1] + m[5]}
}

func TestSimilarityTransformIdentity(t *testing.T) {
	m := similarityTransform(sfaceTemplate, sfaceTemplate)
	for i, point := range sfaceTemplate {
		mapped := applyTransform(m, point)
		if math.Abs(mapped[0]-point[0]) > 1e-9 || math.Abs(mapped[1]-point[1]) > 1e-9 {
			t.Fatalf("point %d mapped to %v, want %v", i, mapped, point)
		}
	}
}

func TestSimilarityTransformRecoversScaleAndRotation(t *testing.T) {
	// build a src set = template scaled by 2, rotated by ~10°, translated
	src := [5][2]float64{}
	theta := 10.0 * math.Pi / 180
	cosT, sinT := math.Cos(theta), math.Sin(theta)
	for i, point := range sfaceTemplate {
		x, y := point[0]*2, point[1]*2
		src[i] = [2]float64{cosT*x - sinT*y + 5, sinT*x + cosT*y - 7}
	}
	m := similarityTransform(src, sfaceTemplate)
	for i, want := range sfaceTemplate {
		got := applyTransform(m, src[i])
		if math.Abs(got[0]-want[0]) > 1e-6 || math.Abs(got[1]-want[1]) > 1e-6 {
			t.Fatalf("point %d mapped to %v, want %v", i, got, want)
		}
	}
}

func TestInvertAffineRoundTrip(t *testing.T) {
	m := [6]float64{1.2, -0.4, 10, 0.4, 1.2, -3}
	inv := invertAffine(m)
	point := [2]float64{37, 81}
	forward := applyTransform(m, point)
	back := applyTransform(inv, forward)
	if math.Abs(back[0]-point[0]) > 1e-9 || math.Abs(back[1]-point[1]) > 1e-9 {
		t.Fatalf("round trip gave %v, want %v", back, point)
	}
}

func TestAlignFaceWarpsLandmarksToTemplate(t *testing.T) {
	// a 200x200 dark image with bright 7x7 markers centred on the source
	// landmarks; after alignment each marker must land on its template
	// position (bilinear samples stay inside the marker interior)
	img := NewImage(200, 200)
	for x := 0; x < 200; x++ {
		for y := 0; y < 200; y++ {
			img.Set(x, y, 20, 20, 20)
		}
	}
	var face Face
	scale := 2.0
	for i, point := range sfaceTemplate {
		sx := point[0]*scale + 3
		sy := point[1]*scale - 5
		for dx := -3; dx <= 3; dx++ {
			for dy := -3; dy <= 3; dy++ {
				img.Set(int(sx)+dx, int(sy)+dy, 255, 255, 255)
			}
		}
		face.Landmarks[i] = [2]float32{float32(sx), float32(sy)}
	}
	face.BBoxXYXY = [4]float32{30, 60, 160, 190}

	aligned := alignFace(img, face)
	// the five marker centres must land on the template positions
	for i, point := range sfaceTemplate {
		tx, ty := int(point[0]), int(point[1])
		r, g, b := aligned.At(tx, ty)
		if r < 200 || g < 200 || b < 200 {
			t.Fatalf("landmark %d not bright at template position (%d,%d): rgb=%d,%d,%d",
				i, tx, ty, r, g, b)
		}
	}
}
