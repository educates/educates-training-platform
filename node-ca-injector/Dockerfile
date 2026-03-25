FROM golang:1.24.10 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -o bin/node-ca-injector-${TARGETOS}-${TARGETARCH} \
    cmd/node-ca-injector/main.go

FROM scratch

ARG TARGETOS
ARG TARGETARCH

COPY --from=builder /src/bin/node-ca-injector-${TARGETOS}-${TARGETARCH} /node-ca-injector

ENTRYPOINT ["/node-ca-injector"]
