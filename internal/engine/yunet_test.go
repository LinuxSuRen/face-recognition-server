package engine

import (
	"math"
	"testing"
)

func TestDecodeYuNetHeadsAnchorsScoreAndGating(t *testing.T) {
	// 640x640 padded input, stride 8 → 80x80 anchors
	const gridSize = 80
	heads := NewYuNetHeads()
	cls := make([]float32, gridSize*gridSize)
	obj := make([]float32, gridSize*gridSize)
	bbox := make([]float32, gridSize*gridSize*4)
	kps := make([]float32, gridSize*gridSize*10)

	strong := 12*gridSize + 12            // anchor (row 12, col 12)
	cls[strong], obj[strong] = 0.81, 0.81 // sqrt(0.81*0.81) = 0.81
	logSize := float32(math.Log(150.0 / 8.0))
	bbox[strong*4+0], bbox[strong*4+1] = 0.5, 0.5 // center = (12.5)*8 = 100
	bbox[strong*4+2], bbox[strong*4+3] = logSize, logSize
	kps[strong*10+0], kps[strong*10+1] = 2, 3

	weak := 5
	cls[weak], obj[weak] = 0.81, 0.2 // sqrt ≈ 0.40 below the 0.8 gate

	heads.Cls[8], heads.Obj[8], heads.Bbox[8], heads.Kps[8] = cls, obj, bbox, kps

	faces := decodeYuNetHeads(heads, 640, 640, 0.8, 0.3, 50)
	if len(faces) != 1 {
		t.Fatalf("expected the strong anchor only, got %d faces", len(faces))
	}
	face := faces[0]
	// center (100,100), size 150 → (25,25,175,175)
	want := [4]float32{25, 25, 175, 175}
	for i := range want {
		if float32(math.Abs(float64(face.BBoxXYXY[i]-want[i]))) > 0.01 {
			t.Fatalf("bbox dimension %d = %v, want %v", i, face.BBoxXYXY[i], want[i])
		}
	}
	// landmark 0: ((2+12)*8, (3+12)*8) = (112, 120)
	if lm := face.Landmarks[0]; abs32(lm[0]-112) > 0.01 || abs32(lm[1]-120) > 0.01 {
		t.Fatalf("unexpected landmark: %+v", lm)
	}
	if math.Abs(float64(face.Score)-0.81) > 1e-5 {
		t.Fatalf("score = %v, want 0.81", face.Score)
	}
}

func TestDecodeYuNetHeadsClampsRawScores(t *testing.T) {
	const gridSize = 80
	heads := NewYuNetHeads()
	cls := make([]float32, gridSize*gridSize)
	obj := make([]float32, gridSize*gridSize)
	bbox := make([]float32, gridSize*gridSize*4)
	index := 12*gridSize + 12
	cls[index], obj[index] = 1.4, 1.2 // above 1: clamped to 1 → score 1
	logSize := float32(math.Log(80.0 / 8.0))
	bbox[index*4+0], bbox[index*4+1] = 0.5, 0.5
	bbox[index*4+2], bbox[index*4+3] = logSize, logSize
	heads.Cls[8], heads.Obj[8], heads.Bbox[8] = cls, obj, bbox

	faces := decodeYuNetHeads(heads, 640, 640, 0.8, 0.3, 50)
	if len(faces) != 1 || faces[0].Score != 1 {
		t.Fatalf("clamping failed: %+v", faces)
	}
}

func TestScaleFacesMapsPerAxis(t *testing.T) {
	faces := []Face{{
		BBoxXYXY:  [4]float32{100, 100, 300, 300},
		Landmarks: [5][2]float32{{50, 60}},
	}}
	scaled := scaleFaces(faces, 0.5, 2.0)
	if scaled[0].BBoxXYXY != [4]float32{50, 200, 150, 600} {
		t.Fatalf("unexpected scaled bbox: %+v", scaled[0].BBoxXYXY)
	}
	if scaled[0].Landmarks[0] != [2]float32{25, 120} {
		t.Fatalf("unexpected scaled landmark: %+v", scaled[0].Landmarks[0])
	}
}
