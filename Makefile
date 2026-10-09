# =============================================================================
# Educates Training Platform — build system
# =============================================================================
#
# Plain `make` produces a complete locally-built system: the educates CLI
# (with compiled-in defaults pointing at the local registry), all core
# platform images and the operator image, pushed to localhost:5001. Then:
#
#   client-programs/bin/educates-<platform> local cluster create
#
# deploys the locally built system with no further configuration.
#
# Knobs (env vars or `make VAR=value`):
#   IMAGE_REPOSITORY  registry for images (default localhost:5001)
#   PACKAGE_VERSION   image tag (default latest)
#   TARGET_PLATFORMS  image platforms; DEFAULTS TO THE CURRENT HOST
#                     ARCHITECTURE ONLY. Multi-arch is explicit opt-in:
#                     TARGET_PLATFORMS=linux/amd64,linux/arm64
#   PUSH_IMAGES       false = load into the Docker daemon instead of
#                     pushing (single platform only; default true)
#   CLI_VERSION / CLI_IMAGE_REPOSITORY
#                     compiled-in CLI defaults (default: PACKAGE_VERSION /
#                     IMAGE_REPOSITORY). A non-semver CLI_VERSION marks the
#                     binary as a dev build, which auto-targets its
#                     registry for all platform images at deploy time.
#
# `make` regenerates committed embedded artifacts (operator CRDs, CLI-embedded
# chart and schemas) when their sources changed — a dirty tree after `make`
# means those need committing. It also packages the runtime subchart
# tarballs the operator embeds, but those are gitignored build output.
#
# Prerequisites beyond docker + go: helm (subchart packaging).
# Run `make help` for the target list.
# =============================================================================

IMAGE_REPOSITORY ?= localhost:5001
PACKAGE_VERSION ?= latest

CLI_VERSION ?= $(PACKAGE_VERSION)
CLI_IMAGE_REPOSITORY ?= $(IMAGE_REPOSITORY)

# Informational build metadata stamped into the CLI so otherwise-identical
# development builds (all reporting the floating PACKAGE_VERSION) can be
# told apart via `educates version`. GIT_COMMIT carries the short commit
# plus a "-dirty" suffix for an uncommitted working tree; GIT_COMMIT_DATE
# is that commit's date (deterministic, so the binary stays reproducible
# per commit). Both empty when not in a git checkout.
GIT_COMMIT ?= $(shell git rev-parse --short=7 HEAD 2>/dev/null)$(shell git diff --quiet HEAD 2>/dev/null || echo -dirty)
GIT_COMMIT_DATE ?= $(shell git show -s --format=%cd --date=short HEAD 2>/dev/null)

UNAME_SYSTEM := $(shell uname -s | tr '[:upper:]' '[:lower:]')
UNAME_MACHINE := $(shell uname -m)

TARGET_SYSTEM = $(UNAME_SYSTEM)
TARGET_MACHINE = $(UNAME_MACHINE)

ifeq ($(UNAME_MACHINE),x86_64)
TARGET_MACHINE = amd64
endif

TARGET_PLATFORM = $(TARGET_SYSTEM)-$(TARGET_MACHINE)
BUILDX_BUILDER = educates-multiarch-builder

# Image platforms: current host architecture only, unless TARGET_PLATFORMS
# is set explicitly. Never silently multi-arch — cross-arch builds run
# under QEMU emulation and belong in CI or an explicit opt-in.
ifeq ($(TARGET_PLATFORMS),)
IMAGE_PLATFORMS = linux/$(TARGET_MACHINE)
else
IMAGE_PLATFORMS = $(TARGET_PLATFORMS)
endif

# PUSH_IMAGES=false loads the image into the Docker daemon (`--load`
# semantics, single platform only) instead of pushing to the registry.
ifeq ($(PUSH_IMAGES),false)
DOCKER_BUILDER =
IMAGE_PLATFORMS = linux/$(TARGET_MACHINE)
else
DOCKER_BUILDER = --builder ${BUILDX_BUILDER} --push
endif

# =============================================================================
# Image inventory
# =============================================================================

CORE_IMAGES = session-manager training-portal base-environment \
  docker-registry pause-container secrets-manager tunnel-manager \
  image-cache assets-server lookup-service node-ca-injector

