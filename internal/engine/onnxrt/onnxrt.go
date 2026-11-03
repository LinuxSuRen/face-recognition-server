//go:build cgo

package onnxrt

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
)

// LibraryPathEnv names the environment variable pointing at the onnxruntime
// shared library. The wrapper needs a version matching its bundled C headers
// (1.31.0 at the time of writing).
const LibraryPathEnv = "FACE_RECOGNITION_ORT_LIB"

var envOnce sync.Once

var envErr error

func init() {
	engine.RegisterBackend(New)
}

// initializeEnvironment loads the shared library exactly once. The library
// path comes from LibraryPathEnv, falling back to the platform's usual
// library name.
func initializeEnvironment() error {
	envOnce.Do(func() {
		libPath := os.Getenv(LibraryPathEnv)
		if libPath == "" {
			if runtime.GOOS == "windows" {
				libPath = "onnxruntime.dll"
			} else {
				libPath = "libonnxruntime.so"
			}
		}
		ort.SetSharedLibraryPath(libPath)
		if err := ort.InitializeEnvironment(ort.WithLogLevelWarning()); err != nil {
			envErr = fmt.Errorf("initializing onnxruntime: %w", err)
		}
	})
	return envErr
}

// ortEngine implements engine.Engine with two ONNX sessions.
type ortEngine struct {
	opts engine.Options

	detectorMu          sync.Mutex
	detector            *ort.DynamicAdvancedSession
	detectorInput       *ort.Tensor[float32]
	detectorInputWidth  int
	detectorInputHeight int
	detectorOutputs     []string

	recognizerMu    sync.Mutex
	recognizer      *ort.DynamicAdvancedSession
	recognizerInput *ort.Tensor[float32]

	modelVersion string
}

// New builds the engine from the YuNet and SFace ONNX files. Options arrive
// already defaulted from engine.New.
func New(opts engine.Options) (engine.Engine, error) {
	// fail with classified errors before touching onnxruntime so callers
	// can tell missing models from library problems
	for _, path := range []struct {
		file  string
		label string
	}{
		{opts.DetectorPath, "YuNet detector model"},
		{opts.RecognizerPath, "SFace recognizer model"},
	} {
		if info, err := os.Stat(path.file); err != nil || info.IsDir() {
			return nil, fmt.Errorf("%s not found at %s: %w", path.label, path.file, os.ErrNotExist)
		}
	}
	if err := initializeEnvironment(); err != nil {
		return nil, err
	}

	detectorInputName, detectorOutputNames, err := inputAndOutputNames(opts.DetectorPath)
	if err != nil {
		return nil, err
	}
	// static-input exports (opencv_zoo 2023mar: 640x640) must be fed the
	// exact tensor shape; the pipeline then maps coordinates back per axis
	if inputs, _, infoErr := ort.GetInputOutputInfo(opts.DetectorPath); infoErr == nil && len(inputs) > 0 {
		if dims := inputs[0].Dimensions; len(dims) == 4 && dims[2] > 0 && dims[3] > 0 {
			opts.DetectorFixedHeight = int(dims[2])
			opts.DetectorFixedWidth = int(dims[3])
		}
	}
	detector, err := ort.NewDynamicAdvancedSession(opts.DetectorPath,
		[]string{detectorInputName}, detectorOutputNames, nil)
	if err != nil {
		return nil, fmt.Errorf("loading YuNet model %s: %w", opts.DetectorPath, err)
	}

	recognizerInputName, recognizerOutputName, err := firstInputOutputNames(opts.RecognizerPath)
	if err != nil {
		detector.Destroy()
		return nil, err
	}
	recognizer, err := ort.NewDynamicAdvancedSession(opts.RecognizerPath,
		[]string{recognizerInputName}, []string{recognizerOutputName}, nil)
	if err != nil {
		detector.Destroy()
		return nil, fmt.Errorf("loading SFace model %s: %w", opts.RecognizerPath, err)
	}

	recognizerInput, err := ort.NewTensor[float32](
		ort.NewShape(1, 3, engine.SFaceSize, engine.SFaceSize),
		make([]float32, 3*engine.SFaceSize*engine.SFaceSize))
	if err != nil {
		detector.Destroy()
		recognizer.Destroy()
		return nil, fmt.Errorf("creating SFace input tensor: %w", err)
	}

	return &ortEngine{
		opts:            opts,
		detector:        detector,
		detectorInput:   nil,
		detectorOutputs: detectorOutputNames,
		recognizer:      recognizer,
		recognizerInput: recognizerInput,
		modelVersion:    modelVersion(opts.RecognizerPath),
	}, nil
}

// Extract runs the shared pipeline with the two ONNX sessions.
func (e *ortEngine) Extract(img *engine.Image) ([]engine.ExtractedFace, error) {
	return engine.ExtractWith(img, e.opts, e.detect, e.embed)
}

// ModelVersion identifies the recognizer weights: file name plus a short
// content hash, matching the reference pipeline's scheme.
func (e *ortEngine) ModelVersion() string {
	return e.modelVersion
}

