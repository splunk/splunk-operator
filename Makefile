# Makefile for Splunk Operator

DOCKER_IMAGE := "repo.splunk.com/splunk/products/splunk-operator"

.PHONY: all deps image push

all: deps image

deps:
	@echo Checking vendor dependencies
	@dep ensure

image:
	@echo Building ${DOCKER_IMAGE}
	@operator-sdk build ${DOCKER_IMAGE}

push: get-tag
	@echo Pushing ${DOCKER_IMAGE}:${IMAGE_TAG}
	@docker tag ${DOCKER_IMAGE} ${DOCKER_IMAGE}:${IMAGE_TAG}
	@docker push ${DOCKER_IMAGE}:${IMAGE_TAG}

get-tag:
	@$(eval IMAGE_TAG := $(shell bash -c "git rev-parse HEAD | cut -c8-19"))
