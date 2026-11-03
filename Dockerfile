# syntax=docker/dockerfile:1

FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.26-bookworm AS server
ARG ONNXRUNTIME_VERSION=1.31.0
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends clang curl \
    && curl -fL --retry 4 -o ort.tgz \
       https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz \
    && tar -xzf ort.tgz && rm ort.tgz \
    && rm -rf /var/lib/apt/lists/*
ENV CGO_ENABLED=1 CC=clang
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY scripts/ scripts/
COPY --from=web /src/web/dist internal/httpui/dist
# bake the models so the image works out of the box
RUN ./scripts/download-models.sh models
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM debian:bookworm-slim
ARG ONNXRUNTIME_VERSION=1.31.0
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates ffmpeg \
    && rm -rf /var/lib/apt/lists/*
COPY --from=server /out/server /app/server
COPY --from=server /src/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/lib/ /app/onnxruntime-lib/
COPY --from=server /src/models/ /app/models/
ENV FACE_RECOGNITION_ADDR=:8080 \
    FACE_RECOGNITION_DB=/app/data/faces.sqlite3 \
    FACE_RECOGNITION_MODELS=/app/models \
    FACE_RECOGNITION_ORT_LIB=/app/onnxruntime-lib/libonnxruntime.so
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["/app/server"]
