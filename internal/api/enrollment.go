package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
	"github.com/LinuxSuRen/face-recognition-server/internal/recognizer"
	"github.com/LinuxSuRen/face-recognition-server/internal/store"
)

// RTSP enrollment state machine, mirroring the reference pipeline: the
// first qualified face needs a manual confirmation (valid 5 seconds), then
// samples are auto-collected — at most 5 total, at least 2 seconds apart,
// skipping faces too similar to an already saved sample.
const (
	enrollTargetSamples = 5
	enrollMinInterval   = 2 * time.Second
	enrollConfirmWindow = 5 * time.Second
	enrollMaxSimilarity = 0.93
)

var poseHints = []string{
	"第 1 张：请正对镜头，保持人脸清晰",
	"第 2 张：请向左转头，保持人脸清晰",
	"第 3 张：请向右转头，保持人脸清晰",
	"第 4 张：请稍微抬头，保持人脸清晰",
	"第 5 张：请稍微低头，保持人脸清晰",
}

func poseHint(saved int) string {
	if saved < len(poseHints) {
		return poseHints[saved]
	}
	return "请保持人脸清晰"
}

type enrollmentCandidate struct {
	face engine.ExtractedFace
	jpeg []byte
	at   time.Time
}

// enrollmentController drives one enrollment session.
type enrollmentController struct {
	faces      *facesAPI
	employeeID int64
	// stop ends the RTSP session (completion or fatal error).
	stop func()

	// tunables, overridable in tests
	target        int
	minInterval   time.Duration
	maxSimilarity float64

	mu          sync.Mutex
	status      string // waiting_for_face | awaiting_confirmation | collecting | complete | error
	confirmed   bool
	candidate   *enrollmentCandidate
	savedCount  int
	saved       [][]float32
	lastAttempt time.Time
	hint        string
}

func newEnrollmentController(faces *facesAPI, employeeID int64, stop func()) *enrollmentController {
	return &enrollmentController{
		faces:         faces,
		employeeID:    employeeID,
		stop:          stop,
		target:        enrollTargetSamples,
		minInterval:   enrollMinInterval,
		maxSimilarity: enrollMaxSimilarity,
		status:        "waiting_for_face",
		hint:          poseHint(0),
	}
}

// Snapshot returns the JSON view embedded in every enrollment event.
func (c *enrollmentController) Snapshot() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

// ErrNoCandidate marks a confirm without a pending face.
var ErrNoCandidate = errors.New("当前没有可确认的合格人脸")

// confirm stores the pending first sample and starts auto-collection.
func (c *enrollmentController) confirm(ctx context.Context) (map[string]any, error) {
	c.mu.Lock()
	candidate := c.candidate
	if c.status != "awaiting_confirmation" || candidate == nil {
		c.mu.Unlock()
		return nil, ErrNoCandidate
	}
	if time.Since(candidate.at) > enrollConfirmWindow {
		c.candidate = nil
		c.status = "waiting_for_face"
		c.hint = "确认窗口已过期，请等待新的合格人脸"
		c.mu.Unlock()
		return nil, errors.New(c.hint)
	}
	c.mu.Unlock()

	if err := c.persist(ctx, candidate); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.candidate = nil
	c.confirmed = true
	c.savedCount = 1
	c.lastAttempt = time.Now()
	if c.savedCount >= c.target {
		c.status = "complete"
		c.hint = "第一张样本已确认，本次录入完成"
	} else {
		c.status = "collecting"
		c.hint = "第 1 张已人工确认并保存；" + poseHint(1)
	}
	snapshot := c.snapshotLocked()
	complete := c.status == "complete"
	c.mu.Unlock()
	if complete {
		c.stop()
	}
	return snapshot, nil
}

