package engine

import (
	"image"
	"image/color"
)

// Image is an interleaved 8-bit RGB raster. Both ONNX inputs are RGB
// (the reference OpenCV pipeline feeds them with swapRB=true), so RGB is
// the canonical in-memory format of the whole engine.
type Image struct {
	Pixels []uint8
	Width  int
	Height int
}

// NewImage allocates a black image.
func NewImage(width, height int) *Image {
	return &Image{Pixels: make([]uint8, width*height*3), Width: width, Height: height}
}

// At returns the pixel as separate channels.
func (img *Image) At(x, y int) (r, g, b uint8) {
	offset := (y*img.Width + x) * 3
	return img.Pixels[offset], img.Pixels[offset+1], img.Pixels[offset+2]
}

// Set writes one pixel.
func (img *Image) Set(x, y int, r, g, b uint8) {
	offset := (y*img.Width + x) * 3
	img.Pixels[offset] = r
	img.Pixels[offset+1] = g
	img.Pixels[offset+2] = b
}

// FromGoImage converts any decoded image (NRGBA, YCbCr, …) to RGB.
func FromGoImage(m image.Image) *Image {
	bounds := m.Bounds()
	out := NewImage(bounds.Dx(), bounds.Dy())
	for y := 0; y < out.Height; y++ {
		for x := 0; x < out.Width; x++ {
			r, g, b, _ := m.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			out.Set(x, y, uint8(r>>8), uint8(g>>8), uint8(b>>8))
		}
	}
	return out
}

// ToGoImage wraps the buffer for drawing/encoding APIs.
func (img *Image) ToGoImage() image.Image {
	out := image.NewNRGBA(image.Rect(0, 0, img.Width, img.Height))
	for y := 0; y < img.Height; y++ {
		for x := 0; x < img.Width; x++ {
			r, g, b := img.At(x, y)
			out.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return out
}

// Crop returns the sub-rectangle (clamped to bounds) as a new image that
// shares no state with the source.
func (img *Image) Crop(x1, y1, x2, y2 int) *Image {
	x1 = clamp(x1, 0, img.Width)
	y1 = clamp(y1, 0, img.Height)
	x2 = clamp(x2, 0, img.Width)
	y2 = clamp(y2, 0, img.Height)
	width := max(0, x2-x1)
	height := max(0, y2-y1)
	out := NewImage(width, height)
	for row := 0; row < height; row++ {
		srcOffset := ((y1+row)*img.Width + x1) * 3
		copy(out.Pixels[row*width*3:(row+1)*width*3], img.Pixels[srcOffset:srcOffset+width*3])
	}
	return out
}

func clamp(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

// grayAt converts one pixel with the BT.601 weights OpenCV uses.
func grayAt(img *Image, x, y int) float64 {
	r, g, b := img.At(x, y)
	return 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
}
