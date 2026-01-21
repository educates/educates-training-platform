IMAGE_REPOSITORY = localhost:5001
PACKAGE_VERSION = latest

UNAME_SYSTEM := $(shell uname -s | tr '[:upper:]' '[:lower:]')
UNAME_MACHINE := $(shell uname -m)

TARGET_SYSTEM = $(UNAME_SYSTEM)
TARGET_MACHINE = $(UNAME_MACHINE)

ifeq ($(UNAME_MACHINE),x86_64)
TARGET_MACHINE = amd64
endif

TARGET_PLATFORM = $(TARGET_SYSTEM)-$(TARGET_MACHINE)
BUILDX_BUILDER = educates-multiarch-builder

# Platform configuration - can be overridden by BUILD_PLATFORMS env var or make parameter
ifeq ($(BUILD_PLATFORMS),)
# Default to current platform when BUILD_PLATFORMS is not set
DOCKER_PLATFORM = linux/$(TARGET_MACHINE)
MULTIARCH_PLATFORMS = linux/amd64,linux/arm64
else
# Use BUILD_PLATFORMS when set (allows for custom multiarch builds)
DOCKER_PLATFORM = $(BUILD_PLATFORMS)
MULTIARCH_PLATFORMS = $(BUILD_PLATFORMS)
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

INFO_COLOR = \033[0;36m
NO_COLOR   = \033[m
DEV_UI_SOURCE?=http://localhost:3000

build-extension: setup-buildx ## Build service image to be deployed as a desktop extension
	docker build --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(if $(DOCKER_BUILDER),$(DOCKER_BUILDER)) \
		-t $(IMAGE_REPOSITORY)/educates-docker-extension:$(PACKAGE_VERSION) \
		.

install-extension: build-extension ## Install the extension
	docker extension install --force $(IMAGE_REPOSITORY)/educates-docker-extension:$(PACKAGE_VERSION)

update-extension: build-extension ## Update the extension
	docker extension update --force $(IMAGE_REPOSITORY)/educates-docker-extension:$(PACKAGE_VERSION)

setup-buildx: ## Create buildx builder for multi-arch build, if not exists
	docker buildx create --name $(BUILDX_BUILDER) --driver docker-container --driver-opt default-load=true --driver-opt network=host --use || true
	docker buildx inspect $(BUILDX_BUILDER) --bootstrap

.PHONY: debug
debug: ## Enable debug in the extension
	docker extension dev debug $(IMAGE_REPOSITORY)/educates-docker-extension:$(PACKAGE_VERSION)

.PHONY: source
source: ## Replace the UI source of the extension
	docker extension dev ui-source $(IMAGE_REPOSITORY)/educates-docker-extension:$(PACKAGE_VERSION) $(DEV_UI_SOURCE)

.PHONY: dev-enable
dev-enable: source debug

.PHONY: dev-reset
dev-reset: ## Reset development status of the extension
	docker extension dev reset educates-docker-extension:$(PACKAGE_VERSION)

help: ## Show this help
	@echo Please specify a build target. The choices are:
	@grep -E '^[0-9a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "$(INFO_COLOR)%-30s$(NO_COLOR) %s\n", $$1, $$2}'

.PHONY: help
