ARG REPOSITORY=localhost:5001
ARG TAG=latest

# Themes are not platform-dependent; always pull from the build host's platform
FROM --platform=$BUILDPLATFORM ${REPOSITORY}/educates-base-environment:${TAG} AS themes-source

# Run the Go compiler on the build host platform and cross-compile via GOOS/GOARCH
FROM --platform=$BUILDPLATFORM golang:1.25.6 AS builder

# Set working directory
WORKDIR /src

# Copy go mod files
COPY go.mod go.sum ./

# Copy source code
COPY . .

# Prepare renderer files (matching Makefile logic)
RUN rm -rf pkg/renderer/files && \
    mkdir -p pkg/renderer/files && \
    mkdir -p bin

# Copy themes from base environment
COPY --from=themes-source /opt/eduk8s/etc/themes pkg/renderer/files/

# Build for multiple architectures
ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -o bin/educates-${TARGETOS}-${TARGETARCH} \
    cmd/educates/main.go

# Runtime stage - minimal image with binaries
FROM scratch
ARG TARGETOS
ARG TARGETARCH
# Copy binaries from builder
COPY --from=builder /src/bin/educates-${TARGETOS}-${TARGETARCH} /educates
ENTRYPOINT ["/educates"]