WORKSHOP_IMAGES = jdk8-environment jdk11-environment jdk17-environment \
  jdk21-environment conda-environment

# Build context directories for images whose context isn't ./<name>.
IMAGE_DIR.base-environment = workshop-images/base-environment
$(foreach i,$(WORKSHOP_IMAGES),$(eval IMAGE_DIR.$(i) = workshop-images/$(i)))
IMAGE_DIR.desktop-environment = workshop-images/desktop-environment
IMAGE_DIR.operator = installer/operator
IMAGE_DIR.cli = client-programs

# Per-image build args. Workshop images chain FROM base-environment at
# IMAGE_REPOSITORY:PACKAGE_VERSION; the CLI image stamps the same
# compiled-in defaults as the binary build below.
WORKSHOP_BUILD_ARGS = --build-arg IMAGE_REPOSITORY=$(IMAGE_REPOSITORY) --build-arg PACKAGE_VERSION=$(PACKAGE_VERSION)
$(foreach i,$(WORKSHOP_IMAGES) desktop-environment,$(eval IMAGE_BUILD_ARGS.$(i) = $(WORKSHOP_BUILD_ARGS)))
IMAGE_BUILD_ARGS.cli = --build-arg REPOSITORY=$(IMAGE_REPOSITORY) --build-arg TAG=$(PACKAGE_VERSION) \
  --build-arg PROJECT_VERSION=$(CLI_VERSION) --build-arg IMAGE_REPOSITORY=$(CLI_IMAGE_REPOSITORY) \
  --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg GIT_COMMIT_DATE=$(GIT_COMMIT_DATE)

# =============================================================================
# Verbs
# =============================================================================

.DEFAULT_GOAL := local-build

local-build: build-cli ensure-local-registry build-core-images image-operator ## Build CLI + core images + operator for local testing (default)
	@echo ""
	@echo "Local build complete:"
	@echo "  images: $(IMAGE_REPOSITORY)/educates-*:$(PACKAGE_VERSION) ($(IMAGE_PLATFORMS))"
	@echo "  CLI:    client-programs/bin/educates-$(TARGET_PLATFORM)"
	@echo ""
	@echo "Next: client-programs/bin/educates-$(TARGET_PLATFORM) local cluster create"

all: local-build

build-core-images: setup-buildx $(addprefix image-,$(CORE_IMAGES)) ## Build + push the core platform images

build-workshop-images: setup-buildx $(addprefix image-,$(WORKSHOP_IMAGES)) ## Build + push the optional workshop language images (jdk*, conda)

build-all-images: build-core-images build-workshop-images image-cli ## Build everything: core + workshop images + CLI image

# Generic image rule: `make image-<name>` builds + pushes one image.
# Context is ./<name> unless overridden via IMAGE_DIR.<name> above.
image-%: setup-buildx
	docker build --progress plain --platform $(IMAGE_PLATFORMS) \
	    $(DOCKER_BUILDER) $(IMAGE_BUILD_ARGS.$*) \
		-t $(IMAGE_REPOSITORY)/educates-$*:$(PACKAGE_VERSION) \
		$(or $(IMAGE_DIR.$*),$*)

# The base environment image builds the package fetcher from the CLI module,
# so that module's source is staged into its build context first.
image-base-environment: stage-base-environment-fetcher

# Workshop language images chain FROM the base environment image.
$(addprefix image-,$(WORKSHOP_IMAGES)) image-desktop-environment: image-base-environment

# The operator image go:embeds the runtime subchart tarballs — refresh
# them (and generated CRDs/deepcopy) before baking the image.
image-operator: refresh-operator-embeds

# The CLI image embeds the operator chart + schemas via its build
# context and copies themes from the base-environment image.
image-cli: refresh-cli-embeds image-base-environment

# =============================================================================
# Embedded-artifact freshness
# =============================================================================
# The generated CRDs, deepcopy code, RBAC, CLI chart copy and schemas are
# committed: a dirty tree afterwards means their sources changed, so
# commit the regenerated output (CI enforces sync via
# verify-installer-chart / verify-cli-schemas and the ci-operator drift
# checks). The runtime subchart tarballs the operator embeds are the
# exception: they are gitignored build output, packaged fresh from their
# sources every time, so they can never drift from them.

