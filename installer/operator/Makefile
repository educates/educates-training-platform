# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# YEAR defines the year value used for substituting the YEAR placeholder in the boilerplate header.
YEAR ?= $(shell date +%Y)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

# CRD and RBAC manifests are written directly into the educates-installer
# Helm chart — the chart is the canonical artefact, not a kustomize tree.
# CRDs land in crds/ (Helm's reserved location: installed once, not
# templated, not deleted on uninstall). The ClusterRole lands in
# templates/rbac/ as plain YAML; controller-gen output has no Helm
# template directives, so Helm renders it as-is.
CHART_DIR ?= ../charts/educates-installer
CRD_OUTPUT_DIR ?= $(CHART_DIR)/crds
RBAC_OUTPUT_DIR ?= $(CHART_DIR)/templates/rbac

.PHONY: manifests
manifests: controller-gen ## Generate ClusterRole and CustomResourceDefinition objects.
	@mkdir -p "$(CRD_OUTPUT_DIR)" "$(RBAC_OUTPUT_DIR)"
	"$(CONTROLLER_GEN)" \
		crd paths="./..." output:crd:artifacts:config="$(CRD_OUTPUT_DIR)" \
		'rbac:roleName="educates:installer:manager"' output:rbac:artifacts:config="$(RBAC_OUTPUT_DIR)"
	@# Keep the operator's embedded copy of its CRDs byte-identical to the
	@# chart's crds/. The operator SSA-applies these at startup (internal/crds)
	@# so its running code always matches its CRD schemas regardless of how it
	@# was installed. CI's manifest-drift check covers this copy too.
	rm -rf internal/crds/files
	mkdir -p internal/crds/files
	cp "$(CRD_OUTPUT_DIR)"/*.yaml internal/crds/files/

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	"$(CONTROLLER_GEN)" object:headerFile="hack/boilerplate.go.txt",year=$(YEAR) paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# Local-only kind-based smoke test. Builds the operator image, loads it into
# kind, helm-installs the chart, applies a sample CR, and asserts the
# reconcile log line is emitted. Tears down the kind cluster on exit unless
# KEEP_CLUSTER=true. CI does not run this — kind-in-Actions lands in Phase 2
# alongside chart-install testing.
.PHONY: smoke-test
smoke-test: ## Local kind-based smoke test of the operator + chart.
	bash hack/smoke-test.sh

# Local-only RBAC acceptance test. Installs and tears down the full platform on
# kind with the operator running under only its fine-grained roles (no
# cluster-admin), asserting no RBAC-forbidden errors. The real gate for the
# fine-grained-role change — envtest runs privileged and cannot catch an
# under-grant. Not run in CI (kind-in-Actions is a separate follow-up).
.PHONY: rbac-verify
rbac-verify: ## Local kind-based RBAC acceptance test (install+teardown, cluster-admin off).
	bash hack/rbac-verify.sh

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	"$(GOLANGCI_LINT)" run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	"$(GOLANGCI_LINT)" run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	"$(GOLANGCI_LINT)" config verify

##@ Vendoring

# Vendored upstream charts live inside the operator module so //go:embed
# in internal/charts/ can reach them directly. See vendored-charts/README.md
# for layout and the rationale.
VENDORED_CHARTS_DIR := $(shell pwd)/vendored-charts

# Each entry: <chart-name>=<version>=<upstream-tarball-URL>
# Order doesn't matter; vendor-charts iterates and verifies each one
# against ../vendored-charts/SHA256SUMS.
VENDORED_CHARTS := \
	cert-manager=v1.20.3=https://charts.jetstack.io/charts/cert-manager-v1.20.3.tgz \
	contour=0.7.0=https://github.com/projectcontour/helm-charts/releases/download/contour-0.7.0/contour-0.7.0.tgz \
	external-dns=1.21.1=https://github.com/kubernetes-sigs/external-dns/releases/download/external-dns-helm-chart-1.21.1/external-dns-1.21.1.tgz \
	kyverno=3.8.1=https://kyverno.github.io/kyverno/kyverno-3.8.1.tgz

.PHONY: vendor-charts
vendor-charts: ## Download upstream Helm charts into vendored-charts/ and verify against SHA256SUMS.
	@set -e; \
	if [ ! -f "$(VENDORED_CHARTS_DIR)/SHA256SUMS" ]; then \
		echo "missing $(VENDORED_CHARTS_DIR)/SHA256SUMS — record expected hashes there before running this target"; \
		exit 1; \
	fi; \
	for entry in $(VENDORED_CHARTS); do \
		name=$${entry%%=*}; rest=$${entry#*=}; \
		version=$${rest%%=*}; url=$${rest#*=}; \
		file="$$name-$$version.tgz"; \
		dest="$(VENDORED_CHARTS_DIR)/$$file"; \
		echo ">> $$file"; \
		tmp=$$(mktemp); \
		curl -sSfL -o "$$tmp" "$$url"; \
		got=$$(shasum -a 256 "$$tmp" | awk '{print $$1}'); \
		want=$$(awk -v f="$$file" '$$2==f {print $$1}' "$(VENDORED_CHARTS_DIR)/SHA256SUMS"); \
		if [ -z "$$want" ]; then \
			echo "no SHA256 recorded for $$file in $(VENDORED_CHARTS_DIR)/SHA256SUMS"; \
			rm -f "$$tmp"; exit 1; \
		fi; \
		if [ "$$got" != "$$want" ]; then \
			echo "checksum mismatch for $$file: got $$got, want $$want"; \
			rm -f "$$tmp"; exit 1; \
		fi; \
		mv "$$tmp" "$$dest"; \
		echo "   ok ($$got)"; \
	done

.PHONY: verify-vendored-charts
verify-vendored-charts: ## Re-verify SHA256 of every tarball already in vendored-charts/.
	@set -e; cd "$(VENDORED_CHARTS_DIR)" && shasum -a 256 -c SHA256SUMS

# In-repo subcharts of the umbrella `educates-training-platform` chart.
# Operator-driven installs use the same tarball+go:embed pattern as the
# upstream cluster-services charts (cert-manager, Contour, etc.) so the
# install mechanism stays uniform across phases. The tarballs are
# committed to the repo because:
#   - the operator image embeds them at build time;
#   - the source charts are deliberately stable (subchart version bumps
#     are a reviewable git event, not a side effect of every operator
#     build);
#   - editors / `go build` work without a separate generate step.
# Regenerate after editing any of the listed subcharts.
LOCAL_SUBCHARTS_SRC := $(shell pwd)/../charts/educates-training-platform/charts
LOCAL_SUBCHARTS := secrets-manager lookup-service session-manager node-ca-injector remote-access

.PHONY: package-local-charts
package-local-charts: ## Repackage in-repo subcharts (secrets-manager, lookup-service, session-manager) into vendored-charts/.
	@set -e; \
	for name in $(LOCAL_SUBCHARTS); do \
		src="$(LOCAL_SUBCHARTS_SRC)/$$name"; \
		if [ ! -d "$$src" ]; then \
			echo "skip $$name (not present at $$src)"; \
			continue; \
		fi; \
		echo ">> packaging $$name from $$src"; \
		helm package "$$src" --destination "$(VENDORED_CHARTS_DIR)"; \
	done

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager ./cmd

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.20.1

#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell v='$(call gomodver,sigs.k8s.io/controller-runtime)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_VERSION manually (controller-runtime replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?([0-9]+)\.([0-9]+).*/release-\1.\2/')

#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell v='$(call gomodver,k8s.io/api)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_K8S_VERSION manually (k8s.io/api replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?[0-9]+\.([0-9]+).*/1.\1/')

GOLANGCI_LINT_VERSION ?= v2.11.4

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))
	@test -f .custom-gcl.yml && { \
		echo "Building custom golangci-lint with plugins..." && \
		$(GOLANGCI_LINT) custom --destination $(LOCALBIN) --name golangci-lint-custom && \
		mv -f $(LOCALBIN)/golangci-lint-custom $(GOLANGCI_LINT); \
	} || true

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef

define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef
