// Package onnxrt binds the ONNX Runtime shared library to the pure-Go engine
// pipeline. The binding requires CGO; without it this package compiles to
// nothing and engine.New reports ErrEngineUnavailable, so CGO-free builds
// still produce a runnable server (health + UI) without inference.
package onnxrt