refresh-operator-embeds: ## Regenerate CRDs + deepcopy + the subchart tarballs the operator embeds
	$(MAKE) -C installer/operator manifests generate package-local-charts
	$(MAKE) generate-installer-rbac

refresh-cli-embeds: refresh-operator-embeds embed-installer-chart generate-cli-schemas ## Refresh everything the CLI embeds (chart + schemas)

package-local-charts: ## Package the runtime subcharts the operator embeds into its vendored-charts/
	$(MAKE) -C installer/operator package-local-charts

generate-installer-rbac: package-local-charts ## Regenerate the operator's fine-grained chart-install ClusterRole from the vendored charts
	@# Renders every vendored chart, the upstream ones and the runtime
	@# subcharts packaged above, and emits templates/rbac/charts-role.yaml
	@# (educates-installer-charts). Run after `make vendor-charts`; CI's
	@# ci-operator drift check covers the result.
	./hack/generate-installer-rbac.sh

generate-cli-schemas:
	@# Regenerates EducatesConfig.schema.json from the platform CRDs.
	@# Run after `make manifests` in installer/operator/ when CRD shapes change.
	go run ./client-programs/hack/gen-cli-schemas

verify-cli-schemas: generate-cli-schemas
	@# Fails when the committed EducatesConfig.schema.json differs from
	@# freshly generated output. Run by client-programs CI.
	@if ! git diff --exit-code -- client-programs/pkg/config/v1alpha1/schemas/EducatesConfig.schema.json; then \
		echo "ERROR: EducatesConfig.schema.json drifted from the CRDs. Run 'make generate-cli-schemas' and commit the result."; \
		exit 1; \
	fi

embed-installer-chart:
	@# Refreshes the CLI-embedded copy of the operator chart from the
	@# canonical source. Run whenever installer/charts/educates-installer
	@# changes shape — Chart.yaml updates, new templates, new CRDs.
	@# The copy is committed (single-source-of-truth via this target);
	@# CI runs verify-installer-chart to catch drift.
	rm -rf client-programs/pkg/deployer/chart/files
	mkdir -p client-programs/pkg/deployer/chart/files
	cp -r installer/charts/educates-installer/. client-programs/pkg/deployer/chart/files/

verify-installer-chart: embed-installer-chart
	@# Fails when the committed embedded chart copy differs from the
	@# canonical chart. Run by client-programs CI.
	@if ! git diff --exit-code -- client-programs/pkg/deployer/chart/files; then \
		echo "ERROR: embedded operator chart drifted from installer/charts/educates-installer. Run 'make embed-installer-chart' and commit the result."; \
		exit 1; \
	fi

# =============================================================================
# CLI binary
# =============================================================================

build-cli: refresh-cli-embeds stage-renderer-files ## Build the educates CLI for the current host platform
	mkdir -p client-programs/bin
	(cd client-programs; go build -gcflags=all="-N -l" \
		-ldflags "-X 'main.projectVersion=$(CLI_VERSION)' -X 'main.imageRepository=$(CLI_IMAGE_REPOSITORY)' -X 'main.gitCommit=$(GIT_COMMIT)' -X 'main.buildDate=$(GIT_COMMIT_DATE)'" \
		-o bin/educates-$(TARGET_PLATFORM) cmd/educates/main.go)

build-client-programs: build-cli
client-programs-educates: build-cli

# pkg/renderer/hugo.go embeds pkg/renderer/files/* via //go:embed, but that
# directory is gitignored and populated at build time from the base-environment
# themes. go vet/build/test fail without it ("no matching files found"), so any
# CLI compile or CI-parity run must stage it first.
stage-renderer-files: ## Stage the gitignored CLI theme files the renderer embeds
	rm -rf client-programs/pkg/renderer/files
	mkdir -p client-programs/pkg/renderer/files
	cp -rp workshop-images/base-environment/opt/eduk8s/etc/themes client-programs/pkg/renderer/files/

# The base environment image builds the package fetcher from the CLI module,
# so that module's source is staged into its context too. The renderer is not
# in the fetcher's dependency graph, so the embedded themes are not needed.
stage-base-environment-fetcher: ## Stage the fetcher source the base image builds
	rm -rf workshop-images/base-environment/client-programs
	mkdir -p workshop-images/base-environment/client-programs
	tar -cf - --exclude bin --exclude pkg/renderer/files -C client-programs . \
		| tar -xf - -C workshop-images/base-environment/client-programs

