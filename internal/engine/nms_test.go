package engine

import "testing"

func TestNMSKeepsBestAndDropsOverlaps(t *testing.T) {
	detections := []Face{
		{BBoxXYXY: [4]float32{0, 0, 10, 10}, Score: 0.9},
		{BBoxXYXY: [4]float32{1, 1, 11, 11}, Score: 0.8}, // heavily overlaps first
		{BBoxXYXY: [4]float32{100, 100, 110, 110}, Score: 0.7},
	}
	kept := nms(detections, 0.3)
	if len(kept) != 2 {
		t.Fatalf("expected 2 kept, got %d: %+v", len(kept), kept)
	}
	if kept[0].Score != 0.9 || kept[1].Score != 0.7 {
		t.Fatalf("expected kept order 0.9,0.7 got %+v", kept)
	}
}

func TestIOUIdenticalBoxesIsOne(t *testing.T) {
	box := [4]float32{2, 2, 8, 8}
	if got := iou(box, box); got != 1 {
		t.Fatalf("iou of identical boxes = %v, want 1", got)
	}
}
