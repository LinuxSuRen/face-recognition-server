package engine

// iou returns the intersection-over-union of two xyxy boxes.
func iou(a, b [4]float32) float32 {
	x1 := max(a[0], b[0])
	y1 := max(a[1], b[1])
	x2 := min(a[2], b[2])
	y2 := min(a[3], b[3])
	inter := max(0, x2-x1) * max(0, y2-y1)
	if inter == 0 {
		return 0
	}
	areaA := (a[2] - a[0]) * (a[3] - a[1])
	areaB := (b[2] - b[0]) * (b[3] - b[1])
	return inter / (areaA + areaB - inter)
}

// nms keeps detections greedily by score and drops any detection whose IoU
// with an already kept box exceeds the threshold.
func nms(detections []Face, threshold float64) []Face {
	order := make([]int, len(detections))
	for i := range order {
		order[i] = i
	}
	// insertion sort by score descending; n is tiny (< a few hundred)
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && detections[order[j]].Score > detections[order[j-1]].Score; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	kept := make([]Face, 0, len(detections))
	for _, idx := range order {
		candidate := detections[idx]
		suppressed := false
		for i := range kept {
			if iou(candidate.BBoxXYXY, kept[i].BBoxXYXY) > float32(threshold) {
				suppressed = true
				break
			}
		}
		if !suppressed {
			kept = append(kept, candidate)
		}
	}
	return kept
}
