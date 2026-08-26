FROM --platform=$BUILDPLATFORM golang:1.26 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -o bin/assets-server-${TARGETOS}-${TARGETARCH} \
    main.go

FROM scratch

ARG TARGETOS
ARG TARGETARCH

COPY --from=builder /src/bin/assets-server-${TARGETOS}-${TARGETARCH} /assets-server

USER 1001

EXPOSE 8080

VOLUME ["/opt/app-root/data"]

ENTRYPOINT ["/assets-server"]

CMD ["--dir", "/opt/app-root/data", "--host", "0.0.0.0"]
