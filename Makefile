# Makefile for Splunk Operator

.PHONY: all builder builder-image image package local clean run fmt lint

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

all: image

builder:
	@echo Creating container image to build splunk-operator
	@docker build -f ./build/Dockerfile.builder -t splunk/splunk-operator-builder .

builder-image:
	@echo Using builder container to build splunk-operator
	@mkdir -p ./build/_output/bin
	@docker run -v /var/run/docker.sock:/var/run/docker.sock -v ${PWD}:/opt/app-root/src/splunk-operator -w /opt/app-root/src/splunk-operator -u root -it splunk/splunk-operator-builder bash -c "operator-sdk build --verbose splunk/splunk-operator"

builder-test:
	@echo Running unit tests for splunk-operator inside of builder container
	@docker run -v /var/run/docker.sock:/var/run/docker.sock -v ${PWD}:/opt/app-root/src/splunk-operator -w /opt/app-root/src/splunk-operator -u root -it splunk/splunk-operator-builder bash -c "go test -v -covermode=count -coverprofile=coverage.out --timeout=300s github.com/splunk/splunk-operator/pkg/splunk/resources github.com/splunk/splunk-operator/pkg/splunk/spark github.com/splunk/splunk-operator/pkg/splunk/enterprise github.com/splunk/splunk-operator/pkg/splunk/reconcile github.com/splunk/splunk-operator/pkg/splunk/client"

image:
	@echo Building splunk-operator image
	@operator-sdk build --verbose splunk/splunk-operator

local:
	@echo Building splunk-operator-local binary only
	@mkdir -p ./build/_output/bin
	@go build -v -o ./build/_output/bin/splunk-operator-local ./cmd/manager

test:
	@echo Running unit tests for splunk-operator
	@go test -v -covermode=count -coverprofile=coverage.out --timeout=300s github.com/splunk/splunk-operator/pkg/splunk/resources github.com/splunk/splunk-operator/pkg/splunk/spark github.com/splunk/splunk-operator/pkg/splunk/enterprise github.com/splunk/splunk-operator/pkg/splunk/reconcile github.com/splunk/splunk-operator/pkg/splunk/client

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
	@./clair-scanner -c http://0.0.0.0:6060 --ip ${SCANNER_LOCALIP} -r clair-scanner-logs/results.json -l clair-scanner-logs/results.log splunk/splunk-operator

generate:
	@echo Running operator-sdk generate k8s
	@operator-sdk generate k8s
	@echo Running operator-sdk generate crds
	@cp deploy/rbac.yaml deploy/role.yaml
	@operator-sdk generate crds
	@rm -f deploy/role.yaml deploy/crds/*_cr.yaml
	@echo Rebuilding deploy/crds/combined.yaml
	@echo "---" > deploy/crds/combined.yaml
	@cat deploy/crds/enterprise.splunk.com_standalones_crd.yaml >> deploy/crds/combined.yaml
	@echo "---" >> deploy/crds/combined.yaml
	@cat deploy/crds/enterprise.splunk.com_licensemasters_crd.yaml >> deploy/crds/combined.yaml
	@echo "---" >> deploy/crds/combined.yaml
	@cat deploy/crds/enterprise.splunk.com_searchheadclusters_crd.yaml >> deploy/crds/combined.yaml
	@echo "---" >> deploy/crds/combined.yaml
	@cat deploy/crds/enterprise.splunk.com_indexerclusters_crd.yaml >> deploy/crds/combined.yaml
	@echo "---" >> deploy/crds/combined.yaml
	@cat deploy/crds/enterprise.splunk.com_sparks_crd.yaml >> deploy/crds/combined.yaml
	@echo Rebuilding deploy/all-in-one-scoped.yaml
	@cat deploy/crds/combined.yaml deploy/rbac.yaml deploy/operator.yaml > deploy/all-in-one-scoped.yaml
	@echo Rebuilding deploy/all-in-one-cluster.yaml
	@cat deploy/crds/combined.yaml deploy/rbac.yaml deploy/cluster_operator.yaml > deploy/all-in-one-cluster.yaml

package: lint fmt generate
	@build/package.sh

clean: stop_clair_scanner
	@rm -rf ./build/_output
	@docker rmi splunk/splunk-operator || true
	@rm -f clair-scanner
	@rm -rf clair-scanner-logs

run:
	@OPERATOR_NAME=splunk-operator operator-sdk run --local

fmt:
	@gofmt -l -w `find ./ -name "*.go"`

lint:
	@golint ./...