# =============================================================================
# CI parity — run the same checks as the GitHub Actions workflows locally.
# Drift checks regenerate in place and fail on any diff, so a failure may
# leave generated files modified in the working tree (same as CI flags).
# =============================================================================

ci: ci-cli ci-operator ## Run all CI checks locally (CLI + operator)

ci-cli: stage-renderer-files ## CI parity for the CLI (client-programs-ci.yaml)
	cd client-programs && go vet ./...
	cd client-programs && go build ./...
	cd client-programs && go test ./...
	$(MAKE) verify-installer-chart
	$(MAKE) verify-cli-schemas

ci-operator: package-local-charts ## CI parity for the operator (installer-operator-ci.yaml)
	./hack/lint-chart-versions.sh
	cd installer/operator && go vet ./...
	cd installer/operator && go build ./...
	$(MAKE) -C installer/operator manifests
	$(MAKE) generate-installer-rbac
	@git diff --exit-code -- installer/charts/educates-installer/crds installer/charts/educates-installer/templates/rbac \
		|| { echo "ERROR: generated CRDs/RBAC drifted. Run 'make -C installer/operator manifests && make generate-installer-rbac' and commit."; exit 1; }
	$(MAKE) -C installer/operator generate
	@git diff --exit-code -- installer/operator/api \
		|| { echo "ERROR: generated DeepCopy drifted. Run 'make -C installer/operator generate' and commit."; exit 1; }
	$(MAKE) -C installer/operator test
	$(MAKE) -C installer/operator lint

# The always-on localhost:5001 registry must exist before images can be
# pushed to it. The freshly built CLI deploys it (idempotent, no cluster
# needed). Skipped when not pushing or when targeting another registry.
ensure-local-registry: build-cli
ifeq ($(PUSH_IMAGES),false)
	@echo "PUSH_IMAGES=false: images load into the Docker daemon; skipping local registry"
else ifneq ($(IMAGE_REPOSITORY),localhost:5001)
	@echo "IMAGE_REPOSITORY=$(IMAGE_REPOSITORY): assuming the registry is reachable"
else
	@docker container inspect -f '{{.State.Running}}' educates-registry 2>/dev/null | grep -q true || \
		client-programs/bin/educates-$(TARGET_PLATFORM) local registry deploy
endif

# =============================================================================
# Release preparation
# =============================================================================
# Stamp the committed tree to a concrete release version. Run as the final
# stabilization step on a release/* branch and commit the result, so the
# committed tree — and anything built from a checkout — tracks the last
# released version (see developer-docs/release-procedures.md). The release
# workflow re-runs the same stamping from the git tag at publish time, so
# this is idempotent on a properly prepared release.
#
#   make release-prep VERSION=4.0.1
#
# Needs helm: it repackages the runtime subchart tarballs the operator
# embeds, which are gitignored, and rewrites embed.go to match, alongside
# the Chart.yaml versions and the CLI's embedded chart copy. REGISTRY_HOST /
# REGISTRY_NAMESPACE default to the committed canonical values so the
# image-registry annotations stay ghcr.io/educates; override them only
# when preparing a fork release.
REGISTRY_HOST ?= ghcr.io
REGISTRY_NAMESPACE ?= educates

release-prep: ## Stamp the committed tree to VERSION (release/* branch step; commit the result)
	@test -n "$(VERSION)" || { echo "VERSION is required, e.g. make release-prep VERSION=4.0.1" >&2; exit 1; }
	./hack/stamp-release-version.sh "$(VERSION)" "$(REGISTRY_HOST)" "$(REGISTRY_NAMESPACE)"
	@echo ""
	@echo "Stamped the tree to $(VERSION). Review 'git status' / 'git diff' and commit on the release branch."

# =============================================================================
# Docker Desktop extension
# =============================================================================

# The extension Makefile shares the root knob names; TARGET_PLATFORMS
# and PUSH_IMAGES flow through automatically when set on the command
# line or in the environment, so the extension is always built for the
# same platforms as the educates-cli image it builds FROM.
build-docker-extension: image-cli ## Build the Docker Desktop extension
	$(MAKE) -C docker-extension build-extension IMAGE_REPOSITORY=$(IMAGE_REPOSITORY) PACKAGE_VERSION=$(PACKAGE_VERSION)

