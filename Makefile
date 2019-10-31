# Makefile for Splunk Operator

.PHONY: all image package local clean run fmt lint

all: image

image: deploy/all-in-one.yaml
	@echo Building splunk-operator image
	@operator-sdk build splunk/splunk-operator

package: deploy/all-in-one.yaml
	@build/package.sh

local:
	@mkdir -p ./build/_output/bin
	@go build -o ./build/_output/bin/splunk-operator-local ./cmd/manager

clean:
	@rm -rf ./build/_output
	@docker rmi splunk/splunk-operator || true

run:
	@OPERATOR_NAME=splunk-operator operator-sdk up local

fmt:
	@gofmt -l -w `find ./ -name "*.go"`

lint:
	@golint ./...

deploy/all-in-one.yaml: deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml deploy/service_account.yaml deploy/role.yaml deploy/role_binding.yaml deploy/operator.yaml
	@echo Rebuilding deploy/all-in-one.yaml
	@cat deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml deploy/service_account.yaml deploy/role.yaml deploy/role_binding.yaml deploy/operator.yaml > deploy/all-in-one.yaml
