#syntax=docker/dockerfile:1.3-labs

# The upstream registry image ships a binary built with an older Go
# toolchain and dependency set than the current security fixes require.
# Rebuild the same distribution release from source (pinned by commit)
# with a current Go toolchain and the flagged modules raised to their
# fixed versions, then overlay the binary on the official image so the
# image configuration and entrypoint stay upstream's.
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS registry-build

ARG TARGETARCH

RUN <<EOF
    set -eo pipefail
    DISTRIBUTION_VERSION=3.1.1
    DISTRIBUTION_COMMIT=9a8d98b679740cd514aa7e7d84d23d442a5ef54c
    git clone --depth 1 --branch v${DISTRIBUTION_VERSION} https://github.com/distribution/distribution /distribution
    cd /distribution
    test "$(git rev-parse HEAD)" = "${DISTRIBUTION_COMMIT}"
    go get \
        golang.org/x/crypto@v0.54.0 \
        golang.org/x/net@v0.57.0 \
        golang.org/x/text@v0.41.0 \
        google.golang.org/grpc@v1.82.1
    go mod tidy
    go mod vendor
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
        go build -trimpath -ldflags "-s -w" -o /registry-binary ./cmd/registry
EOF

FROM registry:3.1.1

COPY --from=registry-build /registry-binary /bin/registry