// Close releases the sessions and cached tensors.
func (e *ortEngine) Close() error {
	e.detectorMu.Lock()
	if e.detectorInput != nil {
		e.detectorInput.Destroy()
		e.detectorInput = nil
	}
	e.detectorMu.Unlock()
	e.recognizerMu.Lock()
	e.recognizerInput.Destroy()
	e.recognizerMu.Unlock()
	return errors.Join(e.detector.Destroy(), e.recognizer.Destroy())
}

// detect feeds a RGB NCHW tensor through YuNet and returns the raw
// multi-head output mapped by output name.
func (e *ortEngine) detect(input []float32, width, height int) (*engine.YuNetHeads, error) {
	e.detectorMu.Lock()
	defer e.detectorMu.Unlock()

	if e.detectorInput == nil || e.detectorInputWidth != width || e.detectorInputHeight != height {
		if e.detectorInput != nil {
			e.detectorInput.Destroy()
		}
		tensor, err := ort.NewTensor[float32](
			ort.NewShape(1, 3, int64(height), int64(width)), input)
		if err != nil {
			e.detectorInput = nil
			return nil, fmt.Errorf("creating YuNet input tensor: %w", err)
		}
		e.detectorInput = tensor
		e.detectorInputWidth = width
		e.detectorInputHeight = height
	} else {
		copy(e.detectorInput.GetData(), input)
	}

	outputs := make([]ort.Value, len(e.detectorOutputs))
	if err := e.detector.Run([]ort.Value{e.detectorInput}, outputs); err != nil {
		return nil, fmt.Errorf("running YuNet: %w", err)
	}

	heads := engine.NewYuNetHeads()
	for index, output := range outputs {
		if output == nil {
			continue
		}
		tensor, ok := output.(*ort.Tensor[float32])
		if !ok {
			output.Destroy()
			return nil, fmt.Errorf("unexpected YuNet output type %T", output)
		}
		applyHeadOutput(heads, e.detectorOutputs[index], tensor.GetData())
		output.Destroy()
	}
	return heads, nil
}

// applyHeadOutput routes one raw tensor into the heads by its name
// (cls_8 / obj_16 / bbox_32 / kps_8 ...).
func applyHeadOutput(heads *engine.YuNetHeads, name string, data []float32) {
	kind, stride, ok := splitHeadName(name)
	if !ok {
		return
	}
	values := make([]float32, len(data))
	copy(values, data)
	switch kind {
	case "cls":
		heads.Cls[stride] = values
	case "obj":
		heads.Obj[stride] = values
	case "bbox":
		heads.Bbox[stride] = values
	case "kps":
		heads.Kps[stride] = values
	}
}

// splitHeadName parses names like "cls_8" into the head kind and stride.
func splitHeadName(name string) (string, int, bool) {
	for _, stride := range []int{8, 16, 32} {
		for _, kind := range []string{"cls", "obj", "bbox", "kps"} {
			if name == kind+"_"+fmt.Sprint(stride) {
				return kind, stride, true
			}
		}
	}
	return "", 0, false
}

// embed feeds an aligned 112x112 tensor through SFace and returns the raw
// embedding.
func (e *ortEngine) embed(input []float32) ([]float32, error) {
	e.recognizerMu.Lock()
	defer e.recognizerMu.Unlock()

	copy(e.recognizerInput.GetData(), input)
	outputs := []ort.Value{nil}
	if err := e.recognizer.Run([]ort.Value{e.recognizerInput}, outputs); err != nil {
		return nil, fmt.Errorf("running SFace: %w", err)
	}
	output := outputs[0]
	defer output.Destroy()

	tensor, ok := output.(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("unexpected SFace output type %T", output)
	}
	data := tensor.GetData()
	out := make([]float32, len(data))
	copy(out, data)
	return out, nil
}

// firstInputOutputNames reads the model's first input and output names so
// the adapter does not hardcode them.
func firstInputOutputNames(modelPath string) (string, string, error) {
	inputs, outputs, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return "", "", fmt.Errorf("reading model io info %s: %w", modelPath, err)
	}
	if len(inputs) == 0 || len(outputs) == 0 {
		return "", "", fmt.Errorf("model %s has no inputs or no outputs", modelPath)
	}
	return inputs[0].Name, outputs[0].Name, nil
}

// inputAndOutputNames returns the first input and every output name (YuNet
// exposes twelve raw heads: cls/obj/bbox/kps for strides 8/16/32).
func inputAndOutputNames(modelPath string) (string, []string, error) {
	inputs, outputs, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return "", nil, fmt.Errorf("reading model io info %s: %w", modelPath, err)
	}
	if len(inputs) == 0 || len(outputs) == 0 {
		return "", nil, fmt.Errorf("model %s has no inputs or no outputs", modelPath)
	}
	names := make([]string, 0, len(outputs))
	for _, output := range outputs {
		names = append(names, output.Name)
	}
	return inputs[0].Name, names, nil
}

func modelVersion(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return path
	}
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%s:%x", filepath.Base(path), sum[:6])
}
