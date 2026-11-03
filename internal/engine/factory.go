package engine

// Factory builds an Engine from ONNX files. The onnxrt package (CGO builds)
// registers the real implementation during init; without CGO the factory
// stays nil and New reports ErrEngineUnavailable.
type Factory func(opts Options) (Engine, error)

var registered Factory

// RegisterBackend installs the engine factory. It is called from init() of
// the onnxrt package and is not safe for concurrent use by design: it runs
// exactly once during package initialization.
func RegisterBackend(factory Factory) {
	registered = factory
}

// New creates the engine from the registered backend.
func New(opts Options) (Engine, error) {
	if registered == nil {
		return nil, ErrEngineUnavailable
	}
	return registered(opts.withDefaults())
}
