# Makefile for Splunk Operator

.PHONY: all builder builder-image image package local clean run fmt lint

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
	@docker run -v /var/run/docker.sock:/var/run/docker.sock -v ${PWD}:/opt/app-root/src/splunk-operator -w /opt/app-root/src/splunk-operator -u root -it splunk/splunk-operator-builder bash -c "go test -v -covermode=count -coverprofile=coverage.out --timeout=300s github.com/splunk/splunk-operator/pkg/splunk/resources github.com/splunk/splunk-operator/pkg/splunk/spark github.com/splunk/splunk-operator/pkg/splunk/enterprise github.com/splunk/splunk-operator/pkg/splunk/reconcile"

image:
	@echo Building splunk-operator image
	@operator-sdk build --verbose splunk/splunk-operator

local:
	@echo Building splunk-operator-local binary only
	@mkdir -p ./build/_output/bin
	@go build -v -o ./build/_output/bin/splunk-operator-local ./cmd/manager

test:
	@echo Running unit tests for splunk-operator
	@go test -v -covermode=count -coverprofile=coverage.out --timeout=300s github.com/splunk/splunk-operator/pkg/splunk/resources github.com/splunk/splunk-operator/pkg/splunk/spark github.com/splunk/splunk-operator/pkg/splunk/enterprise github.com/splunk/splunk-operator/pkg/splunk/reconcile

generate:
	@echo Running operator-sdk generate k8s
	@operator-sdk generate k8s
	@echo Running operator-sdk generate crds
	@cp deploy/rbac.yaml deploy/role.yaml
	@operator-sdk generate crds
	@rm -f deploy/role.yaml deploy/crds/*_cr.yaml
	@echo "  - name: v1alpha1\n    served: true\n    storage: false" >> deploy/crds/enterprise.splunk.com_splunkenterprises_crd.yaml
	@echo Rebuilding deploy/crds/combined.yaml
	@echo "---" > deploy/crds/combined.yaml
	@cat deploy/crds/enterprise.splunk.com_splunkenterprises_crd.yaml >> deploy/crds/combined.yaml
	@echo "---" >> deploy/crds/combined.yaml
	@cat deploy/crds/enterprise.splunk.com_standalones_crd.yaml >> deploy/crds/combined.yaml
	@echo "---" >> deploy/crds/combined.yaml
	@cat deploy/crds/enterprise.splunk.com_licensemasters_crd.yaml >> deploy/crds/combined.yaml
	@echo "---" >> deploy/crds/combined.yaml
	@cat deploy/crds/enterprise.splunk.com_searchheads_crd.yaml >> deploy/crds/combined.yaml
	@echo "---" >> deploy/crds/combined.yaml
	@cat deploy/crds/enterprise.splunk.com_indexers_crd.yaml >> deploy/crds/combined.yaml
	@echo "---" >> deploy/crds/combined.yaml
	@cat deploy/crds/enterprise.splunk.com_sparks_crd.yaml >> deploy/crds/combined.yaml
	@echo Rebuilding deploy/all-in-one-scoped.yaml
	@cat deploy/crds/combined.yaml deploy/rbac.yaml deploy/operator.yaml > deploy/all-in-one-scoped.yaml
	@echo Rebuilding deploy/all-in-one-cluster.yaml
	@cat deploy/crds/combined.yaml deploy/rbac.yaml deploy/cluster_operator.yaml > deploy/all-in-one-cluster.yaml

package: lint fmt generate
	@build/package.sh

clean:
	@rm -rf ./build/_output
	@docker rmi splunk/splunk-operator || true

run:
	@OPERATOR_NAME=splunk-operator operator-sdk run --local

fmt:
	@gofmt -l -w `find ./ -name "*.go"`

lint:
	@golint ./...
