ARG REPOSITORY=localhost:5001
ARG TAG=latest

# Add this stage before the builder stage
FROM ${REPOSITORY}/educates-base-environment:${TAG} AS themes-source

# Multi-stage build for client-programs
FROM golang:1.24.10-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /src

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

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

RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -o bin/educates-${TARGETOS}-${TARGETARCH} \
    cmd/educates/main.go

# Runtime stage - minimal image with binaries
FROM scratch
# Build for multiple architectures
ARG TARGETOS
ARG TARGETARCH
# Copy binaries from builder
COPY --from=builder /src/bin/educates-${TARGETOS}-${TARGETARCH} /educates
ENTRYPOINT ["/educates"]