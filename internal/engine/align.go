package engine

import "math"

// SFaceSize is the SFace input resolution.
const SFaceSize = 112

// sfaceTemplate holds the canonical five landmark positions of an aligned
// 112x112 face (right eye, left eye, nose tip, right mouth corner, left
// mouth corner). These are the anchor points OpenCV's FaceRecognizerSF
// alignCrop maps detected landmarks onto.
var sfaceTemplate = [5][2]float64{
	{38.2946, 51.6963},
	{73.5318, 51.5014},
	{56.0252, 71.7366},
	{41.5493, 92.3655},
	{70.7299, 92.2041},
}

// similarityTransform solves the least-squares similarity transform
// (rotation + uniform scale + translation) mapping src points onto dst
// points. For five landmark pairs the solution is the unique closed form
// z = Σ conj(a_i)·b_i / Σ |a_i|² over mean-centred points, which coincides
// with both the Umeyama estimate and the linear system OpenCV's SFace
// reference solves.
//
// It returns the 2x3 affine matrix [m00 m01 t0; m10 m11 t1] such that
// dst ≈ M · src.
func similarityTransform(src, dst [5][2]float64) [6]float64 {
	var meanSrcX, meanSrcY, meanDstX, meanDstY float64
	for i := 0; i < 5; i++ {
		meanSrcX += src[i][0]
		meanSrcY += src[i][1]
		meanDstX += dst[i][0]
		meanDstY += dst[i][1]
	}
	meanSrcX /= 5
	meanSrcY /= 5
	meanDstX /= 5
	meanDstY /= 5

	var cNum, sNum, denom float64
	for i := 0; i < 5; i++ {
		ax := src[i][0] - meanSrcX
		ay := src[i][1] - meanSrcY
		bx := dst[i][0] - meanDstX
		by := dst[i][1] - meanDstY
		// z = Σ (ax - i·ay)(bx + i·by) / Σ (ax² + ay²)
		cNum += ax*bx + ay*by
		sNum += ax*by - ay*bx
		denom += ax*ax + ay*ay
	}
	if denom == 0 {
		return [6]float64{1, 0, 0, 0, 1, 0}
	}
	c := cNum / denom
	s := sNum / denom
	// M = [[c, -s], [s, c]], translation moves the src mean onto the dst mean.
	return [6]float64{
		c, -s, meanDstX - (c*meanSrcX - s*meanSrcY),
		s, c, meanDstY - (s*meanSrcX + c*meanSrcY),
	}
}

// invertAffine inverts a 2x3 affine matrix (non-degenerate).
func invertAffine(m [6]float64) [6]float64 {
	det := m[0]*m[4] - m[1]*m[3]
	if det == 0 {
		return [6]float64{1, 0, 0, 0, 1, 0}
	}
	invDet := 1 / det
	// inverse of the linear part
	i00 := m[4] * invDet
	i01 := -m[1] * invDet
	i10 := -m[3] * invDet
	i11 := m[0] * invDet
	// translation of the inverse
	tx := -(i00*m[2] + i01*m[5])
	ty := -(i10*m[2] + i11*m[5])
	return [6]float64{i00, i01, tx, i10, i11, ty}
}

// alignFace warps the image so the detected landmarks land on the SFace
// template, with bilinear sampling and constant black borders — the OpenCV
// warpAffine defaults used by alignCrop.
func alignFace(img *Image, face Face) *Image {
	var src [5][2]float64
	for i := 0; i < 5; i++ {
		src[i] = [2]float64{float64(face.Landmarks[i][0]), float64(face.Landmarks[i][1])}
	}
	forward := similarityTransform(src, sfaceTemplate)
	inverse := invertAffine(forward)

	out := NewImage(SFaceSize, SFaceSize)
	for y := 0; y < SFaceSize; y++ {
		for x := 0; x < SFaceSize; x++ {
			sx := inverse[0]*float64(x) + inverse[1]*float64(y) + inverse[2]
			sy := inverse[3]*float64(x) + inverse[4]*float64(y) + inverse[5]
			r, g, b := sampleBilinear(img, sx, sy)
			out.Set(x, y, r, g, b)
		}
	}
	return out
}

// sampleBilinear returns a clamped bilinear sample as packed RGB channels.
func sampleBilinear(img *Image, x, y float64) (uint8, uint8, uint8) {
	if x < 0 || y < 0 || x >= float64(img.Width-1) || y >= float64(img.Height-1) {
		if x < -1 || y < -1 || x > float64(img.Width) || y > float64(img.Height) {
			return 0, 0, 0
		}
		// edge clamp for samples just outside the border
		x = clampF(x, 0, float64(img.Width-1))
		y = clampF(y, 0, float64(img.Height-1))
	}
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	x1, y1 := min(x0+1, img.Width-1), min(y0+1, img.Height-1)
	fx, fy := x-math.Floor(x), y-math.Floor(y)

	r00, g00, b00 := img.At(x0, y0)
	r10, g10, b10 := img.At(x1, y0)
	r01, g01, b01 := img.At(x0, y1)
	r11, g11, b11 := img.At(x1, y1)

	lerp := func(v00, v10, v01, v11 uint8) uint8 {
		top := float64(v00)*(1-fx) + float64(v10)*fx
		bottom := float64(v01)*(1-fx) + float64(v11)*fx
		return uint8(clampF(top*(1-fy)+bottom*fy, 0, 255) + 0.5)
	}
	return lerp(r00, r10, r01, r11), lerp(g00, g10, g01, g11), lerp(b00, b10, b01, b11)
}
