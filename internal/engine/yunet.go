package engine

// YunetRowStride is the number of float32 values per candidate row in the
// raw detector output: x, y, w, h, five landmark pairs and the score.
const YunetRowStride = 15

// decodeYuNet converts the raw detector tensor rows into faces in original
// image coordinates. The detector may emit non-finite or degenerate rows for
// extreme aspect ratios; those are skipped like the reference pipeline does.
//
// scale maps detector-input coordinates back to the original image (the
// input was downscaled so its longer side is <= detectorMaxSide).
func decodeYuNet(rows []float32, scale float64, scoreThreshold float64, nmsThreshold float64, maxDetections int) []Face {
	candidates := make([]Face, 0, len(rows)/YunetRowStride)
	for offset := 0; offset+YunetRowStride <= len(rows); offset += YunetRowStride {
		row := rows[offset : offset+YunetRowStride]
		score := row[14]
		if !isFinite(score) || score < float32(scoreThreshold) {
			continue
		}
		valid := true
		for _, v := range row[:14] {
			if !isFinite(v) {
				valid = false
				break
			}
		}
		x, y, w, h := row[0], row[1], row[2], row[3]
		if !valid || w <= 0 || h <= 0 {
			continue
		}
		face := Face{Score: score}
		face.BBoxXYXY = [4]float32{x / float32(scale), y / float32(scale),
			(x + w) / float32(scale), (y + h) / float32(scale)}
		for i := 0; i < 5; i++ {
			face.Landmarks[i] = [2]float32{
				row[4+2*i] / float32(scale),
				row[5+2*i] / float32(scale),
			}
		}
		candidates = append(candidates, face)
	}
	kept := nms(candidates, nmsThreshold)
	if len(kept) > maxDetections {
		kept = kept[:maxDetections]
	}
	return kept
}

func isFinite(v float32) bool {
	return !(v != v || v > 3.4e38 || v < -3.4e38)
}
