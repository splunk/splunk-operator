# Makefile for Splunk Operator

SPLUNK_OPERATOR_IMAGE := "repo.splunk.com/splunk/products/splunk-operator"
SPLUNK_DFS_IMAGE := "repo.splunk.com/splunk/products/splunk-dfs"
SPLUNK_SPARK_IMAGE := "repo.splunk.com/splunk/products/splunk-spark"

.PHONY: all dep splunk-operator splunk-dfs splunk-spark push install uninstall rebuild

all: splunk-opeerator splunk-dfs splunk-spark

dep:
	@echo Checking vendor dependencies
	@dep ensure

splunk-operator: dep
	@echo Building ${SPLUNK_OPERATOR_IMAGE}
	@operator-sdk build ${SPLUNK_OPERATOR_IMAGE}

splunk-dfs:
	@cd dockerfiles/splunk && docker build -t repo.splunk.com/splunk/products/splunk-dfs .

splunk-spark:
	@cd dockerfiles/spark && docker build -t repo.splunk.com/splunk/products/splunk-spark .

push: get-commit-id
	@echo Pushing ${SPLUNK_OPERATOR_IMAGE}:${COMMIT_ID}
	@docker tag ${SPLUNK_OPERATOR_IMAGE} ${SPLUNK_OPERATOR_IMAGE}:${COMMIT_ID}
	@docker push ${SPLUNK_OPERATOR_IMAGE}:${COMMIT_ID}

install:
	@kubectl create -f deploy/service_account.yaml
	@kubectl create -f deploy/role.yaml
	@kubectl create -f deploy/role_binding.yaml
	@kubectl create -f deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml
	@kubectl create -f deploy/operator.yaml

uninstall:
	@kubectl delete splunkenterprises.enterprise.splunk.com --all
	@kubectl delete -f deploy/operator.yaml
	@kubectl delete -f deploy/role.yaml
	@kubectl delete -f deploy/role_binding.yaml
	@kubectl delete -f deploy/service_account.yaml
	@kubectl delete -f deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml

rebuild: uninstall splunk-operator push install

get-commit-id:
	@$(eval COMMIT_ID := $(shell bash -c "git rev-parse HEAD | cut -c8-19"))
