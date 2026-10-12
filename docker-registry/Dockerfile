#syntax=docker/dockerfile:1.3-labs

# The upstream registry image ships a binary built with an older Go
# toolchain and dependency set than the current security fixes require.
# Rebuild the same distribution release from source (pinned by commit)
# with a current Go toolchain and the flagged modules raised to their
# fixed versions, then overlay the binary on the official image so the
# image configuration and entrypoint stay upstream's.
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS registry-build

ARG TARGETARCH

# Pin the exact Go release: the golang:1.26 image can lag a patch behind,
# and the current patch carries stdlib security fixes the binary must have.
ENV GOTOOLCHAIN=go1.26.9

RUN <<EOF
    set -eo pipefail
    DISTRIBUTION_VERSION=3.1.2
    DISTRIBUTION_COMMIT=3220848f15d9279c66a41aa0a257469e6aede1e9
    git clone --depth 1 --branch v${DISTRIBUTION_VERSION} https://github.com/distribution/distribution /distribution
    cd /distribution
    test "$(git rev-parse HEAD)" = "${DISTRIBUTION_COMMIT}"
    go get \
        go.opentelemetry.io/otel/exporters/otlp/otlptrace@v1.45.0 \
        go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.45.0 \
        go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.45.0 \
        golang.org/x/crypto@v0.58.0 \
        golang.org/x/net@v0.61.0 \
        golang.org/x/text@v0.43.0 \
        google.golang.org/grpc@v1.83.2
    go mod tidy
    go mod vendor
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
        go build -trimpath -ldflags "-s -w" -o /registry-binary ./cmd/registry
EOF

FROM registry:3.1.2

COPY --from=registry-build /registry-binary /bin/registry
