# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 2.1.0

# SPLUNK_ENTERPRISE_IMAGE defines the splunk docker tag that is used as default image.
SPLUNK_ENTERPRISE_IMAGE ?= "docker.io/splunk/splunk:edge"

# WATCH_NAMESPACE defines if its clusterwide operator or namespace specific
# by default we leave it as clusterwide if it has to be namespace specific, 
# add namespace to this
WATCH_NAMESPACE ?= ""

# NAMESPACE defines default namespace where operator will be installed 
NAMESPACE ?= "splunk-operator"

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# splunk/splunk-operator-bundle:$VERSION and splunk/splunk-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= splunk/splunk-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.25.0

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
SCANNER_LOCALIP := $(shell ifconfig | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | grep -Eo '([0-9]*\.){3}[0-9]*' | grep -v '127.0.0.1' | awk '{print $1}' | head -n 1)
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
	rm config/crd/bases/_.yaml

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test  -v -covermode=count -coverprofile=coverage.out --timeout=300s   ./pkg/splunk/common ./pkg/splunk/enterprise ./pkg/splunk/controller ./pkg/splunk/client ./pkg/splunk/util ./controllers 

##@ Build

build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

docker-build: test ## Build docker image with the manager.
	docker build -t ${IMG} .

docker-push: ## Push docker image with the manager.
	docker push ${IMG}

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl replace -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

deploy: manifests kustomize uninstall ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(SED) "s/namespace: splunk-operator/namespace: ${NAMESPACE}/g"  config/default/kustomization.yaml
	$(SED) "s/value: WATCH_NAMESPACE_VALUE/value: \"${WATCH_NAMESPACE}\"/g"  config/default/kustomization.yaml
	$(SED) "s|SPLUNK_ENTERPRISE_IMAGE|${SPLUNK_ENTERPRISE_IMAGE}|g"  config/default/kustomization.yaml
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	RELATED_IMAGE_SPLUNK_ENTERPRISE=${SPLUNK_ENTERPRISE_IMAGE} WATCH_NAMESPACE=${WATCH_NAMESPACE} $(KUSTOMIZE) build config/default | kubectl create -f -
	$(SED) "s/namespace: ${NAMESPACE}/namespace: splunk-operator/g"  config/default/kustomization.yaml
	$(SED) "s/value: \"${WATCH_NAMESPACE}\"/value: WATCH_NAMESPACE_VALUE/g"  config/default/kustomization.yaml
	$(SED) "s|${SPLUNK_ENTERPRISE_IMAGE}|SPLUNK_ENTERPRISE_IMAGE|g"  config/default/kustomization.yaml
 
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -


CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.7.0)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

ENVTEST = $(shell pwd)/bin/setup-envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

## Generate bundle manifests and metadata, then validate generated files.
## In addition, copy the newly generated crd files to helm crds.
.PHONY: bundle
bundle: manifests kustomize
	operator-sdk generate kustomize manifests -q
	cp config/default/kustomization-cluster.yaml config/default/kustomization.yaml
	$(SED) "s/namespace: splunk-operator/namespace: ${NAMESPACE}/g"  config/default/kustomization.yaml
	$(SED) "s|SPLUNK_ENTERPRISE_IMAGE|${SPLUNK_ENTERPRISE_IMAGE}|g"  config/default/kustomization.yaml
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle $(BUNDLE_GEN_FLAGS)
	operator-sdk bundle validate ./bundle
	cp bundle/manifests/enterprise.splunk.com* helm-chart/splunk-operator/crds

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.18.1/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)



.PHONY: code/sec
code/sec: $GOBIN/gosec ## Run gosec
	gosec -severity medium --confidence medium -quiet ./...

$GOBIN/gosec:
	go get -u github.com/securego/gosec/cmd/gosec

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

lint:
	@golint ./...

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
	mkdir -p release-${VERSION}
	cp config/default/kustomization-namespace.yaml config/default/kustomization.yaml
	$(SED) "s/namespace: splunk-operator/namespace: ${NAMESPACE}/g"  config/default/kustomization.yaml
	$(SED) "s|SPLUNK_ENTERPRISE_IMAGE|${SPLUNK_ENTERPRISE_IMAGE}|g"  config/default/kustomization.yaml
	$(SED) "s/ClusterRole/Role/g"  config/rbac/role.yaml
	$(SED) "s/ClusterRole/Role/g"  config/rbac/role_binding.yaml
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	RELATED_IMAGE_SPLUNK_ENTERPRISE=${SPLUNK_ENTERPRISE_IMAGE} WATCH_NAMESPACE=${WATCH_NAMESPACE} $(KUSTOMIZE) build config/default > release-${VERSION}/splunk-operator-namespace.yaml
	$(SED) "s/Role/ClusterRole/g"  config/rbac/role.yaml
	$(SED) "s/Role/ClusterRole/g"  config/rbac/role_binding.yaml


# generate artifacts needed to deploy operator, this is current way of doing it, need to fix this
generate-artifacts-cluster: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	mkdir -p release-${VERSION}
	cp config/default/kustomization-cluster.yaml config/default/kustomization.yaml
	$(SED) "s/namespace: splunk-operator/namespace: ${NAMESPACE}/g"  config/default/kustomization.yaml
	$(SED) "s|SPLUNK_ENTERPRISE_IMAGE|${SPLUNK_ENTERPRISE_IMAGE}|g"  config/default/kustomization.yaml
	$(SED) "s/WATCH_NAMESPACE_VALUE/\"${WATCH_NAMESPACE}\"/g"  config/default/kustomization.yaml
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	RELATED_IMAGE_SPLUNK_ENTERPRISE=${SPLUNK_ENTERPRISE_IMAGE} WATCH_NAMESPACE=${WATCH_NAMESPACE} $(KUSTOMIZE) build config/default > release-${VERSION}/splunk-operator-cluster.yaml

generate-artifacts: generate-artifacts-namespace generate-artifacts-cluster
	echo "artifacts generation complete"

#############################

GO_DOWNLOAD_URL=https://go.dev/dl/go1.17.7.darwin-amd64.pkg
export OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/v1.17.0
OPERATOR_SDK_DOWNLOAD_URL=curl -LO ${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH}
MINIKUBE_DOWNLOAD_URL=https://storage.googleapis.com/minikube/releases/latest/minikube-$(OS)-$(ARCH)
KUBECTL_DOWNLOAD_URL="https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/$(OS)/$(ARCH)/kubectl"

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
	@docker rmi  $(IMG) || true
	@rm -f clair-scanner
	@rm -rf clair-scanner-logs

cleanup: 
	@./tools/cleanup.sh

.PHONY: setup/ginkgo
setup/ginkgo:
	@echo Installing ginkgo
	@go get github.com/onsi/ginkgo/ginkgo
	@echo Installing gomega
	@go get github.com/onsi/gomega/...

.PHONY: setup/golint
setup/golint:
	@echo Installing golint
	@go get -u golang.org/x/lint/golint
