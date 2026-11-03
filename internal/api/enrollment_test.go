package api

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
	"github.com/LinuxSuRen/face-recognition-server/internal/store"
)

func enrollmentImage() *engine.Image {
	return engine.NewImage(300, 300)
}

func newEnrollmentTest(t *testing.T) (*enrollmentController, *fakeEngine, *store.Store, *atomic.Int32) {
	t.Helper()
	fake := &fakeEngine{faces: []engine.ExtractedFace{goodFace()}}
	faceStore, err := store.Open(filepath.Join(t.TempDir(), "faces.sqlite3"))
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(func() { faceStore.Close() })
	created, err := faceStore.CreateEmployee(context.Background(), "E010", "录入员工", "", true)
	if err != nil {
		t.Fatalf("creating employee: %v", err)
	}
	faces := &facesAPI{store: faceStore, engine: fake}
	var stopped atomic.Int32
	controller := newEnrollmentController(faces, created.ID, func() { stopped.Add(1) })
	return controller, fake, faceStore, &stopped
}

func enrollmentPayload(t *testing.T, controller *enrollmentController, img *engine.Image) map[string]any {
	t.Helper()
	result, err := controller.analyze(context.Background(), img, []byte("jpeg-bytes"))
	if err != nil {
		t.Fatalf("analyze failed: %v", err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("analyze returned %T, want map[string]any", result)
	}
	return payload
}

func enrollmentState(payload map[string]any) map[string]any {
	return payload["enrollment"].(map[string]any)
}

// tiltedFace mimics the same person with a different pose: ~0.894 cosine to
// goodFace — above the 0.35 consistency floor, below the 0.93 dedup gate.
func tiltedFace() engine.ExtractedFace {
	face := goodFace()
	face.Embedding = []float32{1, 0.5, 0, 0}
	return face
}

func TestEnrollmentWaitsForConfirmationThenCollects(t *testing.T) {
	controller, fake, faceStore, stopped := newEnrollmentTest(t)
	ctx := context.Background()

	// first qualified face awaits manual confirmation
	payload := enrollmentPayload(t, controller, enrollmentImage())
	state := enrollmentState(payload)
	if state["state"] != "awaiting_confirmation" || state["candidate_ready"] != true {
		t.Fatalf("expected awaiting_confirmation: %v", state)
	}

	// confirm stores sample 1 and switches to collecting
	snapshot, err := controller.confirm(ctx)
	if err != nil {
		t.Fatalf("confirm failed: %v", err)
	}
	if snapshot["state"] != "collecting" || snapshot["saved_count"].(int) != 1 {
		t.Fatalf("unexpected confirm snapshot: %v", snapshot)
	}
	samples, err := faceStore.ListTemplates(ctx, employeeIDOf(t, faceStore))
	if err != nil || len(samples) != 1 {
		t.Fatalf("sample not stored: %v %+v", err, samples)
	}
	if samples[0].ModelVersion != "sface:test" || samples[0].ImageMimeType != "image/jpeg" {
		t.Fatalf("unexpected stored sample: %+v", samples[0])
	}

	// within the 2s interval nothing is saved; the hint explains it
	fake.faces = []engine.ExtractedFace{tiltedFace()}
	payload = enrollmentPayload(t, controller, enrollmentImage())
	state = enrollmentState(payload)
	if state["saved_count"].(int) != 1 || state["state"] != "collecting" {
		t.Fatalf("interval must not save: %v", state)
	}

	// after the interval an identical face is skipped as too similar
	fake.faces = []engine.ExtractedFace{goodFace()}
	controller.lastAttempt = time.Now().Add(-3 * time.Second)
	payload = enrollmentPayload(t, controller, enrollmentImage())
	state = enrollmentState(payload)
	if state["saved_count"].(int) != 1 {
		t.Fatalf("similar face must be skipped: %v", state)
	}

	// a same-person different-pose face passes both gates and is saved
	fake.faces = []engine.ExtractedFace{tiltedFace()}
	controller.lastAttempt = time.Now().Add(-3 * time.Second)
	payload = enrollmentPayload(t, controller, enrollmentImage())
	state = enrollmentState(payload)
	if state["saved_count"].(int) != 2 {
		t.Fatalf("distinct pose must be saved: %v", state)
	}

	// a face inconsistent with the employee's library is rejected
	fake.faces = []engine.ExtractedFace{{
		Face: goodFace().Face, Quality: goodFace().Quality, Embedding: []float32{0, 0, 0, 1},
	}}
	controller.lastAttempt = time.Now().Add(-3 * time.Second)
	payload = enrollmentPayload(t, controller, enrollmentImage())
	state = enrollmentState(payload)
	if state["saved_count"].(int) != 2 {
		t.Fatalf("inconsistent face must be rejected: %v", state)
	}

	if stopped.Load() != 0 {
		t.Fatalf("session must not stop before the target")
	}
}

func TestEnrollmentCompletesAtTargetAndStops(t *testing.T) {
	controller, _, _, stopped := newEnrollmentTest(t)
	ctx := context.Background()
	// disable throttles so the full cycle runs in milliseconds
	controller.minInterval = 0
	controller.maxSimilarity = 1.01

	if _, err := controller.confirm(ctx); !errors.Is(err, ErrNoCandidate) {
		t.Fatalf("confirm without candidate must be ErrNoCandidate, got %v", err)
	}
	enrollmentPayload(t, controller, enrollmentImage())
	if _, err := controller.confirm(ctx); err != nil {
		t.Fatalf("confirm failed: %v", err)
	}
	for i := 0; i < 4; i++ {
		enrollmentPayload(t, controller, enrollmentImage())
	}
	snapshot := controller.Snapshot()
	if snapshot["saved_count"].(int) != 5 || snapshot["state"] != "complete" {
		t.Fatalf("expected completion at 5 samples: %v", snapshot)
	}
	if stopped.Load() == 0 {
		t.Fatalf("completion must stop the session")
	}
}

func TestEnrollmentHandlesMissingFacesAndDeletedEmployee(t *testing.T) {
	controller, fake, _, _ := newEnrollmentTest(t)
	ctx := context.Background()

	fake.faces = nil
	payload := enrollmentPayload(t, controller, enrollmentImage())
	state := enrollmentState(payload)
	if state["state"] != "waiting_for_face" {
		t.Fatalf("no face must keep waiting_for_face: %v", state)
	}
	if hint, _ := state["hint"].(string); !strings.Contains(hint, "未检测到清晰人脸") {
		t.Fatalf("hint must explain the missing face: %v", state["hint"])
	}

	employeeID := employeeIDOf(t, controller.faces.store)
	if err := controller.faces.store.DeleteEmployee(ctx, employeeID); err != nil {
		t.Fatalf("deleting employee: %v", err)
	}
	fake.faces = []engine.ExtractedFace{goodFace()}
	payload = enrollmentPayload(t, controller, enrollmentImage())
	state = enrollmentState(payload)
	if state["state"] != "error" {
		t.Fatalf("deleted employee must end in error: %v", state)
	}
}

func employeeIDOf(t *testing.T, faceStore *store.Store) int64 {
	t.Helper()
	employees, _, err := faceStore.ListEmployees(context.Background(), store.EmployeeFilter{
		IncludeDisabled: true, Page: 1, PageSize: 10,
	})
	if err != nil || len(employees) == 0 {
		t.Fatalf("listing employees: %v %+v", err, employees)
	}
	return employees[0].ID
}
