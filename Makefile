# Makefile for Splunk Operator

.PHONY: all splunk-operator package run

all: splunk-operator

splunk-operator: deploy/all-in-one.yaml
	@echo Building splunk-operator image
	@operator-sdk build splunk-operator
	@docker tag splunk-operator:latest splunk/splunk-operator:latest

package: deploy/all-in-one.yaml
	@build/package.sh

run:
	@OPERATOR_NAME=splunk-operator operator-sdk up local

deploy/all-in-one.yaml: deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml deploy/service_account.yaml deploy/role.yaml deploy/role_binding.yaml deploy/operator.yaml
	@echo Rebuilding deploy/all-in-one.yaml
	@cat deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml deploy/service_account.yaml deploy/role.yaml deploy/role_binding.yaml deploy/operator.yaml > deploy/all-in-one.yaml
