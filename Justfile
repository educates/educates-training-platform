# Educates Training Platform Build System
import 'docker-extension/Justfile'
import 'project-docs/Justfile'

# Variables
IMAGE_REPOSITORY := "localhost:5001"
PACKAGE_VERSION := "latest"
RELEASE_VERSION := "0.0.1"

# System detection
UNAME_SYSTEM := `uname -s | tr '[:upper:]' '[:lower:]'`
UNAME_MACHINE := `uname -m`

# Platform configuration
TARGET_SYSTEM := UNAME_SYSTEM
TARGET_MACHINE := if UNAME_MACHINE == "x86_64" { "amd64" } else { UNAME_MACHINE }
TARGET_PLATFORM := TARGET_SYSTEM + "-" + TARGET_MACHINE
DOCKER_PLATFORM := "linux/" + TARGET_MACHINE

# Default recipe
default: push-all-images build-client-programs

# Build all images
build-all-images: build-session-manager build-training-portal build-base-environment build-jdk8-environment build-jdk11-environment build-jdk17-environment build-jdk21-environment build-conda-environment build-docker-registry build-pause-container build-secrets-manager build-tunnel-manager build-image-cache build-assets-server build-lookup-service

# Push all images
push-all-images: push-session-manager push-training-portal push-base-environment push-jdk8-environment push-jdk11-environment push-jdk17-environment push-jdk21-environment push-conda-environment push-docker-registry push-pause-container push-secrets-manager push-tunnel-manager push-image-cache push-assets-server push-lookup-service push-installer-bundle

# Build core images
build-core-images: build-session-manager build-training-portal build-base-environment build-docker-registry build-pause-container build-secrets-manager build-tunnel-manager build-image-cache build-assets-server build-lookup-service

# Push core images
push-core-images: push-session-manager push-training-portal push-base-environment push-docker-registry push-pause-container push-secrets-manager push-tunnel-manager push-image-cache push-assets-server push-lookup-service

# Session Manager
build-session-manager:
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} -t {{IMAGE_REPOSITORY}}/educates-session-manager:{{PACKAGE_VERSION}} session-manager

push-session-manager: build-session-manager
    docker push {{IMAGE_REPOSITORY}}/educates-session-manager:{{PACKAGE_VERSION}}

# Training Portal
build-training-portal:
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} -t {{IMAGE_REPOSITORY}}/educates-training-portal:{{PACKAGE_VERSION}} training-portal

push-training-portal: build-training-portal
    docker push {{IMAGE_REPOSITORY}}/educates-training-portal:{{PACKAGE_VERSION}}

# Base Environment
build-base-environment:
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} -t {{IMAGE_REPOSITORY}}/educates-base-environment:{{PACKAGE_VERSION}} workshop-images/base-environment

push-base-environment: build-base-environment
    docker push {{IMAGE_REPOSITORY}}/educates-base-environment:{{PACKAGE_VERSION}}

# JDK Environments
build-jdk8-environment: build-base-environment
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} --build-arg PACKAGE_VERSION={{PACKAGE_VERSION}} -t {{IMAGE_REPOSITORY}}/educates-jdk8-environment:{{PACKAGE_VERSION}} workshop-images/jdk8-environment

push-jdk8-environment: build-jdk8-environment
    docker push {{IMAGE_REPOSITORY}}/educates-jdk8-environment:{{PACKAGE_VERSION}}

build-jdk11-environment: build-base-environment
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} --build-arg PACKAGE_VERSION={{PACKAGE_VERSION}} -t {{IMAGE_REPOSITORY}}/educates-jdk11-environment:{{PACKAGE_VERSION}} workshop-images/jdk11-environment

push-jdk11-environment: build-jdk11-environment
    docker push {{IMAGE_REPOSITORY}}/educates-jdk11-environment:{{PACKAGE_VERSION}}

build-jdk17-environment: build-base-environment
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} --build-arg PACKAGE_VERSION={{PACKAGE_VERSION}} -t {{IMAGE_REPOSITORY}}/educates-jdk17-environment:{{PACKAGE_VERSION}} workshop-images/jdk17-environment

push-jdk17-environment: build-jdk17-environment
    docker push {{IMAGE_REPOSITORY}}/educates-jdk17-environment:{{PACKAGE_VERSION}}

build-jdk21-environment: build-base-environment
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} --build-arg PACKAGE_VERSION={{PACKAGE_VERSION}} -t {{IMAGE_REPOSITORY}}/educates-jdk21-environment:{{PACKAGE_VERSION}} workshop-images/jdk21-environment

push-jdk21-environment: build-jdk21-environment
    docker push {{IMAGE_REPOSITORY}}/educates-jdk21-environment:{{PACKAGE_VERSION}}

# Conda Environment
build-conda-environment: build-base-environment
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} --build-arg PACKAGE_VERSION={{PACKAGE_VERSION}} -t {{IMAGE_REPOSITORY}}/educates-conda-environment:{{PACKAGE_VERSION}} workshop-images/conda-environment

