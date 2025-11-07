# =============================================================================
# Educates Training Platform - Build System
# =============================================================================
#
# This Makefile provides a comprehensive build system for the Educates training
# platform, supporting both single-platform and multi-architecture builds.
#
# =============================================================================
# CONFIGURATION PARAMETERS
# =============================================================================
#
# The following parameters can be set via environment variables or make arguments:
#
# PUSH_IMAGES
#   Description: Controls whether images are pushed to registry or loaded locally
#   Note: If PUSH_IMAGES is false, the images are loaded locally with --load
#         and only one image is built for the current platform.
#   Default: true (images are pushed to a registry)
#   Values: true/false
#   Examples:
#     - Load locally: PUSH_IMAGES=false
#     - Push to registry: PUSH_IMAGES=true (or omit)
#   Usage:
#     make build-all-images PUSH_IMAGES=false
#     export PUSH_IMAGES=false && make build-all-images
#
# TARGET_PLATFORMS
#   Description: Controls target platform(s) for Docker builds
#   Default: Current platform (e.g., linux/amd64 on x86_64 systems)
#   Note: If PUSH_IMAGES is false, TARGET_PLATFORMS is ignored.
#   Examples:
#     - Single platform: TARGET_PLATFORMS=linux/arm64
#     - Multi-platform: TARGET_PLATFORMS=linux/amd64,linux/arm64
#   Usage:
#     make build-all-images TARGET_PLATFORMS=linux/arm64,linux/amd64
#     export TARGET_PLATFORMS=linux/arm64 && make build-all-images
#
# IMAGE_REPOSITORY
#   Description: Docker registry/repository for built images
#   Default: localhost:5001
#   Examples:
#     - Local registry: localhost:5001
#     - Docker Hub: yourusername/educates
#     - Private registry: registry.company.com/educates
#
# PACKAGE_VERSION
#   Description: Version tag for built images
#   Default: latest
#   Examples: v1.0.0, dev, latest
#
# =============================================================================
# BUILD TARGETS
# =============================================================================
#
# Main Targets:
#   all                    - Build all images and client programs
#   build-all-images       - Build all Docker images
#   build-core-images      - Build core platform images only
#   build-client-programs  - Build Go client programs
#
# Individual Image Targets:
#   build-session-manager     - Session management service
#   build-training-portal    - Web portal interface
#   build-base-environment   - Base workshop environment
#   build-jdk8-environment   - Java 8 workshop environment
#   build-jdk11-environment  - Java 11 workshop environment
#   build-jdk17-environment  - Java 17 workshop environment
#   build-jdk21-environment  - Java 21 workshop environment
#   build-conda-environment  - Python/Conda workshop environment
#   build-desktop-environment- Desktop GUI workshop environment
#   build-docker-registry    - Local Docker registry
#   build-pause-container    - Kubernetes pause container
#   build-secrets-manager    - Secrets management service
#   build-tunnel-manager     - Network tunneling service
#   build-image-cache        - Image caching service
#   build-assets-server      - Static assets server
#   build-lookup-service     - Service discovery
#
# Utility Targets:
#   setup-buildx           - Setup Docker buildx for multiarch builds
#   clean-buildx           - Clean up builder
#   list-platforms         - Show supported platforms
#   help                   - Show available targets
#
# =============================================================================
# USAGE EXAMPLES
# =============================================================================
#
# Basic usage (builds for current platform, loads locally):
#   make build-all-images
#
# Build for specific platform:
#   make build-all-images TARGET_PLATFORMS=linux/arm64
#
# Multi-architecture build:
#   make build-all-images TARGET_PLATFORMS=linux/amd64,linux/arm64
#
# Don't push to registry:
#   make build-all-images PUSH_IMAGES=false
#
# Combined parameters:
#   make build-all-images TARGET_PLATFORMS=linux/amd64,linux/arm64 PUSH_IMAGES=true
#
# Using environment variables:
#   export TARGET_PLATFORMS=linux/arm64,linux/amd64
#   export PUSH_IMAGES=true
#   make build-all-images
#
# Build specific service:
#   make build-training-portal TARGET_PLATFORMS=linux/arm64
#
# =============================================================================
# PLATFORM DETECTION
# =============================================================================
#
# The Makefile automatically detects the current system:
#   - Operating System: Darwin, Linux, Windows
#   - Architecture: amd64, arm64, etc.
#   - Normalizes x86_64 to amd64 for consistency
#
# The target for build-client-programs is always set to the current platform, so that
# on macos a binary will be built for the current platform. But the push-client-programs
# target will create a multiarch image for the target platforms and the corresponding binaries,
# in the form of "educates-cli" image, and will also generate an "educates-client-programs" oci artifact
# with all the binaries for the target platforms. (Pushed via imgpkg)
#
# =============================================================================
#
IMAGE_REPOSITORY = localhost:5001
PACKAGE_VERSION = latest
RELEASE_VERSION = 0.0.1

