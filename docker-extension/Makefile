IMAGE_REPOSITORY ?= localhost:5001
PACKAGE_VERSION ?= latest

# Create an alias for the image repository that sanitizes invalid characters
# Docker Compose project names must consist only of lowercase alphanumeric characters,
# hyphens, and underscores, and start with a letter or number
IMAGE_ALIAS := $(shell echo $(IMAGE_REPOSITORY) | tr '[:upper:]' '[:lower:]' | sed 's/:/_/g' | sed 's/[^a-z0-9_-]//g' | sed 's/^[^a-z0-9]/a&/')
IMAGE = $(IMAGE_REPOSITORY)/educates-docker-extension:$(PACKAGE_VERSION)
IMAGE_ALIAS_FULL = $(IMAGE_ALIAS)/educates-docker-extension:$(PACKAGE_VERSION)

UNAME_SYSTEM ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
UNAME_MACHINE ?= $(shell uname -m)

TARGET_SYSTEM = $(UNAME_SYSTEM)
TARGET_MACHINE = $(UNAME_MACHINE)

ifeq ($(UNAME_MACHINE),x86_64)
TARGET_MACHINE = amd64
endif

TARGET_PLATFORM = $(TARGET_SYSTEM)-$(TARGET_MACHINE)
BUILDX_BUILDER ?= educates-multiarch-builder

TARGET_PLATFORMS ?=

# Platform configuration - can be overridden by TARGET_PLATFORMS env var or make parameter
ifeq ($(TARGET_PLATFORMS),)
# Default to both platforms when TARGET_PLATFORMS is not set
MULTIARCH_PLATFORMS = linux/amd64,linux/arm64
else
# Use TARGET_PLATFORMS when set (allows for custom multiarch builds)
MULTIARCH_PLATFORMS = $(TARGET_PLATFORMS)
endif

DOCKER_BUILDER = --builder ${BUILDX_BUILDER} --push

INFO_COLOR = \033[0;36m
NO_COLOR   = \033[m
DEV_UI_SOURCE?=http://localhost:3000

print-vars:
	@echo "--- Makefile Variables ---"
	@echo "OS:      $(UNAME_SYSTEM)"
	@echo "Arch:    $(UNAME_MACHINE)"
	@echo "Shell:   $(SHELL)"
	@echo "TARGET_SYSTEM: $(TARGET_SYSTEM)"
	@echo "TARGET_MACHINE: $(TARGET_MACHINE)"
	@echo "TARGET_PLATFORM: $(TARGET_PLATFORM)"
	@echo "MULTIARCH_PLATFORMS: $(MULTIARCH_PLATFORMS)"
	@echo "DOCKER_BUILDER: $(DOCKER_BUILDER)"


build-extension: setup-buildx ## Build service image to be deployed as a desktop extension
	docker build --pull --progress plain --platform $(MULTIARCH_PLATFORMS) \
	    $(DOCKER_BUILDER) \
		-t $(IMAGE) \
		.
	docker pull $(IMAGE)

# Internal target to ensure alias tag is always up to date after a build+push+pull.
# The inspect-then-tag guard was intentionally removed: if the alias already exists
# but points to the old digest, the guard would skip the retag and leave a stale image.
.PHONY: _ensure-alias-tag
_ensure-alias-tag: build-extension
	@if [ "$(IMAGE)" != "$(IMAGE_ALIAS_FULL)" ]; then \
		echo "Tagging $(IMAGE) as $(IMAGE_ALIAS_FULL)"; \
		docker tag $(IMAGE) $(IMAGE_ALIAS_FULL); \
	fi

install-extension: _ensure-alias-tag ## Install the extension
	docker extension install --force $(IMAGE_ALIAS_FULL)

update-extension: _ensure-alias-tag ## Update the extension
	docker extension update --force $(IMAGE_ALIAS_FULL)

# builder management
setup-buildx: ## Setup builder for multiarch builds
	docker buildx create --name $(BUILDX_BUILDER) --driver docker-container --driver-opt default-load=true --driver-opt network=host --use || true
	docker buildx inspect $(BUILDX_BUILDER) --bootstrap

clean-buildx: ## Clean up builder
	docker buildx rm $(BUILDX_BUILDER) || true

.PHONY: debug
debug: _ensure-alias-tag ## Enable debug in the extension
	docker extension dev debug $(IMAGE_ALIAS_FULL)

.PHONY: source
source: _ensure-alias-tag ## Replace the UI source of the extension
	docker extension dev ui-source $(IMAGE_ALIAS_FULL) $(DEV_UI_SOURCE)

.PHONY: dev-enable
dev-enable: source debug

.PHONY: dev-reset
dev-reset: ## Reset development status of the extension
	docker extension dev reset $(IMAGE_ALIAS_FULL)

help: ## Show this help
	@echo Please specify a build target. The choices are:
	@grep -E '^[0-9a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "$(INFO_COLOR)%-30s$(NO_COLOR) %s\n", $$1, $$2}'

.PHONY: help
