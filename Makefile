# Makefile for Splunk Operator

.PHONY: all builder builder-image image package local clean run fmt lint

all: builder builder-image

builder: deploy/all-in-one-scoped.yaml deploy/all-in-one-cluster.yaml
	@echo Creating container image to build splunk-operator
	@docker build -f ./build/Dockerfile.builder -t splunk/splunk-operator-builder .

builder-image:
	@echo Using builder container to build splunk-operator
	@mkdir -p ./build/_output/bin
	@docker run -v /var/run/docker.sock:/var/run/docker.sock -v ${PWD}:/opt/app-root/src/splunk-operator -w /opt/app-root/src/splunk-operator -u root -it splunk/splunk-operator-builder bash -c "operator-sdk build --verbose splunk/splunk-operator"

image: deploy/all-in-one-scoped.yaml deploy/all-in-one-cluster.yaml
	@echo Building splunk-operator image
	@operator-sdk build --verbose splunk/splunk-operator

local: deploy/all-in-one-scoped.yaml deploy/all-in-one-cluster.yaml
	@echo Building splunk-operator-local binary only
	@mkdir -p ./build/_output/bin
	@go build -v -o ./build/_output/bin/splunk-operator-local ./cmd/manager

test:
	@Echo Running unit tests for splunk-operator
	@go test -v -covermode=count -coverprofile=coverage.out --timeout=300s github.com/splunk/splunk-operator/pkg/splunk/resources github.com/splunk/splunk-operator/pkg/splunk/spark github.com/splunk/splunk-operator/pkg/splunk/enterprise github.com/splunk/splunk-operator/pkg/splunk/deploy

package: deploy/all-in-one-scoped.yaml deploy/all-in-one-cluster.yaml
	@build/package.sh

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