UNAME_SYSTEM := $(shell uname -s | tr '[:upper:]' '[:lower:]')
UNAME_MACHINE := $(shell uname -m)

TARGET_SYSTEM = $(UNAME_SYSTEM)
TARGET_MACHINE = $(UNAME_MACHINE)

ifeq ($(UNAME_MACHINE),x86_64)
TARGET_MACHINE = amd64
endif

TARGET_PLATFORM = $(TARGET_SYSTEM)-$(TARGET_MACHINE)
BUILDX_BUILDER = educates-multiarch-builder

# Platform configuration - can be overridden by TARGET_PLATFORMS env var or make parameter
ifeq ($(TARGET_PLATFORMS),)
# Default to current platform when TARGET_PLATFORMS is not set
DOCKER_PLATFORM = linux/$(TARGET_MACHINE)
MULTIARCH_PLATFORMS = linux/amd64,linux/arm64
else
# Use TARGET_PLATFORMS when set (allows for custom multiarch builds)
DOCKER_PLATFORM = $(TARGET_PLATFORMS)
MULTIARCH_PLATFORMS = $(TARGET_PLATFORMS)
endif

# Push/Load configuration - can be overridden by PUSH_IMAGES env var or make parameter
ifeq ($(PUSH_IMAGES),false)
# Load images locally when PUSH_IMAGES is not true (default)
DOCKER_BUILDER =
MULTIARCH_PLATFORMS = $(DOCKER_PLATFORM)
else
# Push images to registry when PUSH_IMAGES is true
DOCKER_BUILDER = --builder ${BUILDX_BUILDER} --push
endif

all: build-all-images # deploy-installer deploy-workshop

# Multiarch build targets
build-all-images: setup-buildx build-session-manager build-training-portal \
  build-base-environment build-jdk8-environment build-jdk11-environment \
  build-jdk17-environment build-jdk21-environment \
  build-conda-environment build-docker-registry \
  build-pause-container build-secrets-manager build-tunnel-manager \
  build-image-cache build-assets-server build-lookup-service \
  build-cli-image

build-core-images: setup-buildx build-session-manager build-training-portal \
  build-base-environment build-docker-registry build-pause-container \
  build-secrets-manager build-tunnel-manager build-image-cache \
  build-assets-server build-lookup-service

build-session-manager:
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-session-manager:$(PACKAGE_VERSION) \
		session-manager

build-training-portal:
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-training-portal:$(PACKAGE_VERSION) \
		training-portal

build-base-environment:
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-base-environment:$(PACKAGE_VERSION) \
		workshop-images/base-environment

build-jdk8-environment: build-base-environment
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
		--build-arg PACKAGE_VERSION=$(PACKAGE_VERSION) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-jdk8-environment:$(PACKAGE_VERSION) \
		workshop-images/jdk8-environment

build-jdk11-environment: build-base-environment
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		--build-arg PACKAGE_VERSION=$(PACKAGE_VERSION) \
		-t $(IMAGE_REPOSITORY)/educates-jdk11-environment:$(PACKAGE_VERSION) \
		workshop-images/jdk11-environment

build-jdk17-environment: build-base-environment
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		--build-arg PACKAGE_VERSION=$(PACKAGE_VERSION) \
		-t $(IMAGE_REPOSITORY)/educates-jdk17-environment:$(PACKAGE_VERSION) \
		workshop-images/jdk17-environment

