package engine

// resizeArea downsamples with box filtering, the pure-Go equivalent of
// OpenCV INTER_AREA for shrink factors >= 1. Each destination pixel averages
// the exact source rectangle, matching the area-weighted reference closely
// enough that detector scores stay stable.
func resizeArea(img *Image, width, height int) *Image {
	if width <= 0 || height <= 0 || img.Width == 0 || img.Height == 0 {
		return NewImage(max(width, 0), max(height, 0))
	}
	out := NewImage(width, height)
	scaleX := float64(img.Width) / float64(width)
	scaleY := float64(img.Height) / float64(height)
	for y := 0; y < height; y++ {
		srcY1 := float64(y) * scaleY
		srcY2 := float64(y+1) * scaleY
		for x := 0; x < width; x++ {
			srcX1 := float64(x) * scaleX
			srcX2 := float64(x+1) * scaleX
			var sumR, sumG, sumB, area float64
			for sy := int(srcY1); sy < min(int(srcY2)+1, img.Height); sy++ {
				if sy >= img.Height {
					break
				}
				fy := boxOverlap(float64(sy), float64(sy+1), srcY1, srcY2)
				if fy <= 0 {
					continue
				}
				for sx := int(srcX1); sx < min(int(srcX2)+1, img.Width); sx++ {
					if sx >= img.Width {
						break
					}
					fx := boxOverlap(float64(sx), float64(sx+1), srcX1, srcX2)
					if fx <= 0 {
						continue
					}
					weight := fx * fy
					r, g, b := img.At(sx, sy)
					sumR += float64(r) * weight
					sumG += float64(g) * weight
					sumB += float64(b) * weight
					area += weight
				}
			}
			if area == 0 {
				continue
			}
			out.Set(x, y, uint8(sumR/area+0.5), uint8(sumG/area+0.5), uint8(sumB/area+0.5))
		}
	}
	return out
}

func boxOverlap(a1, a2, b1, b2 float64) float64 {
	lo := max(a1, b1)
	hi := min(a2, b2)
	if hi <= lo {
		return 0
	}
	return hi - lo
}

// bicubicKernel is the cubic convolution kernel with a = -0.75, OpenCV's
// INTER_CUBIC coefficient.
func bicubicKernel(t float64) float64 {
	t = abs(t)
	if t <= 1 {
		return (1.5*t-2.5)*t*t + 1
	}
	if t < 2 {
		return ((-0.5*t+2.5)*t-4)*t + 2
	}
	return 0
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// resizeBicubic upsamples (or mildly downsamples) with the OpenCV INTER_CUBIC
// kernel, used for the 224x224 blur-normalization of face crops.
func resizeBicubic(img *Image, width, height int) *Image {
	out := NewImage(width, height)
	scaleX := float64(img.Width) / float64(width)
	scaleY := float64(img.Height) / float64(height)
	// OpenCV maps destination centers back to source coordinates without
	// half-pixel offset compensation for INTER_CUBIC.
	for y := 0; y < height; y++ {
		srcY := (float64(y)+0.5)*scaleY - 0.5
		y0 := int(floor64(srcY))
		for x := 0; x < width; x++ {
			srcX := (float64(x)+0.5)*scaleX - 0.5
			x0 := int(floor64(srcX))
			var sumR, sumG, sumB, sumW float64
			for m := -2; m <= 2; m++ {
				wy := bicubicKernel(srcY - float64(y0+m))
				if wy == 0 {
					continue
				}
				for n := -2; n <= 2; n++ {
					w := wy * bicubicKernel(srcX-float64(x0+n))
					if w == 0 {
						continue
					}
					sx := clamp(x0+n, 0, img.Width-1)
					sy := clamp(y0+m, 0, img.Height-1)
					r, g, b := img.At(sx, sy)
					sumR += float64(r) * w
					sumG += float64(g) * w
					sumB += float64(b) * w
					sumW += w
				}
			}
			if sumW == 0 {
				continue
			}
			out.Set(x, y, uint8(clampF(sumR/sumW, 0, 255)+0.5),
				uint8(clampF(sumG/sumW, 0, 255)+0.5),
				uint8(clampF(sumB/sumW, 0, 255)+0.5))
		}
	}
	return out
}

func floor64(v float64) float64 {
	if v >= 0 || v == float64(int(v)) {
		return float64(int(v))
	}
	return float64(int(v) - 1)
}

func clampF(v, low, high float64) float64 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
