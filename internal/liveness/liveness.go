// Package liveness implements passive presentation-attack detection for
// face login. The current checker analyses consecutive frames: a live face
// shows independent micro-movements of individual landmarks (blinks,
// nostril motion, slight head rotation), while a held photo or screen
// replay either stays static or moves all landmarks rigidly together.
package liveness

import (
	"fmt"
	"math"

	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
)

// Result of a liveness check.
type Result struct {
	// Live reports whether the frames appear to show a real face.
	Live bool `json:"live"`
	// Score is the confidence in [0,1]; above Threshold means live.
	Score float64 `json:"score"`
	// Reason explains a failed check.
	Reason string `json:"reason"`
}

// MinFrames is the smallest number of consecutive frames that can produce
// a meaningful liveness signal.
const MinFrames = 3

// Check analyses consecutive face detections for signs of life. faces must
// contain at least MinFrames entries, each with landmarks from a separate
// frame in chronological order. threshold is the minimum liveness score
// (0-1) required to pass.
func Check(faces []engine.ExtractedFace, threshold float64) Result {
	if len(faces) < MinFrames {
		return Result{Live: false, Score: 0, Reason: "帧数不足"}
	}

	// Normalise landmarks: express each landmark relative to the face
	// bounding-box center and scaled by the face size so head movement
	// doesn't dominate the signal.
	normalized := make([][5][2]float64, len(faces))
	for i, face := range faces {
		box := face.Face.BBoxXYXY
		cx := (float64(box[0]) + float64(box[2])) / 2
		cy := (float64(box[1]) + float64(box[3])) / 2
		size := math.Max(float64(box[2]-box[0]), float64(box[3]-box[1]))
		if size < 1 {
			size = 1
		}
		for j, landmark := range face.Face.Landmarks {
			normalized[i][j][0] = (float64(landmark[0]) - cx) / size
			normalized[i][j][1] = (float64(landmark[1]) - cy) / size
		}
	}

	// For each landmark axis, compute the standard deviation across frames.
	// Live faces show independent per-landmark variation (blinks, mouth
	// micro-movement); rigid photo movement shifts all landmarks together.
	axisStdDev := make([]float64, 0, 10)
	for landmark := 0; landmark < 5; landmark++ {
		for axis := 0; axis < 2; axis++ {
			var values []float64
			for _, frame := range normalized {
				values = append(values, frame[landmark][axis])
			}
			axisStdDev = append(axisStdDev, stdDev(values))
		}
	}

	// The liveness signal: landmarks with independent variance above a
	// noise floor. A perfectly rigid photo (even with hand shake) has all
	// axes moving in lock-step after normalisation, so their relative
	// variance is near zero.
	liveAxes := 0
	for _, deviation := range axisStdDev {
		if deviation > 0.002 { // noise floor from landmark detection jitter
			liveAxes++
		}
	}

	// At least 3 of 10 axes should show independent variation for a real
	// face (both eyes, nose and mouth all contribute).
	score := float64(liveAxes) / 10.0

	result := Result{Score: math.Round(score*1000) / 1000}
	if score >= threshold {
		result.Live = true
	} else {
		result.Live = false
		result.Reason = fmt.Sprintf("画面变化不足（%d/10 个关键点有独立运动，需 ≥%d）", liveAxes, int(threshold*10))
	}
	return result
}

func stdDev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	var mean float64
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	var variance float64
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(values) - 1)
	return math.Sqrt(variance)
}
