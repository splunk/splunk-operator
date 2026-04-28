# Default environment is default
ENVIRONMENT ?= default

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 3.1.0

# SPLUNK_ENTERPRISE_IMAGE defines the splunk docker tag that is used as default image.
SPLUNK_ENTERPRISE_IMAGE ?= "docker.io/splunk/splunk"

# WATCH_NAMESPACE defines if its clusterwide operator or namespace specific
# by default we leave it as clusterwide if it has to be namespace specific,
# add namespace to this
WATCH_NAMESPACE ?= ""

# SPLUNK_GENERAL_TERMS is used for the mandatory acknowledgment mechanism for
# the Splunk General Terms (SGT) https://www.splunk.com/en_us/legal/splunk-general-terms.html.
# See README for more information on the required value.
SPLUNK_GENERAL_TERMS ?= ""

# NAMESPACE defines default namespace where operator will be installed
NAMESPACE ?= "splunk-operator"

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=${CHANNELS}
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=${DEFAULT_CHANNEL}
endif
BUNDLE_METADATA_OPTS ?= ${BUNDLE_CHANNELS} ${BUNDLE_DEFAULT_CHANNEL}

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# splunk/splunk-operator-bundle:$VERSION and splunk/splunk-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= splunk/splunk-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= ${IMAGE_TAG_BASE}-bundle:v${VERSION}

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
# Automatically derive the version from go.mod
ENVTEST_VERSION := $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
ENVTEST_K8S_VERSION := $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')

ignore-not-found ?= True

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Security Scanner Variables
SCANNER_DATE := `date +%Y-%m-%d`
SCANNER_DATE_YEST := `TZ=GMT+24 +%Y:%m:%d`
SCANNER_VERSION := v8
SCANNER_LOCALIP := $(shell addr=$$(hostname -I 2>/dev/null | tr ' ' '\n' | grep -E '^[0-9]+(\.[0-9]+){3}$$' | grep -v '^127\.' | head -n 1); if [ -n "$$addr" ]; then printf '%s' "$$addr"; else ifconfig | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | grep -Eo '([0-9]*\.){3}[0-9]*' | grep -v '127.0.0.1' | awk '{print $$1}' | head -n 1; fi)
ifeq ($(shell uname), Linux)
	SCANNER_FILE = clair-scanner_linux_amd64
else ifeq ($(shell uname), Darwin)
	SCANNER_FILE = clair-scanner_darwin_amd64
else
	SCANNER_FILE = clair-scanner_windows_amd64.exe
endif

SED := sed -i
ifeq ($(shell uname), Linux)
	SED = sed -i
else ifeq ($(shell uname), Darwin)
	SED = sed -i ""
else
	SED = sed -i
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	rm -f config/crd/bases/_.yaml

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

scheck: ## Run static check against code
	go install honnef.co/go/tools/cmd/staticcheck@2022.1
	staticcheck ./...

vet: setup/ginkgo	 ## Run go vet against code.
	go vet ./...

test: manifests generate fmt vet setup-envtest ## Run tests.
	REPORT_FILE="$${UNIT_TEST_REPORT_FILE:-unit_test.xml}"; \
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use ${ENVTEST_K8S_VERSION} --bin-dir $(LOCALBIN) -p path)" ginkgo --junit-report=$$REPORT_FILE --output-dir=`pwd` -vv --trace --keep-going --timeout=$${TEST_TIMEOUT:-170m} --cover --covermode=count --coverprofile=coverage.out ./pkg/splunk/common ./pkg/splunk/enterprise ./pkg/splunk/client ./pkg/splunk/util ./internal/controller ./pkg/splunk/splkcontroller


##@ Documentation

docs-preview: ## Preview documentation locally with Jekyll (requires Ruby and bundler)
	@echo "Installing dependencies locally..."
	@cd docs && bundle config set --local path vendor/bundle && bundle install
	@echo "Starting Jekyll server for documentation preview..."
	@cd docs && bundle exec jekyll serve --livereload
	@echo "Documentation available at http://localhost:4000/splunk-operator"

##@ Helm

