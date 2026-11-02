# syntax=docker/dockerfile:1

FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.26-bookworm AS server
ARG ONNXRUNTIME_VERSION=1.20.1
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends clang curl \
    && curl -fL --retry 4 -o ort.tgz \
       https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz \
    && tar -xzf ort.tgz -C /usr/local && rm ort.tgz \
    && rm -rf /var/lib/apt/lists/*
ENV CGO_ENABLED=1 \
    CGO_CFLAGS=-I/usr/local/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/include \
    CGO_LDFLAGS="-L/usr/local/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/lib -lonnxruntime"
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/web/dist internal/httpui/dist
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM debian:bookworm-slim
ARG ONNXRUNTIME_VERSION=1.20.1
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=server /out/server /app/server
COPY --from=server /usr/local/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/lib/libonnxruntime.so /usr/lib/
COPY scripts/download-models.sh /app/scripts/download-models.sh
ENV FACE_RECOGNITION_ADDR=:8080 \
    FACE_RECOGNITION_DB=/app/data/faces.sqlite3 \
    FACE_RECOGNITION_MODELS=/app/models
EXPOSE 8080
ENTRYPOINT ["/app/server"]
