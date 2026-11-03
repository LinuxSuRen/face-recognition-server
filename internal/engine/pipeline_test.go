package engine

import (
	"math"
	"testing"
)

// headsWithFace builds raw detector heads with one detection at the given
// stride-8 anchor, using OpenCV box semantics: center offsets in grid
// cells plus log-scale size, landmark offsets in grid cells.
func headsWithFace(paddedWidth, paddedHeight, column, row int,
	offX, offY, logW, logH, lmDX, lmDY float32) *YuNetHeads {
	gridWidth := paddedWidth / 8
	anchors := gridWidth * (paddedHeight / 8)
	heads := NewYuNetHeads()
	cls := make([]float32, anchors)
	obj := make([]float32, anchors)
	bbox := make([]float32, anchors*4)
	kps := make([]float32, anchors*10)
	index := row*gridWidth + column
	cls[index], obj[index] = 0.9, 0.9 // sqrt → 0.9, above the 0.8 gate
	bbox[index*4+0], bbox[index*4+1] = offX, offY
	bbox[index*4+2], bbox[index*4+3] = logW, logH
	kps[index*10+0], kps[index*10+1] = lmDX, lmDY
	heads.Cls[8], heads.Obj[8], heads.Bbox[8], heads.Kps[8] = cls, obj, bbox, kps
	return heads
}

func embedRunner(t *testing.T, calls *int) RecognizerRunner {
	t.Helper()
	return func(input []float32) ([]float32, error) {
		if calls != nil {
			*calls++
		}
		if len(input) != SFaceSize*SFaceSize*3 {
			t.Fatalf("aligned tensor size %d, want %d", len(input), SFaceSize*SFaceSize*3)
		}
		vector := make([]float32, 128)
		vector[0] = 1
		return vector, nil
	}
}

func TestExtractFacesMapsCoordinatesAndEmbeds(t *testing.T) {
	// 2560x1920 source: the pipeline downscales to 1280x960 (×0.5) before
	// detection and maps results back with ×2. Anchor (40,40) with center
	// offset 0.5 and log size ln(300/8) yields a 300x300 box at (324,324).
	img := checkerImage(2560, 1920)
	var detectorInputW, detectorInputH int
	embedCalls := 0
	logSize := float32(math.Log(300.0 / 8.0))

	faces, err := extractFaces(img, Options{}.withDefaults(),
		func(input []float32, width, height int) (*YuNetHeads, error) {
			detectorInputW, detectorInputH = width, height
			if len(input) != width*height*3 {
				t.Fatalf("tensor size %d does not match %dx%d", len(input), width, height)
			}
			return headsWithFace(width, height, 40, 40, 0.5, 0.5, logSize, logSize, 2, 3), nil
		},
		embedRunner(t, &embedCalls))
	if err != nil {
		t.Fatalf("extractFaces failed: %v", err)
	}
	if detectorInputW != 1280 || detectorInputH != 960 {
		t.Fatalf("detector input was %dx%d, want 1280x960", detectorInputW, detectorInputH)
	}
	if len(faces) != 1 {
		t.Fatalf("expected 1 face, got %d", len(faces))
	}
	// detector box: center (324,324), size 300 → (174,174,474,474)
	want := [4]float32{174, 174, 474, 474}
	for i := range want {
		if abs32(faces[0].Face.BBoxXYXY[i]-want[i]*2) > 0.01 {
			t.Fatalf("bbox dimension %d mapped to %v, want ~%v",
				i, faces[0].Face.BBoxXYXY[i], want[i]*2)
		}
	}
	// landmark 0: ((2+40)*8, (3+40)*8) = (336, 344) → ×2
	if lm := faces[0].Face.Landmarks[0]; abs32(lm[0]-672) > 0.01 || abs32(lm[1]-688) > 0.01 {
		t.Fatalf("landmark mapped to %v, want [672 688]", lm)
	}
	if embedCalls != 1 || faces[0].Embedding == nil || len(faces[0].Embedding) != 128 {
		t.Fatalf("embedding not extracted: calls=%d embedding=%v", embedCalls, faces[0].Embedding)
	}
	if !faces[0].Quality.Accepted {
		t.Fatalf("checkerboard face should pass quality, reasons=%v", faces[0].Quality.Reasons)
	}
}

func TestExtractFacesSkipsEmbeddingForTinyFaces(t *testing.T) {
	img := checkerImage(2560, 1920)
	embedCalls := 0
	logSize := float32(math.Log(15.0 / 8.0)) // 15px in detector coords → 30px in source
	faces, err := extractFaces(img, Options{}.withDefaults(),
		func(_ []float32, width, height int) (*YuNetHeads, error) {
			return headsWithFace(width, height, 40, 40, 0.5, 0.5, logSize, logSize, 0, 0), nil
		},
		embedRunner(t, &embedCalls))
	if err != nil {
		t.Fatalf("extractFaces failed: %v", err)
	}
	if len(faces) != 1 {
		t.Fatalf("expected the face itself without embedding, got %d", len(faces))
	}
	if faces[0].Embedding != nil {
		t.Fatalf("tiny face must not carry an embedding")
	}
	if embedCalls != 0 {
		t.Fatalf("recognizer must not run for blocked faces, ran %d times", embedCalls)
	}
}

func TestExtractFacesFeedsFixedDetectorInputSize(t *testing.T) {
	// a static-input YuNet export (640x640) must receive exactly that
	// tensor even for a 512x512 source, with per-axis coordinate mapping
	img := checkerImage(512, 512)
	var inputW, inputH int
	opts := Options{}.withDefaults()
	opts.DetectorFixedWidth = 640
	opts.DetectorFixedHeight = 640
	logSize := float32(math.Log(125.0 / 8.0)) // 125px box
	faces, err := extractFaces(img, opts,
		func(input []float32, width, height int) (*YuNetHeads, error) {
			inputW, inputH = width, height
			return headsWithFace(width, height, 12, 12, 0.5, 0.5, logSize, logSize, 2, 3), nil
		},
		embedRunner(t, nil))
	if err != nil {
		t.Fatalf("extractFaces failed: %v", err)
	}
	if inputW != 640 || inputH != 640 {
		t.Fatalf("detector input was %dx%d, want 640x640", inputW, inputH)
	}
	if len(faces) != 1 {
		t.Fatalf("expected 1 face, got %d", len(faces))
	}
	// detector box: center (100,100), size 125 → (37.5,37.5,162.5,162.5)
	// scaled by 512/640 = 0.8 → (30,30,130,130)
	want := [4]float32{30, 30, 130, 130}
	for i := range want {
		if abs32(faces[0].Face.BBoxXYXY[i]-want[i]) > 0.01 {
			t.Fatalf("bbox dimension %d mapped to %v, want %v",
				i, faces[0].Face.BBoxXYXY, want)
		}
	}
}

func TestPadToMultipleAddsZeroEdges(t *testing.T) {
	img := filledImage(100, 60, 77)
	padded := padToMultiple(img, 32)
	if padded.Width != 128 || padded.Height != 64 {
		t.Fatalf("padded to %dx%d, want 128x64", padded.Width, padded.Height)
	}
	if r, g, b := padded.At(99, 59); r != 77 || g != 77 || b != 77 {
		t.Fatalf("content changed inside the original area")
	}
	if r, _, _ := padded.At(127, 63); r != 0 {
		t.Fatalf("padding must be zero")
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