push-conda-environment: build-conda-environment
    docker push {{IMAGE_REPOSITORY}}/educates-conda-environment:{{PACKAGE_VERSION}}

# Desktop Environment
build-desktop-environment: build-base-environment
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} --build-arg PACKAGE_VERSION={{PACKAGE_VERSION}} -t {{IMAGE_REPOSITORY}}/educates-desktop-environment:{{PACKAGE_VERSION}} workshop-images/desktop-environment

push-desktop-environment: build-desktop-environment
    docker push {{IMAGE_REPOSITORY}}/educates-desktop-environment:{{PACKAGE_VERSION}}

# Docker Registry
build-docker-registry:
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} -t {{IMAGE_REPOSITORY}}/educates-docker-registry:{{PACKAGE_VERSION}} docker-registry

push-docker-registry: build-docker-registry
    docker push {{IMAGE_REPOSITORY}}/educates-docker-registry:{{PACKAGE_VERSION}}

# Pause Container
build-pause-container:
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} -t {{IMAGE_REPOSITORY}}/educates-pause-container:{{PACKAGE_VERSION}} pause-container

push-pause-container: build-pause-container
    docker push {{IMAGE_REPOSITORY}}/educates-pause-container:{{PACKAGE_VERSION}}

# Secrets Manager
build-secrets-manager:
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} -t {{IMAGE_REPOSITORY}}/educates-secrets-manager:{{PACKAGE_VERSION}} secrets-manager

push-secrets-manager: build-secrets-manager
    docker push {{IMAGE_REPOSITORY}}/educates-secrets-manager:{{PACKAGE_VERSION}}

# Tunnel Manager
build-tunnel-manager:
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} -t {{IMAGE_REPOSITORY}}/educates-tunnel-manager:{{PACKAGE_VERSION}} tunnel-manager

push-tunnel-manager: build-tunnel-manager
    docker push {{IMAGE_REPOSITORY}}/educates-tunnel-manager:{{PACKAGE_VERSION}}

# Image Cache
build-image-cache:
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} -t {{IMAGE_REPOSITORY}}/educates-image-cache:{{PACKAGE_VERSION}} image-cache

push-image-cache: build-image-cache
    docker push {{IMAGE_REPOSITORY}}/educates-image-cache:{{PACKAGE_VERSION}}

# Assets Server
build-assets-server:
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} -t {{IMAGE_REPOSITORY}}/educates-assets-server:{{PACKAGE_VERSION}} assets-server

push-assets-server: build-assets-server
    docker push {{IMAGE_REPOSITORY}}/educates-assets-server:{{PACKAGE_VERSION}}

# Lookup Service
build-lookup-service:
    docker buildx build --progress plain --platform {{DOCKER_PLATFORM}} -t {{IMAGE_REPOSITORY}}/educates-lookup-service:{{PACKAGE_VERSION}} lookup-service

push-lookup-service: build-lookup-service
    docker push {{IMAGE_REPOSITORY}}/educates-lookup-service:{{PACKAGE_VERSION}}

# Installer verification and bundle
verify-installer-config:
    #!/usr/bin/env bash
    if [[ -f "developer-testing/educates-installer-values.yaml" ]]; then
        ytt --file carvel-packages/installer/bundle/config --data-values-file developer-testing/educates-installer-values.yaml
    else
        echo "No values file found. Please create developer-testing/educates-installer-values.yaml"
        exit 1
    fi

push-installer-bundle:
    ytt -f carvel-packages/installer/config/images.yaml -f carvel-packages/installer/config/schema.yaml -v imageRegistry.host={{IMAGE_REPOSITORY}} -v version={{PACKAGE_VERSION}} > carvel-packages/installer/bundle/kbld/kbld-images.yaml
    cat carvel-packages/installer/bundle/kbld/kbld-images.yaml | kbld -f - --imgpkg-lock-output carvel-packages/installer/bundle/.imgpkg/images.yml
    imgpkg push -b {{IMAGE_REPOSITORY}}/educates-installer:{{RELEASE_VERSION}} -f carvel-packages/installer/bundle
    mkdir -p developer-testing
    ytt -f carvel-packages/installer/config/app.yaml -f carvel-packages/installer/config/schema.yaml -v imageRegistry.host={{IMAGE_REPOSITORY}} -v version={{RELEASE_VERSION}} > developer-testing/educates-installer-app.yaml

# Platform deployment
deploy-platform:
    #!/usr/bin/env bash
    if [[ -f "developer-testing/educates-installer-values.yaml" ]]; then
        ytt --file carvel-packages/installer/bundle/config --data-values-file developer-testing/educates-installer-values.yaml | kapp deploy -a label:installer=educates-installer.app -f - -y
    else
        echo "No values file found. Please create developer-testing/educates-installer-values.yaml"
        exit 1
    fi