HELM_OPERATOR_CHART = helm-chart/splunk-operator

.PHONY: helm-lint
helm-lint: ## Lint Helm charts
	helm lint $(HELM_OPERATOR_CHART)

.PHONY: helm-test
helm-test: setup/helm-unittest ## Run Helm chart unit tests
	helm unittest $(HELM_OPERATOR_CHART)

.PHONY: helm-check
helm-check: helm-lint helm-test ## Run Helm lint and unit tests

##@ Build

build: setup/ginkgo manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

docker-build: #test ## Build docker image with the manager.
	docker build -t ${IMG} .

docker-push: ## Push docker image with the manager.
	docker push ${IMG}

# Docker-buildx is used to build the image for multiple OS/platforms
# IMG is a mandatory argument to specify the image name
# Pass only what is required, the rest will use the Dockerfile defaults
PLATFORMS ?= linux/amd64,linux/arm64

docker-buildx:
	@if [ -z "${IMG}" ]; then \
	        echo "Error: IMG is a mandatory argument. Usage: make docker-buildx IMG=<image_name> ...."; \
	        exit 1; \
	    fi; \
	    docker buildx inspect project-v3-builder >/dev/null 2>&1 || docker buildx create --name project-v3-builder; \
	    docker buildx use project-v3-builder; \
	    if echo "${BASE_IMAGE}" | grep -q "distroless"; then \
	        DOCKERFILE="Dockerfile.distroless"; \
	    else \
	        DOCKERFILE="Dockerfile"; \
	        if [ -n "${BUILDER_IMAGE}" ]; then \
	            BUILDER_IMAGE_ARG="--build-arg BUILDER_IMAGE=${BUILDER_IMAGE}"; \
	        else \
	            BUILDER_IMAGE_ARG=""; \
	        fi; \
	    fi; \
	    if [ -n "${BASE_IMAGE}" ]; then \
	        BASE_IMAGE_ARG="--build-arg BASE_IMAGE=${BASE_IMAGE}"; \
	    else \
	        BASE_IMAGE_ARG=""; \
	    fi; \
	    if [ -n "${BASE_IMAGE_VERSION}" ]; then \
	        BASE_IMAGE_VERSION_ARG="--build-arg BASE_IMAGE_VERSION=${BASE_IMAGE_VERSION}"; \
	    else \
	        BASE_IMAGE_VERSION_ARG=""; \
	    fi; \
	    docker buildx build --push \
	        --platform="${PLATFORMS}" \
	        $$BASE_IMAGE_ARG \
	        $$BASE_IMAGE_VERSION_ARG \
	        $$BUILDER_IMAGE_ARG \
	        --tag "${IMG}" -f "$$DOCKERFILE" .

.PHONY: setup/kubectl
setup/kubectl:
	@if [ -z "${KUBECTL_VERSION}" ] || [ -z "${CI_BIN_DIR}" ]; then \
		echo "Error: KUBECTL_VERSION and CI_BIN_DIR are required"; \
		exit 1; \
	fi
	@mkdir -p "${CI_BIN_DIR}"
	@if [ ! -x "${CI_BIN_DIR}/kubectl" ]; then \
		curl -fsSL -o "${CI_BIN_DIR}/kubectl" "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl"; \
		chmod +x "${CI_BIN_DIR}/kubectl"; \
	fi

.PHONY: setup/eksctl
setup/eksctl:
	@if [ -z "${EKSCTL_VERSION}" ] || [ -z "${CI_BIN_DIR}" ]; then \
		echo "Error: EKSCTL_VERSION and CI_BIN_DIR are required"; \
		exit 1; \
	fi
	@mkdir -p "${CI_BIN_DIR}"
	@if [ ! -x "${CI_BIN_DIR}/eksctl" ]; then \
		tmp_archive="/tmp/eksctl-${EKSCTL_VERSION}-amd64.tar.gz"; \
		curl --fail --show-error --silent --location --retry 3 --retry-delay 2 -o "$$tmp_archive" "https://github.com/eksctl-io/eksctl/releases/download/${EKSCTL_VERSION}/eksctl_$$(uname -s)_amd64.tar.gz"; \
		if ! tar -tzf "$$tmp_archive" eksctl >/dev/null 2>&1; then \
			echo "Downloaded eksctl archive is invalid: $$tmp_archive" >&2; \
			wc -c "$$tmp_archive" >&2 || true; \
			sed -n '1,20p' "$$tmp_archive" >&2 || true; \
			rm -f "$$tmp_archive"; \
			exit 1; \
		fi; \
		tar -xzf "$$tmp_archive" -C "${CI_BIN_DIR}" eksctl; \
		chmod +x "${CI_BIN_DIR}/eksctl"; \
		rm -f "$$tmp_archive"; \
	fi

