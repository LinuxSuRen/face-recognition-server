package engine

import "math"

// buildNCHW converts an RGB image into a float32 CHW tensor with per-channel
// scaling: value = (pixel - mean) * scale. The reference pipeline feeds
// YuNet with scale 1/255 and SFace with mean 127.5 and scale 1/128, both
// expecting RGB channel order.
func buildNCHW(img *Image, mean, scale float64) []float32 {
	plane := img.Width * img.Height
	tensor := make([]float32, plane*3)
	for y := 0; y < img.Height; y++ {
		for x := 0; x < img.Width; x++ {
			r, g, b := img.At(x, y)
			offset := y*img.Width + x
			tensor[offset] = float32((float64(r) - mean) * scale)
			tensor[plane+offset] = float32((float64(g) - mean) * scale)
			tensor[plane*2+offset] = float32((float64(b) - mean) * scale)
		}
	}
	return tensor
}

// normalizeEmbedding returns a unit-length copy of the vector. SFace outputs
// are near-unit already; normalizing makes cosine similarity a plain dot
// product and stabilizes thresholds across model builds.
func normalizeEmbedding(values []float32) []float32 {
	var norm float64
	for _, v := range values {
		norm += float64(v) * float64(v)
	}
	norm = sqrt(norm)
	if norm <= 1e-12 {
		return nil
	}
	out := make([]float32, len(values))
	for i, v := range values {
		out[i] = float32(float64(v) / norm)
	}
	return out
}

// cosine returns the cosine similarity of two unit-length vectors.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return -1
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

func sqrt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
}
