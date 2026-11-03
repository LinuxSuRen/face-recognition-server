# face-recognition-server

<p align="center">
  <img src="docs/logo.svg" width="110" alt="face-recognition-server logo" />
</p>

[![ci](https://github.com/LinuxSuRen/face-recognition-server/actions/workflows/ci.yml/badge.svg)](https://github.com/LinuxSuRen/face-recognition-server/actions/workflows/ci.yml)
[![release](https://github.com/LinuxSuRen/face-recognition-server/actions/workflows/release.yml/badge.svg)](https://github.com/LinuxSuRen/face-recognition-server/actions/workflows/release.yml)
[![GitHub release](https://img.shields.io/github/v/release/LinuxSuRen/face-recognition-server)](https://github.com/LinuxSuRen/face-recognition-server/releases)
[![container image](https://img.shields.io/badge/image-ghcr.io-2496ed)](https://github.com/LinuxSuRen/face-recognition-server/pkgs/container/face-recognition-server)

Standalone face recognition service written in **Go + Vue 3**, built on
[ONNX Runtime](https://onnxruntime.ai/) inference of two open models from the
Apache-2.0 licensed [opencv_zoo](https://github.com/opencv/opencv_zoo):

| Model | Role |
| --- | --- |
| [YuNet](https://github.com/opencv/opencv_zoo/tree/main/models/face_detection_yunet) | face detection with 5-point landmarks |
| [SFace](https://github.com/opencv/opencv_zoo/tree/main/models/face_recognition_sface) | face embedding / recognition |

The whole pipeline — detection decoding, NMS, landmark alignment, embedding
post-processing and matching — is pure Go on top of the ONNX Runtime C API.
Employee profiles and face samples live in one SQLite file; enrollment
images are stored with their 128-d normalized embeddings.

## Features

- **Face library management** (REST + Vue UI): employee profiles, sample
  upload, camera capture, deletion, previews.
- **Image recognition**: multi-face detection with known / unknown verdicts,
  similarity scores and quality-aware `unusable` classification.
- **Enrollment quality gates**: minimum face size (48 px), area ratio
  (≥ 1.5%), Laplacian blur, single-person check, and a consistency floor
  against the employee's existing samples (cosine ≥ 0.35).
- **RTSP live recognition** over SSE: ffmpeg pulls the stream, the server
  analyzes the latest frame at its own pace, the browser renders boxes with
  zero polling; the local camera works too (frames posted from the browser).
- **RTSP enrollment**: the first qualified face requires a manual
  confirmation, then up to 5 samples are auto-collected with pose hints, a
  2-second minimum interval and similarity-based skipping.
- **External recognition API** with bearer tokens (scopes `verify`,
  `identify`, `enroll`) so other systems can add face login on top of this
  platform.
- **Platform login**: local accounts (bcrypt) plus **face login** — bind
  your face under 我的人脸 and sign in from the login page's camera — and
  OIDC federated login (authorization code + PKCE; works with any compliant
  issuer such as [home-gate](https://github.com/LinuxSuRen/home-gate)).
  Management endpoints require a session; users and tokens are managed by
  admins.

## External API (face login for other systems)

Admin issues a token in the UI (系统管理 → 对外接口令牌). The token carries
scopes; `Authorization: Bearer face_...` authenticates:

```bash
# enroll a subject (creates the profile on first use)
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -F image=@photo.jpg -F subject=user-42 -F name=Alice \
  http://localhost:8080/api/v1/enroll

# 1:1 verify — the login check
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -F image=@login.jpg -F subject=user-42 \
  http://localhost:8080/api/v1/verify
# -> {"subject":"user-42","verified":true,"similarity":0.87,"reason":"ok"}

# 1:N identify — who is in this picture?
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -F image=@photo.jpg http://localhost:8080/api/v1/identify
```

`verified` requires similarity ≥ `FACE_RECOGNITION_KNOWN_THRESHOLD`;
`reason` distinguishes `ok`, `below_threshold`, `subject_not_found`,
`subject_not_enrolled` and `no_usable_face`.

## Quick start

```bash
# 1. download the ONNX models (~38 MB total, into ./models)
./scripts/download-models.sh          # or scripts/download-models.ps1

# 2. build & run (CGO + onnxruntime shared library + ffmpeg for RTSP)
make build && ./bin/server

# open http://localhost:8080 — the first boot creates the admin account
# (password from FACE_RECOGNITION_ADMIN_PASSWORD or printed in the log)
```

Docker (the published image ships the web UI, the ONNX models, onnxruntime
and ffmpeg; keep the face database on a volume):

```bash
docker run -d --name face-recognition -p 8080:8080 \
  -v face-data:/app/data ghcr.io/linuxsuren/face-recognition-server:latest
# or a pinned version
docker run -d -p 8080:8080 -v face-data:/app/data \
  ghcr.io/linuxsuren/face-recognition-server:0.0.1
```

Releases are cut by pushing a `v*` tag: the
[release workflow](.github/workflows/release.yml) publishes the multi-arch
GHCR image, the linux/amd64 binary tarball with checksums and the GitHub
release in one go.

> The onnxruntime shared library version must match the one expected by
> [onnxruntime_go](https://github.com/yalue/onnxruntime_go) (1.31.0 at the
> time of writing). `ffmpeg` must be on `PATH` for RTSP features.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `FACE_RECOGNITION_ADDR` | `:8080` | HTTP listen address; when the port is already in use the server logs a warning and drifts to a free OS-assigned port on the same host |
| `FACE_RECOGNITION_DB` | `data/faces.sqlite3` | SQLite database file |
| `FACE_RECOGNITION_MODELS` | `models` | directory with the ONNX models |
| `FACE_RECOGNITION_ORT_LIB` | platform default | path to the onnxruntime shared library |
| `FACE_RECOGNITION_KNOWN_THRESHOLD` | `0.45` | cosine similarity for a known face |
| `FACE_RECOGNITION_UNKNOWN_THRESHOLD` | `0.35` | below this a face is reported unknown |
| `FACE_RECOGNITION_MAX_IMAGE_MB` | `10` | upload size limit in MB |
| `FACE_RECOGNITION_AUTH` | `enabled` | session auth for management endpoints; `disabled` is the single-user escape hatch |
| `FACE_RECOGNITION_ADMIN_PASSWORD` | random, logged once | bootstrap admin password on first boot |
| `FACE_RECOGNITION_OIDC_ISSUER` | — | OIDC issuer URL (e.g. home-gate); enables SSO |
| `FACE_RECOGNITION_OIDC_CLIENT_ID` | — | OIDC client id |
| `FACE_RECOGNITION_OIDC_CLIENT_SECRET` | — | OIDC client secret |
| `FACE_RECOGNITION_OIDC_REDIRECT_URL` | — | OAuth callback URL (`/api/v1/auth/oauth/callback`) |

## HTTP API

All list endpoints share the pagination contract
`data` / `page` / `total` / `page_size` with `page` starting at 1.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/healthz` | status, engine and database readiness |
| POST | `/api/v1/auth/login` | username/password → session cookie |
| POST | `/api/v1/auth/face-login` | face photo → session cookie (face login) |
| POST | `/api/v1/auth/logout` | drop the session |
| GET | `/api/v1/auth/session` | current user, auth/SSO state |
| GET | `/api/v1/auth/oauth/{start,callback}` | OIDC authorization-code + PKCE flow |
| GET / POST / DELETE | `/api/v1/me/faces` | manage my bound login faces |
| POST / GET | `/api/v1/users` | create / page platform users (admin) |
| PATCH | `/api/v1/users/{id}` | rename, reset password, toggle, role (admin) |
| POST / GET | `/api/v1/tokens` | issue (scope-picked) / list API tokens (admin) |
| DELETE | `/api/v1/tokens/{id}` | revoke a token (admin) |
| POST | `/api/v1/enroll` | bearer: enroll a face for a subject |
| POST | `/api/v1/verify` | bearer: 1:1 verify for face login |
| POST | `/api/v1/identify` | bearer: 1:N identify |
| POST / GET | `/api/v1/faces/employees` | create / page employees |
| GET / PATCH / DELETE | `/api/v1/faces/employees/{id}` | detail (+ samples) / update / delete |
| POST / GET | `/api/v1/faces/employees/{id}/samples` | upload / page face samples |
| GET | `/api/v1/faces/employees/{id}/samples/{sid}/image` | sample image bytes |
| DELETE | `/api/v1/faces/employees/{id}/samples/{sid}` | delete one sample |
| POST | `/api/v1/faces/recognize` | multipart `image` → faces + summary |
| GET / PUT | `/api/v1/rtsp/configuration` | RTSP source URL and transport |
| POST | `/api/v1/rtsp/sessions` | start a session, `{mode:"enroll",employee_id}` optional |
| GET | `/api/v1/rtsp/sessions/{id}` | session snapshot |
| GET | `/api/v1/rtsp/sessions/{id}/events` | SSE: state + analyzed frames |
| POST | `/api/v1/rtsp/sessions/{id}/confirm` | confirm the first enrollment face |
| DELETE | `/api/v1/rtsp/sessions/{id}` | stop the session |

The UI communicates the same contract in Chinese; backend errors carry
stable English `detail` phrases that the frontend maps to friendly copy.

## Architecture

```
cmd/server            entrypoint: config, store, engine (best effort), HTTP
internal/auth         accounts, sessions, API tokens, OIDC client
internal/engine       pure-Go pipeline: YuNet decode/NMS, SFace alignment,
                      quality gates, image decode/encode
internal/engine/onnxrt  ONNX Runtime binding (cgo build tag + factory)
internal/recognizer   matching (max-template cosine) + enrollment selection
internal/store        SQLite registry (employees + face_templates)
internal/api          HTTP handlers, SSE, RTSP enrollment controller
internal/rtsp         ffmpeg subprocess, latest-frame sessions, events
web/                  Vue 3 + Vite UI, embedded into the binary
```

Without CGO the server still builds and serves health + UI + profile
management; recognition endpoints report the engine as unavailable.

## Development

```bash
make test-nocgo             # pure-Go unit tests, no C toolchain needed
make vet
make web-install web-build  # Vue 3 + Vite frontend
make build                  # full binary (CGO + onnxruntime)
go test -tags integration ./internal/engine/...   # real-model tests
go test -tags e2e ./internal/e2e/ -v              # boots the real binary:
                            # employee/sample/recognize over HTTP plus an
                            # RTSP loop (ffmpeg serves the sample photo)
```

CI ([ci.yml](.github/workflows/ci.yml)) runs five lanes: formatting/vet/unit
tests, the web build, the CGO build with real-model integration tests, the
end-to-end suite and a no-push Docker image build.

## License

Apache-2.0. The ONNX models are downloaded from opencv_zoo (Apache-2.0) and
are not committed to this repository.
