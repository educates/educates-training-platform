ARG REPOSITORY=localhost:5001
ARG CLI_IMAGE_NAME=educates-client-programs
ARG TAG=latest

FROM ${REPOSITORY}/${CLI_IMAGE_NAME}:${TAG} AS client-programs

FROM --platform=$BUILDPLATFORM node:18.12-alpine3.16 AS client-builder
WORKDIR /ui
# cache packages in layer
COPY ui/package.json /ui/package.json
COPY ui/package-lock.json /ui/package-lock.json
RUN --mount=type=cache,target=/usr/src/app/.npm \
    npm set cache /usr/src/app/.npm && \
    npm ci
# install
COPY ui /ui
RUN npm run build

FROM debian:trixie-slim

ARG TARGETARCH

LABEL org.opencontainers.image.title="Educates Docker Desktop Extension" \
    org.opencontainers.image.description="Spin up a local Educates Training Platform workshop" \
    org.opencontainers.image.vendor="Educates" \
    org.opencontainers.image.licenses="Apache-2.0" \
    com.docker.desktop.extension.api.version="0.3.4" \
    com.docker.extension.screenshots="" \
    com.docker.desktop.extension.icon="https://raw.githubusercontent.com/educates/educates-training-platform/main/project-assets/educates-logo.svg" \
    com.docker.extension.detailed-description="Spin up a local Educates Training Platform workshop" \
    com.docker.extension.publisher-url="https://github.com/educates/educates-training-platform" \
    com.docker.extension.additional-urls="" \
    com.docker.extension.categories="" \
    com.docker.extension.changelog=""

RUN <<EOF
apt-get update && \
apt-get install -y ca-certificates curl gnupg && \
install -m 0755 -d /etc/apt/keyrings && \
curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg && \
chmod a+r /etc/apt/keyrings/docker.gpg && \
echo \
    "deb [arch="$(dpkg --print-architecture)" signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian \
    "$(. /etc/os-release && echo "$VERSION_CODENAME")" stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null && \
apt-get update && \
apt-get install -y containerd.io docker-ce docker-ce-cli docker-compose-plugin
EOF

COPY --from=client-programs educates-linux-${TARGETARCH} /educates

COPY docker-compose.yaml .
COPY metadata.json .
COPY logo.svg .
COPY --from=client-builder /ui/build ui

CMD ["/educates", "docker", "extension", "backend", "--socket", "/run/guest-services/backend.sock"]
