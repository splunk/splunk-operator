# Makefile for Splunk Operator

.PHONY: all splunk-operator push publish-repo publish-playground publish install uninstall start stop rebuild

all: splunk-operator

splunk-operator:
	@echo Building splunk-operator image
	@operator-sdk build splunk-operator

push:
	@./push_images.sh splunk-operator

publish-repo: get-commit-id
	@echo Publishing repo.splunk.com/splunk/products/splunk-operator:${COMMIT_ID}
	@docker tag splunk-operator:latest repo.splunk.com/splunk/products/splunk-operator:${COMMIT_ID}
	@docker push repo.splunk.com/splunk/products/splunk-operator:${COMMIT_ID}

publish-playground: get-commit-id
	@echo Publishing cloudrepo-docker-playground.jfrog.io/pcp/splunk-operator:${COMMIT_ID}
	@docker tag splunk-operator:latest cloudrepo-docker-playground.jfrog.io/pcp/splunk-operator:${COMMIT_ID}
	@docker push cloudrepo-docker-playground.jfrog.io/pcp/splunk-operator:${COMMIT_ID}

publish: publish-repo publish-playground

package:
	@build/package.sh

install:
	@if ! `kubectl get splunkenterprises.enterprise.splunk.com &> /dev/null`; then kubectl create -f deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml; fi
	@kubectl create -f deploy/service_account.yaml
	@kubectl create -f deploy/role.yaml
	@kubectl create -f deploy/role_binding.yaml
	@if [[ ! -f splunk-enterprise.lic ]]; then wget --quiet -O splunk-enterprise.lic http://go/splunk-nfr-license; fi
	@kubectl create configmap splunk-enterprise.lic --from-file=splunk-enterprise.lic

uninstall:
	@kubectl delete -f deploy/role_binding.yaml
	@kubectl delete -f deploy/role.yaml
	@kubectl delete -f deploy/service_account.yaml
	@if `kubectl get splunkenterprises.enterprise.splunk.com &> /dev/null`; then kubectl delete -f deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml &> /dev/null || true; fi
	@kubectl delete configmap splunk-enterprise.lic

run:
	@OPERATOR_NAME=splunk-operator operator-sdk up local

start:
	@kubectl create -f deploy/operator.yaml

stop:
	@kubectl delete splunkenterprises.enterprise.splunk.com --all
	@kubectl delete -f deploy/operator.yaml

rebuild: splunk-operator stop push start

get-commit-id:
	@$(eval COMMIT_ID := $(shell bash -c "git rev-parse HEAD | cut -c1-12"))