.PHONY: setup/helm
setup/helm:
	@if [ -z "${HELM_VERSION}" ] || [ -z "${CI_BIN_DIR}" ]; then \
		echo "Error: HELM_VERSION and CI_BIN_DIR are required"; \
		exit 1; \
	fi
	@mkdir -p "${CI_BIN_DIR}"
	@if [ ! -x "${CI_BIN_DIR}/helm" ]; then \
		tmp_archive="/tmp/helm-${HELM_VERSION}-linux-amd64.tar.gz"; \
		curl -fsSL -o "$$tmp_archive" "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz"; \
		tar -xzf "$$tmp_archive" -C /tmp linux-amd64/helm; \
		mv /tmp/linux-amd64/helm "${CI_BIN_DIR}/helm"; \
		chmod +x "${CI_BIN_DIR}/helm"; \
		rm -rf /tmp/linux-amd64 "$$tmp_archive"; \
	fi

.PHONY: setup/kuttl
setup/kuttl:
	@if [ -z "${KUTTL_VERSION}" ] || [ -z "${CI_BIN_DIR}" ]; then \
		echo "Error: KUTTL_VERSION and CI_BIN_DIR are required"; \
		exit 1; \
	fi
	@mkdir -p "${CI_BIN_DIR}"
	@normalized_version="v$${KUTTL_VERSION#v}"; \
	target="${CI_BIN_DIR}/kubectl-kuttl"; \
	versioned="$${target}-$${normalized_version}"; \
	[ -f "$$versioned" ] || { \
		set -e; \
		asset_url="https://github.com/kudobuilder/kuttl/releases/download/$${normalized_version}/kubectl-kuttl_$${normalized_version#v}_linux_x86_64"; \
		echo "Downloading $$asset_url"; \
		curl --retry 5 --retry-all-errors --retry-delay 2 -fsSL -o "$$versioned" "$$asset_url"; \
		chmod +x "$$versioned"; \
	}; \
	ln -sf "$$versioned" "$$target"



##@ Deployment
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side --force-conflicts -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=${ignore-not-found} -f -

deploy: manifests kustomize uninstall ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(SED) "s/namespace: splunk-operator/namespace: ${NAMESPACE}/g"  config/${ENVIRONMENT}/kustomization.yaml
	$(SED) "s/value: WATCH_NAMESPACE_VALUE/value: \"${WATCH_NAMESPACE}\"/g"  config/${ENVIRONMENT}/kustomization.yaml
	$(SED) "s|SPLUNK_ENTERPRISE_IMAGE|${SPLUNK_ENTERPRISE_IMAGE}|g"  config/${ENVIRONMENT}/kustomization.yaml
	$(SED) "s/value: SPLUNK_GENERAL_TERMS_VALUE/value: \"${SPLUNK_GENERAL_TERMS}\"/g"  config/${ENVIRONMENT}/kustomization.yaml
	$(SED) 's/\("sokVersion": \)"[^"]*"/\1"$(VERSION)"/' config/manager/controller_manager_telemetry.yaml
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	RELATED_IMAGE_SPLUNK_ENTERPRISE=${SPLUNK_ENTERPRISE_IMAGE} WATCH_NAMESPACE=${WATCH_NAMESPACE} SPLUNK_GENERAL_TERMS=${SPLUNK_GENERAL_TERMS} $(KUSTOMIZE) build config/${ENVIRONMENT} | kubectl apply --server-side --force-conflicts -f -
	$(SED) "s/namespace: ${NAMESPACE}/namespace: splunk-operator/g"  config/${ENVIRONMENT}/kustomization.yaml
	$(SED) "s/value: \"${WATCH_NAMESPACE}\"/value: WATCH_NAMESPACE_VALUE/g"  config/${ENVIRONMENT}/kustomization.yaml
	$(SED) "s|${SPLUNK_ENTERPRISE_IMAGE}|SPLUNK_ENTERPRISE_IMAGE|g"  config/${ENVIRONMENT}/kustomization.yaml
	$(SED) "s/value: \"${SPLUNK_GENERAL_TERMS}\"/value: SPLUNK_GENERAL_TERMS_VALUE/g"  config/${ENVIRONMENT}/kustomization.yaml

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/${ENVIRONMENT} | kubectl delete -f -

