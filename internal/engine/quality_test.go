package engine

import "testing"

func filledImage(width, height int, value uint8) *Image {
	img := NewImage(width, height)
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.Set(x, y, value, value, value)
		}
	}
	return img
}

func checkerImage(width, height int) *Image {
	img := NewImage(width, height)
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			if (x/2+y/2)%2 == 0 {
				img.Set(x, y, 255, 255, 255)
			} else {
				img.Set(x, y, 0, 0, 0)
			}
		}
	}
	return img
}

func TestQualityAcceptsSharpLargeFace(t *testing.T) {
	img := checkerImage(400, 400)
	face := Face{BBoxXYXY: [4]float32{50, 50, 150, 150}}
	quality := assessQuality(img, face, Options{}.withDefaults())
	if !quality.Accepted {
		t.Fatalf("expected accepted, reasons=%v", quality.Reasons)
	}
	if quality.BlurVariance < 40 {
		t.Fatalf("checkerboard blur variance too low: %v", quality.BlurVariance)
	}
	if quality.FaceSize != 100 {
		t.Fatalf("face size = %d, want 100", quality.FaceSize)
	}
}

func TestQualityRejectsBlurryFace(t *testing.T) {
	img := filledImage(400, 400, 128)
	face := Face{BBoxXYXY: [4]float32{50, 50, 150, 150}}
	quality := assessQuality(img, face, Options{}.withDefaults())
	if quality.Accepted {
		t.Fatalf("flat image must fail the blur gate")
	}
	if len(quality.Reasons) != 1 || quality.Reasons[0] != "image_too_blurry" {
		t.Fatalf("unexpected reasons: %v", quality.Reasons)
	}
	// blur alone must not block embedding extraction
	if !quality.embeddingAllowed() {
		t.Fatalf("blur is a soft gate; embedding should stay allowed")
	}
}

func TestQualityRejectsTinyFace(t *testing.T) {
	img := checkerImage(1000, 1000)
	face := Face{BBoxXYXY: [4]float32{100, 100, 120, 120}}
	quality := assessQuality(img, face, Options{}.withDefaults())
	if quality.Accepted {
		t.Fatalf("tiny face must be rejected")
	}
	if !containsReason(quality.Reasons, "face_too_small") {
		t.Fatalf("expected face_too_small, got %v", quality.Reasons)
	}
	if !containsReason(quality.Reasons, "face_area_too_small") {
		t.Fatalf("expected face_area_too_small, got %v", quality.Reasons)
	}
	if quality.embeddingAllowed() {
		t.Fatalf("size failures must block embedding extraction")
	}
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
