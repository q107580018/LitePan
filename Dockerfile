# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web

WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm config set registry https://registry.npmmirror.com \
    && npm ci

COPY web/ ./
RUN npm run build


FROM --platform=$BUILDPLATFORM golang:1.26.6-bookworm AS build

WORKDIR /src

# 与 go.mod 的 go 1.26.6 对齐；local 禁止再去拉 toolchain，避免 proxy.golang.org 中断
ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0 \
    GOPROXY=https://goproxy.cn,direct
ARG BUILD_TAGS=fuse
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web /src/internal/api/web /src/internal/api/web

RUN GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -tags "${BUILD_TAGS}" -trimpath -ldflags="-s -w" -o /out/litepan ./cmd/litepan


FROM debian:bookworm-slim AS runtime

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tzdata fuse3 \
    && sed -i 's/^#user_allow_other/user_allow_other/' /etc/fuse.conf 2>/dev/null || true \
    && grep -q '^user_allow_other' /etc/fuse.conf || echo user_allow_other >> /etc/fuse.conf \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

RUN mkdir -p /app/data/log /app/strm /app/mounts

COPY --from=build /out/litepan /app/litepan

ENV LITEPAN_DATA_DIR=/app/data \
    LITEPAN_STRM_DIR=/app/strm \
    LITEPAN_LISTEN=:5211 \
    LITEPAN_LOG_LEVEL=info \
    TZ=Asia/Shanghai

EXPOSE 5211 42069/tcp 42069/udp

VOLUME ["/app/data", "/app/strm", "/app/mounts"]

ENTRYPOINT ["/app/litepan"]