install-docker-extension: build-docker-extension
	$(MAKE) -C docker-extension install-extension IMAGE_REPOSITORY=$(IMAGE_REPOSITORY) PACKAGE_VERSION=$(PACKAGE_VERSION)

update-docker-extension: build-docker-extension
	$(MAKE) -C docker-extension update-extension IMAGE_REPOSITORY=$(IMAGE_REPOSITORY) PACKAGE_VERSION=$(PACKAGE_VERSION)

# =============================================================================
# Cluster conveniences
# =============================================================================

restart-training-platform: ## Restart the deployed platform components
	kubectl rollout restart deployment/secrets-manager -n educates
	kubectl rollout restart deployment/session-manager -n educates

deploy-workshop: ## Deploy the lab-k8s-fundamentals sample workshop
	kubectl apply -f https://github.com/educates/lab-k8s-fundamentals/releases/download/9.0/workshop.yaml
	kubectl apply -f https://github.com/educates/lab-k8s-fundamentals/releases/download/9.0/trainingportal.yaml
	STATUS=1; ATTEMPTS=0; ROLLOUT_STATUS_CMD="kubectl rollout status deployment/training-portal -n lab-k8s-fundamentals-ui"; until [ $$STATUS -eq 0 ] || $$ROLLOUT_STATUS_CMD || [ $$ATTEMPTS -eq 5 ]; do sleep 5; $$ROLLOUT_STATUS_CMD; STATUS=$$?; ATTEMPTS=$$((ATTEMPTS + 1)); done

delete-workshop:
	-kubectl delete trainingportal,workshop lab-k8s-fundamentals --cascade=foreground

open-workshop:
	URL=`kubectl get trainingportal/lab-k8s-fundamentals -o go-template={{.status.educates.url}}`; (test -x /usr/bin/xdg-open && xdg-open $$URL) || (test -x /usr/bin/open && open $$URL) || true

# =============================================================================
# Documentation
# =============================================================================

project-docs/venv:
	python3 -m venv project-docs/venv
	project-docs/venv/bin/pip install -r project-docs/requirements.txt

build-project-docs: project-docs/venv ## Build the Sphinx documentation
	source project-docs/venv/bin/activate && make -C project-docs html

open-project-docs:
	open project-docs/_build/html/index.html || \
        xdg-open project-docs/_build/html/index.html

clean-project-docs:
	rm -rf project-docs/venv
	rm -rf project-docs/_build

# =============================================================================
# Housekeeping
# =============================================================================

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
	rm -rf installer/operator/bin
	rm -rf project-docs/venv
	rm -rf project-docs/_build

prune-registry:
	docker exec educates-registry registry garbage-collect /etc/distribution/config.yml --delete-untagged=true

prune-all: prune-docker prune-builds prune-registry ## Clean caches and build artifacts

setup-buildx: ## Set up the buildx builder used for image pushes
	docker buildx create --name $(BUILDX_BUILDER) --driver docker-container --driver-opt default-load=true --driver-opt network=host --use || true
	docker buildx inspect $(BUILDX_BUILDER) --bootstrap

clean-buildx: ## Remove the buildx builder
	docker buildx rm $(BUILDX_BUILDER) || true

list-platforms:
	@echo "Image platforms: $(IMAGE_PLATFORMS)"

help: ## Show available targets
	@echo "Common targets (see the header comment for the knobs):"
	@grep -hE '^[a-zA-Z0-9_.%-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-26s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Any single image: make image-<name> (e.g. image-training-portal)"
	@echo "  names: $(CORE_IMAGES) $(WORKSHOP_IMAGES) desktop-environment operator cli"

.PHONY: local-build all build-core-images build-workshop-images build-all-images \
  refresh-operator-embeds refresh-cli-embeds package-local-charts \
  generate-cli-schemas verify-cli-schemas embed-installer-chart verify-installer-chart \
  build-cli build-client-programs client-programs-educates ensure-local-registry \
  stage-renderer-files ci ci-cli ci-operator release-prep \
  build-docker-extension install-docker-extension update-docker-extension \
  restart-training-platform deploy-workshop delete-workshop open-workshop \
  build-project-docs open-project-docs clean-project-docs \
  prune-images prune-docker prune-builds prune-registry prune-all \
  setup-buildx clean-buildx list-platforms help