## Location to install dependencies to
LOCALBIN ?= "$(shell pwd)/bin"
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Versions
KUSTOMIZE_VERSION ?= v5.4.3
CONTROLLER_TOOLS_VERSION ?= v0.18.0
GOLANGCI_LINT_VERSION ?= v2.1.0
GOSEC_VERSION ?= v2.22.4
GOVULNCHECK_VERSION ?= v1.1.4
HELM_UNITTEST_VERSION ?= v1.0.3

CONTROLLER_GEN = $(LOCALBIN)/controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_TOOLS_VERSION}

KUSTOMIZE = $(LOCALBIN)/kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

ENVTEST = $(LOCALBIN)/setup-envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: setup-envtest
setup-envtest: envtest ## Set up ENVTEST binaries for the correct version
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
	  echo "Error setting up envtest"; exit 1; }

GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

# go-install-tool will 'go install' any package with custom target and target binary name
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

## Generate bundle manifests and metadata, then validate generated files.
.PHONY: bundle
bundle: manifests kustomize
	operator-sdk generate kustomize manifests -q
	cp config/default/kustomization-cluster.yaml config/default/kustomization.yaml
	$(SED) "s/namespace: splunk-operator/namespace: ${NAMESPACE}/g"  config/default/kustomization.yaml
	$(SED) "s|SPLUNK_ENTERPRISE_IMAGE|${SPLUNK_ENTERPRISE_IMAGE}|g"  config/default/kustomization.yaml
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle ${BUNDLE_GEN_FLAGS}
	operator-sdk bundle validate ./bundle
	operator-sdk bundle validate bundle --select-optional suite=operatorframework

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t ${BUNDLE_IMG} .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=${BUNDLE_IMG}

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.55.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= ${BUNDLE_IMG}

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= ${IMAGE_TAG_BASE}-catalog:v${VERSION}

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index ${CATALOG_BASE_IMG}
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag ${CATALOG_IMG} --bundles ${BUNDLE_IMGS} ${FROM_INDEX_OPT}

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=${CATALOG_IMG}



.PHONY: code/sec
code/sec: setup/gosec ## Run gosec
	$(LOCALBIN)/gosec -severity medium --confidence medium -quiet ./...

.PHONY: setup/gosec
setup/gosec: $(LOCALBIN)/gosec

$(LOCALBIN)/gosec: $(LOCALBIN)
	$(call go-install-tool,$(LOCALBIN)/gosec,github.com/securego/gosec/v2/cmd/gosec,$(GOSEC_VERSION))

.PHONY: code/vulncheck
code/vulncheck: setup/govulncheck ## Run govulncheck
	$(LOCALBIN)/govulncheck ./...

.PHONY: setup/govulncheck
setup/govulncheck: $(LOCALBIN)/govulncheck

$(LOCALBIN)/govulncheck: $(LOCALBIN)
	$(call go-install-tool,$(LOCALBIN)/govulncheck,golang.org/x/vuln/cmd/govulncheck,$(GOVULNCHECK_VERSION))

.PHONY: cluster-up
cluster-up:
	@test/deploy-cluster.sh up

.PHONY: cluster-down
cluster-down:
	@test/deploy-cluster.sh down