build-jdk21-environment: build-base-environment
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		--build-arg PACKAGE_VERSION=$(PACKAGE_VERSION) \
		-t $(IMAGE_REPOSITORY)/educates-jdk21-environment:$(PACKAGE_VERSION) \
		workshop-images/jdk21-environment

build-conda-environment: build-base-environment
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		--build-arg PACKAGE_VERSION=$(PACKAGE_VERSION) \
		-t $(IMAGE_REPOSITORY)/educates-conda-environment:$(PACKAGE_VERSION) \
		workshop-images/conda-environment

build-desktop-environment: build-base-environment
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		--build-arg PACKAGE_VERSION=$(PACKAGE_VERSION) \
		-t $(IMAGE_REPOSITORY)/educates-desktop-environment:$(PACKAGE_VERSION) \
		workshop-images/desktop-environment

build-docker-registry:
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-docker-registry:$(PACKAGE_VERSION) \
		docker-registry

build-pause-container:
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-pause-container:$(PACKAGE_VERSION) \
		pause-container

build-secrets-manager:
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-secrets-manager:$(PACKAGE_VERSION) \
		secrets-manager

build-tunnel-manager:
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-tunnel-manager:$(PACKAGE_VERSION) \
		tunnel-manager

build-image-cache:
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-image-cache:$(PACKAGE_VERSION) \
		image-cache

build-assets-server:
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-assets-server:$(PACKAGE_VERSION) \
		assets-server

build-lookup-service:
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-lookup-service:$(PACKAGE_VERSION) \
		lookup-service

verify-installer-config:
ifneq ("$(wildcard developer-testing/educates-installer-values.yaml)","")
	@ytt --file carvel-packages/installer/bundle/config --data-values-file developer-testing/educates-installer-values.yaml
else
	@echo "No values file found. Please create developer-testing/educates-installer-values.yaml"
	exit 1
endif

push-installer-bundle:
	ytt -f carvel-packages/installer/config/images.yaml -f carvel-packages/installer/config/schema.yaml -v imageRegistry.host=$(IMAGE_REPOSITORY) -v version=$(PACKAGE_VERSION) > carvel-packages/installer/bundle/kbld/kbld-images.yaml
   # For local development, we just need to lock educates images. Everything else can be referenced by tag from real origin.
	cat carvel-packages/installer/bundle/kbld/kbld-images.yaml | kbld -f - --imgpkg-lock-output carvel-packages/installer/bundle/.imgpkg/images.yml
	imgpkg push -b $(IMAGE_REPOSITORY)/educates-installer:$(RELEASE_VERSION) -f carvel-packages/installer/bundle
	mkdir -p developer-testing
	ytt -f carvel-packages/installer/config/app.yaml -f carvel-packages/installer/config/schema.yaml -v imageRegistry.host=$(IMAGE_REPOSITORY) -v version=$(RELEASE_VERSION) > developer-testing/educates-installer-app.yaml

deploy-platform:
ifneq ("$(wildcard developer-testing/educates-installer-values.yaml)","")
	ytt --file carvel-packages/installer/bundle/config --data-values-file developer-testing/educates-installer-values.yaml | kapp deploy -a label:installer=educates-installer.app -f - -y
else
	@echo "No values file found. Please create developer-testing/educates-installer-values.yaml"
	exit 1
endif

delete-platform:
	kapp delete -a label:installer=educates-installer.app -y

deploy-platform-app: push-installer-bundle
ifeq ("$(wildcard developer-testing/educates-installer-values.yaml)","")
	@echo "No values file found. Please create developer-testing/educates-installer-values.yaml"
	exit 1
endif
	-kubectl apply -f carvel-packages/installer/config/rbac.yaml
	kubectl create secret generic educates-installer --from-file=developer-testing/educates-installer-values.yaml -o yaml --dry-run=client | kubectl apply -n educates-installer -f -
	kubectl apply --namespace educates-installer -f developer-testing/educates-installer-app.yaml

delete-platform-app:
	kubectl delete --namespace educates-installer -f developer-testing/educates-installer-app.yaml
	-kubectl delete secret educates-installer -n educates-installer
	-kubectl delete -f carvel-packages/installer/config/rbac.yaml

