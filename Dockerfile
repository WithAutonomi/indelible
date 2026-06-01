# syntax=docker/dockerfile:1.7

# antd daemon is bundled into the image so indelible ships as a complete,
# self-sufficient product — the network daemon is essential to its function
# regardless of which Autonomi network is targeted. Pulled from ant-sdk's
# published multi-arch image (buildx selects the matching arch per target
# platform); keep ANTD_IMAGE in lockstep with .antd-version. release.yml
# passes the pinned tag explicitly; this default keeps `docker compose
# up --build` and bare `docker build` working out of the box.
ARG ANTD_IMAGE=ghcr.io/withautonomi/antd:v0.9.0
FROM ${ANTD_IMAGE} AS antd

# Build frontend on the native arch — JS output is arch-independent.
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Build backend on the native arch, cross-compile to TARGETARCH. Going through
# QEMU emulation is 5-10x slower than native + cross-compile.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags "-s -w -X github.com/WithAutonomi/indelible/internal/buildinfo.Version=${VERSION}" \
    -o /indelible ./cmd/indelible

# Runtime — non-root, persistent volume on /var/lib/indelible.
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g 65532 -S indelible \
 && adduser -u 65532 -S -G indelible -h /var/lib/indelible -s /sbin/nologin indelible \
 && mkdir -p /var/lib/indelible \
 && chown -R indelible:indelible /var/lib/indelible
COPY --from=backend /indelible /usr/local/bin/indelible
# Bundled antd daemon (see ANTD_IMAGE note at top). Lands in PATH with its
# executable bit preserved, so indelible's managed mode (and a bare
# `docker run`) can spawn it without an external daemon.
COPY --from=antd /usr/local/bin/antd /usr/local/bin/antd
# Be self-sufficient by default: a bare `docker run` of this image manages its
# own antd child process and connects to mainnet with zero extra config.
# docker-compose overrides this to "false" and points INDELIBLE_ANTD_URL at a
# dedicated antd container so the daemon restarts independently.
ENV INDELIBLE_ANTD_MANAGED=true
USER indelible
WORKDIR /var/lib/indelible
VOLUME ["/var/lib/indelible"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1
ENTRYPOINT ["indelible"]
