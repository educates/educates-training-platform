FROM golang:1.25.6 AS builder-image

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod tidy
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -o assets-server \
    main.go


FROM fedora:42

RUN useradd -u 1001 -g 0 -M -d /opt/app-root/src default && \
    mkdir -p /opt/app-root/src && \
    chown -R 1001:0 /opt/app-root

WORKDIR /opt/app-root

COPY --from=builder-image /app/assets-server /opt/app-root/bin/

USER 1001

EXPOSE 8080

VOLUME ["/opt/app-root/data"]

ENTRYPOINT ["/opt/app-root/bin/assets-server"]

CMD ["--dir", "/opt/app-root/data", "--host", "0.0.0.0"]