.PHONY: int-test
int-test:
	@echo Run integration test
	@test/run-tests.sh

.PHONY: helm-package
helm-package:
	@rm -f helm-chart/splunk-enterprise/charts/splunk-operator-*.tgz
	@helm package helm-chart/splunk-operator --destination .
	@mv splunk-operator-*.tgz helm-chart/splunk-enterprise/charts/

.PHONY: helm-kuttl-test
helm-kuttl-test:
	@if [ -z "${KUTTL_CONFIG}" ]; then \
		echo "Error: KUTTL_CONFIG is required"; \
		exit 1; \
	fi
	@kubectl kuttl test --config "${KUTTL_CONFIG}" --report xml

lang:
	@echo Running bias language linter
	@tools/bias_language_linter.sh

stop_clair_scanner:
	@docker stop clair_db || true
	@docker rm clair_db || true
	@docker stop clair || true
	@docker rm clair || true

setup_clair_scanner: stop_clair_scanner
	@mkdir -p clair-scanner-logs
	@docker pull arminc/clair-db:${SCANNER_DATE} || docker pull arminc/clair-db:${SCANNER_DATE_YEST} || echo "WARNING: Failed to pull daily image, defaulting to latest" >> clair-scanner-logs/clair_setup_errors.log ; docker pull arminc/clair-db:latest
	@docker run -d --name clair_db arminc/clair-db:${SCANNER_DATE} || docker run -d --name clair_db arminc/clair-db:${SCANNER_DATE_YEST} || docker run -d --name clair_db arminc/clair-db:latest
	@docker run -p 6060:6060 --link clair_db:postgres -d --name clair --restart on-failure arminc/clair-local-scan:v2.0.6
	@wget https://github.com/arminc/clair-scanner/releases/download/${SCANNER_VERSION}/${SCANNER_FILE}
	@mv ${SCANNER_FILE} clair-scanner
	@chmod +x clair-scanner
	@echo "Waiting for clair daemon to start"
	@retries=0 ; while( ! wget -T 10 -q -O /dev/null http://0.0.0.0:6060/v1/namespaces ) ; do sleep 1 ; echo -n "." ; if [ $$retries -eq 10 ] ; then echo " Timeout, aborting." ; exit 1 ; fi ; retries=$$(($$retries+1)) ; done
	@echo "Clair daemon started."

run_clair_scan:
	@./clair-scanner -c http://0.0.0.0:6060 --ip ${SCANNER_LOCALIP} -r clair-scanner-logs/results.json -l clair-scanner-logs/results.log ${IMG}


# generate artifacts needed to deploy operator, this is current way of doing it, need to fix this
generate-artifacts-namespace: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(SED) 's/\("sokVersion": \)"[^"]*"/\1"$(VERSION)"/' config/manager/controller_manager_telemetry.yaml
	mkdir -p release-${VERSION}
	cp config/default/kustomization-namespace.yaml config/default/kustomization.yaml
	cp config/rbac/kustomization-namespace.yaml config/rbac/kustomization.yaml
	$(SED) "s/namespace: splunk-operator/namespace: ${NAMESPACE}/g"  config/default/kustomization.yaml
	$(SED) "s|SPLUNK_ENTERPRISE_IMAGE|${SPLUNK_ENTERPRISE_IMAGE}|g"  config/default/kustomization.yaml
	$(SED) "s/ClusterRole/Role/g"  config/rbac/role.yaml
	$(SED) "s/ClusterRole/Role/g"  config/rbac/role_binding.yaml
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	RELATED_IMAGE_SPLUNK_ENTERPRISE=${SPLUNK_ENTERPRISE_IMAGE} WATCH_NAMESPACE=${WATCH_NAMESPACE} SPLUNK_GENERAL_TERMS=${SPLUNK_GENERAL_TERMS} $(KUSTOMIZE) build config/default > release-${VERSION}/splunk-operator-namespace.yaml
	$(SED) "s/Role/ClusterRole/g"  config/rbac/role.yaml
	$(SED) "s/Role/ClusterRole/g"  config/rbac/role_binding.yaml


# generate artifacts needed to deploy operator, this is current way of doing it, need to fix this
generate-artifacts-cluster: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(SED) 's/\("sokVersion": \)"[^"]*"/\1"$(VERSION)"/' config/manager/controller_manager_telemetry.yaml
	mkdir -p release-${VERSION}
	cp config/default/kustomization-cluster.yaml config/default/kustomization.yaml
	cp config/rbac/kustomization-cluster.yaml config/rbac/kustomization.yaml
	$(SED) "s/namespace: splunk-operator/namespace: ${NAMESPACE}/g"  config/default/kustomization.yaml
	$(SED) "s|SPLUNK_ENTERPRISE_IMAGE|${SPLUNK_ENTERPRISE_IMAGE}|g"  config/default/kustomization.yaml
	$(SED) "s/WATCH_NAMESPACE_VALUE/\"${WATCH_NAMESPACE}\"/g"  config/default/kustomization.yaml
	$(SED) "s/SPLUNK_GENERAL_TERMS_VALUE/\"${SPLUNK_GENERAL_TERMS}\"/g"  config/default/kustomization.yaml
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	RELATED_IMAGE_SPLUNK_ENTERPRISE=${SPLUNK_ENTERPRISE_IMAGE} WATCH_NAMESPACE=${WATCH_NAMESPACE} SPLUNK_GENERAL_TERMS=${SPLUNK_GENERAL_TERMS} $(KUSTOMIZE) build config/default > release-${VERSION}/splunk-operator-cluster.yaml


generate-crds: manifests kustomize ## Generate CRD artifacts
	mkdir -p release-${VERSION}
	$(KUSTOMIZE) build config/crd > release-${VERSION}/splunk-operator-crds.yaml

generate-artifacts: generate-artifacts-namespace generate-artifacts-cluster generate-crds
	echo "artifacts generation complete"

#############################

GO_DOWNLOAD_URL=https://go.dev/dl/go1.17.7.darwin-amd64.pkg
export OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/v1.17.0
OPERATOR_SDK_DOWNLOAD_URL=curl -LO ${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH}
MINIKUBE_DOWNLOAD_URL=https://storage.googleapis.com/minikube/releases/latest/minikube-${OS}-${ARCH}
KUBECTL_DOWNLOAD_URL="https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/${OS}/${ARCH}/kubectl"

.PHONY: setup/devsetup
setup/devsetup:
	@echo Installing go
	@curl -Lo go.tar.gz ${GO_DOWNLOAD_URL} && tar -C /usr/local -xvzf  go.tar.gz
	@curl -Lo kubectl ${KUBECTL_DOWNLOAD_URL} && chmod +x kubectl && sudo mv kubectl /usr/local/bin/
	@echo Installing Kubectl
	@curl -Lo kubectl ${KUBECTL_DOWNLOAD_URL} && chmod +x kubectl && sudo mv kubectl /usr/local/bin/
	@echo Installing operator-sdk
	@curl -LO ${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH}
	@sudo chmod +x operator-sdk_${OS}_${ARCH} && sudo mv operator-sdk_${OS}_${ARCH} /usr/local/bin/operator-sdk


clean: stop_clair_scanner
	@rm -rf ./build/_output
	@docker rmi  ${IMG} || true
	@rm -f clair-scanner
	@rm -rf clair-scanner-logs

cleanup:
	@./tools/cleanup.sh

.PHONY: setup/ginkgo
setup/ginkgo:
	@echo Installing ginkgo
	@go install -mod=mod github.com/onsi/ginkgo/v2/ginkgo@$(shell go list -m -f '{{.Version}}' github.com/onsi/ginkgo/v2)

.PHONY: setup/helm-unittest
setup/helm-unittest:
	@helm plugin list 2>/dev/null | grep -q unittest || \
		helm plugin install https://github.com/helm-unittest/helm-unittest.git --version $(HELM_UNITTEST_VERSION)

.PHONY: build-installer
build-installer: manifests generate kustomize
	mkdir -p dist
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default > dist/install.yaml
