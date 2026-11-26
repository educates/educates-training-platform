# Build the secrets-manager binary.
# Pin the builder to the build host's platform ($BUILDPLATFORM) so Go
# cross-compiles to the target platform via GOOS/GOARCH below, instead of
# running the toolchain under QEMU emulation for non-native target
# architectures during a multiarch build.
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Download the modules before copying the source, so that a source change
# does not invalidate the downloaded layer.
COPY go.mod go.sum ./
RUN go mod download

# Copy the Go source (relies on .dockerignore to filter).
COPY . .

# TARGETOS/TARGETARCH are supplied by the builder for the platform being
# produced; GOOS/GOARCH cross-compile to it from the native builder. The
# module builds on its own, outside the repository's Go workspace.
RUN CGO_ENABLED=0 GOWORK=off GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -o secrets-manager ./cmd/secrets-manager

# Package the static binary into distroless, which runs it as the nonroot
# user. See https://github.com/GoogleContainerTools/distroless.
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/secrets-manager /secrets-manager
USER 65532:65532

ENTRYPOINT ["/secrets-manager"]