restart-training-platform:
	kubectl rollout restart deployment/secrets-manager -n educates
	kubectl rollout restart deployment/session-manager -n educates

client-programs-educates:
	rm -rf client-programs/pkg/renderer/files
	mkdir client-programs/pkg/renderer/files
	mkdir -p client-programs/bin
	cp -rp workshop-images/base-environment/opt/eduk8s/etc/themes client-programs/pkg/renderer/files/
	(cd client-programs; go build -gcflags=all="-N -l" -o bin/educates-$(TARGET_PLATFORM) cmd/educates/main.go)

build-client-programs: client-programs-educates

push-client-programs: build-client-programs
	(cd client-programs; GOOS=linux GOARCH=amd64 go build -o bin/educates-linux-amd64 cmd/educates/main.go)
	(cd client-programs; GOOS=linux GOARCH=arm64 go build -o bin/educates-linux-arm64 cmd/educates/main.go)
	(cd client-programs; GOOS=linux GOARCH=amd64 go build -o bin/educates-linux-amd64 cmd/educates/main.go)
	(cd client-programs; GOOS=linux GOARCH=arm64 go build -o bin/educates-linux-arm64 cmd/educates/main.go)
	imgpkg push -i $(IMAGE_REPOSITORY)/educates-client-programs:$(PACKAGE_VERSION) -f client-programs/bin

build-cli-image:
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE_REPOSITORY)/educates-cli:$(PACKAGE_VERSION) \
		client-programs

build-docker-extension : build-cli-image
	$(MAKE) -C docker-extension build-extension REPOSITORY=$(IMAGE_REPOSITORY) TAG=$(PACKAGE_VERSION)

install-docker-extension : build-docker-extension
	$(MAKE) -C docker-extension install-extension REPOSITORY=$(IMAGE_REPOSITORY) TAG=$(PACKAGE_VERSION)

update-docker-extension : build-docker-extension
	$(MAKE) -C docker-extension update-extension REPOSITORY=$(IMAGE_REPOSITORY) TAG=$(PACKAGE_VERSION)

project-docs/venv :
	python3 -m venv project-docs/venv
	project-docs/venv/bin/pip install -r project-docs/requirements.txt
 
build-project-docs : project-docs/venv
	source project-docs/venv/bin/activate && make -C project-docs html

open-project-docs :
	open project-docs/_build/html/index.html || \
        xdg-open project-docs/_build/html/index.html

clean-project-docs:
	rm -rf project-docs/venv
	rm -rf project-docs/_build

deploy-workshop:
	kubectl apply -f https://github.com/educates/lab-k8s-fundamentals/releases/download/8.3/workshop.yaml
	kubectl apply -f https://github.com/educates/lab-k8s-fundamentals/releases/download/8.3/trainingportal.yaml
	STATUS=1; ATTEMPTS=0; ROLLOUT_STATUS_CMD="kubectl rollout status deployment/training-portal -n lab-k8s-fundamentals-ui"; until [ $$STATUS -eq 0 ] || $$ROLLOUT_STATUS_CMD || [ $$ATTEMPTS -eq 5 ]; do sleep 5; $$ROLLOUT_STATUS_CMD; STATUS=$$?; ATTEMPTS=$$((ATTEMPTS + 1)); done

delete-workshop:
	-kubectl delete trainingportal,workshop lab-k8s-fundamentals --cascade=foreground

open-workshop:
	URL=`kubectl get trainingportal/lab-k8s-fundamentals -o go-template={{.status.educates.url}}`; (test -x /usr/bin/xdg-open && xdg-open $$URL) || (test -x /usr/bin/open && open $$URL) || true

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

# builder management
setup-buildx: ## Setup builder for multiarch builds
	docker buildx create --name $(BUILDX_BUILDER) --driver docker-container --driver-opt default-load=true --driver-opt network=host --use || true
	docker buildx inspect $(BUILDX_BUILDER) --bootstrap

clean-buildx: ## Clean up builder
	docker buildx rm $(BUILDX_BUILDER) || true

# Multiarch utility targets
list-platforms: ## List available platforms for multiarch builds
	@echo "Supported platforms: $(MULTIARCH_PLATFORMS)"