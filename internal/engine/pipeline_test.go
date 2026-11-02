package engine

import "testing"

func TestExtractFacesMapsCoordinatesAndEmbeds(t *testing.T) {
	// 2560x1920 source: the pipeline must downscale to 1280x960 (scale 0.5)
	// before detection and map results back to source coordinates.
	img := checkerImage(2560, 1920)
	var detectorInputW, detectorInputH int
	embedCalls := 0

	faces, err := extractFaces(img, Options{}.withDefaults(),
		func(input []float32, width, height int) ([]float32, int, error) {
			detectorInputW, detectorInputH = width, height
			if len(input) != width*height*3 {
				t.Fatalf("tensor size %d does not match %dx%d", len(input), width, height)
			}
			rows := yunetRow(100, 100, 200, 200, 0.95, 150)
			return rows, 1, nil
		},
		func(input []float32) ([]float32, error) {
			embedCalls++
			if len(input) != SFaceSize*SFaceSize*3 {
				t.Fatalf("aligned tensor size %d, want %d", len(input), SFaceSize*SFaceSize*3)
			}
			vector := make([]float32, 128)
			vector[0] = 1
			return vector, nil
		})
	if err != nil {
		t.Fatalf("extractFaces failed: %v", err)
	}
	if detectorInputW != 1280 || detectorInputH != 960 {
		t.Fatalf("detector input was %dx%d, want 1280x960", detectorInputW, detectorInputH)
	}
	if len(faces) != 1 {
		t.Fatalf("expected 1 face, got %d", len(faces))
	}
	face := faces[0]
	if face.Face.BBoxXYXY != [4]float32{200, 200, 600, 600} {
		t.Fatalf("bbox mapped to %v, want [200 200 600 600]", face.Face.BBoxXYXY)
	}
	if face.Face.Landmarks[0] != [2]float32{300, 302} {
		t.Fatalf("landmark mapped to %v, want [300 302]", face.Face.Landmarks[0])
	}
	if embedCalls != 1 || face.Embedding == nil || len(face.Embedding) != 128 {
		t.Fatalf("embedding not extracted: calls=%d embedding=%v", embedCalls, face.Embedding)
	}
	if !face.Quality.Accepted {
		t.Fatalf("checkerboard face should pass quality, reasons=%v", face.Quality.Reasons)
	}
}

func TestExtractFacesSkipsEmbeddingForTinyFaces(t *testing.T) {
	img := checkerImage(2560, 1920)
	embedCalls := 0
	faces, err := extractFaces(img, Options{}.withDefaults(),
		func(_ []float32, width, height int) ([]float32, int, error) {
			// 30px face in detector coordinates = 60px in source: the 48px
			// gate passes, but the area-ratio gate fails and blocks embedding
			rows := yunetRow(400, 400, 30, 30, 0.95, 410)
			return rows, 1, nil
		},
		func(_ []float32) ([]float32, error) {
			embedCalls++
			return make([]float32, 128), nil
		})
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
