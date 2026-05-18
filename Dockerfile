# syntax=docker/dockerfile:1.7

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
USER indelible
WORKDIR /var/lib/indelible
VOLUME ["/var/lib/indelible"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1
ENTRYPOINT ["indelible"]
