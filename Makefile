# Makefile for Splunk Operator

.PHONY: all dep splunk-operator splunk-dfs splunk-spark push-operator push-dfs push-spark push publish-repo publish-playground publish install uninstall start stop rebuild

all: splunk-operator

dep:
	@echo Checking vendor dependencies
	@dep ensure

splunk-operator: dep
	@echo Building splunk-operator image
	@operator-sdk build splunk-operator

splunk-dfs:
	@echo Building splunk-dfs image
	@cd dockerfiles/splunk && docker build -t splunk-dfs .

splunk-spark:
	@echo Building splunk-spark image
	@cd dockerfiles/spark && docker build -t splunk-spark .

push-operator:
	@./push_images.sh splunk-operator

push-dfs:
	@./push_images.sh splunk-dfs

push-spark:
	@./push_images.sh splunk-spark

push: push-operator push-dfs push-spark

publish-repo: get-commit-id
	@echo Publishing repo.splunk.com/splunk/products/splunk-operator:${COMMIT_ID}
	@docker tag splunk-operator:latest repo.splunk.com/splunk/products/splunk-operator:${COMMIT_ID}
	@docker push repo.splunk.com/splunk/products/splunk-operator:${COMMIT_ID}

publish-playground:
	@echo Publishing cloudrepo-docker-playground.jfrog.io/pcp/splunk-operator:${COMMIT_ID}
	@docker tag splunk-operator:latest cloudrepo-docker-playground.jfrog.io/pcp/splunk-operator:${COMMIT_ID}
	@docker push cloudrepo-docker-playground.jfrog.io/pcp/splunk-operator:${COMMIT_ID}

publish: publish-repo publish-playground

install:
	@kubectl create -f deploy/service_account.yaml
	@kubectl create -f deploy/role.yaml
	@kubectl create -f deploy/role_binding.yaml
	@kubectl create -f deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml

uninstall:
	@kubectl delete -f deploy/role.yaml
	@kubectl delete -f deploy/role_binding.yaml
	@kubectl delete -f deploy/service_account.yaml
	@kubectl delete -f deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml

start:
	@kubectl create -f deploy/operator.yaml

stop:
	@kubectl delete splunkenterprises.enterprise.splunk.com --all
	@kubectl delete -f deploy/operator.yaml

rebuild: splunk-operator stop push-operator start

get-commit-id:
	@$(eval COMMIT_ID := $(shell bash -c "git rev-parse HEAD | cut -c8-19"))