delete-platform:
    kapp delete -a label:installer=educates-installer.app -y

deploy-platform-app: push-installer-bundle
    #!/usr/bin/env bash
    if [[ ! -f "developer-testing/educates-installer-values.yaml" ]]; then
        echo "No values file found. Please create developer-testing/educates-installer-values.yaml"
        exit 1
    fi
    kubectl apply -f carvel-packages/installer/config/rbac.yaml || true
    kubectl create secret generic educates-installer --from-file=developer-testing/educates-installer-values.yaml -o yaml --dry-run=client | kubectl apply -n educates-installer -f -
    kubectl apply --namespace educates-installer -f developer-testing/educates-installer-app.yaml

delete-platform-app:
    kubectl delete --namespace educates-installer -f developer-testing/educates-installer-app.yaml || true
    kubectl delete secret educates-installer -n educates-installer || true
    kubectl delete -f carvel-packages/installer/config/rbac.yaml || true

restart-training-platform:
    kubectl rollout restart deployment/secrets-manager -n educates
    kubectl rollout restart deployment/session-manager -n educates

# Client programs
client-programs-educates:
    rm -rf client-programs/pkg/renderer/files
    mkdir client-programs/pkg/renderer/files
    mkdir -p client-programs/bin
    cp -rp workshop-images/base-environment/opt/eduk8s/etc/themes client-programs/pkg/renderer/files/
    cd client-programs && go build -gcflags=all="-N -l" -o bin/educates-{{TARGET_PLATFORM}} cmd/educates/main.go

build-client-programs: client-programs-educates

push-client-programs: build-client-programs
    #!/usr/bin/env bash
    if [[ "{{UNAME_SYSTEM}}" == "darwin" ]]; then
        (cd client-programs; GOOS=linux GOARCH=amd64 go build -o bin/educates-linux-amd64 cmd/educates/main.go)
        (cd client-programs; GOOS=linux GOARCH=arm64 go build -o bin/educates-linux-arm64 cmd/educates/main.go)
    elif [[ "{{UNAME_SYSTEM}}" == "linux" ]]; then
        if [[ "{{TARGET_MACHINE}}" == "arm64" ]]; then
            (cd client-programs; GOOS=linux GOARCH=amd64 go build -o bin/educates-linux-amd64 cmd/educates/main.go)
        elif [[ "{{TARGET_MACHINE}}" == "amd64" ]]; then
            (cd client-programs; GOOS=linux GOARCH=arm64 go build -o bin/educates-linux-arm64 cmd/educates/main.go)
        fi
    fi
    imgpkg push -i {{IMAGE_REPOSITORY}}/educates-client-programs:{{PACKAGE_VERSION}} -f client-programs/bin

# Workshop deployment
deploy-workshop:
    #!/usr/bin/env bash
    kubectl apply -f https://github.com/educates/lab-k8s-fundamentals/releases/download/7.4/workshop.yaml
    kubectl apply -f https://github.com/educates/lab-k8s-fundamentals/releases/download/7.4/trainingportal.yaml
    STATUS=1; ATTEMPTS=0; ROLLOUT_STATUS_CMD="kubectl rollout status deployment/training-portal -n lab-k8s-fundamentals-ui"
    until [ $STATUS -eq 0 ] || $ROLLOUT_STATUS_CMD || [ $ATTEMPTS -eq 5 ]; do
        sleep 5
        $ROLLOUT_STATUS_CMD
        STATUS=$?
        ATTEMPTS=$((ATTEMPTS + 1))
    done

delete-workshop:
    kubectl delete trainingportal,workshop lab-k8s-fundamentals --cascade=foreground || true

open-workshop:
    #!/usr/bin/env bash
    URL=$(kubectl get trainingportal/lab-k8s-fundamentals -o go-template=\{\{.status.educates.url\}\})
    if command -v xdg-open >/dev/null 2>&1; then
        xdg-open "$URL"
    elif command -v open >/dev/null 2>&1; then
        open "$URL"
    else
        echo "Workshop URL: $URL"
    fi

# Cleanup recipes
prune-images:
    docker image prune --force

prune-docker:
    docker system prune --force

prune-builds:
    rm -rf workshop-images/base-environment/opt/gateway/build
    rm -rf workshop-images/base-environment/opt/gateway/node_modules
    rm -rf workshop-images/base-environment/opt/helper/node_modules
    rm -rf workshop-images/base-environment/opt/helper/out
    rm -rf workshop-images/base-environment/opt/renderer/build
    rm -rf workshop-images/base-environment/opt/renderer/node_modules
    rm -rf training-portal/venv
    rm -rf client-programs/bin
    rm -rf client-programs/pkg/renderer/files
    rm -rf project-docs/venv
    rm -rf project-docs/_build

prune-registry:
    docker exec educates-registry registry garbage-collect /etc/docker/registry/config.yml --delete-untagged=true

prune-all: prune-docker prune-builds prune-registry

# List all available recipes
list:
    @just --list