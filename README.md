# face-recognition-server

Standalone face recognition service written in **Go + Vue 3**, built on the
[ONNX Runtime](https://onnxruntime.ai/) inference of two open models from the
Apache-2.0 licensed [opencv_zoo](https://github.com/opencv/opencv_zoo):

| Model | Role |
| --- | --- |
| [YuNet](https://github.com/opencv/opencv_zoo/tree/main/models/face_detection_yunet) | face detection (5-point landmarks) |
| [SFace](https://github.com/opencv/opencv_zoo/tree/main/models/face_recognition_sface) | face embedding / recognition |

The project keeps the whole pipeline — detection decoding, NMS, landmark
alignment, embeddings and matching — in pure Go on top of the ONNX Runtime C
API, so the server ships as one binary plus one shared library. Employee
profiles and face templates live in a single SQLite file; enrollment images
are stored as samples and converted to normalized embeddings.

## Features

- Employee face library management (REST + Vue UI): profiles, sample upload,
  deletion, previews.
- Image recognition: multi-face detection with known / unknown verdicts and
  similarity scores.
- Quality gating on enrollment: minimum face size, face area ratio, blur.
- RTSP live recognition and enrollment with a manual first-sample
  confirmation.

## Quick start

```bash
# 1. download the ONNX models (~38 MB total, into ./models)
./scripts/download-models.sh        # or scripts/download-models.ps1 on Windows

# 2. build & run (CGO + onnxruntime shared library required for inference)
make build && ./bin/server

# open http://localhost:8080
```

Docker:

```bash
docker build -t face-recognition-server .
docker run -p 8080:8080 -v face-data:/app/data face-recognition-server
```

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `FACE_RECOGNITION_ADDR` | `:8080` | HTTP listen address |
| `FACE_RECOGNITION_DB` | `data/faces.sqlite3` | SQLite database file |
| `FACE_RECOGNITION_MODELS` | `models` | Directory with the ONNX models |
| `FACE_RECOGNITION_KNOWN_THRESHOLD` | `0.45` | cosine similarity for a known face |
| `FACE_RECOGNITION_UNKNOWN_THRESHOLD` | `0.30` | below this a face is reported unknown |
| `FACE_RECOGNITION_MAX_IMAGE_MB` | `20` | upload size limit in MB |

## Development

```bash
make test-nocgo   # pure-Go unit tests, no C toolchain needed
make vet
make web-install web-build   # Vue 3 + Vite frontend
```

The inference adapter is isolated behind a `cgo` build tag: without a C
compiler the server still builds and serves the UI, and recognition endpoints
report that the engine is unavailable.

## License

Apache-2.0. The ONNX models are downloaded from opencv_zoo (Apache-2.0) and
are not committed to this repository.