// analyze implements rtsp.Analyzer for enrollment sessions.
func (c *enrollmentController) analyze(ctx context.Context, img *engine.Image, jpeg []byte) (any, error) {
	if c.faces.engine == nil {
		return nil, errors.New(engineUnavailableDetail)
	}
	faces, err := c.faces.engine.Extract(img)
	if err != nil {
		return nil, err
	}
	selected, selectErr := recognizer.SelectEnrollmentFace(faces)

	payload := map[string]any{
		"faces": enrollmentFaces(faces),
	}
	c.mu.Lock()
	// fatal preconditions checked on every frame
	if employee, err := c.faces.store.GetEmployee(ctx, c.employeeID); err != nil ||
		!employee.Enabled {
		c.status = "error"
		c.hint = "员工不存在或已停用，录入已停止"
		payload["enrollment"] = c.snapshotLocked()
		stop := c.stop
		c.mu.Unlock()
		stop()
		return payload, nil
	}
	if c.status == "error" {
		payload["enrollment"] = c.snapshotLocked()
		c.mu.Unlock()
		return payload, nil
	}
	if selectErr != nil {
		if c.status != "complete" && c.status != "error" {
			if c.confirmed {
				c.status = "collecting"
			} else {
				c.status = "waiting_for_face"
			}
			c.hint = poseHint(c.savedCount) + "；" + friendlyEnrollmentError(selectErr)
			c.candidate = nil
		}
		payload["enrollment"] = c.snapshotLocked()
		c.mu.Unlock()
		return payload, nil
	}
	if c.status == "complete" {
		payload["enrollment"] = c.snapshotLocked()
		c.mu.Unlock()
		return payload, nil
	}

	if !c.confirmed {
		c.candidate = &enrollmentCandidate{face: selected, jpeg: jpeg, at: time.Now()}
		c.status = "awaiting_confirmation"
		c.hint = poseHint(0) + "；请确认画面中是所选员工，点击确认后保存第 1 张"
		payload["enrollment"] = c.snapshotLocked()
		c.mu.Unlock()
		return payload, nil
	}

	// auto-collect phase
	c.status = "collecting"
	if time.Since(c.lastAttempt) < c.minInterval {
		remaining := c.minInterval - time.Since(c.lastAttempt)
		c.hint = poseHint(c.savedCount) + "；距离上次保存需至少 2 秒，请等待 " +
			remaining.Truncate(time.Millisecond).String()
		payload["enrollment"] = c.snapshotLocked()
		c.mu.Unlock()
		return payload, nil
	}
	if reason, similar := c.tooSimilar(selected.Embedding); similar {
		c.lastAttempt = time.Now()
		c.hint = "与已保存样本过于相似（" + reason + "），请调整姿态"
		payload["enrollment"] = c.snapshotLocked()
		c.mu.Unlock()
		return payload, nil
	}
	c.mu.Unlock()

	if err := c.persist(ctx, &enrollmentCandidate{face: selected, jpeg: jpeg, at: time.Now()}); err != nil {
		c.mu.Lock()
		c.hint = friendlyEnrollmentError(err)
		payload["enrollment"] = c.snapshotLocked()
		c.mu.Unlock()
		return payload, nil
	}

	c.mu.Lock()
	c.lastAttempt = time.Now()
	c.savedCount++
	if c.savedCount >= c.target {
		c.status = "complete"
		c.hint = "本次录入完成，共新增 5 张样本"
	} else {
		c.hint = "上一张已保存；" + poseHint(c.savedCount)
	}
	payload["enrollment"] = c.snapshotLocked()
	complete := c.status == "complete"
	c.mu.Unlock()
	if complete {
		c.stop()
	}
	return payload, nil
}

func (c *enrollmentController) snapshotLocked() map[string]any {
	return map[string]any{
		"state":           c.status,
		"employee_id":     c.employeeID,
		"saved_count":     c.savedCount,
		"target_samples":  c.target,
		"candidate_ready": c.status == "awaiting_confirmation" && c.candidate != nil,
		"hint":            c.hint,
	}
}

// persist stores one sample and registers its embedding for dedup. The new
// face must also agree with the employee's existing library, like a manual
// photo upload.
func (c *enrollmentController) persist(ctx context.Context, candidate *enrollmentCandidate) error {
	existing, err := c.faces.store.ListTemplates(ctx, c.employeeID)
	if err != nil {
		return err
	}
	var samples [][]float32
	for _, template := range existing {
		if template.ModelVersion != c.faces.engine.ModelVersion() || template.EmbeddingDim == 0 {
			continue
		}
		samples = append(samples, decodeTemplateEmbedding(template.Embedding))
	}
	if !recognizer.ConsistentWith(candidate.face.Embedding, samples, recognizer.MinEnrollmentConsistency) {
		return errors.New("face does not match this employee's existing samples")
	}
	_, err = c.faces.store.AddTemplate(ctx, c.employeeID, store.Enrollment{
		Embedding:     candidate.face.Embedding,
		ModelVersion:  c.faces.engine.ModelVersion(),
		QualityScore:  candidate.face.Quality.Score,
		FaceBBox:      candidate.face.Face.BBoxXYXY,
		Image:         candidate.jpeg,
		ImageMimeType: "image/jpeg",
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.saved = append(c.saved, candidate.face.Embedding)
	c.mu.Unlock()
	return nil
}

// tooSimilar reports whether the embedding matches a saved sample of this
// session closely enough to skip.
func (c *enrollmentController) tooSimilar(embedding []float32) (string, bool) {
	for index, saved := range c.saved {
		if recognizer.ConsistentWith(embedding, [][]float32{saved}, c.maxSimilarity) {
			return fmt.Sprintf("与第 %d 张相似", index+1), true
		}
	}
	return "", false
}

// friendlyEnrollmentError maps engine/store errors to actionable hints.
func friendlyEnrollmentError(err error) string {
	message := err.Error()
	switch {
	case errors.Is(err, recognizer.ErrNoFace):
		return "未检测到清晰人脸"
	case errors.Is(err, recognizer.ErrMultipleFaces):
		return "画面中有多张人脸，请仅保留所选员工"
	case errors.Is(err, store.ErrInvalidEmbedding):
		return "特征提取失败，请重试"
	}
	return message
}

// enrollmentFaces renders boxes without identity for the live view.
func enrollmentFaces(faces []engine.ExtractedFace) []facePayload {
	payloads := make([]facePayload, 0, len(faces))
	for i, face := range faces {
		payloads = append(payloads, facePayload{
			ID:         i + 1,
			ClassName:  "face",
			Confidence: face.Quality.DetectorConfidence,
			BBoxXYXY:   roundBox(face.Face.BBoxXYXY),
			Quality: faceQualityPayload{
				Accepted:     face.Quality.Accepted,
				Score:        face.Quality.Score,
				Reasons:      face.Quality.Reasons,
				BlurVariance: face.Quality.BlurVariance,
				FaceSize:     face.Quality.FaceSize,
			},
		})
		if payloads[i].Quality.Reasons == nil {
			payloads[i].Quality.Reasons = []string{}
		}
	}
	return payloads
}
