package liveness

import (
	"testing"

	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
)

// liveFrame creates a face whose bbox shifts with the whole face (tracking)
// while each landmark has independent micro-movement.
func liveFrame(shiftX, shiftY float32, jitter [5][2]float32) engine.ExtractedFace {
	base := [4]float32{100 + shiftX, 100 + shiftY, 300 + shiftX, 300 + shiftY}
	face := engine.ExtractedFace{
		Face: engine.Face{
			BBoxXYXY:  base,
			Landmarks: [5][2]float32{},
		},
	}
	for i := range face.Face.Landmarks {
		face.Face.Landmarks[i] = [2]float32{
			150 + float32(i)*20 + shiftX + jitter[i][0],
			150 + float32(i)*10 + shiftY + jitter[i][1],
		}
	}
	return face
}

func TestLiveFacePasses(t *testing.T) {
	// a live face: each landmark moves independently across frames (blinks,
	// nostril flare, slight mouth movement), even with overall head motion
	jitters := [][5][2]float32{
		{{0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}},
		{{0.5, 0.2}, {-0.3, 0.4}, {0.1, -0.5}, {0.6, 0}, {-0.2, 0.3}},
		{{-0.4, 0.5}, {0.2, -0.1}, {0.7, 0.3}, {-0.5, 0.4}, {0.1, -0.6}},
		{{0.3, -0.2}, {-0.6, 0.1}, {0.4, 0.6}, {0.2, -0.3}, {0.8, 0.1}},
	}
	shifts := [][2]float32{{0, 0}, {2, 1}, {-1, -2}, {1, 3}}
	faces := make([]engine.ExtractedFace, len(jitters))
	for i, jitter := range jitters {
		faces[i] = liveFrame(shifts[i][0], shifts[i][1], jitter)
	}
	result := Check(faces, 0.3)
	if !result.Live {
		t.Fatalf("live face failed: %+v", result)
	}
}

func TestStaticPhotoFails(t *testing.T) {
	// a static photo: all frames identical (no movement at all)
	base := liveFrame(0, 0, [5][2]float32{})
	faces := []engine.ExtractedFace{base, base, base, base}
	result := Check(faces, 0.3)
	if result.Live {
		t.Fatalf("static photo passed: %+v", result)
	}
}

func TestRigidPhotoShakeFails(t *testing.T) {
	// a rigidly shaken photo: whole face (bbox AND landmarks) shifts
	// together, so after center-normalisation all relative positions are
	// constant and per-axis variance is near zero
	shifts := [][2]float32{{0, 0}, {5, 3}, {-4, -2}, {3, -1}}
	faces := make([]engine.ExtractedFace, len(shifts))
	for i, shift := range shifts {
		faces[i] = liveFrame(shift[0], shift[1], [5][2]float32{})
	}
	result := Check(faces, 0.3)
	if result.Live {
		t.Fatalf("rigidly shaken photo passed: %+v", result)
	}
}

func TestTooFewFramesFails(t *testing.T) {
	base := liveFrame(0, 0, [5][2]float32{})
	result := Check([]engine.ExtractedFace{base, base}, 0.3)
	if result.Live {
		t.Fatalf("too few frames passed: %+v", result)
	}
}
