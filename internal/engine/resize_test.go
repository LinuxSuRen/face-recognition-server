package engine

import "testing"

func TestResizeAreaIdentityKeepsPixels(t *testing.T) {
	img := checkerImage(64, 64)
	out := resizeArea(img, 64, 64)
	for i := range img.Pixels {
		if img.Pixels[i] != out.Pixels[i] {
			t.Fatalf("same-size resize changed pixel %d", i)
		}
	}
}

func TestResizeAreaHalvesConstantImage(t *testing.T) {
	img := filledImage(100, 60, 77)
	out := resizeArea(img, 50, 30)
	if out.Width != 50 || out.Height != 30 {
		t.Fatalf("unexpected size %dx%d", out.Width, out.Height)
	}
	for i := range out.Pixels {
		if out.Pixels[i] != 77 {
			t.Fatalf("constant image resampled to %d at %d", out.Pixels[i], i)
		}
	}
}

func TestResizeBicubicKeepsConstantImage(t *testing.T) {
	img := filledImage(50, 50, 200)
	out := resizeBicubic(img, blurReferenceSize, blurReferenceSize)
	for i := range out.Pixels {
		if out.Pixels[i] != 200 {
			t.Fatalf("constant image resampled to %d at %d", out.Pixels[i], i)
		}
	}
}
