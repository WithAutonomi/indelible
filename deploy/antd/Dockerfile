# Companion image for the antd daemon.
#
# Transitional: ant-sdk publishes binaries only — no upstream Dockerfile or
# image. This wrapper downloads the binary from the ant-sdk GitHub release for
# the requested ANTD_VERSION and runs it as a non-root service. Move this
# upstream into WithAutonomi/ant-sdk once they own the image lifecycle.
#
# Build:
#   docker buildx build \
#     --platform linux/amd64 \
#     --build-arg ANTD_VERSION=v0.7.1 \
#     -t ghcr.io/withautonomi/antd:v0.7.1 \
#     deploy/antd
#
# linux/arm64 is blocked on WithAutonomi/ant-sdk publishing antd-linux-arm64
# (tracked as V2-275). Until then this image is amd64-only.

FROM alpine:3.21

ARG ANTD_VERSION=v0.7.1
ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN apk add --no-cache ca-certificates wget tzdata \
 && addgroup -g 65532 -S antd \
 && adduser -u 65532 -S -G antd -h /var/lib/antd -s /sbin/nologin antd \
 && mkdir -p /var/lib/antd \
 && chown -R antd:antd /var/lib/antd

# Download the binary matching this build's TARGETOS/TARGETARCH.
RUN ARTIFACT="antd-${TARGETOS}-${TARGETARCH}" \
 && URL="https://github.com/WithAutonomi/ant-sdk/releases/download/${ANTD_VERSION}/${ARTIFACT}" \
 && echo "Fetching $URL" \
 && wget -q -O /usr/local/bin/antd "$URL" \
 && chmod +x /usr/local/bin/antd

USER antd
WORKDIR /var/lib/antd
VOLUME ["/var/lib/antd"]
EXPOSE 8082
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8082/health || exit 1
ENTRYPOINT ["antd"]
CMD ["--port", "8082"]
