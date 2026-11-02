package engine

import (
	"math"
	"testing"
)

func yunetRow(x, y, w, h float32, score float32, landmarkStart float32) []float32 {
	row := make([]float32, YunetRowStride)
	row[0], row[1], row[2], row[3] = x, y, w, h
	for i := 0; i < 5; i++ {
		row[4+2*i] = landmarkStart + float32(i)*2
		row[5+2*i] = landmarkStart + float32(i)*2 + 1
	}
	row[14] = score
	return row
}

func TestDecodeYuNetFiltersAndScales(t *testing.T) {
	rows := []float32{}
	rows = append(rows, yunetRow(50, 60, 100, 120, 0.95, 200)...)  // keep
	rows = append(rows, yunetRow(10, 10, 40, 40, 0.10, 100)...)    // below threshold
	rows = append(rows, yunetRow(0, 0, float32(math.NaN()), 10, 0.99, 0)...) // degenerate
	rows = append(rows, yunetRow(300, 300, 80, 80, 0.85, 350)...)  // keep

	faces := decodeYuNet(rows, 0.5, 0.8, 0.3, 50)
	if len(faces) != 2 {
		t.Fatalf("expected 2 faces, got %d: %+v", len(faces), faces)
	}
	first := faces[0]
	if first.BBoxXYXY != [4]float32{100, 120, 300, 360} {
		t.Fatalf("unexpected scaled bbox: %+v", first.BBoxXYXY)
	}
	if first.Landmarks[0] != [2]float32{400, 402} {
		t.Fatalf("unexpected scaled landmark: %+v", first.Landmarks[0])
	}
	if first.Score != 0.95 {
		t.Fatalf("unexpected score: %v", first.Score)
	}
}

func TestDecodeYuNetCapsDetections(t *testing.T) {
	rows := []float32{}
	for i := 0; i < 8; i++ {
		rows = append(rows, yunetRow(float32(i*200), 0, 50, 50, 0.9, 0)...)
	}
	faces := decodeYuNet(rows, 1.0, 0.8, 0.3, 3)
	if len(faces) != 3 {
		t.Fatalf("expected cap of 3, got %d", len(faces))
	}
}
