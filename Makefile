# Makefile for Splunk Operator

.PHONY: all image package local clean run fmt lint

all: image

builder-image:
	@echo Creating container image to build splunk-operator
	@docker build -f ./build/Dockerfile.builder -t splunk/splunk-operator-builder .

builder: deploy/all-in-one-scoped.yaml deploy/all-in-one-cluster.yaml
	@echo Using container to build splunk-operator
	@mkdir -p ./build/_output/bin
	@docker run -v ${PWD}:/opt/app-root/src/splunk-operator -u root -it splunk/splunk-operator-builder bash -c "cd /opt/app-root/src/splunk-operator && go build -v -o ./build/_output/bin/splunk-operator ./cmd/manager"

image: deploy/all-in-one-scoped.yaml deploy/all-in-one-cluster.yaml
	@echo Building splunk-operator image
	@operator-sdk build splunk/splunk-operator

package: deploy/all-in-one-scoped.yaml deploy/all-in-one-cluster.yaml
	@build/package.sh

local: deploy/all-in-one-scoped.yaml deploy/all-in-one-cluster.yaml
	@mkdir -p ./build/_output/bin
	@go build -v -o ./build/_output/bin/splunk-operator-local ./cmd/manager

clean:
	@rm -rf ./build/_output
	@docker rmi splunk/splunk-operator || true

run:
	@OPERATOR_NAME=splunk-operator operator-sdk up local

fmt:
	@gofmt -l -w `find ./ -name "*.go"`

lint:
	@golint ./...

deploy/all-in-one-scoped.yaml: deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml deploy/rbac.yaml deploy/operator.yaml
	@echo Rebuilding deploy/all-in-one-scoped.yaml
	@cat deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml deploy/rbac.yaml deploy/operator.yaml > deploy/all-in-one-scoped.yaml

deploy/all-in-one-cluster.yaml: deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml deploy/rbac.yaml deploy/cluster_operator.yaml
	@echo Rebuilding deploy/all-in-one-cluster.yaml
	@cat deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml deploy/rbac.yaml deploy/cluster_operator.yaml > deploy/all-in-one-cluster.yaml
