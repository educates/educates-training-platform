FROM --platform=$BUILDPLATFORM golang:1.26 AS builder

WORKDIR /src

COPY . .

# The module is outside the Go workspace that CI tests, so every image
# build runs its tests, for the build platform, before building.
RUN CGO_ENABLED=0 go test ./...

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -o bin/image-puller-${TARGETOS}-${TARGETARCH} \
    .

FROM scratch

ARG TARGETOS
ARG TARGETARCH

COPY --from=builder /src/bin/image-puller-${TARGETOS}-${TARGETARCH} /image-puller

USER 1001

ENTRYPOINT ["/image-puller"]

CMD ["wait"]
