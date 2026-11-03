// Package recognizer matches face embeddings against enrolled templates
// with open-set logic: every face gets a known / unknown verdict plus the
// best candidate and its margin, mirroring the reference pipeline's
// max-template scoring (one employee's score is the maximum over their
// samples, so distinct enrollment poses never dilute each other).
package recognizer

import "math"

// Thresholds bounds the cosine similarity decisions.
type Thresholds struct {
	// Known is the similarity at or above which the best candidate wins.
	Known float64
	// Unknown is the similarity below which the face is reported unknown.
	Unknown float64
}

// DefaultThresholds matches the reference pipeline defaults.
var DefaultThresholds = Thresholds{Known: 0.45, Unknown: 0.35}

// Template is one enrolled embedding in the matcher index.
type Template struct {
	EmployeeID   int64
	EmployeeNo   string
	Name         string
	Embedding    []float32
	ModelVersion string
}

// Result is the verdict for one face.
type Result struct {
	// Status is known, unknown or unusable.
	Status string `json:"recognition_status"`
	// EmployeeID, EmployeeNo and Name are set for known faces.
	EmployeeID   int64  `json:"employee_id,omitempty"`
	EmployeeNo   string `json:"employee_no,omitempty"`
	Name         string `json:"name,omitempty"`
	// Similarity is the best employee's max-template cosine.
	Similarity float64 `json:"similarity"`
	// Candidate fields always describe the best employee, even when the
	// verdict is unknown, so the UI can explain the decision.
	CandidateEmployeeID int64   `json:"candidate_employee_id,omitempty"`
	CandidateEmployeeNo string  `json:"candidate_employee_no,omitempty"`
	CandidateName       string  `json:"candidate_name,omitempty"`
	Margin              float64 `json:"margin"`
	// Reason explains non-known verdicts: empty_face_library,
	// below_threshold or poor_quality.
	Reason string `json:"reason,omitempty"`
}

// Match ranks employees for one embedding and applies the thresholds.
func Match(embedding []float32, templates []Template, thresholds Thresholds) Result {
	type employeeScore struct {
		id     int64
		no     string
		name   string
		score  float64
	}
	query := normalize(embedding)
	if query == nil {
		return Result{Status: "unusable", Reason: "poor_quality"}
	}
	best := map[int64]*employeeScore{}
	for _, template := range templates {
		if len(template.Embedding) != len(query) {
			continue
		}
		candidate := normalize(template.Embedding)
		if candidate == nil {
			// zero or broken vectors cannot support a verdict
			continue
		}
		score := dot(query, candidate)
		current, exists := best[template.EmployeeID]
		if !exists {
			best[template.EmployeeID] = &employeeScore{
				id: template.EmployeeID, no: template.EmployeeNo, name: template.Name, score: score}
		} else if score > current.score {
			current.score = score
		}
	}
	if len(best) == 0 {
		return Result{Status: "unknown", Reason: "empty_face_library"}
	}
	ranked := make([]*employeeScore, 0, len(best))
	for _, item := range best {
		ranked = append(ranked, item)
	}
	// insertion sort descending; employee count is small
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && ranked[j].score > ranked[j-1].score; j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}
	top := ranked[0]
	result := Result{
		Status:              "unknown",
		Similarity:          round4(top.score),
		CandidateEmployeeID: top.id,
		CandidateEmployeeNo: top.no,
		CandidateName:       top.name,
	}
	if len(ranked) > 1 {
		result.Margin = round4(top.score - ranked[1].score)
	}
	if top.score <= thresholds.Unknown {
		result.Reason = "below_threshold"
		return result
	}
	result.Status = "known"
	result.EmployeeID = top.id
	result.EmployeeNo = top.no
	result.Name = top.name
	return result
}

func normalize(values []float32) []float32 {
	if len(values) == 0 {
		return nil
	}
	var norm float64
	for _, v := range values {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm <= 1e-12 {
		return nil
	}
	out := make([]float32, len(values))
	for i, v := range values {
		out[i] = float32(float64(v) / norm)
	}
	return out
}

func dot(a, b []float32) float64 {
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

func round4(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return float64(int64(v*10000+0.5)) / 10000
}
