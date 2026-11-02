#!/usr/bin/env sh
# Downloads the YuNet (face detection) and SFace (face recognition) ONNX
# models from the Apache-2.0 licensed opencv_zoo repository.
set -e

OUTPUT_DIR="${1:-models}"
mkdir -p "$OUTPUT_DIR"

download() {
    url="$1"
    name="$2"
    target="$OUTPUT_DIR/$name"
    if [ -f "$target" ]; then
        echo "skip existing $name"
        return
    fi
    attempt=0
    while true; do
        attempt=$((attempt + 1))
        echo "downloading $name (attempt $attempt)"
        if curl -fL --retry 4 --retry-delay 3 -o "$target" "$url"; then
            break
        fi
        if [ "$attempt" -ge 5 ]; then
            echo "failed to download $name" >&2
            exit 1
        fi
        sleep 3
    done
    sha256sum "$target"
}

download \
    "https://github.com/opencv/opencv_zoo/raw/main/models/face_detection_yunet/face_detection_yunet_2023mar.onnx" \
    "face_detection_yunet.onnx"
download \
    "https://github.com/opencv/opencv_zoo/raw/main/models/face_recognition_sface/face_recognition_sface_2021dec.onnx" \
    "face_recognition_sface.onnx"

echo "models ready in $OUTPUT_DIR"
